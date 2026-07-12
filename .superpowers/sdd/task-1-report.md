# 任务 1 报告：连续签到领域模型与历史兼容计算

## 实现内容

- 在 `CheckinRecord` 和 `CheckinMonthSummary` 增加 `ConsecutiveCount`，JSON 字段均为 `consecutive_count`。
- 签到时读取昨天记录计算本次连续天数；昨天缺失时从 1 开始。
- 对连续值为 0 的历史记录按相邻日期递归回溯计算，不回写记录，也不改变历史奖励。
- 第 4 天和第 16 天额外奖励、下一档里程碑均改为依据连续天数；`MonthCount` 继续仅表示当月签到次数。
- 概览在今天已签到时取今天记录；今天未签但昨天已签时保留昨天连续值；今天和昨天均未签时返回 0。

## 验证证据

- RED：`go test ./internal/service -run 'TestCheckin'` 在实现前因缺少 `ConsecutiveCount` 和连续计算方法失败。
- GREEN：`go test ./internal/service -run 'TestCheckin' -count=1` 通过。
- 回归：`go test ./internal/service -count=1` 通过。
- 静态检查：`git diff --check` 通过。

## 影响范围与后续依赖

- 本任务仅修改服务领域模型与服务逻辑，未修改 Ent、仓储映射、数据库迁移、前端或文档。
- 在任务 2 接入数据库列和仓储映射前，持久化记录仍不会保存新增连续值；服务运行时的值为默认 0，并由兼容回溯逻辑处理。
- 连续值为 0 的历史数据链很长时会产生逐日查询；这是兼容期行为，新签到记录持久化后可在连续链上提前终止回溯。
