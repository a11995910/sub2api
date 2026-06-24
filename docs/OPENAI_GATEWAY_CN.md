# OpenAI 网关功能说明

## 功能入口

OpenAI 平台分组的 API Key 可通过 OpenAI 兼容入口调用文本、图片和向量能力。网关统一走现有 API Key 鉴权、分组校验、账号调度、并发控制、余额与配额校验、用量记录和计费链路。

| 入口 | 说明 |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI Chat Completions 兼容入口。OpenAI 分组走 OpenAI 网关，非 OpenAI 分组按既有 Claude/Gemini 兼容链路处理。 |
| `POST /v1/responses` | OpenAI Responses 兼容入口，支持子路径 `/v1/responses/*subpath`。 |
| `GET /v1/responses` | OpenAI Responses WebSocket 入口。 |
| `POST /v1/embeddings` | OpenAI Embeddings 兼容入口，仅 OpenAI 分组可用。 |
| `POST /v1/images/generations` | OpenAI 图片生成入口，仅 OpenAI 分组可用。 |
| `POST /v1/images/edits` | OpenAI 图片编辑入口，仅 OpenAI 分组可用。 |
| `POST /chat/completions`、`POST /responses`、`POST /embeddings`、`POST /images/generations`、`POST /images/edits` | 不带 `/v1` 前缀的兼容别名，鉴权、调度和错误处理与 `/v1` 入口一致。 |
| `POST /backend-api/codex/responses` | Codex 直连兼容入口，内部复用 Responses 网关处理。 |

非 OpenAI 分组访问 Embeddings 或图片入口时，网关返回 `404`，错误类型为 `not_found_error`，并记录本地功能门禁类运维限制标记。

## 图片上游兼容模式

OpenAI 图片入口默认按原生 Images API 转发：`/v1/images/generations` 转到上游 `/v1/images/generations`，`/v1/images/edits` 转到上游 `/v1/images/edits`。若某个 OpenAI APIKey 上游只支持 `/v1/chat/completions` 生成图片，可在该分组绑定渠道的 `features_config` 中配置：

```json
{
  "openai_images_upstream": {
    "mode": "chat_completions"
  }
}
```

启用后，该渠道下的 `/v1/images/generations` 和 `/v1/images/edits` 请求仍对下游保持 OpenAI Images API 形态，但网关会把请求转换为非流式 Chat Completions 请求发送到上游 `{base_url}/v1/chat/completions`。文生图请求发送普通文本消息；图生图请求会把本地 multipart 上传图片转成 `data:image/*;base64`，并与 JSON 请求中的 `images[].image_url` 一起放入 `messages[].content[]` 的 `image_url.url` 多模态字段。上游返回的 Markdown 图片、普通图片 URL、`data:image/*;base64` 或 JSON 字段 `url`、`image_url`、`b64_json` 会被重新包装为 Images API 响应，并继续进入图片计费和用量记录链路。

该模式只支持 OpenAI APIKey 账号和非流式图片入口，不支持图片流式返回。未启用该配置时，图片入口仍要求 `gpt-image-*` 这类原生 OpenAI 图片模型，避免普通文本模型被误识别为图片模型。

## Embeddings 请求流程

`POST /v1/embeddings` 要求请求体为合法 JSON，且必须包含非空字符串 `model`。请求通过后端 `OpenAIGatewayHandler.Embeddings` 处理：

1. 从请求上下文读取 API Key、用户和分组信息。
2. 读取并校验请求体，设置运维请求上下文和标准入口 `/v1/embeddings`。
3. 按分组渠道映射解析请求模型，必要时替换请求体中的 `model`。
4. 校验用户并发、账号并发、余额、订阅、API Key 配额和用户平台配额。
5. 使用 OpenAI 调度器选择具备 `embeddings` 能力的 OpenAI 账号。
6. 将请求转发到账号 `base_url` 对应的 `/v1/embeddings`，默认上游为 `https://api.openai.com/v1/embeddings`。
7. 透传上游成功响应，提取 `usage.prompt_tokens`、`usage.input_tokens`、`usage.total_tokens` 等字段用于用量记录。
8. 上游出现可切换账号的错误时，按网关账号切换策略排除失败账号并重试；切换耗尽后返回上游失败错误。

