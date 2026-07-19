# OpenAI 分组自动视频协议 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 OpenAI 分组的视频入口改造成不依赖模型名的通用异步 `/v1/videos` 网关，自动发现上游协议，并让模型测试台可使用任意当前 Key 模型完成创建、轮询和播放。

**Architecture:** 创建请求继续复用现有 OpenAI Chat Completions handler 的账号选择、故障切换和用量记录循环，但在 `OpenAIVideoContext` 存在时由 service 分流到通用视频 forwarder。异步任务通过现有 `GatewayCache` 粘性会话能力绑定创建账号；协议能力由 `gatewayCache` 的可选扩展接口按账号和映射后模型缓存。状态与内容使用独立 lookup handler，只访问任务绑定账号。

**Tech Stack:** Go、Gin、Redis、PostgreSQL、Vue 3、TypeScript、Vitest、Go `httptest`。

## Global Constraints

- 所有代码注释、文档和提交说明使用中文。
- 不记录或返回 API Key、完整提示词、参考图 data URL、上游签名视频 URL。
- 不按 Seedance、Jing、Kling、Veo、Sora 等模型名决定协议。
- 只有明确 `404`、`405` 或 endpoint unsupported 错误可以触发 Chat Completions 回退。
- `400` 业务错误、`401`、`403`、`429`、`5xx`、超时和断线不得回退，避免重复创建任务。
- 创建成功只记录一次视频用量；轮询与下载不扣费。
- Grok 现有视频生成、资格校验、状态和内容行为不得改变。
- 生产构建只能来自已提交并推送的 commit；本次先在正式 VPS 隔离 staging 验证，未经用户明确确认不得切 prod。

---

### Task 1: 通用视频请求、响应与协议判定

**Files:**
- Create: `backend/internal/service/openai_video.go`
- Create: `backend/internal/service/openai_video_test.go`
- Modify: `backend/internal/service/openai_seedance_video.go`
- Modify: `backend/internal/service/openai_seedance_video_test.go`

**Interfaces:**
- Produces: `OpenAIVideoRequest`, `OpenAIVideoResult`, `OpenAIVideoContext`。
- Produces: `SetOpenAIVideoContext(c, meta)`、`openAIVideoContextFromGin(c)`。
- Produces: `NormalizeOpenAIVideoCreateBody(body, mappedModel) ([]byte, OpenAIVideoRequest, error)`。
- Produces: `ParseOpenAIVideoResult(body) (OpenAIVideoResult, error)`。
- Produces: `NormalizeOpenAIVideoStatus(status string) string`。
- Produces: `IsOpenAIVideoEndpointUnsupported(status int, body []byte) bool`。
- Preserves: `BuildSeedanceChatRequest`、`ParseSeedanceChatResponse` 作为旧协议回退实现。

- [ ] **Step 1: 写请求规范化失败测试**

在 `openai_video_test.go` 覆盖：

```go
func TestNormalizeOpenAIVideoCreateBodyUsesMappedModelAndStringSeconds(t *testing.T) {
	body, req, err := NormalizeOpenAIVideoCreateBody([]byte(`{
		"model":"dreamina-seedance-2-0-ep",
		"prompt":"雨夜城市",
		"resolution":"720p",
		"duration":5,
		"reference_images":[{"url":"https://cdn.test/a.png"}]
	}`), "jing-video-2-pro")
	require.NoError(t, err)
	require.Equal(t, "dreamina-seedance-2-0-ep", req.Model)
	require.Equal(t, 5, req.DurationSeconds)
	require.Equal(t, "jing-video-2-pro", gjson.GetBytes(body, "model").String())
	require.Equal(t, "5", gjson.GetBytes(body, "seconds").String())
	require.Equal(t, "https://cdn.test/a.png", gjson.GetBytes(body, "image_urls.0").String())
}
```

- [ ] **Step 2: 运行测试确认 RED**

Run: `cd backend && go test -tags=unit ./internal/service -run 'TestNormalizeOpenAIVideoCreateBody'`

Expected: FAIL，提示 `NormalizeOpenAIVideoCreateBody` 未定义。

- [ ] **Step 3: 实现请求解析和规范化**

`NormalizeOpenAIVideoCreateBody` 必须：校验 `model`、`prompt`；默认 `720p` 和现有默认时长；读取 `duration` 或 `seconds`；输出字符串 `seconds`；合并 `image_urls`、`reference_image_urls`、`image.url` 和 `reference_images[].url`；删除内部兼容字段后写入映射模型。

- [ ] **Step 4: 写响应与端点判定失败测试**

覆盖 `task_id/id/request_id`、`metadata.url`、状态别名、百分比进度，以及以下判定表：

