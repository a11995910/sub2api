# 视频渠道定价兼容实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复视频渠道定价的保存、按秒计费、旧数据兼容和用户价格预览，使修复后的 `dev` 可重新部署 staging 验证。

**Architecture:** 后端继续复用渠道定价表，但仅把显式 `billing_mode=video` 解释为每秒价格；历史 `image/per_request` 视频条目保持按次语义。前端新增纯函数 `resolveVideoPriceQuote()`，集中实现分组覆盖价、渠道层级价、渠道默认价和系统默认价的解析顺序，模型广场和测试台只负责格式化。

**Tech Stack:** Go 1.24、Gin、Testify、Vue 3、TypeScript、Vitest、Vue Test Utils、pnpm、Docker Compose。

## 全局约束

- 所有代码注释、文档和提交说明使用中文。
- 不修改数据库结构，不批量修改历史渠道定价。
- `billing_mode=video` 的 `per_request_price` 和层级价格表示每秒价格。
- 历史 `billing_mode=image/per_request` 视频配置继续按次计费，管理端不得自动改写模式。
- 用户价格解析顺序固定为：分组分辨率覆盖价、渠道分辨率层级价、渠道默认价、已知 Grok 视频系统默认价。
- 修复后只推送 `origin/dev` 并重新部署 staging；未经再次明确确认不得合并 `main` 或更新 prod。

---

## 文件结构

- `backend/internal/handler/admin/channel_handler.go`：管理接口请求枚举校验。
- `backend/internal/handler/admin/channel_handler_test.go`：真实 Gin JSON 绑定回归测试。
- `backend/internal/service/channel_service.go`：渠道价格完整性校验。
- `backend/internal/service/channel_service_test.go`：`video` 必填价格测试。
- `backend/internal/service/openai_gateway_service.go`：视频渠道计费数量和单位转换。
- `backend/internal/service/openai_gateway_record_usage_test.go`：显式视频按秒、历史模式按次的用量测试。
- `frontend/src/utils/modelKind.ts`：历史视频模型的展示分类。
- `frontend/src/utils/__tests__/modelKind.spec.ts`：分类优先级测试。
- `frontend/src/views/admin/channelPricingCompatibility.ts`：管理端渠道计费模式兼容纯函数。
- `frontend/src/views/admin/ChannelsView.vue`：保留后端原始计费模式。
- `frontend/src/views/admin/__tests__/ChannelsView.videoPricing.spec.ts`：管理端历史条目回显测试。
- `frontend/src/utils/videoPricing.ts`：共享视频价格解析纯函数。
- `frontend/src/utils/__tests__/videoPricing.spec.ts`：价格来源、单位和倍率测试。
- `frontend/src/views/user/ModelMarketView.vue`：完整分组字段映射并使用共享报价。
- `frontend/src/views/user/ModelTestView.vue`：使用共享报价计算时长预览。
- `frontend/src/views/user/__tests__/ModelMarketView.spec.ts`：模型广场价格字段与回退测试。
- `frontend/src/views/user/__tests__/ModelTestView.spec.ts`：视频模式和价格预览回归测试。
- `docs/AVAILABLE_CHANNELS_CN.md`、`docs/RECHARGE_MODEL_MARKET_CN.md`：同步计费模式与兼容规则。

### Task 1: 管理端 video 保存契约

**Files:**
- Modify: `backend/internal/handler/admin/channel_handler.go:59-70`
- Modify: `backend/internal/handler/admin/channel_handler_test.go`
- Modify: `backend/internal/service/channel_service.go:611-636`
- Modify: `backend/internal/service/channel_service_test.go:2281-2354`

**Interfaces:**
- Consumes: `service.BillingModeVideo`、现有 `channelModelPricingRequest` 和 `validatePricingBillingMode()`。
- Produces: 管理接口可绑定 `billing_mode=video`；服务层拒绝无默认价且无层级的 video 条目。

- [ ] **Step 1: 写管理端 JSON 绑定失败测试**

在 `channel_handler_test.go` 增加真实 Gin 绑定辅助函数和测试：

