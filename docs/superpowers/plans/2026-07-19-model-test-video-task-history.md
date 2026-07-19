# 模型测试台视频任务记录 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为模型测试台增加服务端持久化的视频任务记录，使慢任务可在刷新或换设备后继续查询，并移除客户端总超时失败。

**Architecture:** PostgreSQL 保存测试任务及实际上游账号，网关只对带内部测试台标记的视频创建请求落库。登录态任务接口负责分页、刷新、内容代理和删除；前端只在页面可见时轮询非终态任务。

**Tech Stack:** Go、Gin、PostgreSQL、Wire、Vue 3、TypeScript、Vitest、pnpm。

## Global Constraints

- 只记录模型测试台流量，不改变普通 `/v1/videos` API 行为。
- 未完成任务不设置总超时；只有上游明确终止才进入失败。
- `completed` 和 `failed` 保留 30 天，未完成任务不自动清理。
- 不保存 API Key 明文或参考图 Base64。
- 所有数据库读取和写入必须按当前用户隔离。
- 每一项生产代码都必须先有失败测试并确认失败原因。
- 不触碰主工作区已有的 `.pnpm-store/`。

---

### Task 1: 数据库迁移与持久化仓储

**Files:**
- Create: `backend/migrations/185_model_test_video_tasks.sql`
- Create: `backend/internal/repository/video_test_task_repo.go`
- Create: `backend/internal/repository/video_test_task_repo_test.go`
- Modify: `backend/internal/repository/wire.go`
- Test: `backend/internal/repository/migrations_schema_integration_test.go`

**Interfaces:**
- Produces: `service.VideoTestTaskStore` 的 PostgreSQL 实现，支持 `Create`、`GetByOwner`、`GetByUpstreamOwner`、`ListByUser`、`UpdatePollResult`、`DeleteByOwner` 和 `DeleteExpiredTerminal`。

- [ ] **Step 1: 写迁移与仓储失败测试**

测试应断言任务按 `user_id` 隔离、重复上游任务幂等、终态更新时间正确、分页倒序，以及清理语句只删除 30 天前的终态。

- [ ] **Step 2: 运行测试确认因表和仓储不存在而失败**

Run: `cd backend && GOCACHE=/tmp/sub2api-go-cache go test ./internal/repository -run 'TestVideoTestTask|TestMigrationSchema'`

Expected: FAIL，错误明确指向 `VideoTestTaskStore` 或 `model_test_video_tasks` 不存在。

- [ ] **Step 3: 实现幂等迁移和原生 SQL 仓储**

迁移必须包含状态 CHECK、规格字段 CHECK、用户列表索引、非终态部分索引和终态清理部分索引。仓储方法必须把 `user_id` 放入所有用户侧查询和删除条件。

- [ ] **Step 4: 运行仓储测试确认通过**

Run: `cd backend && GOCACHE=/tmp/sub2api-go-cache go test ./internal/repository -run 'TestVideoTestTask|TestMigrationSchema'`

Expected: PASS。

- [ ] **Step 5: 提交数据层**

```bash
git add backend/migrations/185_model_test_video_tasks.sql backend/internal/repository/video_test_task_repo.go backend/internal/repository/video_test_task_repo_test.go backend/internal/repository/migrations_schema_integration_test.go backend/internal/repository/wire.go
git commit -m "feat: 持久化模型测试视频任务"
```

### Task 2: 任务领域服务与保留期清理

**Files:**
- Create: `backend/internal/service/video_test_task.go`
- Create: `backend/internal/service/video_test_task_test.go`
- Create: `backend/internal/service/video_test_task_cleanup.go`
- Create: `backend/internal/service/video_test_task_cleanup_test.go`
- Modify: `backend/internal/service/wire.go`

**Interfaces:**
- Consumes: `VideoTestTaskStore`。
- Produces: `VideoTestTaskService.RecordAccepted`、`List`、`Get`、`ApplyPollResult`、`RecordPollError`、`Delete`、`ResolveAccountID`；`VideoTestTaskCleanupService.Start/Stop`。