```go
require.True(t, IsOpenAIVideoEndpointUnsupported(404, []byte(`{"error":{"code":"not_found"}}`)))
require.True(t, IsOpenAIVideoEndpointUnsupported(405, nil))
require.True(t, IsOpenAIVideoEndpointUnsupported(400, []byte(`{"error":{"code":"unsupported_endpoint"}}`)))
require.False(t, IsOpenAIVideoEndpointUnsupported(400, []byte(`{"code":"invalid_request","message":"prompt is required"}`)))
require.False(t, IsOpenAIVideoEndpointUnsupported(502, []byte(`{"message":"temporarily unavailable"}`)))
```

- [ ] **Step 5: 实现响应解析和协议判定**

规范状态到 `queued/in_progress/completed/failed`，进度限制在 `0-100`；解析 URL 仅供服务端内容选择，不直接写给客户端；端点不支持判定使用有限错误码和消息短语，不把普通模型错误或上游故障误判为协议不支持。

- [ ] **Step 6: 运行 Task 1 测试并提交**

Run: `cd backend && go test -tags=unit ./internal/service -run 'OpenAIVideo|Seedance'`

Commit: `git commit -m "feat: 添加通用视频协议解析"`

---

### Task 2: 协议能力缓存与平台无关任务绑定

**Files:**
- Modify: `backend/internal/service/gateway_service.go`
- Modify: `backend/internal/repository/gateway_cache.go`
- Modify: `backend/internal/repository/gateway_cache_integration_test.go`
- Modify: `backend/internal/service/grok_media.go`
- Modify: `backend/internal/service/openai_gateway_grok_test.go`
- Create: `backend/internal/service/openai_video_binding_test.go`

**Interfaces:**
- Produces optional interface `OpenAIVideoProtocolCache`，不扩展 `GatewayCache`，避免要求所有既有测试 mock 增加方法。
- Produces `GetOpenAIVideoProtocol(ctx, accountID, mappedModel)`、`SetOpenAIVideoProtocol(...)`、`DeleteOpenAIVideoProtocol(...)`。
- Produces `VideoTaskSessionHash`、`BindVideoTaskAccount`、`ResolveVideoTaskAccount`。
- Preserves `GrokMediaVideoRequestSessionHash`、`BindGrokMediaVideoRequestAccount`、`ResolveGrokMediaVideoRequestAccount` 作为兼容 wrapper。

- [ ] **Step 1: 写 Redis 协议缓存失败测试**

验证键包含账号和映射模型哈希，值只允许 `videos/chat_completions`，TTL 生效，删除后返回 `redis.Nil`。

- [ ] **Step 2: 运行 repository 测试确认 RED**

Run: `cd backend && go test -tags=integration ./internal/repository -run 'GatewayCacheSuite/TestOpenAIVideoProtocol'`

Expected: FAIL，方法未定义。

- [ ] **Step 3: 实现可选缓存接口**

在 `gatewayCache` 上增加协议方法，键前缀使用 `openai_video_protocol:`；service 通过类型断言使用该接口。Redis 不可用或对象不实现接口时返回“未知协议”，创建链路继续优先尝试 `/v1/videos`。

- [ ] **Step 4: 写通用任务绑定失败测试**

验证相同 `task_id` 在不同用户、API Key、分组下生成不同 session hash；绑定账号只能由原所有者解析。

- [ ] **Step 5: 抽取通用绑定并保留 Grok wrapper**

通用 hash 前缀使用 `video-task:`。Grok wrapper 调用通用实现，现有 Grok 测试保持通过，避免线上旧任务的短 TTL 绑定行为发生不可控变化；如需兼容旧 `grok-video:` key，Resolve 先查新 key，再回查旧 key。

- [ ] **Step 6: 运行 Task 2 测试并提交**

Run: `cd backend && go test -tags=unit ./internal/service -run 'VideoTask|GrokMediaVideoRequest'`

Run: `cd backend && go test -tags=integration ./internal/repository -run 'GatewayCacheSuite'`

Commit: `git commit -m "feat: 添加视频协议缓存和任务绑定"`

---

### Task 3: OpenAI 异步视频创建、查询、下载与自动回退

**Files:**
- Create: `backend/internal/service/openai_video_forward.go`
- Create: `backend/internal/service/openai_video_forward_test.go`
- Modify: `backend/internal/service/openai_gateway_chat_completions.go`
- Modify: `backend/internal/service/openai_gateway_chat_completions_raw.go`
- Modify: `backend/internal/handler/openai_seedance_video.go`
- Create: `backend/internal/handler/openai_video_lookup.go`
- Create: `backend/internal/handler/openai_video_test.go`
- Modify: `backend/internal/handler/openai_chat_completions.go`
- Modify: `backend/internal/server/routes/gateway.go`
- Modify: `backend/internal/server/routes/gateway_test.go`
- Modify: `backend/internal/handler/endpoint.go`

