# 模型测试台视频任务记录设计

## 背景

模型测试台当前在浏览器内完成“创建视频任务、轮询状态、下载内容”的完整流程。轮询存在固定总超时，页面刷新或关闭后内存状态丢失；后端只在 Redis 中保存视频任务与上游账号的临时绑定，默认一小时后无法恢复。因此，前端显示的 `timed out` 只能说明浏览器停止等待，不能说明上游生成失败。

本设计只覆盖模型测试台发起的视频任务。开发者直接调用 `/v1/videos` 的普通 API 流量不进入任务记录，也不改变现有 OpenAI 兼容协议。

## 目标

- 上游接受任务并返回 `task_id` 后，立即持久化任务记录并结束表单的“提交中”状态。
- 页面刷新、关闭或换设备后，用户仍可看到自己的历史任务并继续查询。
- `queued` 和 `in_progress` 不设置总等待超时；只有上游明确返回 `failed` 才标记生成失败。
- 查询网络错误、上游临时错误和账号暂时不可用只更新最近查询错误，不改变生成状态。
- 完成任务支持在线播放和下载，内容请求继续复用已有上游代理与 Range 支持。
- 持久化实际使用的上游账号，使 Redis 绑定过期后仍能恢复查询链路。
- `completed` 和 `failed` 保留 30 天，之后自动清理；未完成任务不自动清理。

## 非目标

- 不为所有 `/v1/videos` 调用建立用户任务中心。
- 不在页面关闭后持续轮询上游。
- 不把生成完成的视频转存到对象存储。
- 不提供取消上游视频任务的能力。
- 不保存 API Key 明文或参考图 Base64 内容。

## 架构

新增 `VideoTestTaskService` 作为领域边界，负责所有权校验、状态转换、查询节流、账号恢复和保留期清理；`VideoTestTaskStore` 隔离 PostgreSQL 实现。模型测试台在 `/v1/videos` 请求上附加内部标记，网关只有在该标记存在且上游已经返回任务 ID 时才写入记录。

用户侧新增登录态接口用于列表、刷新、内容代理和删除。刷新接口从数据库读取持久化账号，不依赖浏览器继续持有原请求，也不依赖 Redis 的一小时 TTL。页面只在可见时轮询未完成任务，重新可见时立即补查。

## 数据模型

新增表 `model_test_video_tasks`：

- `id UUID`：内部公开任务 ID，主键。
- `user_id BIGINT`：任务所属用户。
- `api_key_id BIGINT`：发起任务使用的 API Key，仅保存 ID。
- `group_id BIGINT`：发起任务所属分组。
- `account_id BIGINT`：实际命中的上游账号，用于恢复查询。
- `upstream_task_id TEXT`：上游任务 ID。
- `platform TEXT`：`openai` 或 `grok`，决定刷新和内容代理实现。
- `model TEXT`、`prompt TEXT`、`resolution TEXT`、`duration_seconds INTEGER`、`reference_image_count INTEGER`：测试输入摘要。
- `status TEXT`：仅允许 `queued`、`in_progress`、`completed`、`failed`。
- `progress DOUBLE PRECISION`：可空的归一化进度。
- `response_json JSONB`：最后一次成功的上游状态响应摘要。
- `error_message TEXT`：上游明确失败时的失败原因。
- `last_poll_error TEXT`：最近一次查询错误；查询成功后清空。
- `last_polled_at`、`completed_at`、`failed_at`、`created_at`、`updated_at`：状态时间。

唯一约束使用 `(user_id, api_key_id, upstream_task_id)`，避免同一测试任务重复落库。索引覆盖用户列表、未完成任务和终态清理。外键采用与现有业务表一致的删除策略；记录删除不影响上游任务。

## 创建流程

