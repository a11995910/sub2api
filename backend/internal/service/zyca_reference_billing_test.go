//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestZYCAReferenceBillingRoundsEachProbedDuration(t *testing.T) {
	account := zycaTestAccount()
	body := []byte(`{"model":"zyca-kling-4.0","prompt":"测试","duration":10,"reference_videos":["https://cdn.example/one.mp4","https://cdn.example/two.mp4","https://cdn.example/one.mp4"],"reference_duration":999}`)
	calls := 0
	ctx, err := prepareZYCAReferenceBillingContext(context.Background(), account, "kling-4.0", body, 10,
		func(context.Context, string) (float64, string, error) {
			calls++
			return 2.75, "服务端内容摘要", nil
		})
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	snapshot := zycaReferenceSnapshot(ctx)
	require.Equal(t, 6.0, snapshot.ReferenceSeconds)
	require.Equal(t, 16.0, snapshot.BillableSeconds)
	require.Equal(t, []float64{2.75, 2.75}, snapshot.ReferenceDurations)
	factor, err := zycaReferenceBillingFactor(ctx, account.ID, "kling-4.0", 10)
	require.NoError(t, err)
	require.InDelta(t, 1.6, factor, 1e-9)
	_, err = zycaReferenceBillingFactor(ctx, account.ID+1, "kling-4.0", 10)
	require.Error(t, err)
	_, err = zycaReferenceBillingFactor(ctx, account.ID, "seedance-2.5", 10)
	require.Error(t, err)
	_, err = zycaReferenceBillingFactor(ctx, account.ID, "kling-4.0", 5)
	require.Error(t, err)
}

func TestZYCAReferenceBillingRoundingBoundaries(t *testing.T) {
	for _, test := range []struct {
		name      string
		durations []float64
		expected  float64
	}{
		{"整数不增加", []float64{2, 3}, 5},
		{"逐段取整而非合计取整", []float64{2.25, 2.5}, 6},
		{"不足一秒", []float64{0.25, 0.5}, 2},
		{"刚超过整数", []float64{2.000001, 3}, 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(`{"model":"zyca-seedance-2.0-mini","prompt":"测试","duration":4,"reference_videos":["https://cdn.example/one.mp4","https://cdn.example/two.mp4"]}`)
			index := 0
			ctx, err := prepareZYCAReferenceBillingContext(context.Background(), zycaTestAccount(), "seedance-2.0-mini", body, 4,
				func(context.Context, string) (float64, string, error) {
					duration := test.durations[index]
					index++
					return duration, "服务端内容摘要", nil
				})
			require.NoError(t, err)
			snapshot := zycaReferenceSnapshot(ctx)
			require.Equal(t, test.expected, snapshot.ReferenceSeconds)
			require.Equal(t, 4+test.expected, snapshot.BillableSeconds)
			require.Equal(t, test.durations, snapshot.ReferenceDurations)
		})
	}
}

func TestZYCAReferenceBillingIsolationAndFailure(t *testing.T) {
	body := []byte(`{"model":"video","prompt":"测试","duration":10,"reference_videos":["https://cdn.example/video.mp4"]}`)
	for _, test := range []struct{ protocol, model string }{
		{"legacy", "kling-4.0"}, {"unified_json", "seedance-2.5"}, {"zyca", "wan-3.0"}, {"zyca", "minimax-h3-903"},
	} {
		account := zycaTestAccount()
		account.Credentials["video_request_profile"] = test.protocol
		ctx, err := prepareZYCAReferenceBillingContext(context.Background(), account, test.model, body, 10,
			func(context.Context, string) (float64, string, error) {
				t.Fatal("非目标协议或模型不应探测参考视频")
				return 0, "", nil
			})
		require.NoError(t, err)
		require.Nil(t, zycaReferenceSnapshot(ctx))
	}
	for _, duration := range []float64{0, -1, math.NaN(), math.Inf(1), 3601} {
		_, err := prepareZYCAReferenceBillingContext(context.Background(), zycaTestAccount(), "kling-4.0", body, 10,
			func(context.Context, string) (float64, string, error) { return duration, "", nil })
		require.Error(t, err)
	}
	_, err := prepareZYCAReferenceBillingContext(context.Background(), zycaTestAccount(), "kling-4.0", body, 10,
		func(context.Context, string) (float64, string, error) {
			return 0, "", errors.New("私有签名地址不得进入错误信息")
		})
	require.ErrorContains(t, err, "任务未提交")
	require.NotContains(t, err.Error(), "私有签名")
}

