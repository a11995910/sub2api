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

## 上游错误可见性

OpenAI 网关及 Codex 直连入口不会向客户端回传基础设施类上游错误体。上游返回 `5xx`（包括 Cloudflare 的 `520` 至 `524`）时，即使命中管理端错误透传规则，客户端只会收到本地通用文案 `Upstream service temporarily unavailable`；上游域名、CDN 区域、回源地址、IP、错误详情和原始 JSON 不会出现在 HTTP 或 SSE 错误响应中。

参数校验等 `4xx` 错误可按错误透传规则保留可操作提示。网关会从该提示中隐藏 HTTP/HTTPS URL、域名、IP 和 `key`、`client_secret`、`access_token`、`refresh_token` 查询参数。上游错误体仅用于账号状态判断、故障切换和受控运维日志；是否记录错误体摘要由 `gateway.log_upstream_error_body` 配置控制，不影响客户端响应。

该规则覆盖 `/v1/responses`、`/responses`、`/backend-api/codex/responses`、Chat Completions、Embeddings 及其流式错误终止事件。流式响应开始后，Codex Responses 入口仍以 `response.failed` 事件返回本地安全文案，避免连接直接结束。

## API Key 层 OpenAI Fast 模式

用户侧 API Key 创建和编辑接口支持布尔字段 `openai_fast_mode_enabled`，数据库列为 `api_keys.openai_fast_mode_enabled`，默认值为 `false`。开启后，该 Key 发起的 OpenAI 网关请求如果没有显式携带 `service_tier`，服务端会在转发上游前默认补入 `service_tier="priority"`。

该开关只补默认值，不覆盖客户端显式选择：客户端已经传入 `priority`、`flex`、`auto`、`default` 或 `scale` 时，网关尊重原值并继续按现有归一化逻辑处理。补入后的 `priority` 仍会经过全局 OpenAI fast policy，因此管理员配置的 `filter` 或 `block` 规则仍然生效。最终保留下来的 `service_tier` 会进入用量日志和计费链路，用于区分 priority/flex/default 等成本。

覆盖入口包括 `/v1/chat/completions`、`/v1/responses`、Anthropic Messages 到 OpenAI Responses 的兼容入口，以及 OpenAI Responses WebSocket/Realtime 路径。非 OpenAI 网关链路不会读取该开关。

系统级 Fast/Flex 策略还可以通过 `user_ids` 限定 Sub2API 用户。用户 ID 来自鉴权后的 API Key 所属用户，不读取客户端请求体；用户专属规则优先于全局规则，组内仍按配置顺序首条命中。管理端只接受大于 0、不重复的安全整数。

该配置保存在 `openai_fast_policy_settings` JSON 中。回滚到不支持 `user_ids` 的旧版本前，必须恢复发布前设置快照或删除所有带 `user_ids` 的规则，否则旧版本会把用户专属规则按全局规则执行。

## 账号调度与粘性会话

OpenAI 账号调度使用账号 `priority`、运行时并发负载、最近使用时间、模型能力、分组归属和运行态共同决定候选账号。`priority` 数值越小优先级越高；账号 `status` 不是 `active`、`schedulable=false`、临时不可调度、运行时被限流或不支持当前入口能力时，不会进入本次候选。

请求携带 `session_id`、`conversation_id`、`prompt_cache_key`，或可从请求内容生成稳定会话种子时，网关会维护 OpenAI 粘性会话绑定。粘性会话用于让同一会话尽量继续使用同一上游账号，减少上下文漂移；绑定账号不可用、离开当前分组、模型或入口能力不匹配、运行态被阻断时，系统会清理绑定并重新按候选池选择。

负载感知调度启用时，如果粘性会话绑定的账号优先级低于当前分组内另一个可用 OpenAI 账号，并且该更高优先级账号的运行时负载未满，系统会清理旧粘性绑定并回到优先级排序重新选择。更高优先级账号已满载、不可用、被排除、不支持当前请求或与粘性账号同优先级时，系统会保留粘性绑定；若绑定账号自身没有可用并发槽，则按粘性会话等待配置生成等待计划。

## 图片上游兼容模式

### 图片响应格式与临时 URL

图片分组可在管理端设置默认传输方式 `image_response_format`，取值为 `b64_json` 或 `url`，默认 `b64_json`。该配置只在客户未传 `response_format` 时生效；客户在 `/v1/images/generations` 或 `/v1/images/edits` 请求中显式传入的格式优先。

URL 模式会在超分或 2K/4K 二段增强完成后，将最终图片保存到 `IMAGE_STORAGE_PATH`，并返回以下形式的公开地址：

