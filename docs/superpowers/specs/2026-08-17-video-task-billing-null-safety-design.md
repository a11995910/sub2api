# 视频任务计费空值与失败释放修复设计

## 结论

本次仅修复 Sub2API 本地代码，不连接、修改或重启生产环境。修复聚焦视频任务计费仓储边界：尚未取得上游任务 ID 时向 PostgreSQL 写入 `NULL`，读取可空文本时安全还原为空字符串，修正失败状态更新 SQL 的参数类型冲突，并以回归测试覆盖连续预留、失败释放和后台对账读取。

`sd5-seedance-2.0` 不在现有沧元协议、渠道定价和账号映射中。本次不猜测其含义，也不新增该模型；该配置问题与 `sd4-*` 的计费故障分开处理。

## 已确认根因

生产只读证据表明存在三项相互叠加的缺陷：

1. `ReserveAndCreate` 把领域对象的空 `UpstreamTaskID` 直接写为 `''`。部分唯一索引会把 `(platform, '')` 视为有效键，导致第二条任务插入冲突。
2. `UpdatePoll` 的 `$2` 同时参与 `VARCHAR` 列赋值和未显式定型的 `IN` 表达式，PostgreSQL 报 `inconsistent types deduced for parameter $2`，明确的上游 4xx 因此无法进入失败释放。
3. `scanVideoTaskBilling` 和审核列表扫描把数据库可空的 `upstream_task_id`、`last_poll_error` 直接扫描到 Go `string`。后台对账遇到 `NULL` 后持续失败；若只修复写入 `NULL`，读取路径会立即暴露同类错误。

当前生产有一条历史任务处于 `submitting/reserved`，冻结金额为 `6.86`。它的清理和余额释放必须在代码经 staging 验证后另行执行，并继续受生产数据操作门禁约束。

## 方案选择

### 采用：仓储边界最小修复

- 领域模型继续使用 `string`，避免把 `*string` 或 `sql.NullString` 扩散到 handler、service、审核页面和使用记录。
- `ReserveAndCreate` 在 SQL 参数边界将空白上游任务 ID 转换为 `nil`，非空值先去除首尾空白再写入。
- 两个数据库扫描函数使用局部 `sql.NullString` 接收 `upstream_task_id` 和 `last_poll_error`，有效时赋值，无值时保留领域对象零值。
- `UpdatePoll` 在 Go 中计算终态布尔值，SQL 只使用独立布尔参数决定 `terminal_at`，避免同一个字符串参数跨上下文推断类型。

### 不采用：领域模型整体改为可空类型

语义最显式，但会扩大接口、JSON、服务和测试改动，不符合当前故障的最小修复目标。

### 不采用：查询中统一 `COALESCE(column, '')`

改动较小，但把数据库空值语义隐藏在 SQL 字符串中，容易遗漏新的查询和审核扫描路径，也不能解决写入及 `UpdatePoll` 类型冲突。

## 数据流与错误处理

新任务预留时，余额移动到冻结余额与任务插入仍在同一事务内。插入使用 `upstream_task_id=NULL`，因此多条尚未获得上游 ID 的任务可以并存；插入失败仍由事务完整回滚。

上游明确失败时，`UpdatePoll` 先把任务标记为失败，再由现有 `Release` 事务把冻结金额退回可用余额。上游结果不确定时继续进入人工审核，不改变现有防止误退费的策略。后台对账可以读取可空字段，并在提交截止后继续处理历史任务。

本次不改变共享的 `billingErrorDetails` HTTP 状态映射。该函数影响所有计费入口，若要把未知内部错误从 403 改为 500，应单独盘点已有业务错误类型后再修改。

## 测试设计

1. 仓储预留测试要求空 `UpstreamTaskID` 以 `nil` 传入 INSERT；旧实现应因参数为 `''` 而失败。
2. 连续两条无上游任务 ID 的 PostgreSQL 集成测试，两条均能写入且数据库值为 `NULL`。
3. 扫描测试同时返回 `upstream_task_id=NULL` 和 `last_poll_error=NULL`，仓储读取及审核列表均成功。
4. `UpdatePoll` 测试要求传入独立终态布尔参数；失败状态更新后可继续执行 `Release`。
5. 运行 repository、video task billing、video reconciliation 相关测试及完整后端测试；没有可用本地 PostgreSQL 时，必须明确报告集成测试未运行，不得用 sqlmock 结果冒充数据库验证。

## 发布与数据恢复边界

本地修复完成后只提交并推送代码，不自动触发 staging 或 prod。后续 staging 应使用独立数据库验证：并发预留、上游 4xx 释放、任务创建后绑定上游 ID、对账处理超时任务。

生产修复必须另行取得明确授权，并在操作前重新备份和核对余额不变量。历史任务不得直接删除；应先把空上游 ID 转换为 `NULL`，再通过经过验证的失败释放流程退回冻结金额。`sd5` 仅在沧元正式文档确认、渠道价格配置和账号模型映射同时具备后才允许加入。
