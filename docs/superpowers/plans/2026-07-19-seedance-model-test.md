# Seedance 模型测试台支持实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让无渠道的 Seedance API Key 分组在模型测试台读取模型、按视频模式调用 Helix Chat Completions，并按 BytePlus 官方默认价格计费。

**Architecture:** 前端按当前 API Key 调用 `/v1/models` 合并无渠道模型，并将 Seedance 识别为视频模型。后端仅对显式 Seedance 模型开放 OpenAI 分组的视频端点，把统一视频请求转换为 Helix Chat Completions，同步解析最终视频或异步任务信息，并复用现有视频用量记录和按秒计费。

**Tech Stack:** Go 1.26、Gin、req/v3、Vue 3、TypeScript、Vitest、Docker Compose。

## Global Constraints

- 所有代码注释、文档和提交说明使用中文。
- 不修改线上账号凭据、分组绑定或渠道配置。
- 分组价格和渠道价格优先，Seedance 官方价格只作为系统默认回退。
- 普通 OpenAI 模型继续拒绝视频端点，Grok 视频链路保持不变。
- Fast、Mini、Mini HC 不提供 1080p；完整版提供 480p、720p、1080p。
- 不新增数据库迁移，不新增 4K 测试台选项。

---

### Task 1: Seedance 模型分类、分辨率和默认价格

**Files:**
- Modify: `frontend/src/utils/modelKind.ts`
- Modify: `frontend/src/utils/videoPricing.ts`
- Test: `frontend/src/utils/__tests__/modelKind.spec.ts`
- Test: `frontend/src/utils/__tests__/videoPricing.spec.ts`
- Modify: `backend/internal/service/billing_service.go`
- Test: `backend/internal/service/billing_service_test.go`

**Interfaces:**
- Produces: `isSeedanceVideoModel(name: string): boolean`
- Produces: `seedanceDefaultVideoPrice(model, resolution) (float64, bool)`
- Consumes: `resolveVideoPriceQuote`、`BillingService.CalculateVideoCost`

- [ ] **Step 1: 写前端失败测试**

```ts
expect(resolveModelKind({ name: 'dreamina-seedance-2-0-mini-ep', pricing: null })).toBe('video')
expect(videoResolutionsForModel('dreamina-seedance-2-0-ep')).toEqual(['480p', '720p', '1080p'])
expect(videoResolutionsForModel('dreamina-seedance-2-0-fast-ep')).toEqual(['480p', '720p'])
expect(resolveVideoPriceQuote(seedanceMini480pInput)?.basePrice).toBe(0.04)
expect(resolveVideoPriceQuote(seedanceFull1080pInput)?.basePrice).toBe(0.37)
```

- [ ] **Step 2: 运行前端测试确认失败**

Run: `cd frontend && pnpm vitest run src/utils/__tests__/modelKind.spec.ts src/utils/__tests__/videoPricing.spec.ts`

Expected: Seedance 被识别为 `token`，且默认价格为 `null`。

- [ ] **Step 3: 写后端失败测试**

```go
func TestCalculateVideoCost_SeedanceOfficialDefaults(t *testing.T) {
    svc := NewBillingService(&config.Config{}, nil)
    got := svc.CalculateVideoCost("dreamina-seedance-2-0-mini-ep", "480p", 1, 4, nil, 1)
    require.InDelta(t, 0.16, got.ActualCost, 1e-9)
}
```

- [ ] **Step 4: 运行后端测试确认失败**

Run: `cd backend && go test -tags=unit ./internal/service -run 'TestCalculateVideoCost_SeedanceOfficialDefaults'`

Expected: 实际费用使用旧图片兜底价，不等于 `0.16`。

- [ ] **Step 5: 实现最小价格和分类逻辑**

```ts
export function isSeedanceVideoModel(name: string): boolean {
  return name.trim().toLowerCase().startsWith('dreamina-seedance-')
}
```

```go
func getDefaultSeedanceVideoPrice(model, resolution string) (float64, bool) {
    model = strings.ToLower(strings.TrimSpace(model))
    resolution = NormalizeVideoBillingResolutionOrDefault(resolution)
    switch {
    case strings.Contains(model, "seedance-2-0-mini"):
        return map[string]float64{"480p": 0.04, "720p": 0.08}[resolution], resolution != "1080p"
    case strings.Contains(model, "seedance-2-0-fast"):
        return map[string]float64{"480p": 0.06, "720p": 0.12}[resolution], resolution != "1080p"
    case strings.Contains(model, "seedance-2-0"):
        return map[string]float64{"480p": 0.07, "720p": 0.15, "1080p": 0.37}[resolution], true
    default:
        return 0, false
    }
}
```

- [ ] **Step 6: 运行测试并提交**

Run: `cd frontend && pnpm vitest run src/utils/__tests__/modelKind.spec.ts src/utils/__tests__/videoPricing.spec.ts`

Run: `cd backend && go test -tags=unit ./internal/service -run 'SeedanceOfficialDefaults|DefaultVideoPrice'`

Commit: `git commit -m "feat: 添加 Seedance 官方默认价格"`