```go
func bindCreateChannelRequestForTest(t *testing.T, body string) error {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/channels", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	var req createChannelRequest
	return c.ShouldBindJSON(&req)
}

func TestCreateChannelRequestAcceptsVideoBillingMode(t *testing.T) {
	err := bindCreateChannelRequestForTest(t, `{
		"name":"视频渠道",
		"model_pricing":[{
			"platform":"grok",
			"models":["grok-imagine-video"],
			"billing_mode":"video",
			"per_request_price":0.07
		}]
	}`)
	require.NoError(t, err)
}

func TestCreateChannelRequestRejectsUnknownBillingMode(t *testing.T) {
	err := bindCreateChannelRequestForTest(t, `{
		"name":"错误渠道",
		"model_pricing":[{"models":["m"],"billing_mode":"unknown"}]
	}`)
	require.Error(t, err)
}
```

- [ ] **Step 2: 运行测试确认 video 绑定失败**

Run: `cd backend && go test ./internal/handler/admin -run 'TestCreateChannelRequest(AcceptsVideo|RejectsUnknown)BillingMode' -count=1`

Expected: `AcceptsVideoBillingMode` 因 `oneof` 缺少 `video` 失败，未知枚举测试通过。

- [ ] **Step 3: 最小修改请求枚举**

把 DTO 标签修改为：

```go
BillingMode string `json:"billing_mode" binding:"omitempty,oneof=token per_request image video"`
```

- [ ] **Step 4: 写服务层 video 必填价格失败测试**

在 `TestValidatePricingBillingMode` 表中加入：

```go
{
	name:    "video no price no intervals - invalid",
	pricing: []ChannelModelPricing{{BillingMode: BillingModeVideo}},
	wantErr: true,
	errMsg:  "per-second price or intervals required",
},
{
	name: "video with default price - valid",
	pricing: []ChannelModelPricing{{
		BillingMode: BillingModeVideo,
		PerRequestPrice: testPtrFloat64(0.07),
	}},
},
{
	name: "video with resolution interval - valid",
	pricing: []ChannelModelPricing{{
		BillingMode: BillingModeVideo,
		Intervals: []PricingInterval{{TierLabel: "720p", PerRequestPrice: testPtrFloat64(0.14)}},
	}},
},
```

- [ ] **Step 5: 运行测试确认缺价 video 被错误接受**

Run: `cd backend && go test ./internal/service -run TestValidatePricingBillingMode -count=1`

Expected: `video no price no intervals - invalid` 失败，因为当前服务层返回 nil。

- [ ] **Step 6: 最小修改服务校验并运行定向测试**

把 `checkBillingModeRequirements()` 分开处理单位文案：

```go
if p.BillingMode == BillingModeVideo && p.PerRequestPrice == nil && len(p.Intervals) == 0 {
	return infraerrors.BadRequest(
		"BILLING_MODE_MISSING_PRICE",
		"per-second price or intervals required for video billing mode",
	)
}
if (p.BillingMode == BillingModePerRequest || p.BillingMode == BillingModeImage) &&
	p.PerRequestPrice == nil && len(p.Intervals) == 0 {
	return infraerrors.BadRequest(
		"BILLING_MODE_MISSING_PRICE",
		"per-request price or intervals required for per_request/image billing mode",
	)
}
```

Run: `cd backend && go test ./internal/handler/admin ./internal/service -run 'TestCreateChannelRequest|TestValidatePricingBillingMode' -count=1`

Expected: PASS。

- [ ] **Step 7: 提交 Task 1**

```bash
git add backend/internal/handler/admin/channel_handler.go \
  backend/internal/handler/admin/channel_handler_test.go \
  backend/internal/service/channel_service.go \
  backend/internal/service/channel_service_test.go
git commit -m "fix: 修复视频渠道定价保存校验"
```

### Task 2: 显式 video 按秒计费

**Files:**
- Modify: `backend/internal/service/openai_gateway_service.go:7259-7304`
- Modify: `backend/internal/service/openai_gateway_record_usage_test.go:2086-2126`

**Interfaces:**
- Consumes: `NormalizeVideoBillingDurationSecondsOrDefault()`、`ResolvedPricing.Mode` 和 `CalculateCostUnified()`。
- Produces: 只有 `BillingModeVideo` 使用 `videoCount * durationSeconds` 作为计费数量；`image/per_request` 仍使用 `videoCount`。

