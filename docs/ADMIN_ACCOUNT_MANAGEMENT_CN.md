# 账号管理

账号管理页面用于维护上游 AI 服务账号，支持按平台、类型、状态、分组、隐私模式和关键字筛选账号，并对账号执行单条编辑、状态恢复、令牌刷新、测试、统计查看和批量操作。

## 数据与 Agent Identity 导入

账号页“更多操作 > 数据操作 > 导入”支持多选或拖入 JSON 文件，并根据文件内容识别以下格式：

- Sub2API 账号与代理导出数据：文件包含 `proxies` 和 `accounts` 数组。多个导出文件会在浏览器端校验并合并，再提交账号数据导入接口。
- Codex Agent Identity `auth.json`：根对象使用 `auth_mode=agentIdentity` 或包含 `agent_identity` 对象；同时兼容对应的 camelCase 字段和由多个 Agent Identity 对象组成的 JSON 数组。多个文件的原始 JSON 内容会一次提交给 Codex 导入接口。

两类格式不能在同一次操作中混选。Agent Identity 导入创建 OpenAI OAuth 类型账号，不保存 OAuth access token 或 refresh token；运行时使用 `agent_private_key` 动态签名。导入不自动绑定默认分组，管理员需在导入后手工绑定分组。

Agent Identity 重复判断以 `chatgpt_account_id` 为准：同一 ChatGPT 账号更新已有 Agent Identity 凭据，不同账号分别创建。单个条目解析或字段校验失败不会回滚同批次中已经成功的条目，页面会展示新增、更新、跳过、失败数量以及后端返回的错误和警告。文件解析在浏览器内完成，凭据仅通过管理员导入请求提交，不写入前端日志。

## 批量操作范围

- 勾选表格行或点击表头复选框时，系统只选中当前页可见账号。
- 存在已选账号时，批量操作栏展示删除、重置状态、刷新令牌、启用调度、停止调度和编辑已选账号等操作；这些操作通过账号 ID 列表提交，只作用于已选账号。
- 未选中账号时，批量操作栏展示更新筛选结果入口；该入口按当前筛选条件计算目标账号，作用范围包含所有匹配筛选条件的账号，不限当前页。
- 批量编辑弹窗会显示当前更新范围：已选账号模式显示已选账号数量，筛选结果模式显示当前筛选结果数量。

## 相关接口

- `GET /api/v1/admin/accounts`：按筛选条件分页查询账号列表，并用于筛选结果批量更新前的目标数量预览。
- `POST /api/v1/admin/accounts/bulk-update`：批量更新账号。请求包含 `account_ids` 时按账号 ID 列表更新；请求仅包含 `filters` 时按筛选条件解析全部目标账号。
- `POST /api/v1/admin/accounts/batch-refresh`：批量刷新已选账号令牌。
- `POST /api/v1/admin/accounts/batch-clear-error`：批量重置已选账号错误状态。
- `POST /api/v1/admin/accounts/data`：导入 Sub2API 账号与代理导出数据。
- `POST /api/v1/admin/accounts/import/codex-session`：批量导入 Codex Agent Identity `auth.json`；请求通过 `contents` 传递多个文件内容，并启用已有账号更新。
- `GET /api/v1/keys`、`POST /api/v1/keys`、`GET /api/v1/admin/users/:id/keys` 等 API Key DTO 会返回 `current_concurrency`，表示该 Key 当前正在执行的实时请求数。

## 优先级与调度影响

账号优先级字段用于网关调度，数值越小优先级越高。OpenAI 平台请求会先过滤状态、调度开关、模型能力、分组归属和运行时限制，再结合优先级、实时负载和最近使用时间选择账号。

OpenAI 粘性会话会让同一会话尽量沿用上一次绑定账号。负载感知调度启用时，如果旧绑定账号的优先级低于当前分组中有空位的更高优先级账号，系统会清理旧绑定并重新选择；同优先级账号、没有更高优先级空位或更高优先级账号不可用时，系统保留粘性会话，以保证会话连续性。

