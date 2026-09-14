package service

import (
	"crypto/sha256"
	"fmt"
	"io"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// geminiImageOutputCounterKey 是请求级内联图片计数器挂在 gin.Context 上的键。
const geminiImageOutputCounterKey = "gemini_image_output_counter"

// geminiImageOutputCounter 按图片内容记录一次转发里 Gemini 上游真正回吐的图片。
// Gemini SSE 既可能累积重发，也可能把多张图分散到不同 chunk；跨 payload 保存
// 内容摘要可以同时避免重复计费和增量流少计费。
type geminiImageOutputCounter struct {
	digests        map[geminiInlineImageDigest]struct{}
	sawInlineImage bool
}

type geminiInlineImageDigest [sha256.Size]byte

type geminiInlineImagePartRef struct {
	candidate int
	part      int
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

// observeGeminiImageOutputs 观测一段上游响应（整份或单个 chunk）里的内联图片。
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
	if scanGeminiInlineImageOutputs(payload, counter.digests, nil) {
		counter.sawInlineImage = true
	}
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
// 优先用上游真正返回的内联图片数：走 GeminiMessagesCompatService 的账号多是
// API Key + 自定义模型映射，客户端请求名和上游模型名都可能是站长自取的别名
// （issue #5358 里的 nana-banana-2），isImageGenerationModel 的白名单必然判不出，
// 于是 ImageCount=0，calculateRecordUsageCost 整条按次计费分支不触发，
// 生图请求全部记 $0。
//
// 只有响应里数不出图时（例如上游用 fileData 引用而非 inlineData 回图，或聚合
// 函数丢掉了图片 part）才退回既有的模型名启发式，保证老行为不回退；这里额外
// 也认映射后的上游模型名，与 shouldSkipCodexPlanGatedImageModelCooldown 对
// requestedModel / modelKey 双取的口径一致。
func resolveGeminiImageCount(c *gin.Context, originalModel, mappedModel string) int {
	if counter := geminiImageOutputCounterFromContext(c); counter != nil && counter.sawInlineImage {
		return len(counter.digests)
	}
	if observed := observedGeminiImageOutputs(c); observed > 0 {
		return observed
	}
	if isImageGenerationModel(originalModel) || isImageGenerationModel(mappedModel) {
		return 1
	}
	return 0
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
		ref := duplicates[i]
		next, err := sjson.DeleteBytes(out, fmt.Sprintf("candidates.%d.content.parts.%d", ref.candidate, ref.part))
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
) bool {
	if len(payload) == 0 || seen == nil || !gjson.ValidBytes(payload) {
		return false
	}

	sawInlineImage := false
	candidateIndex := 0
	gjson.GetBytes(payload, "candidates").ForEach(func(_, candidate gjson.Result) bool {
		partIndex := 0
		candidate.Get("content.parts").ForEach(func(_, part gjson.Result) bool {
			data, ok := geminiPartRawInlineImageData(part)
			if ok {
				sawInlineImage = true
			}
			if ok && !part.Get("thought").Bool() {
				digest := geminiInlineImageDataDigest(data)
				if _, exists := seen[digest]; exists {
					if duplicates != nil {
						*duplicates = append(*duplicates, geminiInlineImagePartRef{candidate: candidateIndex, part: partIndex})
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
	return sawInlineImage
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
	if !isGeminiInlineImageMIMEType(strings.ToLower(strings.TrimSpace(mimeType.String()))) {
		return "", false
	}

	// 只认真的带上了 base64 数据的 part，空壳 part 不计费。
	data := strings.TrimSpace(inline.Get("data").String())
	return data, data != ""
}
