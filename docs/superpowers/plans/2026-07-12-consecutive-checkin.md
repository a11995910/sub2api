# 连续签到奖励实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将签到额外奖励改为跨自然月累计、断签重置的连续签到奖励。

**Architecture:** 在签到记录保存每次签到完成后的连续天数。服务在事务内读取昨天记录，必要时从历史日期连续性补算旧记录的连续天数，并只以连续值判断第 4/16 天奖励；月度统计保留原用途。

**Tech Stack:** Go、Ent、PostgreSQL 迁移、Vue 3、TypeScript、Vitest。

## 全局约束

- 连续日依据服务端业务时区，允许跨自然月，漏签即重置。
- 不回写或变更历史奖励金额。
- 新增字段默认值为 0；0 表示需兼容补算的历史记录。
- `dev` 是唯一部署目标；不得合并 `main` 或发布正式环境。

---

### 任务 1：连续签到领域模型与历史兼容计算

**文件：**
- 修改：`backend/internal/service/checkin.go`
- 修改：`backend/internal/service/checkin_service.go`
- 新建：`backend/internal/service/checkin_service_test.go`

**接口：**
- `CheckinRecord.ConsecutiveCount int`：本次签到后的连续天数。
- `CheckinMonthSummary.ConsecutiveCount int`：截至今日的连续天数。
- `checkinExtraReward(settings CheckinSettings, consecutiveCount int)`：按连续天数返回额外奖励。

- [ ] 编写失败测试，覆盖连续第 4 天和第 16 天奖励、断签重置、跨月连续、旧记录 `consecutive_count=0` 的回溯补算。
- [ ] 运行 `go test ./internal/service -run 'TestCheckin'`，确认测试因缺少连续字段和计算逻辑失败。
- [ ] 实现连续天数计算：仅查询昨天记录；旧记录值为 0 时按日期连续回溯；当昨天不存在时返回 1。
- [ ] 将额外奖励和下一档奖励改为以连续天数判断，并在概览中计算当前连续值。
- [ ] 运行 `go test ./internal/service -run 'TestCheckin'`，确认新增用例通过。

### 任务 2：仓储、Ent 与数据库迁移

**文件：**
- 修改：`backend/ent/schema/checkin_record.go`
- 修改：`backend/internal/repository/checkin_record_repo.go`
- 修改：`backend/migrations/130_add_checkin_records.sql`
- 新建：`backend/migrations/175_add_checkin_consecutive_count.sql`
- 生成：`backend/ent/**`

**接口：**
- 仓储读取和写入 `CheckinRecord.ConsecutiveCount`。
- 新迁移为 `checkin_records.consecutive_count INTEGER NOT NULL DEFAULT 0`。

- [ ] 编写或扩展仓储测试，断言连续值在创建和查询后保持一致。
- [ ] 运行目标测试，确认新增字段未接入时失败。
- [ ] 在 Ent schema、仓储映射和新迁移中接入 `consecutive_count`，生成 Ent 代码。
- [ ] 运行 `go test ./internal/repository ./internal/service`，确认仓储与服务测试通过。

### 任务 3：前端契约、展示与文案

**文件：**
- 修改：`frontend/src/api/checkin.ts`
- 修改：`frontend/src/views/user/CheckinView.vue`
- 修改：`frontend/src/i18n/locales/zh.ts`
- 修改：`frontend/src/i18n/locales/en.ts`
- 修改：`frontend/src/views/user/__tests__/CheckinView.spec.ts`
- 修改：`frontend/src/views/admin/SettingsView.vue`（仅在展示文案存在月度含义时）

**接口：**
- 前端读取 `record.consecutive_count` 与 `summary.consecutive_count`。
- `next_extra_milestone` 对应下一连续奖励档位。

- [ ] 编写失败的视图测试，断言下一档提示使用连续天数，规则显示“连续第 4/16 天”。
- [ ] 运行 `./node_modules/.bin/vitest run src/views/user/__tests__/CheckinView.spec.ts`，确认测试因旧文案或旧计数失败。
- [ ] 更新 API 类型、规则与下一档展示，并更新中英文文案。
- [ ] 再次运行目标 Vitest 测试，确认通过。

### 任务 4：文档、完整验证与测试环境部署

**文件：**
- 修改：`docs/CHECKIN_CN.md`
- 修改：`docs/superpowers/specs/2026-07-12-consecutive-checkin-design.md`（仅修正实现后的事实差异）

- [ ] 更新签到说明中的奖励条件、跨月规则、断签规则、接口字段和旧数据兼容方式。
- [ ] 运行 `go test ./internal/service ./internal/repository` 与 `./node_modules/.bin/vitest run src/views/user/__tests__/CheckinView.spec.ts`。
- [ ] 检查 `git diff --check` 与 `git diff`，提交中文提交说明并推送 `origin/dev`。
- [ ] 登录测试 VPS `/opt/sub2api-src`，确认工作区干净、以 `git pull --ff-only origin dev` 获取完全相同的提交，在 VPS 构建并按现有测试环境运行方式部署。
- [ ] 验证测试环境的健康检查、签到概览接口、重复签到冲突和容器日志；报告测试地址、部署提交与未覆盖的真实用户奖励场景。