## API Key 当前并发

API Key 列表、创建响应、详情响应和管理员查看用户 Key 时会返回 `current_concurrency` 字段。该字段来自运行时并发缓存，只用于页面展示和排障参考，不写入 API Key 数据表，也不改变 API Key 的并发限制、配额、状态或调度规则。

当 Redis 并发缓存不可用或没有活跃请求时，服务端返回 `0`。前端应把该字段作为瞬时状态展示，不能用它替代历史用量统计或并发上限配置。

## 请求头覆写

Anthropic 和 OpenAI 平台的 `api_key` 类型账号支持请求头覆写。管理员可在创建账号、编辑账号或批量编辑账号时开启该能力，并在账号 `credentials` 中保存：

- `header_override_enabled`：布尔值，表示是否启用覆写。
- `header_overrides`：对象，键为 Header 名，值为覆写后的 Header 值。

服务端保存账号时会统一校验并规范化该配置：Header 名去除首尾空白并转为小写，Header 值去除首尾空白；名和值都为空的占位行会被丢弃；同名 Header 按大小写不敏感规则去重。每个账号最多保存 64 条覆写，Header 名最长 200 字符，Header 值最长 8192 字符。

请求转发到上游前，系统会按账号配置覆盖同名请求头。覆写只对符合条件的 `anthropic/api_key` 和 `openai/api_key` 账号生效；OAuth、Setup Token、Gemini、Grok、Antigravity 等其他账号类型不会应用该配置。

以下 Header 不允许覆写：连接控制、逐跳传输、认证密钥、Cookie、压缩协商、WebSocket 握手以及逐请求会话隔离相关 Header，例如 `host`、`content-length`、`content-type`、`connection`、`authorization`、`x-api-key`、`x-goog-api-key`、`cookie`、`accept-encoding`、`sec-websocket-*`、`session_id`、`conversation_id`、`x-codex-turn-state`、`x-codex-turn-metadata`、`chatgpt-account-id`、`x-claude-code-session-id`、`x-client-request-id`。保存非法 Header 时接口返回 `400 INVALID_HEADER_OVERRIDE`。

批量编辑中的请求头覆写是整组替换语义：开启批量字段后，提交的 `header_override_enabled` 和 `header_overrides` 会覆盖所选账号或筛选结果账号的原配置；关闭时会清空已有覆写配置。

## Anthropic 转发风险提示

账号列表和账号详情返回的 Anthropic 账号包含只读字段 `anthropic_forwarding_risk`，用于描述当前账号转发形态的风险级别、摘要和原因。该字段由服务端根据账号平台、账号类型、`extra` 配置和代理绑定状态实时计算，不写入数据库。

- Anthropic `oauth` / `setup-token` 账号返回 `high` 风险摘要。该类账号的请求会按 Claude Code 形态转发，上游可看到账号令牌、出口 IP、TLS/HTTP 指纹和会话形态。
- 创建或批量创建 Anthropic `oauth` / `setup-token` 账号时，如果请求未显式配置 `extra.base_rpm`、`extra.max_sessions` 或 `extra.session_idle_timeout_minutes`，服务端会写入保守默认值：`base_rpm=12`、`max_sessions=3`、`session_idle_timeout_minutes=5`。这些默认值用于降低共享账号高频请求和多会话并发带来的风控风险。
- 已存在账号或编辑账号时，服务端不会自动覆盖 `base_rpm`、`max_sessions`、`session_idle_timeout_minutes` 的显式配置；管理员可以在账号编辑或批量编辑中按真实使用量调整。
- Anthropic `apikey` 账号仅在 `extra.anthropic_passthrough=true` 时返回 `medium` 风险摘要。该模式会透传白名单客户端请求头并替换上游认证，上游仍可看到出口 IP 和调用形态。
- 未设置 `extra.base_rpm` 或 `extra.max_sessions` 的 Anthropic `oauth` / `setup-token` 风险摘要会提示缺少账号级请求节流或会话数量上限。
- 未绑定代理的 Anthropic 风险摘要会提示流量使用服务器默认出口。
- 启用 `extra.enable_tls_fingerprint=true` 时，风险摘要会提示 TLS profile 需要与 `User-Agent`、`X-Stainless-*` 运行时字段保持一致。
- 启用 `extra.custom_base_url_enabled=true` 时，风险摘要会提示需要确认自定义中继不会额外泄露或改写指纹。

