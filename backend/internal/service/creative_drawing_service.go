package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"strings"
	"syscall"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

var (
	ErrCreativeDrawingTaskNotFound = infraerrors.NotFound("CREATIVE_DRAWING_TASK_NOT_FOUND", "creative drawing task not found")
	ErrCreativeDrawingInvalidTask  = infraerrors.BadRequest("CREATIVE_DRAWING_INVALID_TASK", "invalid creative drawing task")
)

const (
	creativeDrawingTaskTimeout        = 30 * time.Minute
	creativeDrawingStreamScanMaxBytes = 128 * 1024 * 1024
)

type CreativeDrawingRepository interface {
	Create(ctx context.Context, task *CreativeDrawingTask) error
	GetByID(ctx context.Context, id string) (*CreativeDrawingTask, error)
	ListByUserID(ctx context.Context, userID int64, limit int) ([]CreativeDrawingTask, error)
	ListPending(ctx context.Context, limit int, runningTimeout time.Duration) ([]CreativeDrawingTask, error)
	MarkStaleRunning(ctx context.Context, timeout time.Duration, message string, completedAt time.Time) (int64, error)
	MarkRunning(ctx context.Context, id string, startedAt time.Time) (*CreativeDrawingTask, error)
	MarkSuccess(ctx context.Context, id string, images []CreativeDrawingImageResult, completedAt time.Time) error
	MarkError(ctx context.Context, id string, message string, completedAt time.Time) error
}

type CreativeDrawingService struct {
	repo          CreativeDrawingRepository
	apiKeyService *APIKeyService
	httpClient    *http.Client
	baseURL       string
}

func NewCreativeDrawingService(repo CreativeDrawingRepository, apiKeyService *APIKeyService, cfg *config.Config) *CreativeDrawingService {
	s := &CreativeDrawingService{
		repo:          repo,
		apiKeyService: apiKeyService,
		httpClient: &http.Client{
			Timeout: creativeDrawingTaskTimeout,
		},
		baseURL: resolveCreativeDrawingInternalBaseURL(cfg),
	}
	go s.recoverPendingTasks()
	return s
}

