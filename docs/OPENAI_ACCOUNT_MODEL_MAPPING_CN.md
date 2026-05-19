# OpenAI 账号模型映射

OpenAI 账号的 `credentials.model_mapping` 用于限制账号可承接的请求模型，并可把请求模型映射为上游实际模型。

## 模型白名单语义

- 未配置 `model_mapping` 或配置为空对象时，账号允许承接所有模型。
- 非空 `model_mapping` 表示账号只允许承接映射表中声明的模型。
- 映射表支持精确模型名和现有通配符匹配规则。
- 图片模型等非 GPT-5 系列映射保持独立，不会自动扩大为文本模型能力。

## OpenAI GPT-5 系列兜底

OpenAI 账号使用非空 GPT-5 系列白名单时，系统会自动补齐 `gpt-5.5 -> gpt-5.5` 映射，避免旧前端、外部同步、批量导入或直接数据库写入遗漏该模型后，被本地调度层误判为不支持 `gpt-5.5`。

兜底生效入口包括：

- 后台创建 OpenAI 账号。
- 后台编辑 OpenAI 账号凭证。
- 批量更新 OpenAI 账号凭证。
- CRS 同步 OpenAI OAuth 账号和 OpenAI Responses API Key 账号。
- 数据库写入 `accounts` 表时的触发器保护。

该兜底只在以下条件同时满足时生效：

- 账号平台为 `openai`。
- `model_mapping` 是非空对象。
- 映射表中已有 `gpt-5`、`gpt-5.x` 或 `gpt-5-*` 形式的 GPT-5 系列模型。
- 映射表中尚未显式声明 `gpt-5.5`。

该兜底不会改变以下账号：

- 非 OpenAI 账号。
- 空 `model_mapping` 账号。
- 仅声明图片模型、`auto` 或其他非 GPT-5 系列模型的 OpenAI 账号。
- 已经显式支持 `gpt-5.5` 的账号。
