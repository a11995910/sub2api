# 风控功能说明

## 功能入口

管理员在后台 `风控` 页面维护内容审计配置、查看运行态和审计日志。后端接口位于 `/api/v1/admin/risk-control`，需要管理员权限。

| 入口 | 说明 |
| --- | --- |
| `GET /api/v1/admin/risk-control/config` | 读取内容审计配置。 |
| `PUT /api/v1/admin/risk-control/config` | 保存内容审计配置。 |
| `GET /api/v1/admin/risk-control/status` | 读取运行态状态、队列、worker、前置拦截和审计 API Key 负载。 |
| `POST /api/v1/admin/risk-control/api-keys/test` | 测试审计 API Key，并可携带测试文本和图片。 |
| `GET /api/v1/admin/risk-control/logs` | 分页查询审计日志。 |
| `POST /api/v1/admin/risk-control/users/:user_id/unban` | 解除自动封禁用户。 |
| `DELETE /api/v1/admin/risk-control/hashes` | 删除指定命中哈希。 |
| `DELETE /api/v1/admin/risk-control/hashes/all` | 清空命中哈希。 |

## 配置项

风控总开关由系统设置 `risk_control_enabled` 控制。内容审计配置包含：

| 配置项 | 说明 |
| --- | --- |
| `enabled` | 内容审计配置开关。 |
| `mode` | 审计模式：`off`、`observe`、`pre_block`。 |
| `base_url`、`model`、`api_keys` | OpenAI Moderations 兼容服务地址、模型和审计 API Key 池。 |
| `timeout_ms`、`retry_count` | 审计请求超时和重试次数。 |
| `sample_rate` | 观察模式采样率。 |
| `all_groups`、`group_ids` | 生效分组范围。 |
| `model_filter` | 生效模型范围，支持全部、包含列表和排除列表。 |
| `thresholds` | 各风险分类阈值。 |
| `worker_count`、`queue_size` | 异步审计 worker 数和队列大小。 |
| `pre_hash_check_enabled` | 前置哈希拦截开关。 |
| `blocked_keywords`、`keyword_blocking_mode` | 关键词拦截列表和执行方式。 |
| `record_non_hits` | 是否记录未命中审计日志。 |
| `email_on_hit`、`auto_ban_enabled`、`ban_threshold`、`violation_window_hours` | 命中通知和自动封禁策略。 |
| `hit_retention_days`、`non_hit_retention_days` | 命中和未命中日志保留天数。 |

## 运行模式

- `off`：不执行内容审计。
- `observe`：请求先放行，系统按采样率异步审计并记录日志。
- `pre_block`：请求进入上游前执行审计；命中关键词、命中哈希或审计 API 返回超过阈值时，网关按配置的 `block_status` 和 `block_message` 拦截。

前置拦截模式会统计同步检查运行态，包括活跃检查数、已检查数、放行数、拦截数、错误数和平均延迟。审计 API Key 池会记录每个 Key 的活跃请求、总调用、成功数、错误数、最近状态、最近 HTTP 状态和平均延迟，便于管理员判断是否存在 Key 失效或负载倾斜。

## 审计输入

系统会从支持的网关协议中提取文本和图片输入：

- Anthropic Messages
- OpenAI Responses
- OpenAI Chat Completions
- OpenAI Images
- Gemini

图片输入最多取 1 张用于审计。测试接口允许提交文本和图片，图片大小按服务端限制校验。

## 异常与边界处理

- 风控总开关关闭、内容审计开关关闭或模式为 `off` 时，请求不进入审计链路。
- 请求分组或模型不在配置范围内时，不进入审计链路。
- 审计 API Key 不可用、队列已满或审计调用失败时，系统记录运行态错误；具体放行或拦截行为由当前模式和失败位置决定。
- 命中哈希用于快速识别已确认风险内容；管理员可单条删除或全部清空。
- 自动封禁只作用于达到违规阈值的用户，管理员可通过解除封禁接口恢复用户状态。

## 涉及模块

- 风控服务：`backend/internal/service/content_moderation.go`
- 管理端路由：`backend/internal/server/routes/admin.go`
- 管理端 API：`frontend/src/api/admin/riskControl.ts`
- 管理端页面：`frontend/src/views/admin/RiskControlView.vue`
