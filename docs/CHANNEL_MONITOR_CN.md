# 渠道监控功能说明

## 功能入口与权限

渠道监控用于检查上游模型可用性，或读取已接入账号的用量与余额。管理员在后台“渠道监控”页面创建、编辑、复制、启停、立即运行和查看历史；登录用户只能查看已启用监控的只读状态。功能受 `channel_monitor_enabled` 和 `channel_monitor_mode` 控制：主动监控使用 `v1`，`v2` 提供基于真实请求聚合的被动视图。

管理端主动监控接口：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` / `POST` | `/api/v1/admin/channel-monitors` | 分页查询或创建监控。 |
| `GET` / `PUT` / `DELETE` | `/api/v1/admin/channel-monitors/:id` | 查询、更新或删除监控。 |
| `POST` | `/api/v1/admin/channel-monitors/:id/duplicate` | 幂等复制监控配置。 |
| `POST` | `/api/v1/admin/channel-monitors/:id/run` | 立即执行一次检查。 |
| `GET` | `/api/v1/admin/channel-monitors/:id/history` | 查询检查历史。 |
| `GET` / `POST` | `/api/v1/admin/channel-monitor-templates` | 查询或创建请求模板；详情接口支持更新、删除、查看关联监控和应用模板。 |

用户只读接口为 `GET /api/v1/channel-monitors` 和 `GET /api/v1/channel-monitors/:id/status`。管理端始终可以读取配额快照；用户端仅在 `channel_monitor_show_quota=true` 时返回 `latest_quota`。

## 主动监控模式

每条监控通过 `check_mode` 选择执行方式：

| 模式 | 行为 | 必填数据 |
| --- | --- | --- |
| `probe` | 调用上游模型并校验响应，默认模式。 | HTTPS origin、API Key、主模型。 |
| `quota` | 只读取关联账号的用量或余额，不产生 LLM 请求费用。 | 同平台账号；端点和 API Key 可为空。 |
| `quota_probe` | 同时执行模型探活和账号配额检查，配额快照挂到主模型历史。 | 探活字段和同平台账号。 |

监控支持 `openai`、`anthropic`、`gemini`、`grok`、`antigravity`、`kimi`、`zhipu`、`deepseek`。Antigravity 没有通用探活适配器，只允许 `quota`；其余平台可使用探活和配额组合。`responses` 探活只允许 OpenAI，其他平台使用 `chat_completions`。Kimi、智谱和 DeepSeek 探活复用各自的 OpenAI 兼容端点。

创建和更新时，服务端校验关联账号存在且 `accounts.platform` 与监控 provider 一致。配额模式未关联账号会被拒绝；关联账号删除后，外键把 `account_id` 置空并保留监控，后续运行显示账号未关联。探活端点必须是 HTTPS origin，不允许路径、查询、fragment、localhost、私网、链路本地或元数据地址；API Key 加密保存，列表只返回脱敏值。

检测间隔允许 `15~3600` 秒，抖动必须为非负数且 `interval_seconds - jitter_seconds` 不得低于 15 秒。调度器最多并发执行 5 条监控。请求错误消息和响应片段有长度限制，认证信息在响应与日志中脱敏。

## 配额与余额来源

配额模式复用账号管理的用量服务，并归一为 `MonitorQuotaSnapshot`：

- OpenAI、Anthropic、Gemini、Antigravity、Grok：读取账号已有的用量窗口。
- Kimi Coding Plan：读取 5 小时和周用量窗口。
- 智谱 Coding Plan：读取 5 小时和周用量窗口及套餐等级。
- Kimi 按量账号：读取 CNY 可用余额。
- DeepSeek 按量账号：读取余额、币种明细和上游可用状态。
- 智谱按量账号没有公开余额端点，检查结果返回错误；DeepSeek 不支持 Coding Plan。

同一账号的并发读取由 singleflight 合并。成功快照缓存 5 分钟，失败快照负缓存 60 秒，避免多个最短间隔监控反复请求上游；单次配额抓取总超时为 45 秒。抓取失败仍会生成 `success=false` 快照并写入历史，便于区分凭据、网络、解析和配置错误。

检查状态按以下规则生成：

- 成功且所有窗口低于 90%、余额大于 0：`operational`。
- 任一窗口使用率达到 90%、余额耗尽或关联账号缺失：`degraded`。
- 上游 `401/403` 等凭据失效：`failed`。
- 网络、解析和其他系统错误：`error`。

## 数据与状态流转

核心数据表和字段：

- `channel_monitors`：保存 provider、端点、加密 API Key、模型、调度参数、`check_mode` 和 `account_id`。账号删除时 `account_id` 使用 `ON DELETE SET NULL`。
- `channel_monitor_histories`：保存每次模型状态、延迟、消息和 `quota` JSONB 快照；主动监控明细保留 30 天。
- `channel_monitor_request_templates`：保存可复用的 provider、请求头和请求体模板。
- `settings`：保存 `channel_monitor_enabled`、`channel_monitor_mode`、默认间隔、吞吐量隐藏和 `channel_monitor_show_quota`。
- `accounts.credentials`：国产供应商账号通过 `account_mode`、`api_protocol`、`base_url` 和 `api_key` 决定读取方式；成功的额度或余额探测写入 `accounts.extra` 快照。

监控启用后由调度器按间隔运行，立即运行和后台调度走同一校验与结果持久化链路。删除监控时关联历史通过外键级联清理。请求模板只复制请求配置，不绕过端点安全、模式、账号平台和凭据校验。

## 异常与兼容

- 旧监控缺少 `check_mode` 时按 `probe` 处理，旧客户端不提交新字段时保留原行为。
- `quota` 模式未填写主模型时使用内部占位模型 `quota`，保证历史和时间线结构一致。
- 配额抓取器或设置服务不可用时采用失败关闭，不会静默创建不可运行的配额监控，也不会向用户端泄露快照。
- 上游探测失败不会删除配置；最近一次成功配额快照不会被账号页的失败探测覆盖，但监控历史仍记录本次失败结果。
- `channel_monitor_show_quota=false` 只影响用户接口和页面，管理端排障数据保持完整。

## 验证范围

自动化测试应覆盖 provider 与模式矩阵、关联账号平台校验、账号删除置空、端点 SSRF 防护、API Key 加解密失败、配额与余额响应解析、缓存和并发合并、90% 阈值、用户端展示开关、历史快照持久化以及模板应用。staging 验收使用隔离账号，至少验证一条 `probe`、一条 `quota` 和一条 `quota_probe`，并确认关闭用户展示开关后接口不再返回 `latest_quota`。