- [ ] **Step 1: 写状态机和清理失败测试**

测试覆盖状态别名归一化、终态不可回退、传输错误只更新 `last_poll_error`、五秒内重复刷新不访问上游、30 天终态清理和非终态保留。

- [ ] **Step 2: 运行服务测试确认失败**

Run: `cd backend && GOCACHE=/tmp/sub2api-go-cache go test ./internal/service -run 'TestVideoTestTask'`

Expected: FAIL，错误明确指向任务服务类型或方法不存在。

- [ ] **Step 3: 实现最小领域模型、状态机和清理服务**

`ApplyPollResult` 只允许非终态进入规范状态；`RecordPollError` 不修改 `status`；清理服务每小时调用 `DeleteExpiredTerminal(now.Add(-30*24*time.Hour))`。

- [ ] **Step 4: 运行服务测试确认通过**

Run: `cd backend && GOCACHE=/tmp/sub2api-go-cache go test ./internal/service -run 'TestVideoTestTask'`

Expected: PASS。

- [ ] **Step 5: 提交领域层**

```bash
git add backend/internal/service/video_test_task*.go backend/internal/service/wire.go
git commit -m "feat: 增加视频测试任务状态服务"
```

### Task 3: 网关创建落库与 Redis 过期恢复

**Files:**
- Modify: `backend/internal/service/openai_video_forward.go`
- Modify: `backend/internal/service/openai_seedance_video.go`
- Modify: `backend/internal/service/grok_media.go`
- Modify: `backend/internal/handler/openai_seedance_video.go`
- Modify: `backend/internal/handler/grok_media.go`
- Modify: `backend/internal/handler/wire.go`
- Test: `backend/internal/service/openai_video_forward_test.go`
- Test: `backend/internal/service/openai_video_binding_test.go`
- Test: `backend/internal/handler/openai_video_test.go`
- Test: `backend/internal/handler/openai_gateway_credential_failover_loop_test.go`

**Interfaces:**
- Consumes: `VideoTestTaskService.RecordAccepted` 和 `ResolveAccountID`。
- Produces: `X-Sub2API-Model-Test: video` 标记语义，以及 Redis miss 时按 `(user_id, api_key_id, upstream_task_id)` 恢复账号并重新绑定缓存。

- [ ] **Step 1: 写 OpenAI、Grok 和缓存恢复失败测试**

测试分别断言：无标记不落库；有标记记录最终调度账号和请求摘要；Redis miss 会查持久记录并重建绑定；其他用户或 API Key 无法恢复。

- [ ] **Step 2: 运行定向测试确认失败**

Run: `cd backend && GOCACHE=/tmp/sub2api-go-cache go test ./internal/service ./internal/handler -run 'Test.*VideoTestTask|Test.*VideoTask.*Persistent'`

Expected: FAIL，错误明确指向标记解析、落库钩子或持久恢复尚未实现。

- [ ] **Step 3: 实现创建钩子和持久恢复**

只有值严格等于 `video` 的内部标记才启用记录。记录必须发生在上游返回任务 ID 且最终账号已确定之后、成功响应写回之前。持久恢复成功后用现有 TTL 重建 Redis 绑定。

- [ ] **Step 4: 运行视频网关回归**

Run: `cd backend && GOCACHE=/tmp/sub2api-go-cache go test ./internal/service ./internal/handler -run 'Video|GrokMedia'`

Expected: PASS。

- [ ] **Step 5: 提交网关接线**

```bash
git add backend/internal/service/openai_video_forward.go backend/internal/service/openai_seedance_video.go backend/internal/service/grok_media.go backend/internal/handler/openai_seedance_video.go backend/internal/handler/grok_media.go backend/internal/handler/wire.go backend/internal/service/*video*test.go backend/internal/handler/*video*test.go
git commit -m "feat: 记录测试台视频网关任务"
```

### Task 4: 用户任务接口、状态刷新与内容代理

