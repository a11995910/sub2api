//go:build unit

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUserAvailableChannel_Unauthenticated401(t *testing.T) {
	// 没有 AuthSubject 注入时，handler 应返回 401 且不触达 service 依赖。
	gin.SetMode(gin.TestMode)
	h := &AvailableChannelHandler{} // nil services — 401 路径不会调用它们
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channels/available", nil)

	h.List(c)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestFilterUserVisibleGroups_IntersectionOnly(t *testing.T) {
	// 渠道挂在 {g1, g2, g3}，用户只允许 {g1, g3} —— 响应必须仅含 g1/g3。
	groups := []service.AvailableGroupRef{
		{ID: 1, Name: "g1", Platform: "anthropic"},
		{ID: 2, Name: "g2", Platform: "anthropic"},
		{ID: 3, Name: "g3", Platform: "openai"},
	}
	allowed := map[int64]struct{}{1: {}, 3: {}}

	visible := filterUserVisibleGroups(groups, allowed)
	require.Len(t, visible, 2)
	ids := []int64{visible[0].ID, visible[1].ID}
	require.ElementsMatch(t, []int64{1, 3}, ids)
}

func TestToUserSupportedModels_FiltersByAllowedPlatforms(t *testing.T) {
	// 用户可访问分组只覆盖 anthropic；anthropic 平台的模型保留，openai 模型被剔除。
	src := []service.SupportedModel{
		{Name: "claude-sonnet-4-6", Platform: "anthropic", Pricing: nil},
		{Name: "gpt-4o", Platform: "openai", Pricing: nil},
	}
	allowed := map[string]struct{}{"anthropic": {}}
	out := toUserSupportedModels(src, allowed)
	require.Len(t, out, 1)
	require.Equal(t, "claude-sonnet-4-6", out[0].Name)
}

func TestToUserSupportedModels_NilAllowedPlatformsKeepsAll(t *testing.T) {
	// 显式传 nil allowedPlatforms 表示不做过滤。
	src := []service.SupportedModel{
		{Name: "a", Platform: "anthropic"},
		{Name: "b", Platform: "openai"},
	}
	require.Len(t, toUserSupportedModels(src, nil), 2)
}

func TestToUserSupportedModels_MarksImagePricingKind(t *testing.T) {
	// 图片计费模型必须带 kind=image，供前端把图片分组与普通 token 模型分开渲染。
	src := []service.SupportedModel{
		{
			Name:     "gpt-image-2",
			Platform: service.PlatformOpenAI,
			Pricing:  &service.ChannelModelPricing{BillingMode: service.BillingModeImage},
		},
		{
			Name:     "gpt-5.4",
			Platform: service.PlatformOpenAI,
			Pricing:  &service.ChannelModelPricing{BillingMode: service.BillingModeToken},
		},
	}

	out := toUserSupportedModels(src, nil)

	require.Len(t, out, 2)
	require.Equal(t, modelKindImage, out[0].Kind)
	require.Equal(t, modelKindToken, out[1].Kind)
}

func TestToUserSupportedModels_MarksVideoPricingKind(t *testing.T) {
	models := toUserSupportedModels([]service.SupportedModel{{
		Name:     "grok-imagine-video-1.5",
		Platform: service.PlatformGrok,
		Pricing:  &service.ChannelModelPricing{BillingMode: service.BillingModeVideo},
	}}, nil)

	require.Len(t, models, 1)
	require.Equal(t, modelKindVideo, models[0].Kind)
}

func TestUserAvailableChannel_FieldWhitelist(t *testing.T) {
	// 通过序列化 userAvailableChannel 结构体验证响应形状：
	// 只有 name / description / platforms；不含管理端字段。
	row := userAvailableChannel{
		Name:        "ch",
		Description: "d",
		Platforms: []userChannelPlatformSection{
			{
				Platform:        "anthropic",
				Groups:          []userAvailableGroup{{ID: 1, Name: "g1", Platform: "anthropic"}},
				SupportedModels: []userSupportedModel{},
			},
		},
	}
	raw, err := json.Marshal(row)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	for _, key := range []string{"id", "status", "billing_model_source", "restrict_models"} {
		_, exists := decoded[key]
		require.Falsef(t, exists, "user DTO must not expose %q", key)
	}
	for _, key := range []string{"name", "description", "platforms"} {
		_, exists := decoded[key]
		require.Truef(t, exists, "user DTO must expose %q", key)
	}

	// 验证 section 的字段（platform / groups / supported_models）。
	rawSection, err := json.Marshal(row.Platforms[0])
	require.NoError(t, err)
	var sectionDecoded map[string]any
	require.NoError(t, json.Unmarshal(rawSection, &sectionDecoded))
	for _, key := range []string{"platform", "groups", "supported_models"} {
		_, exists := sectionDecoded[key]
		require.Truef(t, exists, "platform section must expose %q", key)
	}

	// Group DTO 暴露区分专属/公开、订阅类型、默认倍率、高峰倍率规则、图片能力所需的字段，
	// 前端据此渲染 GroupBadge、图片标签，并与 API 密钥页保持一致的视觉。
	rawGroup, err := json.Marshal(row.Platforms[0].Groups[0])
	require.NoError(t, err)
	var groupDecoded map[string]any
	require.NoError(t, json.Unmarshal(rawGroup, &groupDecoded))
	for _, key := range []string{
		"id",
		"name",
		"platform",
		"subscription_type",
		"rate_multiplier",
		"peak_rate_enabled",
		"peak_start",
		"peak_end",
		"peak_rate_multiplier",
		"is_exclusive",
		"allow_image_generation",
		"image_super_resolution_enabled",
		"image_rate_independent",
		"cache_hit_quarter_to_input_enabled",
		"image_rate_multiplier",
		"image_price_1k",
		"image_price_2k",
		"image_price_4k",
	} {
		_, exists := groupDecoded[key]
		require.Truef(t, exists, "group DTO must expose %q", key)
	}

	// pricing interval 白名单：不应暴露 id / sort_order。
	pricing := toUserPricing(&service.ChannelModelPricing{
		BillingMode: service.BillingModeToken,
		Intervals: []service.PricingInterval{
			{ID: 7, MinTokens: 0, MaxTokens: nil, SortOrder: 3},
		},
	})
	require.NotNil(t, pricing)
	require.Len(t, pricing.Intervals, 1)
	rawIv, err := json.Marshal(pricing.Intervals[0])
	require.NoError(t, err)
	var ivDecoded map[string]any
	require.NoError(t, json.Unmarshal(rawIv, &ivDecoded))
	for _, key := range []string{"id", "pricing_id", "sort_order"} {
		_, exists := ivDecoded[key]
		require.Falsef(t, exists, "user pricing interval must not expose %q", key)
	}
}

func TestFilterUserVisibleGroups_ExposesImageControls(t *testing.T) {
	// 可用渠道页需要在创建 API Key 前提示用户哪些分组支持图片生成；
	// 因此用户可见分组 DTO 必须携带图片开关及图片独立倍率。
	groups := []service.AvailableGroupRef{
		{
			ID:                          9,
			Name:                        "image",
			Platform:                    "openai",
			RateMultiplier:              0.3,
			AllowImageGeneration:        true,
			ImageSuperResolutionEnabled: true,
			ImageRateIndependent:        true,
			ImageRateMultiplier:         1.2,
		},
	}

	visible := filterUserVisibleGroups(groups, map[int64]struct{}{9: {}})

	require.Len(t, visible, 1)
	require.True(t, visible[0].AllowImageGeneration)
	require.True(t, visible[0].ImageSuperResolutionEnabled)
	require.True(t, visible[0].ImageRateIndependent)
	require.Equal(t, 1.2, visible[0].ImageRateMultiplier)
}

func TestFilterGroupsForModelKind_SplitsImageGroups(t *testing.T) {
	// 模型广场需要用模型类型切分分组：图片分组只给图片模型，普通分组只给 token/按次模型。
	groups := []userAvailableGroup{
		{ID: 1, Name: "text", AllowImageGeneration: false},
		{ID: 2, Name: "image", AllowImageGeneration: true},
	}

	tokenGroups := filterGroupsForModelKind(groups, modelKindToken)
	imageGroups := filterGroupsForModelKind(groups, modelKindImage)

	require.Len(t, tokenGroups, 1)
	require.Equal(t, int64(1), tokenGroups[0].ID)
	require.Len(t, imageGroups, 1)
	require.Equal(t, int64(2), imageGroups[0].ID)
}

func TestBuildPlatformSections_GroupsByPlatform(t *testing.T) {
	// 一个渠道横跨 anthropic / openai / 空平台：应该生成 2 个 section，
	// 按 platform 字母序排序，各自 groups 和 supported_models 只含同平台条目。
	ch := service.AvailableChannel{
		Name: "ch",
		SupportedModels: []service.SupportedModel{
			{Name: "claude-sonnet-4-6", Platform: "anthropic"},
			{Name: "gpt-4o", Platform: "openai"},
		},
	}
	visible := []userAvailableGroup{
		{ID: 1, Name: "g-openai", Platform: "openai"},
		{ID: 2, Name: "g-ant", Platform: "anthropic"},
		{ID: 3, Name: "g-empty", Platform: ""},
	}
	sections := buildPlatformSections(ch, visible)
	require.Len(t, sections, 2)
	require.Equal(t, "anthropic", sections[0].Platform)
	require.Equal(t, "openai", sections[1].Platform)
	require.Len(t, sections[0].Groups, 1)
	require.Equal(t, int64(2), sections[0].Groups[0].ID)
	require.Len(t, sections[0].SupportedModels, 1)
	require.Equal(t, "claude-sonnet-4-6", sections[0].SupportedModels[0].Name)
}

func TestBuildPlatformSections_AppendsOpenAIImageModelForImageGroup(t *testing.T) {
	// 线上图片分组只有 allow_image_generation 标记，没有单独渠道定价行；
	// 因此用户侧需要补一个独立 image-2 模型，避免把图片能力挂到所有 GPT 模型下面。
	ch := service.AvailableChannel{
		Name: "openai",
		SupportedModels: []service.SupportedModel{
			{
				Name:     "gpt-5.4",
				Platform: service.PlatformOpenAI,
				Pricing:  &service.ChannelModelPricing{BillingMode: service.BillingModeToken},
			},
		},
	}
	visible := []userAvailableGroup{
		{ID: 1, Name: "GPT", Platform: service.PlatformOpenAI},
		{ID: 2, Name: "ChatGPT2API 图片", Platform: service.PlatformOpenAI, AllowImageGeneration: true},
	}

	sections := buildPlatformSections(ch, visible)

	require.Len(t, sections, 1)
	require.Len(t, sections[0].SupportedModels, 2)
	require.Equal(t, "gpt-5.4", sections[0].SupportedModels[0].Name)
	require.Equal(t, modelKindToken, sections[0].SupportedModels[0].Kind)
	require.Equal(t, "image-2", sections[0].SupportedModels[1].Name)
	require.Equal(t, modelKindImage, sections[0].SupportedModels[1].Kind)
	require.Equal(t, string(service.BillingModeImage), sections[0].SupportedModels[1].Pricing.BillingMode)
}
