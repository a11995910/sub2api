package modelleaderboard

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func seedSource(t *testing.T) sourceData {
	t.Helper()
	var raw sourceData
	if err := json.Unmarshal(seed, &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}
func number(v float64) *float64 { return &v }
func TestOriginalIndexAndUnits(t *testing.T) {
	raw := seedSource(t)
	raw.Response.Data = raw.Response.Data[:1]
	item := &raw.Response.Data[0]
	item.Evaluations["artificial_analysis_intelligence_index"] = number(60)
	item.Evaluations["terminalbench_v4_0"] = number(.65)
	item.Evaluations["gpqa"] = nil
	item.Speed = number(100)
	item.Latency = number(.7)
	snapshot, err := parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	model := snapshot.Models[0]
	if *model.Overall != 60 || *model.Scores["coding"] != 65 || model.Scores["reasoning"] != nil || *model.Scores["speed"] != 100 || *model.Scores["latency"] != .7 {
		t.Fatalf("原始指标或单位处理错误：%+v", model)
	}
	item.Speed = number(0)
	item.Latency = number(0)
	item.Evaluations["terminalbench_v4_0"] = number(0)
	snapshot, err = parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Models[0].Scores["speed"] != nil || snapshot.Models[0].Scores["latency"] != nil || *snapshot.Models[0].Scores["coding"] != 0 {
		t.Fatal("缺失性能测量与真实评测零分应区分")
	}
}
func TestRejectInvalidSource(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), -1, 1.1} {
		raw := seedSource(t)
		raw.Response.Data[0].Evaluations["terminalbench_v4_0"] = number(value)
		if _, err := parse(raw); err == nil {
			t.Fatal("无效评测比例必须拒绝")
		}
	}
	raw := seedSource(t)
	raw.Response.Data = append(raw.Response.Data, raw.Response.Data[0])
	if _, err := parse(raw); err == nil {
		t.Fatal("重复 ID 必须拒绝")
	}
	raw = seedSource(t)
	for i := range raw.Response.Data {
		raw.Response.Data[i].Evaluations = map[string]*float64{}
	}
	if _, err := parse(raw); err == nil {
		t.Fatal("缺失智能指数不能覆盖缓存")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureClient(t *testing.T, raw sourceData, calls *atomic.Int32) *http.Client {
	t.Helper()
	body, _ := json.Marshal(raw.Response)
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.String() != sourceURL+apiPath || r.Header.Get("x-api-key") != "test-runtime-key" {
			t.Error("请求必须使用官方 API 与运行时认证头")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})}
}
func TestSyncCacheFailureAndRestart(t *testing.T) {
	t.Setenv("ARTIFICIAL_ANALYSIS_API_KEY", "test-runtime-key")
	dir := t.TempDir()
	s := New(dir)
	var calls atomic.Int32
	s.client = fixtureClient(t, seedSource(t), &calls)
	s.refresh()
	if s.failed || calls.Load() != 1 {
		t.Fatal("官方 API 同步失败")
	}
	cached, err := os.ReadFile(s.cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cached), "test-runtime-key") {
		t.Fatal("缓存不得包含 API Key")
	}
	restored := New(dir)
	if !restored.snapshot.SyncedAt.Equal(s.snapshot.SyncedAt) {
		t.Fatal("重启未恢复最近同步")
	}
	old := s.snapshot.SyncedAt
	s.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	s.refresh()
	if !s.failed || !s.snapshot.SyncedAt.Equal(old) || time.Until(s.nextAttempt) < 59*time.Minute {
		t.Fatal("认证失败必须保留快照且退避")
	}
}
func TestNoCredentialShowsSnapshotWithoutRequests(t *testing.T) {
	t.Setenv("ARTIFICIAL_ANALYSIS_API_KEY", "")
	s := New("")
	s.nextAttempt = time.Time{}
	result := s.Get()
	if result.SyncEnabled || result.Refreshing || len(result.Models) == 0 {
		t.Fatal("未配置凭据时应明确展示快照状态")
	}
}
func TestConcurrentAccessStartsOneRefresh(t *testing.T) {
	t.Setenv("ARTIFICIAL_ANALYSIS_API_KEY", "test-runtime-key")
	s := New("")
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	s.nextAttempt = time.Time{}
	s.snapshot.SyncedAt = time.Now().Add(-25 * time.Hour)
	if result := s.Get(); !result.Stale || !result.Refreshing {
		t.Fatal("过期访问应标记后台同步")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("未发起同步")
	}
	for i := 0; i < 30; i++ {
		s.Get()
	}
	if calls.Load() != 1 {
		t.Fatal("并发访问重复同步")
	}
	close(release)
}
func TestLiveSource(t *testing.T) {
	if os.Getenv("SUB2API_TEST_LIVE_LEADERBOARD") != "1" {
		t.Skip("仅显式开启时访问官方 API")
	}
	s := New("")
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	raw, err := s.download(ctx)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, m := range snapshot.Models {
		if m.Overall != nil {
			count++
		}
	}
	t.Logf("官方 API 校验成功：%d 条配置，%d 条智能指数", len(snapshot.Models), count)
}
