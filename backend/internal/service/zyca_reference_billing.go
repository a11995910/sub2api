package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

type ZYCAReferenceBillingSnapshot struct {
	AccountID              int64     `json:"account_id"`
	UpstreamModel          string    `json:"upstream_model"`
	GeneratedSeconds       int       `json:"generated_seconds"`
	ReferenceSeconds       float64   `json:"reference_seconds"`
	BillableSeconds        float64   `json:"billable_seconds"`
	ReferenceDurations     []float64 `json:"reference_durations"`
	ReferenceContentSHA256 []string  `json:"reference_content_sha256"`
}

type zycaReferenceBillingContextKey struct{}

type zycaReferenceProbe func(context.Context, string) (float64, string, error)

func zycaBillsReferenceVideo(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "kling-4.0", "seedance-2.5", "seedance-2.0", "seedance-2.0-mini":
		return true
	default:
		return false
	}
}

func (s *OpenAIGatewayService) PrepareZYCAReferenceBillingContext(ctx context.Context, account *Account, upstreamModel string, body []byte, generatedSeconds int) (context.Context, error) {
	var trustedOrigins []string
	if s != nil && s.cfg != nil {
		trustedOrigins = s.cfg.Gateway.ZYCATrustedMediaOrigins
	}
	return prepareZYCAReferenceBillingContext(ctx, account, upstreamModel, body, generatedSeconds,
		func(ctx context.Context, rawURL string) (float64, string, error) {
			return lookupZYCAReferenceMetadata(ctx, rawURL, trustedOrigins, zycaMetadataHTTPClient)
		})
}

func prepareZYCAReferenceBillingContext(ctx context.Context, account *Account, upstreamModel string, body []byte, generatedSeconds int, probe zycaReferenceProbe) (context.Context, error) {
	ctx = context.WithValue(ctx, zycaReferenceBillingContextKey{}, (*ZYCAReferenceBillingSnapshot)(nil))
	if ResolveOpenAIVideoRequestProfile(account) != OpenAIVideoRequestProfileZYCA || !zycaBillsReferenceVideo(upstreamModel) {
		return ctx, nil
	}
	_, request, err := ParseOpenAIVideoCreateBody(body)
	if err != nil {
		return ctx, err
	}
	if len(request.VideoURLs) == 0 {
		return ctx, nil
	}
	model := strings.ToLower(strings.TrimSpace(upstreamModel))
	if generatedSeconds <= 0 || request.DurationSeconds != generatedSeconds || len(request.VideoURLs) > zycaVideoModelProfiles[model].maxVideos {
		return ctx, errors.New("ZYCA 参考视频计费参数无效")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	snapshot := &ZYCAReferenceBillingSnapshot{AccountID: account.ID, UpstreamModel: model, GeneratedSeconds: generatedSeconds}
	for index, rawURL := range request.VideoURLs {
		seconds, digest, err := probe(probeCtx, rawURL)
		if err != nil || !validVideoStatsAmount(seconds) || seconds <= 0 || seconds > 3600 {
			return ctx, fmt.Errorf("第 %d 个参考视频缺少有效的可信媒体元数据，请通过已配置的临时资源服务重新上传；任务未提交", index+1)
		}
		snapshot.ReferenceSeconds += math.Ceil(seconds)
		snapshot.ReferenceDurations = append(snapshot.ReferenceDurations, seconds)
		snapshot.ReferenceContentSHA256 = append(snapshot.ReferenceContentSHA256, digest)
	}
	snapshot.BillableSeconds = float64(generatedSeconds) + snapshot.ReferenceSeconds
	return context.WithValue(ctx, zycaReferenceBillingContextKey{}, snapshot), nil
}

func zycaReferenceSnapshot(ctx context.Context) *ZYCAReferenceBillingSnapshot {
	snapshot, _ := ctx.Value(zycaReferenceBillingContextKey{}).(*ZYCAReferenceBillingSnapshot)
	return snapshot
}

func zycaReferenceBillingFactor(ctx context.Context, accountID int64, upstreamModel string, generatedSeconds int) (float64, error) {
	snapshot := zycaReferenceSnapshot(ctx)
	if snapshot == nil {
		return 1, nil
	}
	if snapshot.AccountID != accountID || snapshot.GeneratedSeconds != generatedSeconds || generatedSeconds <= 0 ||
		!zycaBillsReferenceVideo(snapshot.UpstreamModel) ||
		(upstreamModel != "" && snapshot.UpstreamModel != strings.ToLower(strings.TrimSpace(upstreamModel))) ||
		!validVideoStatsAmount(snapshot.ReferenceSeconds) || snapshot.ReferenceSeconds <= 0 ||
		snapshot.BillableSeconds != float64(generatedSeconds)+snapshot.ReferenceSeconds {
		return 0, errors.New("ZYCA 参考视频计费快照与当前任务不匹配")
	}
	return snapshot.BillableSeconds / float64(generatedSeconds), nil
}