- [ ] **Step 1: 把现有 video 渠道测试改成每秒期望**

将 `TestOpenAIGatewayServiceRecordUsage_ChannelVideoBillingUsesVideoModePrice` 的 2 个视频、5 秒、单价 0.071 的期望改为：

```go
require.InDelta(t, 0.071*2*5, usageRepo.lastLog.TotalCost, 1e-12)
require.InDelta(t, 0.071*2*5, usageRepo.lastLog.ActualCost, 1e-12)
```

再增加历史模式回归：

```go
func TestOpenAIGatewayServiceRecordUsage_LegacyImageVideoPriceStaysPerRequest(t *testing.T) {
	// resolver 使用 BillingModeImage，2 个视频、5 秒、2.1/次。
	// 断言 TotalCost 和 ActualCost 均为 4.2，而不是 21。
}
```

- [ ] **Step 2: 运行测试确认显式 video 未乘时长**

Run: `cd backend && go test ./internal/service -run 'ChannelVideoBillingUsesVideoModePrice|LegacyImageVideoPriceStaysPerRequest' -count=1`

Expected: 显式 video 用例期望 0.71、实际 0.142；历史 image 用例通过。

- [ ] **Step 3: 最小修改计费数量**

在渠道定价分支计算请求数：

```go
requestCount := videoCount
if resolved.Mode == BillingModeVideo {
	requestCount *= durationSeconds
}
cost, err := s.billingService.CalculateCostUnified(CostInput{
	Ctx:            ctx,
	Model:          billingModel,
	GroupID:        &gid,
	RequestCount:   requestCount,
	SizeTier:       resolution,
	RateMultiplier: multiplier,
	Resolver:       s.resolver,
	Resolved:       resolved,
})
```

同步注释：`video` 为每秒价，`image/per_request` 为历史按次价。

- [ ] **Step 4: 运行计费定向和服务层全包测试**

Run: `cd backend && go test ./internal/service -run 'Video|Grok' -count=1`

Expected: PASS。

Run: `cd backend && go test ./internal/service -count=1`

Expected: PASS。

- [ ] **Step 5: 提交 Task 2**

```bash
git add backend/internal/service/openai_gateway_service.go \
  backend/internal/service/openai_gateway_record_usage_test.go
git commit -m "fix: 按视频时长计算渠道每秒价格"
```

### Task 3: 历史视频分类与管理端回显

**Files:**
- Modify: `frontend/src/utils/modelKind.ts:14-30`
- Modify: `frontend/src/utils/__tests__/modelKind.spec.ts`
- Create: `frontend/src/views/admin/channelPricingCompatibility.ts`
- Modify: `frontend/src/views/admin/ChannelsView.vue:1392-1404`
- Create: `frontend/src/views/admin/__tests__/ChannelsView.videoPricing.spec.ts`

**Interfaces:**
- Consumes: `resolveModelKind()`、`apiToForm()` 和后端返回的 `billing_mode`。
- Produces: 名称明确为 Grok 视频的历史 `kind=image` 模型进入视频展示模式；管理端保留原始 `image/per_request`。

- [ ] **Step 1: 写历史分类失败测试**

在 `modelKind.spec.ts` 增加：

```ts
it('模型名为 Grok 视频时覆盖历史 kind=image', () => {
  expect(resolveModelKind({
    name: 'grok-imagine-video-1.5',
    kind: 'image',
    pricing: { billing_mode: BILLING_MODE_IMAGE } as UserSupportedModelPricing,
  })).toBe('video')
})
```

- [ ] **Step 2: 运行测试确认被显式 image 截断**

Run: `cd frontend && pnpm exec vitest run src/utils/__tests__/modelKind.spec.ts`

Expected: FAIL，实际返回 `image`。

- [ ] **Step 3: 最小调整分类优先级**

导出并先检查视频模型名：

```ts
export function isVideoModelName(name: string): boolean {
  return name.trim().toLowerCase().startsWith('grok-imagine-video')
}

export function resolveModelKind(model: Pick<UserSupportedModel, 'kind' | 'name' | 'pricing'>): ModelKind {
  if (isVideoModelName(model.name)) return 'video'
  if (model.kind === 'image' || model.kind === 'video') return model.kind
  return modelKindFromPricing(model.pricing, model.name)
}
```