**Files:**
- Create: `backend/internal/handler/video_test_task_handler.go`
- Create: `backend/internal/handler/video_test_task_handler_test.go`
- Modify: `backend/internal/handler/handler.go`
- Modify: `backend/internal/handler/wire.go`
- Modify: `backend/internal/server/routes/user.go`
- Modify: `backend/internal/server/routes/user_test.go`
- Modify: `backend/cmd/server/wire_gen.go`

**Interfaces:**
- Consumes: `VideoTestTaskService`、`OpenAIGatewayService` 和 `AccountRepository`。
- Produces: `GET/POST/DELETE /api/v1/model-test/video-tasks` 任务接口与内容代理。

- [ ] **Step 1: 写鉴权、所有权、刷新和 Range 失败测试**

测试覆盖分页边界、越权返回 404、五秒节流、查询异常仍返回原任务状态、OpenAI/Grok 刷新分派、终态不再查询，以及内容代理透传 `Range`。

- [ ] **Step 2: 运行 Handler 与路由测试确认失败**

Run: `cd backend && GOCACHE=/tmp/sub2api-go-cache go test ./internal/handler ./internal/server/routes -run 'TestVideoTestTask'`

Expected: FAIL，错误明确指向 Handler 或路由不存在。

- [ ] **Step 3: 实现登录态接口和依赖注入**

Handler 必须只从 JWT 上下文获取 `user_id`。刷新错误调用 `RecordPollError` 后返回任务记录和非空 `last_poll_error`；内容接口使用数据库中的账号，不接受客户端账号参数。

- [ ] **Step 4: 重新生成并核对 Wire**

Run: `cd backend && PATH="$(go env GOPATH)/bin:$PATH" go generate ./cmd/server`

Expected: `backend/cmd/server/wire_gen.go` 只出现本次任务所需的新依赖接线。

- [ ] **Step 5: 运行接口测试确认通过**

Run: `cd backend && GOCACHE=/tmp/sub2api-go-cache go test ./internal/handler ./internal/server/routes -run 'TestVideoTestTask'`

Expected: PASS。

- [ ] **Step 6: 提交接口层**

```bash
git add backend/internal/handler/video_test_task_handler*.go backend/internal/handler/handler.go backend/internal/handler/wire.go backend/internal/server/routes/user*.go backend/cmd/server/wire_gen.go
git commit -m "feat: 提供视频测试任务查询接口"
```

### Task 5: 前端任务 API 与无总超时轮询

**Files:**
- Create: `frontend/src/api/videoTestTasks.ts`
- Create: `frontend/src/api/__tests__/videoTestTasks.spec.ts`
- Modify: `frontend/src/api/modelTest.ts`
- Modify: `frontend/src/api/__tests__/modelTest.spec.ts`

**Interfaces:**
- Produces: `VideoTestTask`、`listVideoTestTasks`、`refreshVideoTestTask`、`deleteVideoTestTask`、`videoTestTaskContentURL`；`testVideoGeneration` 只创建任务，不再执行总超时轮询。

- [ ] **Step 1: 写前端 API 失败测试**

测试断言创建请求携带内部标记、创建函数返回任务 ID 后立即结束、任务接口使用登录态 API Client，并正确构造内容 URL。

- [ ] **Step 2: 运行 Vitest 确认失败**

Run: `cd frontend && pnpm exec vitest run src/api/__tests__/modelTest.spec.ts src/api/__tests__/videoTestTasks.spec.ts`

Expected: FAIL，原因是新 API 不存在且旧实现仍等待轮询。

- [ ] **Step 3: 实现任务 API 并移除客户端总超时**

删除 `testVideoGeneration` 内部轮询和 `timeoutMs` 语义；保留现有状态提取工具供任务显示使用。创建请求成功即返回 `{ payload, requestID }`。

- [ ] **Step 4: 运行 API 测试确认通过**

Run: `cd frontend && pnpm exec vitest run src/api/__tests__/modelTest.spec.ts src/api/__tests__/videoTestTasks.spec.ts`

Expected: PASS。

- [ ] **Step 5: 提交前端 API**

