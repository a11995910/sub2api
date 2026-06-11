package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type UpstreamRateMonitorService struct {
	repo      UpstreamRateMonitorRepository
	encryptor SecretEncryptor
	client    *http.Client
}

func NewUpstreamRateMonitorService(repo UpstreamRateMonitorRepository, encryptor SecretEncryptor) *UpstreamRateMonitorService {
	return &UpstreamRateMonitorService{
		repo:      repo,
		encryptor: encryptor,
		client:    newUpstreamRateHTTPClient(),
	}
}

func (s *UpstreamRateMonitorService) List(ctx context.Context, params UpstreamRateMonitorListParams) ([]*UpstreamRateMonitor, int64, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 || params.PageSize > upstreamRateMonitorMaxPageSize {
		params.PageSize = 20
	}
	items, total, err := s.repo.List(ctx, params)
	if err != nil {
		return nil, 0, err
	}
	for _, item := range items {
		s.decryptInPlace(item)
	}
	return items, total, nil
}

func (s *UpstreamRateMonitorService) Get(ctx context.Context, id int64) (*UpstreamRateMonitor, error) {
	m, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s.decryptInPlace(m)
	return m, nil
}

func (s *UpstreamRateMonitorService) Create(ctx context.Context, p UpstreamRateMonitorCreateParams) (*UpstreamRateMonitor, error) {
	baseURL, err := normalizeUpstreamBaseURL(p.BaseURL)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return nil, ErrUpstreamRateMonitorMissingName
	}
	username := strings.TrimSpace(p.Username)
	if username == "" {
		return nil, ErrUpstreamRateMonitorMissingUsername
	}
	password := strings.TrimSpace(p.Password)
	if password == "" {
		return nil, ErrUpstreamRateMonitorMissingPassword
	}
	encrypted, err := s.encryptor.Encrypt(password)
	if err != nil {
		return nil, fmt.Errorf("encrypt upstream password: %w", err)
	}
	m := &UpstreamRateMonitor{
		Name:              name,
		BaseURL:           baseURL,
		Username:          username,
		PasswordEncrypted: encrypted,
		Password:          password,
		Enabled:           p.Enabled,
		LastStatus:        UpstreamRateMonitorStatusUnknown,
		LastSnapshot:      UpstreamRateSnapshot{},
		CreatedBy:         p.CreatedBy,
	}
	if err := s.repo.Create(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *UpstreamRateMonitorService) Update(ctx context.Context, id int64, p UpstreamRateMonitorUpdateParams) (*UpstreamRateMonitor, error) {
	m, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Name != nil {
		name := strings.TrimSpace(*p.Name)
		if name == "" {
			return nil, ErrUpstreamRateMonitorMissingName
		}
		m.Name = name
	}
	if p.BaseURL != nil {
		baseURL, err := normalizeUpstreamBaseURL(*p.BaseURL)
		if err != nil {
			return nil, err
		}
		m.BaseURL = baseURL
	}
	if p.Username != nil {
		username := strings.TrimSpace(*p.Username)
		if username == "" {
			return nil, ErrUpstreamRateMonitorMissingUsername
		}
		m.Username = username
	}
	if p.Enabled != nil {
		m.Enabled = *p.Enabled
	}
	if p.Password != nil && strings.TrimSpace(*p.Password) != "" {
		password := strings.TrimSpace(*p.Password)
		encrypted, err := s.encryptor.Encrypt(password)
		if err != nil {
			return nil, fmt.Errorf("encrypt upstream password: %w", err)
		}
		m.PasswordEncrypted = encrypted
		m.Password = password
	}
	if err := s.repo.Update(ctx, m); err != nil {
		return nil, err
	}
	s.decryptInPlace(m)
	return m, nil
}

func (s *UpstreamRateMonitorService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

func (s *UpstreamRateMonitorService) Refresh(ctx context.Context, id int64) (*UpstreamRateMonitor, error) {
	m, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if m.PasswordDecryptFailed {
		return nil, ErrUpstreamRateMonitorPasswordDecryptFailed
	}

	checkedAt := time.Now().UTC()
	snapshot, refreshErr := s.fetchSnapshot(ctx, m)
	if refreshErr != nil {
		_ = s.repo.MarkRefreshFailed(ctx, id, refreshErr.Error(), checkedAt)
		refreshed, getErr := s.Get(ctx, id)
		if getErr != nil {
			return nil, getErr
		}
		return refreshed, newUpstreamRateMonitorRefreshFailed(refreshErr)
	}
	if err := s.repo.UpdateSnapshot(ctx, id, snapshot, checkedAt); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *UpstreamRateMonitorService) decryptInPlace(m *UpstreamRateMonitor) {
	if m == nil || strings.TrimSpace(m.PasswordEncrypted) == "" {
		return
	}
	plain, err := s.encryptor.Decrypt(m.PasswordEncrypted)
	if err != nil {
		m.Password = ""
		m.PasswordDecryptFailed = true
		return
	}
	m.Password = plain
}

func (s *UpstreamRateMonitorService) fetchSnapshot(ctx context.Context, m *UpstreamRateMonitor) (UpstreamRateSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, upstreamRateMonitorHTTPTimeout)
	defer cancel()

	token, err := s.login(ctx, m)
	if err != nil {
		return nil, err
	}
	groups, err := s.fetchAvailableGroups(ctx, m.BaseURL, token)
	if err != nil {
		return nil, err
	}
	rates, err := s.fetchUserGroupRates(ctx, m.BaseURL, token)
	if err != nil {
		return nil, err
	}
	if err := applyUpstreamGroupRateOverrides(groups, rates); err != nil {
		return nil, err
	}
	return groups, nil
}