**Interfaces:**
- Produces `OpenAIGatewayService.ForwardOpenAIVideoCreate(...)`。
- Produces `OpenAIGatewayService.ForwardOpenAIVideoStatus(...)`。
- Produces `OpenAIGatewayService.ForwardOpenAIVideoContent(...)`。
- Produces handlers `OpenAIVideoGeneration`、`OpenAIVideoStatus`、`OpenAIVideoContent`。
- Consumes Task 1 parsing and Task 2 cache/binding.

- [ ] **Step 1: 写上游 `/v1/videos` 创建失败测试**

使用 `httptest.Server` 验证：目标 URL 为 `{base_url}/v1/videos`；Bearer 和账号 header override 生效；模型先经过账号映射；`seconds` 为字符串；成功响应规范化并返回原请求模型。

- [ ] **Step 2: 运行 service 测试确认 RED**

Run: `cd backend && go test -tags=unit ./internal/service -run 'TestForwardOpenAIVideoCreate'`

Expected: FAIL，forwarder 未定义。

- [ ] **Step 3: 实现创建 forwarder 和自动协议策略**

流程：读取协议缓存；`chat_completions` 直接调用旧 Seedance 同步转换；未知或 `videos` 调用异步端点；成功写缓存；明确端点不支持时写 `chat_completions` 并仅回退一次；其他错误沿用 OpenAI failover 语义且不得执行第二次创建。`OpenAIForwardResult` 填充 `ResponseID`、请求/映射模型、分辨率、时长、参考图数量和 `UpstreamEndpoint=/v1/videos`。

- [ ] **Step 4: 写状态和内容代理失败测试**

验证状态读取 `metadata.url` 但下游只返回 `/v1/videos/{id}/content`；内容请求优先访问上游 `/content`，支持 `Range`，只接受视频 Content-Type，限制响应体，拒绝未允许重定向；上游没有 `/content` 时才允许使用状态中经过 HTTPS 校验的结果 URL。

- [ ] **Step 5: 实现 lookup service 与 handler**

handler 从鉴权上下文取得用户、API Key、分组，通过 `ResolveVideoTaskAccount` 锁定账号。不存在或所有权不匹配统一 404；状态和内容不切换账号、不记录用量。

- [ ] **Step 6: 接入现有 Chat handler 调度与计费**

`OpenAIVideoGeneration` 只读取和验证请求、设置 `OpenAIVideoContext`，然后调用现有 `ChatCompletions` handler。`ForwardAsChatCompletions` 发现视频 context 时进入通用 forwarder。创建成功后，handler 在记录用量前绑定 `task_id`；绑定失败返回 502，不能向客户端交付一个随后无法查询的任务。删除原来只允许 `dreamina-seedance-*` 的路由门禁。

- [ ] **Step 7: 注册两个创建入口和 OpenAI lookup 路由**

注册：

```go
gateway.POST("/videos", videoGenerationHandler)
gateway.POST("/videos/generations", videoGenerationHandler)
gateway.GET("/videos/:task_id", videoStatusHandler)
gateway.GET("/videos/:task_id/content", videoContentHandler)
```

Grok 分组仍进入现有 Grok handler；OpenAI 分组进入通用 handler；其他平台保持 404。

- [ ] **Step 8: 写计费和不重复创建测试**

覆盖：创建成功一条视频 usage；轮询和内容为零条 usage；`400/401/403/429/502/timeout` 不调用 Chat fallback；明确 endpoint unsupported 恰好调用一次 fallback；创建响应无 task ID 不扣费。

- [ ] **Step 9: 运行 Task 3 测试并提交**

Run: `cd backend && go test -tags=unit ./internal/service ./internal/handler ./internal/server/routes -run 'OpenAIVideo|NonGrokVideos|Seedance|GrokMedia'`

Commit: `git commit -m "feat: 支持 OpenAI 自动异步视频协议"`

---

### Task 4: 模型测试台按视频接口意图选择和轮询

**Files:**
- Modify: `frontend/src/api/modelTest.ts`
- Modify: `frontend/src/api/__tests__/modelTest.spec.ts`
- Modify: `frontend/src/views/user/ModelTestView.vue`
- Modify: `frontend/src/views/user/__tests__/ModelTestView.spec.ts`
- Modify: `frontend/src/utils/modelKind.ts`
- Modify: `frontend/src/utils/__tests__/modelKind.spec.ts`