```bash
git add frontend/src/api/modelTest.ts frontend/src/api/videoTestTasks.ts frontend/src/api/__tests__/modelTest.spec.ts frontend/src/api/__tests__/videoTestTasks.spec.ts
git commit -m "feat: 接入视频测试任务接口"
```

### Task 6: 模型测试台任务记录界面

**Files:**
- Modify: `frontend/src/views/user/ModelTestView.vue`
- Modify: `frontend/src/views/user/__tests__/ModelTestView.spec.ts`
- Modify: `frontend/src/i18n/locales/zh.ts`
- Modify: `frontend/src/i18n/locales/en.ts`

**Interfaces:**
- Consumes: Task 5 的任务 API。
- Produces: 视频任务列表、选中任务详情、可见性轮询、播放下载和删除交互。

- [ ] **Step 1: 写页面行为失败测试**

测试断言：创建成功后提交按钮立即恢复；新任务置顶；刷新页面加载历史；只轮询非终态；隐藏页面停止轮询；恢复可见立即补查；查询错误显示等待提示而不触发失败 Toast；完成任务显示视频和下载操作。

- [ ] **Step 2: 运行页面测试确认失败**

Run: `cd frontend && pnpm exec vitest run src/views/user/__tests__/ModelTestView.spec.ts`

Expected: FAIL，原因是任务列表和可见性轮询尚未实现。

- [ ] **Step 3: 实现任务列表和状态详情**

任务列表使用稳定列宽和移动端纵向布局；轮询间隔五秒；组件卸载和页面隐藏时清理定时器及请求控制器；播放 URL 指向登录态内容接口。

- [ ] **Step 4: 运行页面、类型和 lint 检查**

Run: `cd frontend && pnpm exec vitest run src/views/user/__tests__/ModelTestView.spec.ts && pnpm run typecheck && pnpm run lint:check`

Expected: PASS，且无新增警告。

- [ ] **Step 5: 提交页面**

```bash
git add frontend/src/views/user/ModelTestView.vue frontend/src/views/user/__tests__/ModelTestView.spec.ts frontend/src/i18n/locales/zh.ts frontend/src/i18n/locales/en.ts
git commit -m "feat: 展示视频测试任务记录"
```

### Task 7: 文档、全量验证与 dev/staging 发布

**Files:**
- Modify: `docs/api/openai-video.md` 或仓库中实际的视频 API 文档
- Modify: `docs/superpowers/specs/2026-07-19-model-test-video-task-history-design.md`（仅在实现与设计有必要差异时同步）

- [ ] **Step 1: 更新当前行为文档**

记录内部测试台任务标记不属于公开 OpenAI 协议、用户任务接口、无总超时状态语义、30 天终态保留和不转存视频的限制。

- [ ] **Step 2: 运行后端与前端完整验证**

Run: `PATH="$(go env GOPATH)/bin:$PATH" make test`

Expected: PASS；若仓库基线存在无关失败，必须逐项与本次 worktree 基线比较并记录。

Run: `make build`

Expected: PASS。

- [ ] **Step 3: 检查 diff、提交并推送功能分支**

```bash
git status --short
git diff --check
git diff origin/dev...HEAD --stat
git add docs
git commit -m "docs: 说明视频测试任务记录"
git push -u origin codex/model-test-video-tasks
```

- [ ] **Step 4: 合入并推送 dev**

在确认功能分支测试通过后，将其快进或无冲突合入 `dev`，推送 `origin/dev`；生产构建不得来自未推送提交。

- [ ] **Step 5: 正式 VPS 隔离 staging 验证**

在 `/opt/sub2api/repo` 拉取已推送 `dev` commit，以 `sub2api:staging-<commit>` 构建镜像并启动 `sub2api-staging`。验证迁移、`127.0.0.1:18080/health`、任务创建即落库、刷新恢复、旧超时后仍等待、完成播放下载、日志和容器重启次数。

- [ ] **Step 6: 按生产门禁执行同 commit 切换或保留 staging**

只有满足仓库要求的明确生产确认时，才将已验证 commit 合入 `main` 并把 prod 切换到同一镜像；否则保留 staging 结果和回滚信息，不擅自切换生产。