func TestZYCAReferenceBillingSnapshotReservation(t *testing.T) {
	for _, mode := range []BillingMode{BillingModeVideo, BillingModePerRequest} {
		t.Run(string(mode), func(t *testing.T) {
			account := zycaTestAccount()
			svc := newOpenAIRecordUsageServiceForTest(nil, nil, nil, nil)
			channel := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{100}, ModelPricing: []ChannelModelPricing{{
				Platform: PlatformOpenAI, Models: []string{"zyca-kling-4.0"}, BillingMode: mode, PerRequestPrice: testPtrFloat64(0.3),
			}}, AccountStatsPricingRules: []AccountStatsPricingRule{{GroupIDs: []int64{100}, Pricing: []ChannelModelPricing{{
				Platform: PlatformOpenAI, Models: []string{"kling-4.0"}, BillingMode: mode, PerRequestPrice: testPtrFloat64(0.2),
			}}}}}
			svc.channelService = newTestChannelService(makeStandardRepo(channel, map[int64]string{100: PlatformOpenAI}))
			svc.resolver = NewModelPricingResolver(svc.channelService, svc.billingService)
			key := &APIKey{ID: 11, GroupID: groupIDPtr(), Group: &Group{ID: 100, RateMultiplier: 1}}
			body := []byte(`{"model":"zyca-kling-4.0","prompt":"测试","duration":30,"reference_videos":["https://cdn.example/video.mp4"]}`)
			ctx, err := prepareZYCAReferenceBillingContext(context.Background(), account, "kling-4.0", body, 30,
				func(context.Context, string) (float64, string, error) { return 5.25, strings.Repeat("a", 64), nil })
			require.NoError(t, err)
			repo := &fakeVideoTaskBillingRepo{}
			task, err := NewVideoTaskBillingService(repo, svc, nil).Reserve(ctx, VideoTaskReserveInput{
				RequestID: "参考时长费用快照", Platform: PlatformOpenAI, UserID: 7, APIKeyID: key.ID,
				AccountID: account.ID, Account: account, APIKey: key, GroupID: key.GroupID,
				Model: "zyca-kling-4.0", UpstreamModel: "kling-4.0", Resolution: "720p", DurationSeconds: 30,
			})
			require.NoError(t, err)
			var usage VideoTaskUsageContext
			require.NoError(t, json.Unmarshal(task.UsageContextJSON, &usage))
			require.Equal(t, 30, task.DurationSeconds)
			require.Equal(t, 36.0, usage.ZYCAReferenceBilling.BillableSeconds)
			require.NotContains(t, string(task.UsageContextJSON), "cdn.example")
			units := 1.0
			if mode == BillingModeVideo {
				units = 36
			}
			require.InDelta(t, 0.3*units, task.EstimatedCost, 1e-9)
			require.InDelta(t, 0.3*units, usage.CostSnapshot.TotalCost, 1e-9)
			require.InDelta(t, 0.2*units, *usage.AccountStatsSnapshot.Cost, 1e-9)
		})
	}
}

func TestZYCAGeneratedSecondsNotClampedToLegacyLimit(t *testing.T) {
	svc := newOpenAIRecordUsageServiceForTest(nil, nil, nil, nil)
	svc.resolver = newResolverWithChannel(t, []ChannelModelPricing{{
		Platform: "anthropic", Models: []string{"kling-4.0"}, BillingMode: BillingModeVideo, PerRequestPrice: testPtrFloat64(0.2),
	}})
	key := &APIKey{GroupID: groupIDPtr(), Group: &Group{ID: 100, RateMultiplier: 1}}
	for _, protocol := range []string{"zyca", "legacy", "unified_json"} {
		account := zycaTestAccount()
		account.Credentials["video_request_profile"] = protocol
		cost, err := svc.EstimateVideoCostForAccount(context.Background(), account, key, "kling-4.0", "720p", 30)
		require.NoError(t, err)
		expected := 3.0
		if protocol == "zyca" {
			expected = 6
		}
		require.InDelta(t, expected, cost.TotalCost, 1e-9)
	}
}

