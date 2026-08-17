# 视频任务计费空值与失败释放修复 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复视频任务空上游 ID 冲突、失败状态 SQL 类型冲突和可空字段扫描失败，使并发预留、失败释放及后台对账恢复正常。

**Architecture:** 保持 `service.VideoTaskBilling` 的字符串领域接口不变，只在 PostgreSQL 仓储边界转换可空值。终态判断在 Go 中计算为布尔参数，避免 PostgreSQL 对同一字符串参数做冲突类型推断；真实 PostgreSQL 集成测试覆盖 SQL 驱动层无法模拟的行为。

**Tech Stack:** Go 1.26.6、`database/sql`、PostgreSQL 18、`go-sqlmock`、Testcontainers、Testify。

---

### Task 1: 锁定空 ID 写入与可空字段读取

**Files:**
- Modify: `backend/internal/repository/video_task_billing_repo_test.go`
- Modify: `backend/internal/repository/video_task_billing_repo.go`

- [ ] **Step 1: 修改预留测试并新增可空扫描失败用例**

把 `TestVideoTaskBillingRepositoryReserveAndCreateMovesBalanceBeforeInsert` 的第二个 INSERT 参数期望从 `""` 改为 `nil`。新增 `GetByID` 用例，让 `videoTaskBillingRows()` 返回 `upstream_task_id=nil` 和 `last_poll_error=nil`，并断言读取成功且领域字段为空字符串。新增审核列表用例，以相同 NULL 数据验证 `scanVideoTaskReviewItem`。

```go
mock.ExpectQuery(`(?s)INSERT INTO video_task_billings.*RETURNING id, created_at, updated_at`).
    WithArgs(
        "request-1", nil, "openai", int64(7), int64(11), int64(13), int64(17),
        "video-model", "video-model", "", 0, 0, sqlmock.AnyArg(),
        1.25, service.VideoTaskStatusSubmitting, service.VideoTaskBillingReserved,
        sqlmock.AnyArg(), nil, time.Time{},
    )

require.Empty(t, task.UpstreamTaskID)
require.Empty(t, task.LastPollError)
```

- [ ] **Step 2: 运行测试并确认 RED**

```bash
cd backend
go test ./internal/repository -run 'TestVideoTaskBillingRepository(ReserveAndCreateMovesBalanceBeforeInsert|GetByIDHandlesNullableText|ListReviewTasksHandlesNullableText)$' -count=1
```

Expected: FAIL；预留测试报告参数 2 实际为 `""`，读取测试报告不能把 NULL 转成 string。

- [ ] **Step 3: 实现数据库边界空值转换**

在 `ReserveAndCreate` 的 INSERT 前构造参数：

```go
var upstreamTaskID any
if value := strings.TrimSpace(task.UpstreamTaskID); value != "" {
    upstreamTaskID = value
}
```

INSERT 参数使用 `upstreamTaskID`。在 `scanVideoTaskBilling` 和 `scanVideoTaskReviewItem` 中分别声明两个 `sql.NullString`，扫描 `upstream_task_id` 与 `last_poll_error`，扫描成功后仅在 `Valid` 时赋值给领域对象。

- [ ] **Step 4: 运行测试并确认 GREEN**

```bash
cd backend
go test ./internal/repository -run 'TestVideoTaskBillingRepository(ReserveAndCreateMovesBalanceBeforeInsert|GetByIDHandlesNullableText|ListReviewTasksHandlesNullableText)$' -count=1
```

Expected: PASS。

### Task 2: 修复失败状态更新参数类型

**Files:**
- Modify: `backend/internal/repository/video_task_billing_repo_test.go`
- Modify: `backend/internal/repository/video_task_billing_repo.go`

- [ ] **Step 1: 修改 UpdatePoll 测试锁定布尔终态参数**

把 SQL 期望从 `billing_status = $8` 改为 `$7`，参数改为：