func (s *UpstreamRateMonitorService) login(ctx context.Context, m *UpstreamRateMonitor) (string, error) {
	payload := map[string]string{
		"email":    strings.TrimSpace(m.Username),
		"password": m.Password,
	}
	var out upstreamLoginEnvelope
	if err := s.doJSON(ctx, http.MethodPost, joinUpstreamPath(m.BaseURL, "/api/v1/auth/login"), "", payload, &out); err != nil {
		return "", fmt.Errorf("upstream login failed: %w", err)
	}
	if err := validateUpstreamAPIEnvelope(out.Code, out.Message); err != nil {
		return "", fmt.Errorf("upstream login failed: %w", err)
	}
	token := strings.TrimSpace(out.Data.AccessToken)
	if token == "" {
		if out.Data.Requires2FA {
			return "", fmt.Errorf("upstream login requires 2FA")
		}
		return "", fmt.Errorf("upstream login did not return access_token")
	}
	return token, nil
}

func (s *UpstreamRateMonitorService) fetchAvailableGroups(ctx context.Context, baseURL, token string) (UpstreamRateSnapshot, error) {
	var env upstreamAvailableGroupsEnvelope
	if err := s.doJSON(ctx, http.MethodGet, joinUpstreamPath(baseURL, "/api/v1/groups/available"), token, nil, &env); err != nil {
		return nil, fmt.Errorf("fetch upstream available groups failed: %w", err)
	}
	if err := validateUpstreamAPIEnvelope(env.Code, env.Message); err != nil {
		return nil, fmt.Errorf("fetch upstream available groups failed: %w", err)
	}
	if env.Data == nil {
		return UpstreamRateSnapshot{}, nil
	}
	return env.Data, nil
}

func (s *UpstreamRateMonitorService) fetchUserGroupRates(ctx context.Context, baseURL, token string) (map[int64]float64, error) {
	var env upstreamUserGroupRatesEnvelope
	if err := s.doJSON(ctx, http.MethodGet, joinUpstreamPath(baseURL, "/api/v1/groups/rates"), token, nil, &env); err != nil {
		return nil, fmt.Errorf("fetch upstream user group rates failed: %w", err)
	}
	if err := validateUpstreamAPIEnvelope(env.Code, env.Message); err != nil {
		return nil, fmt.Errorf("fetch upstream user group rates failed: %w", err)
	}
	return env.Data, nil
}

func applyUpstreamGroupRateOverrides(snapshot UpstreamRateSnapshot, rates map[int64]float64) error {
	if len(snapshot) == 0 || len(rates) == 0 {
		return nil
	}
	for i := range snapshot {
		rate, ok := rates[snapshot[i].ID]
		if !ok {
			continue
		}
		if rate <= 0 {
			return fmt.Errorf("invalid upstream user group rate for group %d", snapshot[i].ID)
		}
		snapshot[i].RateMultiplier = rate
	}
	return nil
}

func (s *UpstreamRateMonitorService) doJSON(ctx context.Context, method, endpoint, token string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	limited := io.LimitReader(resp.Body, upstreamRateMonitorMaxBodySize+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if len(raw) > upstreamRateMonitorMaxBodySize {
		return fmt.Errorf("response body too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateServiceMessage(string(raw), 300))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

type upstreamLoginEnvelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		AccessToken string `json:"access_token"`
		Requires2FA bool   `json:"requires_2fa"`
	} `json:"data"`
}

type upstreamAvailableGroupsEnvelope struct {
	Code    int                  `json:"code"`
	Message string               `json:"message"`
	Data    UpstreamRateSnapshot `json:"data"`
}

type upstreamUserGroupRatesEnvelope struct {
	Code    int               `json:"code"`
	Message string            `json:"message"`
	Data    map[int64]float64 `json:"data"`
}

func normalizeUpstreamBaseURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", ErrUpstreamRateMonitorInvalidURL
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", ErrUpstreamRateMonitorInvalidURL
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && scheme != "http" {
		return "", ErrUpstreamRateMonitorInvalidURL
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrUpstreamRateMonitorInvalidURLPath
	}
	if parsed.Hostname() == "" {
		return "", ErrUpstreamRateMonitorInvalidURL
	}
	if port := parsed.Port(); port != "" {
		num, err := strconv.Atoi(port)
		if err != nil || num <= 0 || num > 65535 {
			return "", ErrUpstreamRateMonitorInvalidURL
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), monitorEndpointResolveTimeout)
	defer cancel()
	blocked, err := isPrivateOrLoopbackHost(ctx, parsed.Hostname())
	if err != nil {
		return "", ErrUpstreamRateMonitorInvalidURL
	}
	if blocked {
		return "", ErrUpstreamRateMonitorPrivateHost
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func joinUpstreamPath(baseURL, path string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func validateUpstreamAPIEnvelope(code int, message string) error {
	if code == 0 {
		return nil
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = "upstream returned non-zero code"
	}
	return fmt.Errorf("code %d: %s", code, truncateServiceMessage(msg, 300))
}

func newUpstreamRateMonitorRefreshFailed(err error) error {
	message := "refresh upstream rate monitor failed"
	if err != nil {
		message = truncateServiceMessage(err.Error(), 500)
	}
	return infraerrors.New(http.StatusBadGateway, "UPSTREAM_RATE_MONITOR_REFRESH_FAILED", message)
}

func newUpstreamRateHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           safeDialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: upstreamRateMonitorHTTPTimeout,
	}
	return &http.Client{
		Timeout:   upstreamRateMonitorHTTPTimeout,
		Transport: transport,
	}
}

func truncateServiceMessage(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max]
}