func TestZYCATrustedMetadataOriginAndContentBinding(t *testing.T) {
	digest := strings.Repeat("a", 64)
	path := "/temporary-media/" + digest + ".mp4"
	trusted := []string{"https://media.example"}
	target, gotDigest, err := zycaTrustedMetadataURL("https://media.example"+path, trusted)
	require.NoError(t, err)
	require.Equal(t, path+"/metadata", target.Path)
	require.Equal(t, digest, gotDigest)
	for _, raw := range []string{
		"https://attacker.example" + path, "https://media.example.attacker.example" + path,
		"https://media.example:8443" + path, "http://media.example" + path,
		"https://user:password@media.example" + path, "https://media.example" + path + "?duration=1",
		"https://media.example" + path + "#ignored", "https://media.example/video.mp4",
		"https://media.example/temporary-media/%61" + digest[1:] + ".mp4",
	} {
		_, _, err := zycaTrustedMetadataURL(raw, trusted)
		require.Error(t, err)
	}
	_, _, err = zycaTrustedMetadataURL("https://media.example"+path, nil)
	require.Error(t, err)
}

type zycaMetadataTransport func(*http.Request) (*http.Response, error)

func (transport zycaMetadataTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestZYCAOnlyFetchesTrustedMetadataNotVideo(t *testing.T) {
	digest := strings.Repeat("a", 64)
	path := "/temporary-media/" + digest + ".mp4"
	metadata := map[string]any{"version": 1, "sha256": digest, "byteSize": 300 << 20,
		"mimeType": "video/mp4", "durationSeconds": 5.25, "expiresAt": time.Now().Add(time.Hour)}
	body, err := json.Marshal(metadata)
	require.NoError(t, err)
	calls := 0
	client := &http.Client{Transport: zycaMetadataTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "https://media.example"+path+"/metadata", request.URL.String())
		require.Equal(t, http.MethodGet, request.Method)
		require.Empty(t, request.Header.Get("Authorization"))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	seconds, gotDigest, err := lookupZYCAReferenceMetadata(context.Background(), "https://media.example"+path, []string{"https://media.example"}, client)
	require.NoError(t, err)
	require.Equal(t, 5.25, seconds)
	require.Equal(t, digest, gotDigest)
	require.Equal(t, 1, calls)
	_, _, err = lookupZYCAReferenceMetadata(context.Background(), "https://attacker.example"+path, []string{"https://media.example"}, client)
	require.Error(t, err)
	require.Equal(t, 1, calls)
	for field, value := range map[string]any{"version": 2, "sha256": strings.Repeat("b", 64), "byteSize": 0,
		"mimeType": "audio/mp4", "durationSeconds": -1, "expiresAt": time.Now().Add(-time.Second)} {
		original := metadata[field]
		metadata[field] = value
		invalid, err := json.Marshal(metadata)
		require.NoError(t, err)
		_, _, err = parseZYCAReferenceMetadata(invalid, digest, path+"/metadata")
		require.Error(t, err, field)
		metadata[field] = original
	}
}

func TestZYCAMetadataRejectsMissingRedirectedAndOversizedResponses(t *testing.T) {
	digest := strings.Repeat("a", 64)
	videoURL := "https://media.example/temporary-media/" + digest + ".mp4"
	for _, status := range []int{http.StatusNotFound, http.StatusServiceUnavailable, http.StatusFound, http.StatusOK} {
		client := *zycaMetadataHTTPClient
		calls := 0
		client.Transport = zycaMetadataTransport(func(request *http.Request) (*http.Response, error) {
			calls++
			require.Equal(t, videoURL+"/metadata", request.URL.String())
			return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://attacker.example/metadata"}},
				Body: io.NopCloser(strings.NewReader(strings.Repeat("x", (16<<10)+1)))}, nil
		})
		_, _, err := lookupZYCAReferenceMetadata(context.Background(), videoURL, []string{"https://media.example"}, &client)
		require.Error(t, err)
		require.Equal(t, 1, calls)
	}
}
