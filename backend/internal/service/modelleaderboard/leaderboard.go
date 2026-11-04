// Package modelleaderboard 同步 Artificial Analysis 官方 API，保留最近一次有效快照。
package modelleaderboard

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const sourceURL = "https://artificialanalysis.ai"
const apiPath = "/api/v2/data/llms/models"
const refreshInterval = 24 * time.Hour
const maxResponseBytes = 4 << 20

//go:embed seed.json
var seed []byte

type Category struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Unit      string `json:"unit"`
	Ascending bool   `json:"ascending"`
}
type Model struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Slug        string              `json:"slug"`
	Creator     string              `json:"creator"`
	ReleaseDate string              `json:"release_date,omitempty"`
	Overall     *float64            `json:"overall"`
	Scores      map[string]*float64 `json:"scores"`
	InputPrice  *float64            `json:"input_price"`
	OutputPrice *float64            `json:"output_price"`
}
type Snapshot struct {
	Source               string     `json:"source"`
	SourceURL            string     `json:"source_url"`
	MethodologyURL       string     `json:"methodology_url"`
	DataURL              string     `json:"data_url"`
	SyncedAt             time.Time  `json:"synced_at"`
	RefreshIntervalHours int        `json:"refresh_interval_hours"`
	SyncEnabled          bool       `json:"sync_enabled"`
	Stale                bool       `json:"stale"`
	Refreshing           bool       `json:"refreshing"`
	LastAttemptFailed    bool       `json:"last_attempt_failed"`
	Categories           []Category `json:"categories"`
	Models               []Model    `json:"models"`
}

type apiModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	ReleaseDate string `json:"release_date"`
	Creator     struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"model_creator"`
	Evaluations map[string]*float64 `json:"evaluations"`
	Pricing     struct {
		Input  *float64 `json:"price_1m_input_tokens"`
		Output *float64 `json:"price_1m_output_tokens"`
	} `json:"pricing"`
	Speed   *float64 `json:"median_output_tokens_per_second"`
	Latency *float64 `json:"median_time_to_first_token_seconds"`
}
type apiResponse struct {
	Status int        `json:"status"`
	Data   []apiModel `json:"data"`
}

// 只缓存公开响应，不缓存请求头或凭据。
type sourceData struct {
	SyncedAt time.Time   `json:"synced_at"`
	Response apiResponse `json:"response"`
}

type Store struct {
	mu          sync.Mutex
	client      *http.Client
	apiKey      string
	snapshot    Snapshot
	cachePath   string
	nextAttempt time.Time
	refreshing  bool
	failed      bool
}

func New(dataDir string) *Store {
	s := &Store{apiKey: strings.TrimSpace(os.Getenv("ARTIFICIAL_ANALYSIS_API_KEY")), client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
	if s.apiKey == "" && dataDir != "" {
		// 可将凭据置于已有数据卷，避免为了新增环境变量改变发布编排。
		if b, err := os.ReadFile(filepath.Join(dataDir, "artificial-analysis-api-key")); err == nil && len(b) <= 8192 {
			s.apiKey = strings.TrimSpace(string(b))
		}
	}
	var raw sourceData
	if json.Unmarshal(seed, &raw) != nil {
		panic("内置模型榜单格式错误")
	}
	var err error
	s.snapshot, err = parse(raw)
	if err != nil {
		panic("内置模型榜单校验失败")
	}
	if dataDir != "" {
		s.cachePath = filepath.Join(dataDir, "artificial-analysis-leaderboard.json")
		if b, err := os.ReadFile(s.cachePath); err == nil && len(b) <= maxResponseBytes {
			var cached sourceData
			if json.Unmarshal(b, &cached) == nil {
				if parsed, err := parse(cached); err == nil && parsed.SyncedAt.After(s.snapshot.SyncedAt) && !parsed.SyncedAt.After(time.Now()) {
					s.snapshot = parsed
				}
			}
		}
	}
	s.nextAttempt = s.snapshot.SyncedAt.Add(refreshInterval)
	return s
}

// Get 立即返回缓存；每 24 小时由访问触发一次后台同步，同一进程仅允许一个同步。
func (s *Store) Get() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.apiKey != "" && !s.refreshing && !now.Before(s.nextAttempt) {
		s.refreshing = true
		go s.refresh()
	}
	result := s.snapshot
	result.SyncEnabled = s.apiKey != ""
	result.Stale = now.Sub(result.SyncedAt) >= refreshInterval
	result.Refreshing = s.refreshing
	result.LastAttemptFailed = s.failed
	return result
}

func (s *Store) download(ctx context.Context) (sourceData, error) {
	var result sourceData
	if s.apiKey == "" {
		return result, fmt.Errorf("榜单同步凭据尚未配置")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL+apiPath, nil)
	if err != nil {
		return result, fmt.Errorf("榜单请求创建失败")
	}
	req.Header.Set("x-api-key", s.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Sub2API-ModelLeaderboard/1.0")
	resp, err := s.client.Do(req)
	if err != nil {
		return result, fmt.Errorf("榜单数据源不可达")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("榜单数据源返回状态 %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(b) > maxResponseBytes {
		return result, fmt.Errorf("榜单读取失败或超过大小限制")
	}
	if json.Unmarshal(b, &result.Response) != nil {
		return result, fmt.Errorf("榜单 API 响应格式无效")
	}
	result.SyncedAt = time.Now().UTC()
	return result, nil
}

func (s *Store) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	raw, err := s.download(ctx)
	var next Snapshot
	if err == nil {
		next, err = parse(raw)
	}
	s.mu.Lock()
	if err == nil {
		s.snapshot = next
		s.nextAttempt = next.SyncedAt.Add(refreshInterval)
	} else {
		s.nextAttempt = time.Now().Add(time.Hour)
		log.Printf("模型榜单同步失败，继续使用已校验数据：%v", err)
	}
	s.failed = err != nil
	s.refreshing = false
	s.mu.Unlock()
	if err == nil && s.cachePath != "" {
		if s.persist(raw) != nil {
			log.Print("模型榜单缓存写入失败，当前进程继续使用内存数据")
		}
	}
}

func (s *Store) persist(raw sourceData) error {
	b, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.cachePath), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.cachePath), ".artificial-analysis-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(b)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), s.cachePath)
}

var categoryDefinitions = []struct {
	Category
	field string
}{
	{Category{"overall", "Artificial Analysis Intelligence Index", "index", false}, "artificial_analysis_intelligence_index"},
	{Category{"coding", "Terminal-Bench 4.0", "percent", false}, "terminalbench_v4_0"},
	{Category{"reasoning", "GPQA", "percent", false}, "gpqa"},
	{Category{"knowledge", "Humanity's Last Exam", "percent", false}, "hle"},
	{Category{"long_context", "AA-LCR", "percent", false}, "lcr"},
	{Category{"speed", "Output Speed", "tokens/s", false}, ""},
	{Category{"latency", "Time to First Token", "s", true}, ""},
}

func validNumber(value *float64, maximum float64) bool {
	return value == nil || (!math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0 && *value <= maximum)
}
func measuredPositive(value *float64) *float64 {
	if value == nil || *value <= 0 {
		return nil
	}
	return value
}
func parse(raw sourceData) (Snapshot, error) {
	result := Snapshot{Source: "Artificial Analysis", SourceURL: sourceURL + "/leaderboards/models", MethodologyURL: sourceURL + "/methodology", DataURL: sourceURL + "/documentation", SyncedAt: raw.SyncedAt, RefreshIntervalHours: 24}
	if raw.SyncedAt.IsZero() || raw.Response.Status != 200 || len(raw.Response.Data) == 0 || len(raw.Response.Data) > 5000 {
		return result, fmt.Errorf("榜单响应状态或数据无效")
	}
	for _, definition := range categoryDefinitions {
		result.Categories = append(result.Categories, definition.Category)
	}
	seen := map[string]bool{}
	intelligenceCount := 0
	for _, item := range raw.Response.Data {
		if item.ID == "" || item.Name == "" || item.Slug == "" || len(item.Name) > 240 || seen[item.ID] {
			return result, fmt.Errorf("榜单模型标识无效或重复")
		}
		seen[item.ID] = true
		model := Model{ID: item.ID, Name: item.Name, Slug: item.Slug, Creator: item.Creator.Name, ReleaseDate: item.ReleaseDate, Scores: map[string]*float64{}, InputPrice: item.Pricing.Input, OutputPrice: item.Pricing.Output}
		for _, definition := range categoryDefinitions {
			value := item.Evaluations[definition.field]
			if definition.Unit == "percent" {
				if !validNumber(value, 1) {
					return result, fmt.Errorf("榜单评测比例无效")
				}
				if value != nil {
					percent := *value * 100
					value = &percent
				}
			} else if definition.Key == "speed" {
				value = measuredPositive(item.Speed)
			} else if definition.Key == "latency" {
				value = measuredPositive(item.Latency)
			} else if !validNumber(value, 1000) {
				return result, fmt.Errorf("榜单智能指数无效")
			}
			model.Scores[definition.Key] = value
		}
		if !validNumber(item.Speed, 1e8) || !validNumber(item.Latency, 1e8) || !validNumber(model.InputPrice, 1e8) || !validNumber(model.OutputPrice, 1e8) {
			return result, fmt.Errorf("榜单性能或价格数据无效")
		}
		model.Overall = model.Scores["overall"]
		if model.Overall != nil {
			intelligenceCount++
		}
		result.Models = append(result.Models, model)
	}
	if intelligenceCount == 0 {
		return result, fmt.Errorf("榜单智能指数缺失，拒绝覆盖缓存")
	}
	return result, nil
}