---

### Task 2: 按 API Key 发现无渠道模型

**Files:**
- Modify: `frontend/src/api/modelTest.ts`
- Modify: `frontend/src/views/user/ModelTestView.vue`
- Test: `frontend/src/views/user/__tests__/ModelTestView.spec.ts`

**Interfaces:**
- Produces: `listGatewayModels(apiKey: string, signal?: AbortSignal): Promise<string[]>`
- Produces: `gatewayModelsByKeyID: Map<number, string[]>`
- Consumes: 当前选中 `ApiKey`、`UserAvailableGroup` 和渠道模型列表。

- [ ] **Step 1: 写模型合并失败测试**

```ts
mockListGatewayModels.mockResolvedValue([
  'dreamina-seedance-2-0-ep',
  'dreamina-seedance-2-0-mini-ep',
])
await wrapper.get('[data-testid="model-test-api-key"]').setValue(seedanceKey.id)
expect(modelOptions()).toContain('dreamina-seedance-2-0-mini-ep')
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd frontend && pnpm vitest run src/views/user/__tests__/ModelTestView.spec.ts -t '无渠道分组读取网关模型'`

Expected: Seedance 模型选项不存在。

- [ ] **Step 3: 实现网关模型读取**

```ts
export async function listGatewayModels(apiKey: string, signal?: AbortSignal): Promise<string[]> {
  const payload = await getGateway<{ data?: Array<{ id?: string }> }>('/v1/models', apiKey, signal)
  return (payload.data || []).map((item) => String(item.id || '').trim()).filter(Boolean)
}
```

- [ ] **Step 4: 合并当前 Key 的分组模型**

新增按 Key 缓存；无渠道分组使用 `activeKeys` 中的分组信息创建最小 `UserAvailableGroup` 引用，渠道模型同名时保留渠道定价。

```ts
interface GatewayDiscoveredModel {
  name: string
  groupID: number
  platform: string
}
```

- [ ] **Step 5: 验证切换 Key、缓存和失败回退**

Run: `cd frontend && pnpm vitest run src/views/user/__tests__/ModelTestView.spec.ts`

Expected: 新测试通过，原有测试无回归。

- [ ] **Step 6: 提交**

Commit: `git commit -m "feat: 测试台按密钥发现模型"`

---

### Task 3: Seedance Chat Completions 协议转换和响应解析

**Files:**
- Create: `backend/internal/service/openai_seedance_video.go`
- Test: `backend/internal/service/openai_seedance_video_test.go`

**Interfaces:**
- Produces: `IsSeedanceVideoModel(model string) bool`
- Produces: `BuildSeedanceChatRequest(req SeedanceVideoRequest) ([]byte, error)`
- Produces: `ParseSeedanceChatResponse(body []byte) (SeedanceVideoResult, error)`

- [ ] **Step 1: 写请求转换失败测试**

```go
req := SeedanceVideoRequest{Model: "dreamina-seedance-2-0-mini-ep", Prompt: "黑色背景", Resolution: "480p", Duration: 4}
body, err := BuildSeedanceChatRequest(req)
require.NoError(t, err)
require.Equal(t, req.Model, gjson.GetBytes(body, "model").String())
require.Contains(t, gjson.GetBytes(body, "messages.0.content").String(), "480p")
require.Contains(t, gjson.GetBytes(body, "messages.0.content").String(), "4秒")
```

- [ ] **Step 2: 写响应解析失败测试**

覆盖以下响应：消息文本中的 HTTPS 视频 URL、Markdown 视频链接、JSON 字符串中的 `url`、任务 ID、标准错误对象。

```go
result, err := ParseSeedanceChatResponse([]byte(`{"choices":[{"message":{"content":"https://cdn.test/video.mp4"}}],"usage":{"completion_tokens":100}}`))
require.NoError(t, err)
require.Equal(t, "https://cdn.test/video.mp4", result.VideoURL)
```

- [ ] **Step 3: 运行测试确认失败**

Run: `cd backend && go test -tags=unit ./internal/service -run 'Test(Build|Parse)Seedance'`

Expected: 类型和函数尚不存在。

- [ ] **Step 4: 实现纯函数适配器**

请求内容明确包含提示词、分辨率、时长和参考图片 data URL；响应解析只接受 `https` URL，优先结构化 JSON，再解析消息文本。

- [ ] **Step 5: 运行测试并提交**

Run: `cd backend && go test -tags=unit ./internal/service -run 'Seedance'`

Commit: `git commit -m "feat: 添加 Seedance 视频协议适配器"`

---

### Task 4: OpenAI Seedance 视频路由、调度和计费

**Files:**
- Modify: `backend/internal/server/routes/gateway.go`
- Modify: `backend/internal/handler/grok_media.go`
- Create: `backend/internal/handler/openai_seedance_video.go`
- Modify: `backend/internal/service/openai_gateway_service.go`
- Modify: `backend/internal/service/openai_gateway_usage.go`
- Test: `backend/internal/server/routes/gateway_test.go`
- Test: `backend/internal/handler/openai_seedance_video_test.go`
- Test: `backend/internal/service/openai_gateway_record_usage_test.go`

