package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func (s *OpenAIGatewayService) localizeOpenAIImagesJSONResponse(
	ctx context.Context,
	c *gin.Context,
	body []byte,
	opts openAIImagesNonStreamingResponseOptions,
) ([]byte, error) {
	if opts.parsed == nil || !strings.EqualFold(opts.parsed.ResponseFormat, ImageResponseFormatURL) {
		return body, nil
	}
	if s == nil || s.generatedImageStore == nil {
		return nil, fmt.Errorf("generated image storage is unavailable")
	}
	data := gjson.GetBytes(body, "data")
	if !data.IsArray() {
		return body, nil
	}
	origin := s.generatedImagePublicOrigin(ctx, c)
	rewritten := body
	for index, item := range data.Array() {
		var imageBytes []byte
		if encoded := normalizeOpenAIImageBase64(item.Get("b64_json").String()); encoded != "" {
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return nil, fmt.Errorf("decode generated image: %w", err)
			}
			imageBytes = decoded
		} else {
			imageURL := firstNonEmptyString(
				item.Get("url").String(),
				item.Get("image_url").String(),
				item.Get("download_url").String(),
				imageURLFromInvalidOpenAIImageBase64(item.Get("b64_json").String()),
			)
			if imageURL == "" {
				continue
			}
			downloaded, err := s.downloadOpenAIImagesResponseURL(opts, imageURL)
			if err != nil {
				return nil, fmt.Errorf("download generated image: %w", err)
			}
			imageBytes = downloaded
		}
		saved, err := s.generatedImageStore.Save(ctx, imageBytes, time.Now().UTC())
		if err != nil {
			return nil, fmt.Errorf("store generated image: %w", err)
		}
		path := fmt.Sprintf("data.%d", index)
		rewritten, _ = sjson.SetBytes(rewritten, path+".url", s.generatedImageStore.PublicURL(saved.Name, origin))
		rewritten, _ = sjson.DeleteBytes(rewritten, path+".b64_json")
		rewritten, _ = sjson.DeleteBytes(rewritten, path+".image_url")
		rewritten, _ = sjson.DeleteBytes(rewritten, path+".download_url")
	}
	return rewritten, nil
}

func (s *OpenAIGatewayService) localizeOpenAIImageResults(
	ctx context.Context,
	c *gin.Context,
	results []openAIResponsesImageResult,
) ([]openAIResponsesImageResult, error) {
	if s == nil || s.generatedImageStore == nil {
		return nil, fmt.Errorf("generated image storage is unavailable")
	}
	origin := s.generatedImagePublicOrigin(ctx, c)
	localized := make([]openAIResponsesImageResult, 0, len(results))
	for _, imageResult := range results {
		encoded := normalizeOpenAIImageBase64(imageResult.Result)
		if encoded == "" {
			return nil, fmt.Errorf("generated image result is not materialized")
		}
		imageBytes, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("decode generated image: %w", err)
		}
		saved, err := s.generatedImageStore.Save(ctx, imageBytes, time.Now().UTC())
		if err != nil {
			return nil, fmt.Errorf("store generated image: %w", err)
		}
		imageResult.Result = ""
		imageResult.URL = s.generatedImageStore.PublicURL(saved.Name, origin)
		localized = append(localized, imageResult)
	}
	return localized, nil
}

func (s *OpenAIGatewayService) localizeOpenAIImagesStreamingPayload(
	ctx context.Context,
	c *gin.Context,
	payload []byte,
	opts openAIImagesStreamingResponseOptions,
) ([]byte, error) {
	if opts.parsed == nil || !strings.EqualFold(opts.parsed.ResponseFormat, ImageResponseFormatURL) {
		return payload, nil
	}
	if s == nil || s.generatedImageStore == nil {
		return nil, fmt.Errorf("generated image storage is unavailable")
	}
	imageBytes, err := s.imageBytesFromOpenAIImagesStreamingPayload(ctx, opts, payload)
	if err != nil {
		return nil, err
	}
	saved, err := s.generatedImageStore.Save(ctx, imageBytes, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("store generated image: %w", err)
	}
	rewritten, _ := sjson.SetBytes(payload, "url", s.generatedImageStore.PublicURL(saved.Name, s.generatedImagePublicOrigin(ctx, c)))
	rewritten, _ = sjson.DeleteBytes(rewritten, "b64_json")
	rewritten, _ = sjson.DeleteBytes(rewritten, "image_url")
	rewritten, _ = sjson.DeleteBytes(rewritten, "download_url")
	return rewritten, nil
}

func (s *OpenAIGatewayService) generatedImagePublicOrigin(ctx context.Context, c *gin.Context) string {
	if s != nil && s.settingService != nil {
		if settings, err := s.settingService.GetPublicSettings(ctx); err == nil && settings != nil {
			if origin := generatedImagePublicOrigin(settings.APIBaseURL); origin != "" {
				return origin
			}
		}
	}
	if c == nil || c.Request == nil {
		return ""
	}
	// 请求 Host 和转发头都可能由客户端控制；未配置公开 API 地址时返回同域相对 URL。
	return ""
}
