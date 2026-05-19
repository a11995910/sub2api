# OpenAI 账号模型映射

OpenAI 账号的 `credentials.model_mapping` 用于限制账号可承接的请求模型，并可把请求模型映射为上游实际模型。

## 模型白名单语义

- 未配置 `model_mapping` 或配置为空对象时，账号允许承接所有模型。
- 非空 `model_mapping` 表示账号只允许承接映射表中声明的模型。
- 映射表支持精确模型名和现有通配符匹配规则。
- 图片模型等非 GPT-5 系列映射保持独立，不会自动扩大为文本模型能力。

## OpenAI 固定白名单兜底

OpenAI 账号使用非空 GPT-5 或 Codex 固定白名单时，系统会自动补齐当前必须透传的 OpenAI 模型映射，避免旧前端、外部同步、批量导入或直接数据库写入遗漏模型后，被本地调度层误判为不支持对应模型。

当前自动补齐的映射为：

- `codex-auto-review -> codex-auto-review`
- `gpt-4o-audio-preview -> gpt-4o-audio-preview`
- `gpt-4o-realtime-preview -> gpt-4o-realtime-preview`
- `gpt-5.2 -> gpt-5.2`
- `gpt-5.2-2025-12-11 -> gpt-5.2-2025-12-11`
- `gpt-5.2-chat-latest -> gpt-5.2-chat-latest`
- `gpt-5.2-pro -> gpt-5.2-pro`
- `gpt-5.2-pro-2025-12-11 -> gpt-5.2-pro-2025-12-11`
- `gpt-5.3-codex -> gpt-5.3-codex`
- `gpt-5.3-codex-spark -> gpt-5.3-codex-spark`
- `gpt-5.4 -> gpt-5.4`
- `gpt-5.4-2026-03-05 -> gpt-5.4-2026-03-05`
- `gpt-5.4-mini -> gpt-5.4-mini`
- `gpt-5.5 -> gpt-5.5`
- `gpt-image-1 -> gpt-image-1`
- `gpt-image-1.5 -> gpt-image-1.5`
- `gpt-image-2 -> gpt-image-2`

兜底生效入口包括：

- 后台创建 OpenAI 账号。
- 后台编辑 OpenAI 账号凭证。
- 批量更新 OpenAI 账号凭证。
- Codex 会话批量导入账号。
- CRS 同步 OpenAI OAuth 账号和 OpenAI Responses API Key 账号。
- 数据库写入 `accounts` 表时的触发器保护。

该兜底只在以下条件同时满足时生效：

- 账号平台为 `openai`。
- `model_mapping` 是非空对象。
- 映射表中已有 `codex-auto-review`，或已有 `gpt-5`、`gpt-5.x`、`gpt-5-*` 形式的 GPT-5 系列模型。
- 映射表中缺少任一必备透传模型的精确映射。

该兜底不会改变以下账号：

- 非 OpenAI 账号。
- 空 `model_mapping` 账号。
- 仅声明图片模型、`auto` 或其他非 GPT-5/Codex 系列模型的 OpenAI 账号。
- 已经显式支持全部必备透传模型的账号。

如果映射表中存在 `gpt-5.*` 等通配符，账号在调度时仍会按通配符匹配请求模型；兜底逻辑仍会补齐上述精确映射，保证后台展示、批量更新和数据库触发器行为一致。