- [ ] **Step 4: 写管理端历史模式回显失败测试**

在 `channelPricingCompatibility.ts` 将 `apiToForm` 所需的模式归一逻辑抽成可测试纯函数：

```ts
export function preserveChannelBillingMode(entry: Pick<ChannelModelPricing, 'billing_mode'>): BillingMode {
  return entry.billing_mode || BILLING_MODE_TOKEN
}
```

测试先断言 `image` 视频条目返回 `image`、`per_request` 返回 `per_request`，并确认现有代码的名称自动转换导致失败。

- [ ] **Step 5: 删除隐式 video 转换并运行测试**

删除 `ChannelsView.vue` 中“所有模型名含 video 就改为 video”的循环；`apiToForm()` 和账号统计规则映射直接保留后端模式。

Run: `cd frontend && pnpm exec vitest run src/utils/__tests__/modelKind.spec.ts src/views/admin/__tests__/ChannelsView.videoPricing.spec.ts src/views/user/__tests__/ModelTestView.spec.ts`

Expected: PASS，且历史视频只进入视频测试模式。

- [ ] **Step 6: 提交 Task 3**

```bash
git add frontend/src/utils/modelKind.ts \
  frontend/src/utils/__tests__/modelKind.spec.ts \
  frontend/src/views/admin/channelPricingCompatibility.ts \
  frontend/src/views/admin/ChannelsView.vue \
  frontend/src/views/admin/__tests__/ChannelsView.videoPricing.spec.ts
git commit -m "fix: 保留历史视频渠道按次模式"
```

### Task 4: 共享视频价格解析与用户页面一致性

**Files:**
- Create: `frontend/src/utils/videoPricing.ts`
- Create: `frontend/src/utils/__tests__/videoPricing.spec.ts`
- Modify: `frontend/src/views/user/ModelMarketView.vue:292-311,522-530`
- Modify: `frontend/src/views/user/ModelTestView.vue:519-538,568-650`
- Modify: `frontend/src/views/user/__tests__/ModelMarketView.spec.ts`
- Modify: `frontend/src/views/user/__tests__/ModelTestView.spec.ts`

**Interfaces:**
- Consumes: `UserAvailableGroup`、`UserSupportedModelPricing`、`BillingMode`。
- Produces: `resolveVideoPriceQuote(input: VideoPriceInput): VideoPriceQuote | null`。

```ts
export type VideoResolution = '480p' | '720p' | '1080p'
export type VideoBillingUnit = 'second' | 'request'
export type VideoPriceSource = 'group' | 'channel_interval' | 'channel_default' | 'system_default'

export interface VideoPriceInput {
  group: UserAvailableGroup
  pricing: UserSupportedModelPricing | null
  modelName: string
  resolution: VideoResolution
  userGroupRate?: number
}

export interface VideoPriceQuote {
  basePrice: number
  effectivePrice: number
  billingUnit: VideoBillingUnit
  source: VideoPriceSource
}
```

- [ ] **Step 1: 写共享解析器失败测试**

覆盖以下表格用例：

```ts
it.each([
  ['group override', groupWith720p(0.03), videoPricing(0.07, 0.14), 0.03, 'second', 'group'],
  ['channel interval', baseGroup(), videoPricing(0.07, 0.14), 0.14, 'second', 'channel_interval'],
  ['channel default', baseGroup(), videoPricing(0.07), 0.07, 'second', 'channel_default'],
  ['legacy image default', baseGroup(), legacyImagePricing(2.1), 2.1, 'request', 'channel_default'],
  ['system default', baseGroup(), null, 0.14, 'second', 'system_default'],
])('%s', (_name, group, pricing, price, unit, source) => {
  expect(resolveVideoPriceQuote({
    group,
    pricing,
    modelName: 'grok-imagine-video-1.5',
    resolution: '720p',
  })).toMatchObject({ basePrice: price, billingUnit: unit, source })
})
```

另测：独立视频倍率优先、用户专属分组倍率次之、缺失值不按 0 处理、token 定价不伪装成视频单价、普通 Grok 视频 1080p 按后端规则回退 720p 默认价。

- [ ] **Step 2: 运行测试确认解析器不存在**