Embeddings 当前为非流式入口。账号缺少 API Key、`base_url` 非法、上游读取超限或上游返回错误时，网关会按 OpenAI 错误结构返回，并记录运维上游错误事件。

## OpenAI 账号能力

OpenAI 账号的 `credentials.openai_endpoint_capabilities` 用于限制账号可承接的 OpenAI 入口能力。

| 能力值 | 说明 |
| --- | --- |
| `chat_completions` | 账号可承接文本类 OpenAI 请求，包括 Chat Completions、Responses 以及内部转换后的文本链路。 |
| `embeddings` | 账号可承接 Embeddings 请求。 |

未配置能力列表时，系统按兼容默认值处理。后台创建和编辑 OpenAI 账号时，界面会提供文本能力和 Embeddings 能力开关，默认同时启用。

## 账号配额自动暂停

OpenAI 账号可根据 Codex 用量窗口自动从调度候选中临时排除。该机制只影响调度选择，不直接修改账号 `status` 或 `schedulable` 字段。

系统读取账号 `extra` 中的用量快照：

| 字段 | 说明 |
| --- | --- |
| `codex_5h_used_percent` | Codex 5 小时窗口使用率，按百分比保存，例如 `95` 表示 95%。 |
| `codex_7d_used_percent` | Codex 7 天窗口使用率，按百分比保存。 |
| `codex_5h_reset_at`、`codex_7d_reset_at` | 对应窗口的绝对重置时间。时间已过期时，旧使用率不会触发暂停。 |
| `codex_5h_reset_after_seconds`、`codex_7d_reset_after_seconds`、`codex_usage_updated_at` | 无绝对重置时间时，用于推算窗口是否已过期。 |

阈值按 `0~1` 的比例保存，后台界面按百分比展示。

| 字段 | 说明 |
| --- | --- |
| `extra.auto_pause_5h_threshold` | 单账号 5 小时窗口自动暂停阈值。 |
| `extra.auto_pause_7d_threshold` | 单账号 7 天窗口自动暂停阈值。 |
| `extra.auto_pause_5h_disabled` | 对当前账号禁用 5 小时窗口自动暂停。 |
| `extra.auto_pause_7d_disabled` | 对当前账号禁用 7 天窗口自动暂停。 |
| `settings.ops_advanced_settings.openai_account_quota_auto_pause.default_threshold_5h` | 全局 5 小时默认阈值。 |
| `settings.ops_advanced_settings.openai_account_quota_auto_pause.default_threshold_7d` | 全局 7 天默认阈值。 |

判断顺序如下：

1. 单账号禁用标记优先级最高；某个窗口被禁用时，该窗口不触发暂停。
2. 单账号阈值大于 `0` 时优先使用单账号阈值。
3. 单账号阈值为空或小于等于 `0` 时，回退到运维高级设置中的全局默认阈值。
4. 有效使用率大于等于阈值时，该账号不会进入本次 OpenAI 调度候选。
5. 用量窗口已重置或没有可用用量快照时，不触发自动暂停。

## 涉及模块

- 网关路由：`backend/internal/server/routes/gateway.go`
- OpenAI Embeddings 处理器：`backend/internal/handler/openai_embeddings.go`
- OpenAI Embeddings 转发服务：`backend/internal/service/openai_embeddings.go`
- OpenAI Images 处理器：`backend/internal/handler/openai_images.go`
- OpenAI Images 原生与 Chat Completions 兼容转发：`backend/internal/service/openai_images.go`、`backend/internal/service/openai_images_chat_completions.go`
- OpenAI 调度与配额自动暂停：`backend/internal/service/openai_gateway_service.go`、`backend/internal/service/openai_account_scheduler.go`
- 账号创建与编辑界面：`frontend/src/components/account/CreateAccountModal.vue`、`frontend/src/components/account/EditAccountModal.vue`
- 运维高级设置界面：`frontend/src/views/admin/ops/components/OpsSettingsDialog.vue`