1. 测试台向 `/v1/videos` 提交真实请求并添加 `X-Sub2API-Model-Test: video`。
2. 网关继续执行现有鉴权、分组路由、账号调度、故障切换、计费和 Redis 绑定。
3. 上游成功返回 `task_id` 后，网关在响应浏览器前写入任务记录，包含最终使用的账号和规范化状态。
4. 落库成功后返回现有 OpenAI 兼容创建响应；前端立即把任务加入列表并恢复提交按钮。
5. 上游在创建阶段明确拒绝请求时，不创建任务记录，前端显示提交失败。
6. 数据库落库失败时，创建接口返回服务错误，不把一个无法恢复的任务展示成已成功记录。

只有带内部测试台标记的请求触发步骤 3。普通 API 客户端即使使用相同模型和端点，也维持现状。

## 用户接口

- `GET /api/v1/model-test/video-tasks?page=1&page_size=20`：按创建时间倒序返回当前用户任务。
- `POST /api/v1/model-test/video-tasks/:id/refresh`：刷新当前用户的一项任务；五秒内的重复刷新直接返回缓存记录。
- `GET /api/v1/model-test/video-tasks/:id/content`：仅在任务完成后代理视频内容，保留 `Range`、`Content-Range`、`Content-Length` 和内容类型。
- `DELETE /api/v1/model-test/video-tasks/:id`：删除当前用户记录，不承诺取消上游任务。

所有查询都以登录用户 ID 作为仓储条件，不接受客户端传入的 `user_id`、`account_id` 或上游账号信息。

## 状态规则

- 上游 `queued`、`pending`、`submitted` 归一为 `queued`。
- 上游 `running`、`processing`、`in_progress` 归一为 `in_progress`。
- 上游 `succeeded`、`success`、`completed` 归一为 `completed`。
- 上游 `failed`、`error`、`cancelled` 归一为 `failed`；这些都是上游明确终止。
- HTTP 超时、连接错误、HTTP 5xx、限流、账号临时不可用、无法解析的非终态响应只写 `last_poll_error`，保留原状态。
- 已进入 `completed` 或 `failed` 的任务不再回退到非终态。

## 前端交互

视频模式下保留现有表单和结果区，并在主内容下方新增全宽任务记录。记录显示状态、模型、提示词摘要、规格、创建时间和操作；移动端使用纵向条目。点击记录后在结果区显示完整信息。

“提交中”只覆盖创建请求。创建成功后用户可以继续提交其他视频。页面可见时每五秒刷新未完成任务；页面隐藏时停止定时器，恢复可见后立即刷新。查询失败显示“暂时无法查询，仍在等待”，不触发“测试失败”提示。完成后显示播放和下载，失败后显示上游失败原因。

## 清理

后台清理服务启动后立即执行一次，之后每小时删除 `completed_at` 或 `failed_at` 已超过 30 天的终态记录。`queued` 和 `in_progress` 不进入清理条件。用户主动删除不等待保留期。

## 测试与验收

- 迁移测试验证表、约束和索引。
- 仓储测试验证用户隔离、分页、状态更新、删除和 30 天清理。
- 服务测试验证状态归一化、终态不可回退、查询错误不判失败、五秒节流和持久账号恢复。
- Handler 与路由测试验证登录态、越权 404、刷新代理、内容 Range 和删除语义。
- 网关测试验证只有测试台标记触发落库，OpenAI 与 Grok 创建链路都记录实际账号。
- 前端 API 和页面测试验证提交即入列表、多任务并行、刷新恢复、页面可见性轮询、查询错误继续等待和完成后播放下载。
- staging 使用真实慢视频任务验证：提交后立即可见、刷新页面仍存在、等待超过旧超时不失败、最终完成后可播放下载。

## 发布与回滚

功能先合入并推送 `dev`，在正式 VPS 的隔离 staging 使用独立数据库和 Redis 验证。生产切换只使用 staging 验证过的同一 commit。回滚应用镜像不会删除新增表；旧版本会忽略该表，因此无需破坏性数据库回滚。