Run: `cd frontend && pnpm exec vitest run src/utils/__tests__/videoPricing.spec.ts`

Expected: FAIL，模块或函数不存在。

- [ ] **Step 3: 实现最小共享解析器**

实现顺序：

```ts
const groupPrice = groupVideoPrice(input.group, input.resolution)
if (groupPrice != null) return quote(groupPrice, effectiveVideoRate(input), 'second', 'group')

if (input.pricing?.billing_mode === BILLING_MODE_TOKEN) return null

const intervalPrice = matchingResolutionInterval(input.pricing, input.resolution)?.per_request_price
const unit = input.pricing?.billing_mode === BILLING_MODE_VIDEO ? 'second' : 'request'
if (intervalPrice != null) return quote(intervalPrice, effectiveVideoRate(input), unit, 'channel_interval')
if (input.pricing?.per_request_price != null) {
  return quote(input.pricing.per_request_price, effectiveVideoRate(input), unit, 'channel_default')
}

const systemPrice = grokDefaultVideoPrice(input.modelName, input.resolution)
return systemPrice == null ? null : quote(systemPrice, effectiveVideoRate(input), 'second', 'system_default')
```

- [ ] **Step 4: 修复模型广场分组字段并写页面失败测试**

`toAvailableGroup()` 必须复制：

```ts
video_rate_independent: group.video_rate_independent,
video_rate_multiplier: group.video_rate_multiplier,
video_price_480p: group.video_price_480p,
video_price_720p: group.video_price_720p,
video_price_1080p: group.video_price_1080p,
```

页面测试提供仅渠道 720p 层级价、仅分组 720p 覆盖价和历史 image 2.1/次三组数据，断言分别显示渠道每秒价、分组每秒价和按次价，不显示 `-`。

- [ ] **Step 5: 两个页面改用共享报价**

模型广场把 `formatSelectedVideoTier()` 改成接收当前模型：

```ts
function formatSelectedVideoTier(model: GroupMarketModel, resolution: VideoResolution): string {
  const group = selectedGroup.value?.group
  if (!group) return '-'
  const quote = resolveVideoPriceQuote({
    group,
    pricing: model.pricing,
    modelName: model.name,
    resolution,
    userGroupRate: userGroupRates.value[group.id],
  })
  return quote ? formatScaled(quote.effectivePrice, 1) : '-'
}
```

测试台按单位生成预览：

```ts
const quote = resolveVideoPriceQuote({ group, pricing: model.pricing, modelName: model.name, resolution, userGroupRate })
if (!quote) return '-'
const total = quote.billingUnit === 'second'
  ? quote.effectivePrice * normalizedVideoDuration()
  : quote.effectivePrice
return formatVideoQuote(total, quote.billingUnit, resolution, normalizedVideoDuration())
```

- [ ] **Step 6: 运行共享工具和两个页面测试**

Run: `cd frontend && pnpm exec vitest run src/utils/__tests__/videoPricing.spec.ts src/views/user/__tests__/ModelMarketView.spec.ts src/views/user/__tests__/ModelTestView.spec.ts`

Expected: PASS。

- [ ] **Step 7: 提交 Task 4**

```bash
git add frontend/src/utils/videoPricing.ts \
  frontend/src/utils/__tests__/videoPricing.spec.ts \
  frontend/src/views/user/ModelMarketView.vue \
  frontend/src/views/user/ModelTestView.vue \
  frontend/src/views/user/__tests__/ModelMarketView.spec.ts \
  frontend/src/views/user/__tests__/ModelTestView.spec.ts
git commit -m "fix: 统一视频价格预览与渠道计费来源"
```

### Task 5: 文档、全量验证和 staging

**Files:**
- Modify: `docs/AVAILABLE_CHANNELS_CN.md`
- Modify: `docs/RECHARGE_MODEL_MARKET_CN.md`
- Modify: `docs/SOURCE_DEPLOY_CN.md`（只修正文档中 compose 必须叠加基础文件的命令）

**Interfaces:**
- Consumes: Tasks 1-4 的最终行为。
- Produces: 当前实际计费语义、兼容边界和可复现部署命令。

- [ ] **Step 1: 更新业务与部署文档**

文档必须明确：