`anthropic_forwarding_risk` 只用于管理端识别和提示配置风险，不会改变账号请求转发和计费逻辑。创建 Anthropic `oauth` / `setup-token` 账号时写入的默认 `base_rpm` 与 `max_sessions` 会参与现有调度限流和会话数量控制。

## 调试日志脱敏

网关调试日志用于排查客户端原始请求与上游转发请求的差异。开启 `SUB2API_DEBUG_GATEWAY_BODY` 或 `SUB2API_DEBUG_CLAUDE_MIMIC` 时，系统会对认证和会话指纹做脱敏后再写入日志：

- 请求头中的 `authorization`、`x-api-key` 会保留认证类型但隐藏密钥值。
- `X-Claude-Code-Session-Id`、`x-client-request-id` 只保留短前后缀。
- 请求体中的 `metadata.user_id`、`metadata.account_id`、`metadata.session_id`、`metadata.client_id`、`prompt_cache_key` 会替换为 `[redacted]`。
- Claude mimic 诊断行不会写入完整 `metadata.user_id` 或完整系统提示词预览，只保留短标识用于排障关联。

调试日志仍可能包含用户输入内容、模型名、请求结构和上游错误摘要。生产环境仅应在短时排障时开启调试日志，并在排障结束后关闭。

## 边界与异常

- 批量编辑未勾选任何更新字段时不会提交。
- 已选账号模式下账号 ID 为空时不会提交。
- 筛选结果模式下如果当前筛选条件没有命中账号，服务端返回成功和失败数量均为 0，不会写入账号数据。
- 更新分组时会先校验分组是否存在；存在混合渠道风险时需要管理员确认后继续。
- 请求头覆写只在支持的平台和账号类型上展示和保存；非法 Header、重复 Header、非字符串值和超长配置会被服务端拒绝。
- `anthropic_forwarding_risk` 为派生提示字段，旧账号和旧数据不需要迁移；非 Anthropic 账号和未启用透传的 Anthropic API Key 账号不会返回该字段。

## 测试建议

- API Key DTO 测试覆盖 `current_concurrency` 的映射；服务测试覆盖 Redis 并发缓存不可用时返回 `0`。
- 账号服务测试覆盖请求头覆写的合法保存、非法 Header 拒绝、大小写去重、空占位行丢弃和批量编辑整组替换。
- 组件测试覆盖批量操作栏在“已选账号”和“未选账号”两种状态下展示不同入口。
- 页面测试覆盖全选当前页后打开批量编辑时提交 `account_ids`，不提交 `filters`。
- 接口或服务测试覆盖 `POST /api/v1/admin/accounts/bulk-update` 的 `account_ids` 模式、`filters` 模式、空目标、无更新字段和混合渠道风险确认流程。
- DTO 测试覆盖 `anthropic_forwarding_risk` 对 Anthropic OAuth/SetupToken、Anthropic API Key 透传和非 Anthropic 账号的返回差异。
- Handler 测试覆盖创建 Anthropic OAuth/SetupToken 账号时自动写入保守 `base_rpm`、`max_sessions`、`session_idle_timeout_minutes`，且不影响 Anthropic API Key 账号。
- 服务测试覆盖网关调试日志对认证头、Claude Code session、metadata 用户标识和 prompt cache key 的脱敏。
- 组件测试覆盖多个 Agent Identity 文件导入、camelCase 字段兼容、与 Sub2API 导出数据混选拒绝，以及部分成功后刷新账号列表。
