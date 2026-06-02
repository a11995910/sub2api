# OpenAI 账号模型映射

OpenAI 账号的 `credentials.model_mapping` 用于限制账号可承接的请求模型，并可把请求模型映射为上游实际模型。

## 模型白名单语义

- 未配置 `model_mapping` 或配置为空对象时，账号允许承接所有模型。
- 非空 `model_mapping` 表示账号只允许承接映射表中声明的模型。
- 映射表支持精确模型名和现有通配符匹配规则。
- OpenAI 文本、Responses、Chat Completions、Embeddings 和图片入口都会按各自链路读取模型映射；映射后的模型用于上游请求和计费记录。
- 图片模型等非 GPT-5 系列映射保持独立，不会自动扩大为文本模型能力。

## OpenAI 白名单保存规则

OpenAI 官方模型支持会变化，系统不再自动向 `model_mapping` 补齐固定模型。后台创建、编辑、批量更新、CRS 同步和数据库写入都会以当前提交的 `model_mapping` 为准。

- 管理员在账号编辑页选择 6 个模型时，保存后的白名单仍只包含这 6 个模型。
- CRS 同步或外部导入携带 `model_mapping` 时，系统只做凭证清洗和必要字段规范化，不再追加 Codex、GPT-5 或图片模型。
- 历史数据库触发器 `accounts_openai_gpt55_model_mapping_guard` 和 `accounts_openai_required_model_mapping_guard` 已停用，避免保存时重新写入官方已不支持的旧模型。

如果某个 OpenAI 账号需要承接新增模型，必须由管理员在模型白名单或模型映射中显式配置。若官方下线模型，应从对应账号的 `model_mapping` 中移除，避免调度层继续认为该账号支持旧模型。