```text
https://<API 域名>/generated-images/<随机文件名>.<扩展名>
```

公开地址无需 API Key，文件名使用不可枚举随机值。图片从创建起保存 24 小时；满 24 小时后读取端点立即返回 404，后台任务负责定期删除过期文件。服务优先使用系统设置中的“API 端点地址”生成绝对地址；未配置时返回同域相对路径，不读取客户端可控的 Host 或转发头。

URL 模式统一处理上游 Base64、Data URL 和 HTTP/HTTPS 图片 URL，并按真实文件内容识别 PNG、JPEG 或 WebP；单图最大 64MB。存储失败会返回系统错误，不会自动改成 Base64。流式请求的 `partial_image` 事件继续返回 Base64 且不落盘，只有最终 `completed` 图片保存并返回 URL。

单实例可直接使用本地持久卷。多实例部署必须让所有实例共享 `IMAGE_STORAGE_PATH`，否则生成请求和图片读取落到不同实例时会返回 404。

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

## 图片 4K 提升分组

OpenAI 图片分组支持在管理端开启 `4K 提升`，并选择另一个允许生图的 OpenAI 图片分组和目标模型作为提升目标。典型用法是：当前分组先使用 image2 生成基础图片；当请求命中 4K 档位时，再把第一段图片交给 `nano-banana-2` 这类图片模型做二段提升，最终把提升后的图片按原 OpenAI Images API 响应返回给下游。

触发条件如下：

1. 当前 API Key 绑定的分组为 OpenAI 平台，且 `allow_image_generation=true`。
2. 当前分组开启 `image_4k_enhancement_enabled=true`，并配置有效的 `image_4k_enhancement_group_id` 和 `image_4k_enhancement_model`。
3. 请求为非流式图片生成或图片编辑，且 `size` 解析后的计费档位为 `4K`。
4. 目标分组必须是另一个启用状态、允许图片生成的 OpenAI 分组；管理端保存时会拦截缺少目标分组、目标模型、目标分组指向自己、目标分组不存在或目标分组不允许生图的配置。

二段提升会走内部 `/v1/images/edits` 请求，把第一段结果作为参考图传给目标分组。请求中的原始 `size` 会原样传递到二段提升：提示词会明确包含原始尺寸，例如 `3840x2160`；当目标分组渠道启用 `features_config.openai_images_upstream.mode=chat_completions` 时，该尺寸也会继续进入转换后的 Chat Completions 提示词，并用于 `generationConfig.imageConfig.aspectRatio`。因此二段提升不会只传“4K”这种模糊指令，避免最终画幅与用户请求的 `size` 不一致。目标分组调度账号时会优先使用管理端配置的 `image_4k_enhancement_model`；未配置时才沿用目标分组自己的可用图片模型解析（例如账号 `model_mapping` 中的 `nano-banana-2`），不要求 Banana 账号额外声明支持源分组的 `gpt-image-2`。

二段提升提示词要求保留原图内容、主体身份、构图、视角、颜色、光照、画幅比例和可见文字，只提升分辨率、锐度、细节和压缩瑕疵。目标分组返回内联图片时，网关会读取最终图片真实像素并写入 Images API 响应的 `data[].size`，用量记录中的 `image_output_size` 也以该字段为准，便于核实二段提升后的实际输出尺寸。目标分组调用失败、不可用或返回无图片时，系统最多尝试 3 次；仍失败则记录日志并返回第一段原图，不向用户暴露二段提升错误。

启用 `4K 提升` 的分组会优先使用目标图片分组做二段提升；未启用该功能时，才按旧配置 `image_super_resolution_enabled` 和网关外部超分服务继续执行原有 4K 超分逻辑。流式图片响应当前不触发图片分组二段提升；当分组已开启 `4K 提升` 时，流式 4K 请求会保留上游原始结果返回，不再回落到旧外部超分。

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
- OpenAI Images 原生、4K 提升与 Chat Completions 兼容转发：`backend/internal/service/openai_images.go`、`backend/internal/service/image_4k_enhancement.go`、`backend/internal/service/openai_images_chat_completions.go`
- OpenAI 调度与配额自动暂停：`backend/internal/service/openai_gateway_service.go`、`backend/internal/service/openai_account_scheduler.go`
- 账号创建与编辑界面：`frontend/src/components/account/CreateAccountModal.vue`、`frontend/src/components/account/EditAccountModal.vue`
- 运维高级设置界面：`frontend/src/views/admin/ops/components/OpsSettingsDialog.vue`
