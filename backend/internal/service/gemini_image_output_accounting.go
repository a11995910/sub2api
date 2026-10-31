package service

import (
	"crypto/sha256"
	"fmt"
	"io"
	"mime"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// geminiImageOutputCounterKey 是请求级图片计数器挂在 gin.Context 上的键。
const geminiImageOutputCounterKey = "gemini_image_output_counter"

// geminiImageOutputCounter 按图片内容记录一次转发里 Gemini 上游真正回吐的图片。
// Gemini SSE 既可能累积重发，也可能把多张图分散到不同 chunk；跨 payload 保存
// 内容摘要可以同时避免重复计费和增量流少计费。
type geminiImageOutputCounter struct {
	digests map[geminiInlineImageDigest]struct{}
}

type geminiInlineImageDigest [sha256.Size]byte

type geminiInlineImagePartRef struct {
	path string
}

// beginGeminiImageOutputObservation 在每次 Forward 开头重置计数器。
// failover 会拿同一个 gin.Context 重跑转发，不重置就会把上一个账号的图数带进来。
func beginGeminiImageOutputObservation(c *gin.Context) *geminiImageOutputCounter {
	if c == nil {
		return nil
	}
	counter := &geminiImageOutputCounter{digests: make(map[geminiInlineImageDigest]struct{})}
	c.Set(geminiImageOutputCounterKey, counter)
	return counter
}

func geminiImageOutputCounterFromContext(c *gin.Context) *geminiImageOutputCounter {
	if c == nil {
		return nil
	}
	value, ok := c.Get(geminiImageOutputCounterKey)
	if !ok {
		return nil
	}
	counter, _ := value.(*geminiImageOutputCounter)
	return counter
}

// observeGeminiImageOutputs 观测一段上游响应（整份或单个 chunk）里的图片。
// 调用点与 upstreamResponseModelObserver.ObserveGemini 一一对应——那里拿得到
// 解包后的上游响应体，这里需要的是同一份字节。
func observeGeminiImageOutputs(c *gin.Context, payload []byte) {
	counter := geminiImageOutputCounterFromContext(c)
	if counter == nil {
		return
	}
	if counter.digests == nil {
		counter.digests = make(map[geminiInlineImageDigest]struct{})
	}
	scanGeminiInlineImageOutputs(payload, counter.digests, nil)
}

func observedGeminiImageOutputs(c *gin.Context) int {
	counter := geminiImageOutputCounterFromContext(c)
	if counter == nil {
		return 0
	}
	return len(counter.digests)
}

// resolveGeminiImageCount 决定本次请求按几张图计费。
//
// 已接入响应观测时只按真正返回的最终图片计数，兼容 inlineData 与 fileData，并按
// 图片内容或引用去重。纯文本、空响应、thought 中间图片均不默认按一张收费。
// 只有没有接入响应观测的旧调用方才保留模型名兜底。
func resolveGeminiImageCount(c *gin.Context, originalModel, mappedModel string) int {
	if counter := geminiImageOutputCounterFromContext(c); counter != nil {
		return len(counter.digests)
	}
	if isImageGenerationModel(originalModel) || isImageGenerationModel(mappedModel) {
		return 1
	}
	return 0
}

// 图片意图与实际出图数量分别记录，避免零张图片落入文本 Token 计费。
func isGeminiImageGenerationRequest(body []byte, originalModel, mappedModel string) bool {
	if isImageGenerationModel(originalModel) || isImageGenerationModel(mappedModel) {
		return true
	}
	for _, path := range []string{"generationConfig.responseModalities", "generationConfig.response_modalities", "generation_config.responseModalities", "generation_config.response_modalities"} {
		for _, modality := range gjson.GetBytes(body, path).Array() {
			if strings.EqualFold(strings.TrimSpace(modality.String()), "IMAGE") {
				return true
			}
		}
	}
	return false
}

// countGeminiInlineImageOutputs 统计一段 Gemini 响应 JSON 里的唯一最终图片。
// Gemini REST 回 camelCase 的 inlineData，官方 SDK 与部分中转会回 snake_case
// 的 inline_data，两种都要认。内容完全相同的重复 part 只计一次；明确标记为
// thought 的中间图片不属于最终交付结果，不计费。
func countGeminiInlineImageOutputs(payload []byte) int {
	seen := make(map[geminiInlineImageDigest]struct{})
	scanGeminiInlineImageOutputs(payload, seen, nil)
	return len(seen)
}

// deduplicateGeminiInlineImageOutputs 删除同一次响应中重复的最终图片 part。
// 流式调用方传入跨 chunk 复用的 seen；非流式传 nil 即可。
func deduplicateGeminiInlineImageOutputs(payload []byte, seen map[geminiInlineImageDigest]struct{}) ([]byte, int) {
	if seen == nil {
		seen = make(map[geminiInlineImageDigest]struct{})
	}
	duplicates := make([]geminiInlineImagePartRef, 0)
	scanGeminiInlineImageOutputs(payload, seen, &duplicates)
	if len(duplicates) == 0 {
		return payload, 0
	}

	out := payload
	// 从后向前删除，避免同一 parts 数组的下标在删除过程中偏移。
	for i := len(duplicates) - 1; i >= 0; i-- {
		next, err := sjson.DeleteBytes(out, duplicates[i].path)
		if err != nil {
			return payload, 0
		}
		out = next
	}
	return out, len(duplicates)
}

func scanGeminiInlineImageOutputs(
	payload []byte,
	seen map[geminiInlineImageDigest]struct{},
	duplicates *[]geminiInlineImagePartRef,
) {
	if len(payload) == 0 || seen == nil || !gjson.ValidBytes(payload) {
		return
	}

	var scan func(gjson.Result, string)
	scan = func(value gjson.Result, prefix string) {
		if value.IsArray() {
			index := 0
			value.ForEach(func(_, item gjson.Result) bool {
				scan(item, fmt.Sprintf("%s%d.", prefix, index))
				index++
				return true
			})
			return
		}
		if inner := value.Get("response"); inner.IsObject() {
			scan(inner, prefix+"response.")
			return
		}
		candidateIndex := 0
		value.Get("candidates").ForEach(func(_, candidate gjson.Result) bool {
			partIndex := 0
			candidate.Get("content.parts").ForEach(func(_, part gjson.Result) bool {
				data, ok := geminiPartRawInlineImageData(part)
				if !ok {
					data, ok = geminiPartImageFileURI(part)
					if ok {
						data = "file-uri:" + data
					}
				}
				if ok && !part.Get("thought").Bool() {
					digest := geminiInlineImageDataDigest(data)
					if _, exists := seen[digest]; exists {
						if duplicates != nil {
							*duplicates = append(*duplicates, geminiInlineImagePartRef{path: fmt.Sprintf("%scandidates.%d.content.parts.%d", prefix, candidateIndex, partIndex)})
						}
					} else {
						seen[digest] = struct{}{}
					}
				}
				partIndex++
				return true
			})
			candidateIndex++
			return true
		})
	}
	scan(gjson.ParseBytes(payload), "")
}

// fileData 是 Gemini 的图片引用格式；只统计声明为图片的非空 URI，不把普通文本链接计为图片。
func geminiPartImageFileURI(part gjson.Result) (string, bool) {
	file := part.Get("fileData")
	if !file.Exists() {
		file = part.Get("file_data")
	}
	mimeType := file.Get("mimeType")
	if !mimeType.Exists() {
		mimeType = file.Get("mime_type")
	}
	if !isGeminiImageOutputMIMEType(mimeType.String()) {
		return "", false
	}
	uri := file.Get("fileUri")
	if !uri.Exists() {
		uri = file.Get("file_uri")
	}
	if uri.Type != gjson.String {
		return "", false
	}
	value := strings.TrimSpace(uri.String())
	return value, value != ""
}

func geminiInlineImageDataDigest(data string) geminiInlineImageDigest {
	hasher := sha256.New()
	_, _ = io.WriteString(hasher, data)
	var digest geminiInlineImageDigest
	copy(digest[:], hasher.Sum(nil))
	return digest
}

func geminiPartRawInlineImageData(part gjson.Result) (string, bool) {
	inline := part.Get("inlineData")
	if !inline.Exists() {
		inline = part.Get("inline_data")
	}
	if !inline.Exists() {
		return "", false
	}

	mimeType := inline.Get("mimeType")
	if !mimeType.Exists() {
		mimeType = inline.Get("mime_type")
	}
	if !isGeminiImageOutputMIMEType(mimeType.String()) {
		return "", false
	}

	// 只认真的带上了 base64 数据的 part，空壳 part 不计费。
	raw := inline.Get("data")
	if raw.Type != gjson.String {
		return "", false
	}
	data := strings.TrimSpace(raw.String())
	return data, data != ""
}

// 原生响应计数接受图片媒体类型及其参数，不改变兼容接口的图片展示白名单。
func isGeminiImageOutputMIMEType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	return err == nil && strings.HasPrefix(mediaType, "image/") && len(mediaType) > len("image/")
}