func (s *CreativeDrawingService) CreateTask(ctx context.Context, userID int64, req CreativeDrawingCreateTaskRequest) (*CreativeDrawingTask, error) {
	if err := validateCreativeDrawingCreateRequest(req); err != nil {
		return nil, err
	}
	key, err := s.apiKeyService.GetByID(ctx, req.APIKeyID)
	if err != nil {
		return nil, err
	}
	if key.UserID != userID {
		return nil, infraerrors.Forbidden("CREATIVE_DRAWING_KEY_FORBIDDEN", "not authorized to use this api key")
	}
	if key.Group == nil || key.Group.Platform != PlatformOpenAI || !GroupAllowsImageGeneration(key.Group) {
		return nil, infraerrors.Forbidden("CREATIVE_DRAWING_KEY_NOT_DRAWABLE", ImageGenerationPermissionMessage())
	}

	task := &CreativeDrawingTask{
		ID:              uuid.NewString(),
		UserID:          userID,
		APIKeyID:        req.APIKeyID,
		ConversationID:  strings.TrimSpace(req.ConversationID),
		TurnID:          strings.TrimSpace(req.TurnID),
		Mode:            req.Mode,
		Model:           resolveCreativeDrawingGatewayModel(req.Model),
		Prompt:          strings.TrimSpace(req.Prompt),
		Size:            strings.TrimSpace(req.Size),
		Count:           req.Count,
		OutputFormat:    normalizeCreativeDrawingOutputFormat(req.OutputFormat),
		ReferenceImages: normalizeCreativeDrawingReferences(req.ReferenceImages),
		Status:          CreativeDrawingTaskStatusQueued,
		Images:          []CreativeDrawingImageResult{},
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	task.RequestJSON = buildCreativeDrawingGatewayBody(task)
	if err := s.repo.Create(ctx, task); err != nil {
		return nil, fmt.Errorf("create creative drawing task: %w", err)
	}
	go s.executeTask(task.ID)
	return task, nil
}

func (s *CreativeDrawingService) GetTask(ctx context.Context, userID int64, id string) (*CreativeDrawingTask, error) {
	if _, err := s.repo.MarkStaleRunning(ctx, creativeDrawingTaskTimeout, "图片生成超时，请重试", time.Now().UTC()); err != nil {
		logger.L().Warn("creative_drawing.mark_stale_running_failed", zap.Error(err))
	}
	task, err := s.repo.GetByID(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if task.UserID != userID {
		return nil, ErrCreativeDrawingTaskNotFound
	}
	return task, nil
}

func (s *CreativeDrawingService) ListTasks(ctx context.Context, userID int64, limit int) ([]CreativeDrawingTask, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if _, err := s.repo.MarkStaleRunning(ctx, creativeDrawingTaskTimeout, "图片生成超时，请重试", time.Now().UTC()); err != nil {
		logger.L().Warn("creative_drawing.mark_stale_running_failed", zap.Error(err))
	}
	tasks, err := s.repo.ListByUserID(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	for i := range tasks {
		tasks[i].RequestJSON = nil
		tasks[i].ReferenceImages = summarizeCreativeDrawingReferences(tasks[i].ReferenceImages)
		tasks[i].Images = summarizeCreativeDrawingResults(tasks[i].Images)
	}
	return tasks, nil
}

func (s *CreativeDrawingService) executeTask(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), creativeDrawingTaskTimeout)
	defer cancel()

	task, err := s.repo.MarkRunning(ctx, id, time.Now().UTC())
	if err != nil {
		logger.L().Warn("creative_drawing.mark_running_failed", zap.String("task_id", id), zap.Error(err))
		return
	}
	images, err := s.forwardTaskWithRetry(ctx, task)
	now := time.Now().UTC()
	if err != nil {
		msg := normalizeCreativeDrawingTaskError(err, task)
		if markErr := s.repo.MarkError(context.Background(), task.ID, msg, now); markErr != nil {
			logger.L().Warn("creative_drawing.mark_error_failed", zap.String("task_id", task.ID), zap.Error(markErr))
		}
		return
	}
	if err := s.repo.MarkSuccess(context.Background(), task.ID, images, now); err != nil {
		logger.L().Warn("creative_drawing.mark_success_failed", zap.String("task_id", task.ID), zap.Error(err))
	}
}

func (s *CreativeDrawingService) forwardTaskWithRetry(ctx context.Context, task *CreativeDrawingTask) ([]CreativeDrawingImageResult, error) {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		images, err := s.forwardTask(ctx, task)
		if err == nil {
			return images, nil
		}
		lastErr = err
		if !isCreativeDrawingInternalTransportError(err) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
	}
	return nil, lastErr
}

func (s *CreativeDrawingService) recoverPendingTasks() {
	time.Sleep(2 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tasks, err := s.repo.ListPending(ctx, 20, creativeDrawingTaskTimeout)
	if err != nil {
		logger.L().Warn("creative_drawing.recover_pending_failed", zap.Error(err))
		return
	}
	for _, task := range tasks {
		go s.executeTask(task.ID)
	}
}

func (s *CreativeDrawingService) forwardTask(ctx context.Context, task *CreativeDrawingTask) ([]CreativeDrawingImageResult, error) {
	key, err := s.apiKeyService.GetByID(ctx, task.APIKeyID)
	if err != nil {
		return nil, err
	}
	if key.UserID != task.UserID {
		return nil, infraerrors.Forbidden("CREATIVE_DRAWING_KEY_FORBIDDEN", "not authorized to use this api key")
	}
	body := task.RequestJSON
	if body == nil {
		body = buildCreativeDrawingGatewayBody(task)
	}
	endpoint := "/v1/images/generations"
	var req *http.Request
	if task.Mode == CreativeDrawingModeEdit {
		endpoint = "/v1/images/edits"
		editBody, contentType, err := buildCreativeDrawingEditMultipartBody(task)
		if err != nil {
			return nil, err
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+endpoint, editBody)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", contentType)
	} else {
		rawBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal creative drawing request: %w", err)
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+endpoint, bytes.NewReader(rawBody))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+key.Key)
	req.Header.Set("Accept", "application/json")
	if task.Mode == CreativeDrawingModeEdit {
		req.Header.Set("Accept", "text/event-stream")
	}
	req.Header.Set("User-Agent", "sub2api-creative-drawing-worker")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("creative drawing gateway request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New(extractCreativeDrawingGatewayError(respBody, resp.StatusCode))
	}
	if task.Mode == CreativeDrawingModeEdit && isCreativeDrawingEventStream(resp.Header) {
		images, err := parseCreativeDrawingGatewayStreamImages(respBody, task)
		if err != nil {
			return nil, err
		}
		if len(images) == 0 {
			return nil, fmt.Errorf("图片接口没有返回可展示的图片")
		}
		return images, nil
	}
	images, err := parseCreativeDrawingGatewayImages(respBody, task)
	if err != nil {
		return nil, err
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("图片接口没有返回可展示的图片")
	}
	return images, nil
}

func isCreativeDrawingInternalTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func normalizeCreativeDrawingTaskError(err error, task *CreativeDrawingTask) string {
	if err == nil {
		return "图片生成失败"
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "context deadline exceeded") {
		if task != nil && task.Mode == CreativeDrawingModeEdit {
			return "参考图作画超时，请重试；4K 参考图作画耗时较长，可先降低分辨率确认效果"
		}
		return "图片生成超时，请重试"
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "图片生成失败"
	}
	return msg
}

func validateCreativeDrawingCreateRequest(req CreativeDrawingCreateTaskRequest) error {
	if req.APIKeyID <= 0 {
		return infraerrors.BadRequest("CREATIVE_DRAWING_API_KEY_REQUIRED", "请选择用于作画的 API 密钥")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return infraerrors.BadRequest("CREATIVE_DRAWING_PROMPT_REQUIRED", "请输入画面描述")
	}
	if req.Mode != CreativeDrawingModeGenerate && req.Mode != CreativeDrawingModeEdit {
		return infraerrors.BadRequest("CREATIVE_DRAWING_MODE_INVALID", "创作模式无效")
	}
	if req.Mode == CreativeDrawingModeEdit && len(req.ReferenceImages) == 0 {
		return infraerrors.BadRequest("CREATIVE_DRAWING_REFERENCE_REQUIRED", "请先上传至少一张参考图")
	}
	if req.Count <= 0 || req.Count > 4 {
		return infraerrors.BadRequest("CREATIVE_DRAWING_COUNT_INVALID", "图片数量必须在 1 到 4 之间")
	}
	return nil
}

func buildCreativeDrawingGatewayBody(task *CreativeDrawingTask) map[string]any {
	body := map[string]any{
		"model":           resolveCreativeDrawingGatewayModel(task.Model),
		"prompt":          task.Prompt,
		"n":               task.Count,
		"response_format": "b64_json",
		"output_format":   normalizeCreativeDrawingOutputFormat(task.OutputFormat),
	}
	if strings.TrimSpace(task.Size) != "" {
		body["size"] = strings.TrimSpace(task.Size)
	}
	if task.Mode == CreativeDrawingModeEdit {
		images := make([]map[string]string, 0, len(task.ReferenceImages))
		for _, reference := range task.ReferenceImages {
			if imageURL := strings.TrimSpace(reference.DataURL); imageURL != "" {
				images = append(images, map[string]string{"image_url": imageURL})
			}
		}
		body["images"] = images
	}
	return body
}