```go
WithArgs(
    int64(9), service.VideoTaskStatusUnknown, nil, "upstream timeout", sqlmock.AnyArg(),
    false, service.VideoTaskBillingReserved,
)
```

再新增失败状态成功返回用例，期望第六个参数为 `true`，返回一行 `task_status=failed`。

- [ ] **Step 2: 运行测试并确认 RED**

```bash
cd backend
go test ./internal/repository -run 'TestVideoTaskBillingRepositoryUpdatePoll' -count=1
```

Expected: FAIL；旧 SQL 仍传入 completed/failed 两个字符串参数。

- [ ] **Step 3: 使用独立布尔参数更新 terminal_at**

在 `UpdatePoll` 中计算：

```go
terminal := outcome.Status == service.VideoTaskStatusCompleted || outcome.Status == service.VideoTaskStatusFailed
```

SQL 和参数改为：

```sql
terminal_at = CASE WHEN $6 THEN NOW() ELSE terminal_at END
WHERE id = $1 AND billing_status = $7
```

```go
id, outcome.Status, response, outcome.ErrorMessage, nextPollAt,
terminal, service.VideoTaskBillingReserved
```

- [ ] **Step 4: 运行测试并确认 GREEN**

```bash
cd backend
go test ./internal/repository -run 'TestVideoTaskBillingRepositoryUpdatePoll' -count=1
```

Expected: PASS。

### Task 3: 真实 PostgreSQL 回归

**Files:**
- Create: `backend/internal/repository/video_task_billing_repo_integration_test.go`

- [ ] **Step 1: 编写集成测试**

使用现有 `integration_harness_test.go` 和 fixtures 创建独立用户、分组、API Key、账号。通过 `NewVideoTaskBillingRepository(integrationDB)` 连续预留两条空上游 ID 任务并断言数据库均为 NULL；把第一条更新为失败并调用 `Release`，断言余额恢复；调用 `ClaimDue` 读取第二条，验证 NULL 扫描成功。测试清理顺序固定为任务、API Key、账号、用户、分组。

```go
func TestVideoTaskBillingRepositoryNullableReservationAndFailureRelease(t *testing.T) {
    ctx := context.Background()
    suffix := time.Now().UnixNano()
    group := mustCreateGroup(t, integrationEntClient, &service.Group{
        Name: fmt.Sprintf("video-billing-group-%d", suffix), Platform: service.PlatformOpenAI,
    })
    user := mustCreateUser(t, integrationEntClient, &service.User{
        Email: fmt.Sprintf("video-billing-%d@example.com", suffix), Balance: 20,
    })
    account := mustCreateAccount(t, integrationEntClient, &service.Account{
        Name: fmt.Sprintf("video-billing-account-%d", suffix),
        Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
    })
    groupID := group.ID
    apiKey := mustCreateApiKey(t, integrationEntClient, &service.APIKey{
        UserID: user.ID, GroupID: &groupID,
        Key: fmt.Sprintf("sk-video-billing-%d", suffix),
    })
    t.Cleanup(func() {
        _, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM video_task_billings WHERE user_id=$1", user.ID)
        _, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM api_keys WHERE id=$1", apiKey.ID)
        _, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
        _, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE id=$1", account.ID)
        _, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM users WHERE id=$1", user.ID)
        _, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM groups WHERE id=$1", group.ID)
    })

    repo := NewVideoTaskBillingRepository(integrationDB)
    deadline := time.Now().Add(-time.Minute)
    newTask := func(requestID string) *service.VideoTaskBilling {
        return &service.VideoTaskBilling{
            RequestID: requestID, Platform: service.PlatformOpenAI,
            UserID: user.ID, APIKeyID: apiKey.ID, GroupID: &groupID, AccountID: account.ID,
            Model: "sd4-seedance-2.0", UpstreamModel: "sd4-seedance-2.0",
            EstimatedCost: 1.25, TaskStatus: service.VideoTaskStatusSubmitting,
            BillingStatus: service.VideoTaskBillingReserved,
            NextPollAt: deadline, SubmissionDeadline: &deadline,
        }
    }
    first := newTask(fmt.Sprintf("video-first-%d", suffix))
    second := newTask(fmt.Sprintf("video-second-%d", suffix))
    require.NoError(t, repo.ReserveAndCreate(ctx, first))
    require.NoError(t, repo.ReserveAndCreate(ctx, second))

    var nullCount int
    require.NoError(t, integrationDB.QueryRowContext(ctx,
        "SELECT COUNT(*) FROM video_task_billings WHERE id IN ($1,$2) AND upstream_task_id IS NULL",
        first.ID, second.ID).Scan(&nullCount))
    require.Equal(t, 2, nullCount)

    failed, err := repo.UpdatePoll(ctx, first.ID, service.VideoTaskOutcome{
        Status: service.VideoTaskStatusFailed, ErrorMessage: "upstream rejected request",
    }, time.Now())
    require.NoError(t, err)
    require.NoError(t, repo.Release(ctx, failed.ID, failed.LastPollError))

    var balance, frozen float64
    require.NoError(t, integrationDB.QueryRowContext(ctx,
        "SELECT balance, frozen_balance FROM users WHERE id=$1", user.ID).Scan(&balance, &frozen))
    require.InDelta(t, 18.75, balance, 0.00000001)
    require.InDelta(t, 1.25, frozen, 0.00000001)

    due, err := repo.ClaimDue(ctx, 10, time.Minute)
    require.NoError(t, err)
    require.Len(t, due, 1)
    require.Equal(t, second.ID, due[0].ID)
    require.Empty(t, due[0].UpstreamTaskID)
    require.Empty(t, due[0].LastPollError)
}
```

