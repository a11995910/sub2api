# 异步图片任务复用本地图片存储实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 取消 S3/R2 作为异步图片任务启用前提，复用现有 `generated-images` 本地持久化 URL，并阻止异步 Base64 结果进入 Redis。

**Architecture:** 异步请求解析阶段把缺省结果格式规范化为 `url`，显式 `b64_json` 直接拒绝；Worker 仍复用同步图片 Handler，由现有 `GeneratedImageStore` 保存图片。任务服务只依赖 Redis 任务仓库即可启用，对象存储 resolver 仅在已配置时提供可选转存。

**Tech Stack:** Go、Gin、Redis、testify、现有 GeneratedImageStore。

---

### Task 1: 固定异步 URL 结果协议

**Files:**
- Modify: `backend/internal/service/image_task_request_test.go`
- Modify: `backend/internal/service/image_task_request.go`
- Modify: `backend/internal/handler/image_task_generation_handler_test.go`
- Modify: `backend/internal/handler/image_task_handler.go`

- [ ] **Step 1: 写失败测试**

验证异步缺省格式被写为 `url`、显式 `url` 保留、显式 `b64_json` 返回 `ErrUnsupportedImageTaskResponseFormat`；Handler 对该错误返回 HTTP 400 和 `ASYNC_RESPONSE_FORMAT_UNSUPPORTED`。

```go
func TestParseImageTaskRequestRejectsAsyncBase64(t *testing.T) {
    _, err := ParseImageTaskRequest([]byte(`{"async":true,"client_request_id":"req_1","prompt":"dog","response_format":"b64_json"}`))
    require.ErrorIs(t, err, ErrUnsupportedImageTaskResponseFormat)
}
```

- [ ] **Step 2: 运行测试并确认 RED**

```bash
cd backend
go test ./internal/service -run 'TestParseImageTaskRequest' -count=1
go test ./internal/handler -run 'TestAsyncImageHandler' -count=1
```

预期：缺省格式断言失败，拒绝错误尚未定义。

- [ ] **Step 3: 最小实现**

在 `ParseImageTaskRequest` 的 `async` 分支规范化 `response_format`：缺失或空字符串写为 `url`，`url` 保留，其他值返回专用错误。Handler 对专用错误返回稳定错误码。

- [ ] **Step 4: 运行目标测试并确认 GREEN**

```bash
cd backend
go test ./internal/service -run 'TestParseImageTaskRequest' -count=1
go test ./internal/handler -run 'TestAsyncImageHandler' -count=1
```

预期：PASS。

### Task 2: 取消对象存储启用门槛

**Files:**
- Modify: `backend/internal/service/image_task_test.go`
- Modify: `backend/internal/service/image_task.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/internal/handler/image_task_admin_toggle_test.go`

- [ ] **Step 1: 写失败测试**

验证 `NewImageTaskServiceWithResolver` 在 resolver 返回 `(nil,false)` 时仍然 `Enabled()==true`，且合法 URL 结果可直接保存为 `succeeded`；HTTP 测试验证未配置对象存储也能返回 202。

```go
func TestImageTaskServiceEnabledWithoutObjectStorage(t *testing.T) {
    svc := NewImageTaskServiceWithResolver(&imageTaskMemoryStore{}, func() (*ImageResultUploader, bool) {
        return nil, false
    }, time.Hour, time.Minute)
    require.True(t, svc.Enabled())
}
```

- [ ] **Step 2: 运行测试并确认 RED**

```bash
cd backend
go test ./internal/service ./internal/handler -run 'TestImageTaskServiceEnabledWithoutObjectStorage|TestAsyncImageEnablesWithoutRestart' -count=1
```

预期：无对象存储时仍被判定为禁用。

- [ ] **Step 3: 最小实现**

让 `Enabled()` 仅检查任务 store；`CompleteGeneration` 在没有 uploader 时直接保存同步 Handler 已生成的 URL，在 uploader 可用时继续执行原有转存。删除 resolver 关闭时强制写 `RESULT_STORAGE_FAILED` 的分支，并更新 wire 注释和后台切换测试语义。

- [ ] **Step 4: 运行目标测试并确认 GREEN**

```bash
cd backend
go test ./internal/service ./internal/handler -run 'ImageTask|AsyncImage' -count=1
```

预期：PASS。

### Task 3: 文档与完整验证

**Files:**
- Modify: `docs/ASYNC_IMAGE_TASKS_CN.md`
- Modify: `docs/ASYNC_IMAGE_TASKS.md`

- [ ] **Step 1: 更新当前行为文档**

说明异步默认复用 `generated-images`、本地目录需要持久化挂载、S3/R2 为多实例可选增强、异步只支持 URL 结果。

- [ ] **Step 2: 运行格式和完整测试**

```bash
cd backend
gofmt -w internal/service/image_task_request.go internal/service/image_task_request_test.go internal/service/image_task.go internal/service/image_task_test.go internal/handler/image_task_handler.go internal/handler/image_task_generation_handler_test.go internal/handler/image_task_admin_toggle_test.go
go test ./...
go test -race ./internal/service ./internal/repository -run 'ImageTask' -count=1
cd ..
git diff --check
```

预期：全部 PASS，`git diff --check` 无输出。

- [ ] **Step 3: 检查差异并提交**

```bash
git status --short
git diff --stat
git commit -m "fix: 异步生图复用本地图片存储"
```