**Interfaces:**
- `testVideoGeneration` 改用 `POST /v1/videos`。
- 视频模式的候选模型来自当前 API Key 分组全部网关模型，不再要求 `model.kind === 'video'`。
- 创建成功后始终按 task ID 轮询；完成后始终通过内容端点获取 Blob。

- [ ] **Step 1: 写 API 层失败测试**

验证 POST 路径为 `/v1/videos`；创建响应 `queued` 后只轮询同一 ID；完成状态 URL 位于 `metadata.url` 时仍返回任务结果；失败和超时不重新 POST。

- [ ] **Step 2: 运行 API 测试确认 RED**

Run: `cd frontend && pnpm vitest run src/api/__tests__/modelTest.spec.ts -t 'video'`

Expected: FAIL，仍调用 `/v1/videos/generations` 或重复使用旧完成判断。

- [ ] **Step 3: 实现 API 轮询和内容读取**

默认轮询间隔改为 5 秒；状态统一识别四种规范状态；超时错误包含 task ID；取消使用现有 AbortSignal；不在 API 层重新创建任务。

- [ ] **Step 4: 写视图失败测试**

构造名称为 `future-motion-pro`、`kind: token`、来自当前 Key `/v1/models` 的模型。切换视频模式后应出现在选择框，执行时调用 `testVideoGeneration`，完成后调用 `fetchVideoContent`，价格未知不阻止按钮。

- [ ] **Step 5: 实现按接口意图选择模型**

`filteredModels` 在视频模式下使用当前分组全部可用模型；文本和图片继续使用原分类。选中未知模型时视频价格预览为 `-`，参考图默认禁用，直到已有模型能力规则明确支持。异步视频不再只为 Grok 下载 Blob。

- [ ] **Step 6: 运行 Task 4 测试并提交**

Run: `cd frontend && pnpm vitest run src/api/__tests__/modelTest.spec.ts src/views/user/__tests__/ModelTestView.spec.ts src/utils/__tests__/modelKind.spec.ts`

Commit: `git commit -m "feat: 测试台支持通用异步视频"`

---

### Task 5: 文档、完整验证与 staging

**Files:**
- Modify: `README_CN.md`
- Modify: `README.md`
- Modify: `docs/RECHARGE_MODEL_MARKET_CN.md`
- Modify: `docs/superpowers/specs/2026-07-19-auto-video-protocol-design.md`（仅在实现有已确认差异时）

**Interfaces:**
- Documents public `/v1/videos` create/status/content workflow and compatibility alias.
- Documents automatic protocol fallback safety boundary.

- [ ] **Step 1: 更新公开文档**

说明请求字段、字符串 `seconds` 上游归一化、四种状态、内容代理、旧 `/v1/videos/generations` 兼容入口、未知模型由视频模式直接调用，以及只有明确端点不存在才回退。

- [ ] **Step 2: 运行后端完整相关验证**

Run: `cd backend && go test -tags=unit ./internal/service ./internal/handler ./internal/server/routes`

Run: `GOCACHE=/tmp/sub2api-go-cache go test ./... -run '^$'`

- [ ] **Step 3: 运行前端完整验证**

Run: `cd frontend && pnpm test -- --run`

Run: `cd frontend && pnpm build`

- [ ] **Step 4: 检查工作区和提交**

Run: `git diff --check && git status --short && git diff --stat HEAD~4..HEAD`

确认 `.pnpm-store/` 仍未纳入提交，只包含本任务文件。

Commit: `git commit -m "docs: 更新自动视频接口说明"`

- [ ] **Step 5: 推送功能分支**

Run: `git push -u origin codex/auto-video-protocol`

- [ ] **Step 6: 正式 VPS 隔离 staging 构建**

在 `/opt/sub2api/repo` 拉取已推送 commit，构建 `sub2api:staging-<commit>`，使用独立 staging compose、env、PostgreSQL、Redis、数据目录和 `18080`，核对镜像 `--version` 与 commit 一致。

- [ ] **Step 7: 使用“视频”和“视频2”真实验证**

`视频`：统一请求模型映射到 Helix Mini，480p、4 秒，验证创建、状态、内容、单次计费。

`视频2`：统一请求模型映射到 `jing-video-2-pro`，验证 Skylee 创建、状态、内容、单次计费。

同时验证任务所有权隔离、错误日志脱敏、HTTP/HTTPS health、容器健康和旧 Grok 视频回归。

- [ ] **Step 8: 报告 staging 结果并等待生产确认**

不得合并 `dev/main` 或切换 prod。向用户报告已验证 commit、镜像 tag、两条真实任务结果、计费记录和剩余风险，等待明确口头上线命令。