- [ ] **Step 2: 验证测试能捕获旧行为**

先运行修复版确保测试编译，再临时把测试针对的一个实现点恢复为旧行为运行一次。Expected: FAIL；随后立即恢复修复，不得提交临时回退。

- [ ] **Step 3: 运行真实 PostgreSQL 集成测试**

```bash
cd backend
go test -tags integration ./internal/repository -run TestVideoTaskBillingRepositoryNullableReservationAndFailureRelease -count=1 -v
```

Expected: Testcontainers 启动 PostgreSQL/Redis，测试 PASS。Docker 不可用时明确报告，不能以单元测试代替。

### Task 4: 回归验证与提交

**Files:**
- Modify: `backend/internal/repository/video_task_billing_repo.go`
- Modify: `backend/internal/repository/video_task_billing_repo_test.go`
- Create: `backend/internal/repository/video_task_billing_repo_integration_test.go`

- [ ] **Step 1: 格式化并运行目标测试**

```bash
cd backend
gofmt -w internal/repository/video_task_billing_repo.go internal/repository/video_task_billing_repo_test.go internal/repository/video_task_billing_repo_integration_test.go
go test ./internal/repository -run 'VideoTaskBilling' -count=1
go test ./internal/service -run 'VideoTask|OpenAIVideo' -count=1
```

Expected: 全部 PASS。

- [ ] **Step 2: 运行完整后端测试**

```bash
cd backend
go test ./... -count=1
```

Expected: 全部 PASS。

- [ ] **Step 3: 检查影响范围**

```bash
git diff --check
git status --short
git diff -- backend/internal/repository/video_task_billing_repo.go backend/internal/repository/video_task_billing_repo_test.go backend/internal/repository/video_task_billing_repo_integration_test.go
```

Expected: 仅包含本次仓储修复和测试；`AGENTS.md`、`docs/SOURCE_DEPLOY_CN.md` 的既有改动保持未暂存。

- [ ] **Step 4: 提交修复**

```bash
git add backend/internal/repository/video_task_billing_repo.go \
  backend/internal/repository/video_task_billing_repo_test.go \
  backend/internal/repository/video_task_billing_repo_integration_test.go
git commit -m "fix: 修复视频任务计费空值与失败释放"
```

Expected: 提交只包含三个目标文件，不推送、不触发 staging 或 prod。