```text
billing_mode=video 的渠道价格按秒；image/per_request 历史视频价格按次且不自动迁移。
用户预览价格优先读取分组覆盖价，再读取渠道分辨率价、渠道默认价和系统默认价。
新 VPS compose 命令必须同时使用 deploy/docker-compose.yml 和环境 override 文件。
```

- [ ] **Step 2: 运行后端全量测试**

Run: `cd backend && go test ./... -count=1`

Expected: 所有包 PASS。

Run: `cd backend && golangci-lint run ./...`

Expected: 本机存在工具时 0 issues；工具缺失必须如实记录，并以 CI 结果补充。

- [ ] **Step 3: 运行前端全量验证**

Run: `cd frontend && pnpm run lint:check`

Run: `cd frontend && pnpm run typecheck`

Run: `cd frontend && pnpm exec vitest run`

Run: `cd frontend && pnpm run build`

Expected: 全部退出码 0；仅允许记录现有非阻断构建警告。

- [ ] **Step 4: 独立代码审查并修复阻断项**

审查范围从 `8fe8125e6` 到最终 HEAD，重点检查计费单位、历史兼容、API 绑定、页面价格来源和测试缺口。任何 P0/P1 必须修复后重新运行对应测试。

- [ ] **Step 5: 提交文档并推送 dev**

```bash
git add docs/AVAILABLE_CHANNELS_CN.md docs/RECHARGE_MODEL_MARKET_CN.md docs/SOURCE_DEPLOY_CN.md
git commit -m "docs: 同步视频渠道定价与部署流程"
git push origin dev
```

- [ ] **Step 6: 新 VPS 构建可追溯 staging 镜像**

```bash
cd /opt/sub2api/repo
git status --short
git fetch origin
expected_commit="$(git rev-parse origin/dev)"
git switch --detach "$expected_commit"
test "$(git rev-parse HEAD)" = "$expected_commit"
commit="$(git rev-parse --short=12 HEAD)"
date="$(git show -s --format=%cI HEAD)"
docker buildx build -f deploy/Dockerfile \
  --build-arg COMMIT="$commit" \
  --build-arg DATE="$date" \
  -t "sub2api:staging-$commit" --load .
docker run --rm "sub2api:staging-$commit" --version
```

- [ ] **Step 7: 原子更新 staging 镜像变量并部署**

先备份 `/opt/sub2api/env/staging/.env`，只替换 `SUB2API_IMAGE`：

```bash
cp -a /opt/sub2api/env/staging/.env "/opt/sub2api/backups/staging.env.$(date +%Y%m%d-%H%M%S)"
sed "s|^SUB2API_IMAGE=.*|SUB2API_IMAGE=sub2api:staging-$commit|" \
  /opt/sub2api/env/staging/.env > /opt/sub2api/env/staging/.env.new
test "$(grep -c '^SUB2API_IMAGE=' /opt/sub2api/env/staging/.env.new)" -eq 1
mv /opt/sub2api/env/staging/.env.new /opt/sub2api/env/staging/.env
cd /opt/sub2api/repo/deploy
docker compose -p sub2api-staging \
  --env-file /opt/sub2api/env/staging/.env \
  -f docker-compose.yml \
  -f /opt/sub2api/compose/staging/docker-compose.yml \
  up -d
```

- [ ] **Step 8: staging 验收**

```bash
docker compose -p sub2api-staging \
  --env-file /opt/sub2api/env/staging/.env \
  -f docker-compose.yml \
  -f /opt/sub2api/compose/staging/docker-compose.yml ps
curl -fsS http://127.0.0.1:18080/health
curl -fsS -o /dev/null http://127.0.0.1:18080/models
curl -fsS -o /dev/null http://127.0.0.1:18080/model-test
docker logs --since 10m sub2api-staging 2>&1 | grep -Ei 'panic|fatal|migration.*fail' && exit 1 || true
```

再使用隔离测试数据验证：显式 video 价格乘时长、历史 image 价格不乘时长、管理接口能保存 video、页面显示同一价格；测试数据必须记录 ID 并在验证后删除或回滚。

- [ ] **Step 9: 停止在 staging，报告结果并等待生产确认**

报告最终 commit、镜像 tag、测试结果、staging 计费证据、影响范围和残余风险。不得合并 `main` 或更新 prod。