**Interfaces:**
- Produces: `OpenAIGatewayHandler.SeedanceVideoGeneration`
- Produces: `/v1/videos/generations` 对 OpenAI Seedance 的受限入口。
- Consumes: Task 3 的协议转换和解析、现有 OpenAI API Key 账号调度及视频计费。

- [ ] **Step 1: 写路由失败测试**

普通 OpenAI 模型仍返回 404；OpenAI 分组请求 Seedance 模型进入 Seedance handler；Grok 分组仍进入 Grok handler。

- [ ] **Step 2: 写 handler 和计费失败测试**

使用 `httptest.Server` 模拟 Helix `/v1/chat/completions`，验证鉴权头、请求体、同步视频 URL、上游 503 透传，以及 4 秒 Mini 480p 成功用量为 `0.16 USD × 倍率`。

- [ ] **Step 3: 运行测试确认失败**

Run: `cd backend && go test -tags=unit ./internal/server/routes ./internal/handler ./internal/service -run 'Seedance'`

- [ ] **Step 4: 实现受限路由和上游调用**

只允许 `IsSeedanceVideoModel(request.model)` 进入 OpenAI Seedance handler；账号选择必须使用请求模型并保持模型映射。上游固定调用账号 `base_url + /v1/chat/completions`。

- [ ] **Step 5: 记录视频用量**

构造 `OpenAIForwardResult{VideoCount: 1, VideoResolution: resolution, VideoDurationSeconds: duration}`，复用 `calculateOpenAIVideoCost` 和现有异步 usage worker。没有视频 URL/任务成功标志时不记录成功视频成本。

- [ ] **Step 6: 运行测试并提交**

Run: `cd backend && go test -tags=unit ./internal/server/routes ./internal/handler ./internal/service -run 'Seedance|NonGrokVideos'`

Commit: `git commit -m "feat: 支持 Seedance 视频网关"`

---

### Task 5: 测试台 Seedance 生成结果展示

**Files:**
- Modify: `frontend/src/api/modelTest.ts`
- Modify: `frontend/src/views/user/ModelTestView.vue`
- Test: `frontend/src/api/__tests__/modelTest.spec.ts`
- Test: `frontend/src/views/user/__tests__/ModelTestView.spec.ts`

**Interfaces:**
- Consumes: 统一 `/v1/videos/generations` Seedance 响应中的 `video_url` 或兼容 URL 字段。
- Produces: Seedance 生成按钮、价格预览和视频播放器结果。

- [ ] **Step 1: 写失败测试**

验证 Seedance 选择后显示正确分辨率和官方价格；生成响应返回视频 URL 时播放器显示；上游临时不可用时展示原始业务错误。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd frontend && pnpm vitest run src/api/__tests__/modelTest.spec.ts src/views/user/__tests__/ModelTestView.spec.ts -t 'Seedance'`

- [ ] **Step 3: 实现最小兼容解析**

扩展 `extractVideoURL` 和状态判断，保留 Grok 字段兼容；Seedance 同步完成时不进入无意义轮询。

- [ ] **Step 4: 运行测试并提交**

Run: `cd frontend && pnpm vitest run src/api/__tests__/modelTest.spec.ts src/views/user/__tests__/ModelTestView.spec.ts`

Commit: `git commit -m "feat: 测试台支持 Seedance 视频"`

---

### Task 6: 完整验证、文档和发布

**Files:**
- Modify: `docs/superpowers/specs/2026-07-19-seedance-model-test-design.md`（仅当实现与设计存在已确认差异）
- Modify: `docs/superpowers/plans/2026-07-19-seedance-model-test.md`（更新执行勾选）

- [ ] **Step 1: 运行完整验证**

Run: `cd backend && go test -tags=unit ./internal/service ./internal/handler ./internal/server/routes`

Run: `cd frontend && pnpm test -- --run`

Run: `cd frontend && pnpm build`

Run: `git diff --check && git status --short`

- [ ] **Step 2: 推送功能分支**

Run: `git push -u origin codex/fix-seedance-model-test`

- [ ] **Step 3: VPS staging 构建**

在 `/opt/sub2api/repo` 拉取功能分支，构建 `sub2api:staging-<commit>`，验证 `--version`、容器 healthy 和 `18080/health`。

- [ ] **Step 4: staging 真实验证**

使用线上 `视频测试` 分组 API Key 调用 staging `/v1/models`，确认 4 个 Seedance 模型；调用 Mini 480p 4 秒生成。若 Helix 仍返回 `upstream temporarily unavailable`，确认请求已进入 Seedance adapter、错误未扣费，并把上游可用性列为外部风险。

- [ ] **Step 5: 合并和生产切换**

快进合并到 `main` 并推送；prod 复用 staging 同 commit 镜像，只重建应用容器，保留旧镜像回滚点。

- [ ] **Step 6: 生产验证**

验证容器镜像、版本、HTTP/HTTPS health、`/purchase`、`/models` 鉴权、管理接口鉴权和最近错误日志；确认数据库和 Redis 未重启。