func buildCreativeDrawingEditMultipartBody(task *CreativeDrawingTask) (io.Reader, string, error) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	fields := map[string]string{
		"model":           resolveCreativeDrawingGatewayModel(task.Model),
		"prompt":          task.Prompt,
		"n":               fmt.Sprint(task.Count),
		"response_format": "b64_json",
		"output_format":   normalizeCreativeDrawingOutputFormat(task.OutputFormat),
	}
	if task.Mode == CreativeDrawingModeEdit {
		// 4K 参考图作画耗时更长，流式请求可让上游持续返回进度事件，避免长时间无响应被网关 504 截断。
		fields["stream"] = "true"
		fields["partial_images"] = "2"
	}
	if strings.TrimSpace(task.Size) != "" {
		fields["size"] = strings.TrimSpace(task.Size)
	}
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			return nil, "", err
		}
	}
	for index, reference := range task.ReferenceImages {
		dataURL := strings.TrimSpace(reference.DataURL)
		if dataURL == "" {
			continue
		}
		mimeType, data, err := parseCreativeDrawingDataURL(dataURL)
		if err != nil {
			return nil, "", err
		}
		filename := strings.TrimSpace(reference.Name)
		if filename == "" {
			filename = fmt.Sprintf("reference-%d.%s", index+1, creativeDrawingExtensionFromMime(mimeType))
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image"; filename="%s"`, strings.ReplaceAll(filename, `"`, "")))
		header.Set("Content-Type", mimeType)
		part, err := writer.CreatePart(header)
		if err != nil {
			return nil, "", err
		}
		if _, err := part.Write(data); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &buffer, writer.FormDataContentType(), nil
}

func parseCreativeDrawingDataURL(value string) (string, []byte, error) {
	prefix, payload, ok := strings.Cut(value, ",")
	if !ok || !strings.HasPrefix(prefix, "data:") || !strings.Contains(prefix, ";base64") {
		return "", nil, infraerrors.BadRequest("CREATIVE_DRAWING_REFERENCE_INVALID", "参考图格式无效，请重新上传")
	}
	mimeType := strings.TrimPrefix(strings.TrimSuffix(prefix, ";base64"), "data:")
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", nil, infraerrors.BadRequest("CREATIVE_DRAWING_REFERENCE_INVALID", "参考图解析失败，请重新上传")
	}
	return mimeType, data, nil
}

func creativeDrawingExtensionFromMime(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/webp":
		return "webp"
	default:
		return "png"
	}
}

func summarizeCreativeDrawingReferences(input []CreativeDrawingReference) []CreativeDrawingReference {
	out := make([]CreativeDrawingReference, 0, len(input))
	for _, item := range input {
		item.DataURL = ""
		out = append(out, item)
	}
	return out
}

func summarizeCreativeDrawingResults(input []CreativeDrawingImageResult) []CreativeDrawingImageResult {
	out := make([]CreativeDrawingImageResult, 0, len(input))
	for _, item := range input {
		item.B64JSON = ""
		out = append(out, item)
	}
	return out
}

func normalizeCreativeDrawingReferences(input []CreativeDrawingReference) []CreativeDrawingReference {
	out := make([]CreativeDrawingReference, 0, len(input))
	for _, item := range input {
		dataURL := strings.TrimSpace(item.DataURL)
		if dataURL == "" {
			continue
		}
		out = append(out, CreativeDrawingReference{
			ID:        strings.TrimSpace(item.ID),
			Name:      strings.TrimSpace(item.Name),
			Type:      strings.TrimSpace(item.Type),
			DataURL:   dataURL,
			RemoteURL: strings.TrimSpace(item.RemoteURL),
			Source:    strings.TrimSpace(item.Source),
		})
	}
	return out
}

func resolveCreativeDrawingGatewayModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" || model == "auto" {
		return "gpt-image-2"
	}
	return model
}

func normalizeCreativeDrawingOutputFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "jpeg", "jpg":
		return "jpeg"
	case "webp":
		return "webp"
	default:
		return "png"
	}
}

func resolveCreativeDrawingInternalBaseURL(cfg *config.Config) string {
	host := "127.0.0.1"
	port := 8080
	if cfg != nil {
		port = cfg.Server.Port
		trimmed := strings.TrimSpace(cfg.Server.Host)
		if trimmed != "" && trimmed != "0.0.0.0" && trimmed != "::" && trimmed != "[::]" {
			host = trimmed
		}
	}
	if port <= 0 {
		port = 8080
	}
	return "http://" + net.JoinHostPort(host, fmt.Sprint(port))
}

func extractCreativeDrawingGatewayError(body []byte, status int) string {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err == nil {
		if errObj, ok := payload["error"].(map[string]any); ok {
			if message, ok := errObj["message"].(string); ok && strings.TrimSpace(message) != "" {
				return strings.TrimSpace(message)
			}
		}
		for _, key := range []string{"message", "detail"} {
			if message, ok := payload[key].(string); ok && strings.TrimSpace(message) != "" {
				return strings.TrimSpace(message)
			}
		}
	}
	return fmt.Sprintf("图片请求失败：%d", status)
}

func parseCreativeDrawingGatewayImages(body []byte, task *CreativeDrawingTask) ([]CreativeDrawingImageResult, error) {
	var payload struct {
		Created      int64                        `json:"created"`
		OutputFormat string                       `json:"output_format"`
		Size         string                       `json:"size"`
		Data         []CreativeDrawingImageResult `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析图片接口返回失败: %w", err)
	}
	out := make([]CreativeDrawingImageResult, 0, len(payload.Data))
	for i, item := range payload.Data {
		if item.ID == "" {
			item.ID = uuid.NewString()
		}
		if item.OutputFormat == "" {
			item.OutputFormat = firstNonEmptyCreativeString(payload.OutputFormat, task.OutputFormat)
		}
		if item.Size == "" {
			item.Size = firstNonEmptyCreativeString(payload.Size, task.Size)
		}
		if item.CreatedAt == 0 {
			item.CreatedAt = payload.Created
			if item.CreatedAt == 0 {
				item.CreatedAt = time.Now().Unix() + int64(i)
			}
		}
		if strings.TrimSpace(item.URL) == "" && strings.TrimSpace(item.B64JSON) == "" {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func isCreativeDrawingEventStream(headers http.Header) bool {
	contentType := strings.ToLower(strings.TrimSpace(headers.Get("Content-Type")))
	return strings.Contains(contentType, "text/event-stream")
}

func parseCreativeDrawingGatewayStreamImages(body []byte, task *CreativeDrawingTask) ([]CreativeDrawingImageResult, error) {
	out := make([]CreativeDrawingImageResult, 0, maxCreativeDrawingCount(task))
	var streamErr string
	var acc openAISSEDataAccumulator

	processPayload := func(data []byte) {
		if len(data) == 0 || !gjson.ValidBytes(data) {
			return
		}
		eventType := strings.TrimSpace(gjson.GetBytes(data, "type").String())
		if eventType == "error" {
			if msg := strings.TrimSpace(gjson.GetBytes(data, "error.message").String()); msg != "" {
				streamErr = msg
			} else if msg := strings.TrimSpace(gjson.GetBytes(data, "message").String()); msg != "" {
				streamErr = msg
			}
			return
		}
		if !strings.HasSuffix(eventType, ".completed") && eventType != "image_generation.completed" {
			return
		}
		image := creativeDrawingImageResultFromStreamPayload(data, task)
		if strings.TrimSpace(image.URL) == "" && strings.TrimSpace(image.B64JSON) == "" {
			return
		}
		out = append(out, image)
	}

	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), creativeDrawingStreamScanMaxBytes)
	for scanner.Scan() {
		acc.AddLine(scanner.Text(), processPayload)
	}
	acc.Flush(processPayload)
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 && streamErr != "" {
		return nil, errors.New(streamErr)
	}
	return out, nil
}

func creativeDrawingImageResultFromStreamPayload(data []byte, task *CreativeDrawingTask) CreativeDrawingImageResult {
	item := CreativeDrawingImageResult{
		ID:            uuid.NewString(),
		B64JSON:       strings.TrimSpace(gjson.GetBytes(data, "b64_json").String()),
		URL:           strings.TrimSpace(gjson.GetBytes(data, "url").String()),
		RevisedPrompt: strings.TrimSpace(gjson.GetBytes(data, "revised_prompt").String()),
		OutputFormat:  strings.TrimSpace(gjson.GetBytes(data, "output_format").String()),
		Size:          strings.TrimSpace(gjson.GetBytes(data, "size").String()),
		CreatedAt:     gjson.GetBytes(data, "created_at").Int(),
	}
	if item.OutputFormat == "" {
		item.OutputFormat = task.OutputFormat
	}
	if item.Size == "" {
		item.Size = task.Size
	}
	if item.CreatedAt == 0 {
		item.CreatedAt = time.Now().Unix()
	}
	return item
}

func maxCreativeDrawingCount(task *CreativeDrawingTask) int {
	if task != nil && task.Count > 0 {
		return task.Count
	}
	return 1
}

func firstNonEmptyCreativeString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
