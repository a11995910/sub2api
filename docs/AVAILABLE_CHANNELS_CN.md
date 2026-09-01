# 可用渠道展示说明

## 目标

用户在创建 API 密钥前，需要能清楚判断自己可以访问哪些分组、哪些分组支持图片或视频生成，以及模型价格在对应分组倍率下的最终展示价格。

生图请求、分组默认回传类型和响应示例见[生图接口说明](IMAGE_GENERATION_API_CN.md)。

## 分组展示

`GET /api/v1/channels/available` 会按渠道和平台返回用户可访问的分组，并受 `available_channels_enabled` 控制。`GET /api/v1/channels/catalog` 使用相同的响应结构和权限过滤，但不受该页面开关影响，专供登录后的模型广场和模型测试台读取模型目录。用户侧分组字段包含：

- `rate_multiplier`：分组默认文本倍率。
- `allow_image_generation`：该分组是否允许图片生成。
- `image_response_format`：客户未显式传入 `response_format` 时采用的默认图片响应格式。`b64_json` 保持内联 Base64；`url` 将最终图片保存到本地并返回当前 API 域名下 24 小时有效的公开 URL。客户显式参数始终优先。
- `image_super_resolution_enabled`：该分组的图片生成结果是否会在返回前自动执行 4K 超分。
- `image_2k_enhancement_enabled`：该分组命中显式 2K 生图时，是否优先调用另一个图片分组做二段提升。
- `image_2k_enhancement_group_id`：二段 2K 提升使用的目标图片分组 ID；仅管理端配置和内部调度使用，用户侧无需手动传参。
- `image_4k_enhancement_enabled`：该分组命中 4K 生图时，是否优先调用另一个图片分组做二段提升。
- `image_4k_enhancement_group_id`：二段 4K 提升使用的目标图片分组 ID；仅管理端配置和内部调度使用，用户侧无需手动传参。
- `image_4k_enhancement_model`：二段 4K 提升使用的目标图片模型；管理端在选择目标分组后从目标分组候选模型中选择。为空时后端沿用目标分组自动模型解析。
- `image_rate_independent`：图片生成是否使用独立倍率。
- `cache_hit_quarter_to_input_enabled`：缓存命中率目标控制开关。字段名为兼容旧客户端保留，开启后不再固定移动四分之一，而是按用户和分组对时间衰减后的累计命中率进行控制。
- `cache_hit_target_percent`：调整后缓存命中率的回调目标，范围为 `0.01` 至 `100.00`，默认 `90.00`。命中率按 `cache_read_tokens / (input_tokens + cache_creation_tokens + cache_read_tokens)` 计算。
- `cache_hit_target_tolerance_percent`：目标容差带，范围为 `0.00` 至 `50.00`，默认 `0.50`；目标加减容差必须保持在 `0` 至 `100` 之间。加入本次请求后，有效累计命中率只有超过“目标 + 容差”才触发划拨，触发后尽量回到目标。
- `cache_hit_half_life_days`：历史累计权重的时间半衰期，单位为天，范围为 `0.01` 至 `365.00`，默认 `1.00`。每经过一个半衰期，历史提示词和缓存读取权重同时减半；它按实际经过时间连续衰减，不会在自然日边界清零，也不是每天独立计算。
- `image_rate_multiplier`：图片独立倍率，仅 `image_rate_independent=true` 时生效。
- `image_price_1k`、`image_price_2k`、`image_price_4k`：图片生成 1K、2K、4K 单张基础价；为空时后端真实计费会回退默认图片价格。
- `video_rate_independent`：视频生成是否使用独立倍率。
- `video_rate_multiplier`：视频独立倍率，仅 `video_rate_independent=true` 时生效。
- `video_price_480p`、`video_price_720p`、`video_price_1080p`：视频生成各分辨率的每秒覆盖价；为空时继续按渠道定价和系统默认价的顺序解析。

前端“可用渠道”页会将“我可访问的分组”作为独立区域展示。支持图片生成的分组会显示“图片可用”标签，用户在创建 API 密钥前即可识别图片分组。

## 模型与可见号池

两个渠道目录接口的 `platforms[].supported_models[]` 都会返回 `group_ids`，表示当前用户可见且持久账号池实际支持该模型的分组 ID。渠道定价和渠道模型映射仍负责提供候选模型；后端再逐个检查每个分组中 `active + schedulable` 的账号，只有至少一个账号的模型映射允许该模型时，才把分组写入 `group_ids`。账号没有配置模型映射时，继续遵守现有调度语义，由账号类型决定默认可用模型。

可见号池只读取持久配置，不使用临时限流、过载、临时不可调度、过期窗口或运行时阻断状态。因此短期上游波动不会让模型广场和模型测试台中的模型反复出现、消失；管理员把账号停用或关闭持久 `schedulable` 后，模型可见性才会随之更新。

普通用户响应只包含模型对应的分组 ID，不包含账号 ID、账号名称、账号类型、OAuth 类型、凭据、Token 或 Cookie。前端按 `group_ids` 把同一平台的模型关联到真实支持它的分组，避免把渠道候选模型与平台下全部分组做笛卡尔组合。滚动升级期间，旧后端缺少 `group_ids` 时前端仍沿用原有分组规则；新后端返回显式空数组时表示没有可见号池支持该模型。

未开启 `allow_image_generation` 的 OpenAI 分组会继续拒绝专用图片端点、图片模型和显式选择 `tool_choice:image_generation` 的请求。对于新版 CC-Switch / Codex 官方客户端在普通文本请求中默认携带的 `image_generation` tool 能力声明，网关会在转发前移除该 tool，避免文本请求被误判为生图请求而返回 403。

图片分组存在以下后处理方式：

- `image_2k_enhancement_enabled=true` 时，非流式且显式声明 2K `size` 的图片请求会先生成基础图片，再调用 `image_2k_enhancement_group_id` 指向的 OpenAI 图片分组做二段提升。网关会把原请求 `size` 原样传给二段请求，例如 `2048x2048` 或 `2048x1152`，并在目标分组返回 PNG/JPEG 内联图片时按该 `size` 校正最终图片像素；目标分组失败最多尝试 3 次，仍失败时保留基础图片返回。未传 `size` 的默认 2K 请求不会触发二段提升，避免默认生图流量被误放大。
- `image_4k_enhancement_enabled=true` 时，非流式且请求明确落在 4K 档位的图片请求会先生成基础图片，再调用 `image_4k_enhancement_group_id` 指向的 OpenAI 图片分组做二段提升。配置 `image_4k_enhancement_model` 后，二段请求固定使用该目标模型；未配置时后端沿用目标分组模型映射或账号候选自动解析。网关会把原请求 `size` 原样传给二段请求，例如 `3840x2160`，并在目标分组返回 PNG/JPEG 内联图片时按该 `size` 校正最终图片像素，避免元数据与实际尺寸不一致；目标分组失败最多尝试 3 次，仍失败时保留基础图片返回。未传 `size` 的自适应请求按网关默认图片档位处理，不会因为分组开启 4K 提升而自动改写为 4K。
- 未启用图片分组 4K 提升时，若 `image_super_resolution_enabled=true`，网关继续使用旧外部超分服务。同步图片响应会在完整 JSON 返回前改写最终图片；流式图片响应会继续透传上游进度事件和局部图片，待最终完成事件出现后对最终图片执行超分并返回超分后的完成事件。超分失败时保留原图返回并记录日志。

## API Key 默认分组

管理端“系统设置 / 用户默认设置”提供 `API Key 默认分组` 配置，对应后端设置项 `api_key_default_group_id`。

删除分组时，如果仍有 API Key 绑定该分组：

- 管理端删除确认弹框可以临时指定 `API Key 替换分组`，系统会优先把这些 Key 批量迁移到该启用分组，再删除原分组。
- 未指定替换分组时，系统继续使用已配置的有效 API Key 默认分组兜底迁移。
- 未指定替换分组且未配置默认分组：系统会拒绝删除，避免 Key 继续指向已删除分组后静默失效。
- 替换分组或默认分组就是当前要删除的分组：系统会拒绝删除，管理员需要改成其他启用分组。
- 替换分组或默认分组不存在、已停用：系统会拒绝删除，管理员需要重新选择启用状态的分组。

该默认分组只处理“删除分组时的兜底迁移”，不会自动改变新建 Key 的分组选择，也不会影响订阅默认分组配置。

## OpenAI 账号长上下文计费

管理员可在“账号管理”创建或编辑 OpenAI 账号时设置“API 长上下文计费”。该开关对应账号 `extra.openai_long_context_billing_enabled`，默认值为 `false`；只有确认该账号的真实上游会按 OpenAI API 长上下文阈值加价时才应开启。非 OpenAI 账号不会消费该字段，字符串、数字等非布尔值会以 `OPENAI_LONG_CONTEXT_BILLING_INVALID` 拒绝保存。

账号通过 `POST /api/v1/admin/accounts` 创建、通过 `PUT /api/v1/admin/accounts/:id` 编辑，批量编辑继续使用 `POST /api/v1/admin/accounts/bulk-update`。编辑请求没有携带该字段时保留账号当前值；Spark 影子账号继承父账号的有效值，父账号变更后数据库触发器同步影子账号并写入调度事件，避免父子账号采用不同计费口径。

网关只在 OpenAI token 计费路径中应用长上下文规则。分组 `long_context_pricing_enabled` 或账号 `extra.openai_long_context_billing_enabled` 任一开启即可允许使用官方长上下文阶梯；账号开关可为关闭了该能力的分组单独开启，但不能反向关闭分组已经开启的能力。达到模型长上下文阈值并实际采用对应价格时，用量记录 `usage_logs.long_context_billing_applied` 为 `true`，管理端用量列表会在实际费用旁显示长上下文标记；两个开关均关闭、未命中阈值、非 token 计费和历史记录保持 `false`。用量接口返回同名字段，前端不根据金额反推是否发生长上下文计费。

## Grok 账号导入与渠道监控

管理员在“账号管理”创建 Grok OAuth 账号时，可以粘贴一行一个的 Grok Web SSO key。前端调用 `POST /api/v1/admin/grok/sso-to-oauth`，服务端通过 xAI Device Flow 转换为 Grok Build OAuth 凭据并创建账号；批量任务以 3 路并发执行，返回 `created` 与 `failed` 两组逐项结果。导入成功后系统会主动探测账号，探测失败不会伪造配额数据，管理员可在账号列表继续查看错误并手动重试。SSO key、OAuth Token 和转换过程中的凭据只允许出现在请求和运行时日志脱敏链路中，不得写入文档或仓库。

“渠道监控”支持 `provider=grok`。Grok 探活固定使用 `chat_completions` 模式，默认端点为 `https://api.x.ai`，未填写主模型时使用 `grok-4.5`；`responses` 模式会被拒绝。监控请求使用 Bearer API Key，记录主模型和附加模型的状态、延迟与历史结果；上游错误会保留 HTTP 状态用于排障，同时对返回内容中的 xAI key 做脱敏。渠道监控同时支持基于关联账号读取用量或余额，完整模式、接口、数据表和异常规则见[渠道监控功能说明](CHANNEL_MONITOR_CN.md)。

自动化覆盖包括账号 SSO 导入接口与批量结果、Grok 主动探测、Grok 监控默认模型与请求格式、错误脱敏、长上下文账号校验与继承、计费结果和用量字段契约。上线验收仍需使用隔离账号分别验证一次 SSO 导入、手动监控运行和一笔可明确判断是否命中长上下文阈值的请求。

## 管理端专属分组授权用户

管理端“分组管理”页中，标准计费且开启 `is_exclusive=true` 的专属分组会显示用户查看入口。管理员可以查看当前仍享受该分组的用户列表，并按邮箱、用户名、用户备注或授权备注搜索。

列表接口为 `GET /api/v1/admin/groups/:id/users`，仅支持标准专属分组。公开分组和订阅分组没有 `user_allowed_groups` 授权语义，接口会拒绝查询。

列表数据来自 `user_allowed_groups` 与 `users` 表关联，只展示当前有效授权：

- `user_allowed_groups.group_id` 等于当前分组。
- 用户未被软删除，即 `users.deleted_at IS NULL`。
- 授权未过期，即 `user_allowed_groups.expires_at IS NULL OR user_allowed_groups.expires_at > NOW()`。

列表会展示用户邮箱、用户名、状态、角色、余额、并发、用户级 RPM、授权来源 `source`、关联订单 `source_order_id`、授权到期时间 `expires_at`、授权创建时间和更新时间。`expires_at` 为空表示永久授权；邀请奖励等限时授权会显示具体到期时间。授权到期后的迁移和删除仍由限时授权过期处理逻辑负责，管理端列表只反映当前有效授权状态。

## 价格展示口径

支持模型悬浮价格显示的是用户视角的最终价格，而不是原始模型单价：

- 文本、缓存、按次模型：`原始价格 * 当前有效分组倍率`。
- 用户存在专属分组倍率时，优先使用 `/api/v1/groups/rates` 返回的专属倍率。
- 图片计费模型：若 `image_rate_independent=true`，使用 `image_rate_multiplier`；否则使用当前有效分组倍率。
- 视频计费模型：若 `video_rate_independent=true`，使用 `video_rate_multiplier`；否则使用当前有效分组倍率；总价为“分辨率每秒价格 × 视频时长”，参考图可参与图生视频但不额外收费。
- 启用 `cache_hit_quarter_to_input_enabled` 的分组，会按“用户 + 分组 + 目标 + 容差 + 半衰期 + 状态代次”累计提示词 token 和调整后缓存读取 token。处理每次请求前，系统先按 `2^(-经过秒数/半衰期秒数)` 同比衰减两项历史权重，再加入本次请求。加入后未超过“目标 + 容差”时不划拨；超过后只把本次请求实际拥有且为回到目标所必需的缓存读取 token 划入普通输入，不会回溯改写历史请求。低命中时期的影响会随时间衰减，因此后续高命中流量可以逐步恢复划拨；半衰期越短，恢复越快。
- 累计状态通过 Redis 原子脚本更新，多个 API Key、多个账号和并发请求共享同一用户分组口径。分组保存后 `updated_at` 形成新的状态代次，新请求不再继承旧配置的累计值；半衰期功能使用 `cache_hit_target:v3` 状态，不继承旧版 `v2` 的永久累计值。当前状态采用 30 天滑动过期，但 30 天只表示连续无请求后的状态清理期限，不是统计周期。Redis 不可用时退化为同样带容差的单请求控制，不阻断请求与计费。
- 划拨只改变 `input_tokens` 与 `cache_read_tokens` 的分类，两者之和以及请求总 token 保持不变。统计、账单、余额、订阅额度、API Key 配额和账号配额都读取调整后的用量日志，真实上游缓存和返回给客户端的响应不变，历史用量不回填。
- `usage_logs` 保存原始普通输入、原始缓存读取、本次划拨量、目标、容差、调整后有效累计提示词、调整后有效累计缓存读取、累计命中率和状态代次。两个有效累计字段是 Redis 衰减权重四舍五入后的审计近似值，不等同于自然日原始 token 总和；管理端使用记录的 Token 明细会展示这些审计值，普通用户使用记录不返回内部控制字段。

如果同一平台下用户可访问多个分组，价格卡会按分组分别展示最终价格，避免用户误以为所有分组价格相同。

### 渠道模型分时倍率

管理端可为渠道的 `token` 模式模型定价配置 `time_pricing`，结构包含 IANA 时区 `timezone` 和每日重复的 `periods`。每个时段包含 `start_time`、`end_time` 和 `multiplier`；时间接受 `HH:mm` 或 `HH:mm:ss`，区间按左闭右开计算，`00:00` / `00:00:00` 可作为当天结束时间。时段不能重叠，开始时间必须早于结束时间，倍率至少为 `0.01` 且最多保留两位小数。

请求命中渠道定价时，系统按请求定价时刻换算到配置时区，并把对应时段倍率乘到渠道 token 成本上；未命中时段、没有配置或读取到历史脏配置时安全回退为 `1`。请求处理和用量记录共用固定的定价时刻，避免跨时段的长请求在不同阶段采用不同倍率。分组倍率、分组高峰倍率和其他既有倍率仍按原顺序参与最终计费。

分时倍率只适用于主渠道 `channel_model_pricing` 的 token 定价。`per_request`、`image`、`video` 模式会拒绝分时配置；分组模型定价和账号统计定价也不接受该字段，因为这些链路没有相同的请求时间语义。删除全部时段等同于关闭分时倍率。数据库通过 `channel_model_pricing.time_pricing` JSONB 保存配置，旧记录为空，不需要回填。

### 渠道服务层级与上下文区间倍率

管理端主渠道定价的 `token` 模式支持可选的 Fast/Flex 服务层级倍率。`fast_multiplier` 同时适用于 `fast` 和 `priority` 请求，`flex_multiplier` 适用于 `flex` 请求；未配置时继续使用模型目录中的服务层级价格或系统默认倍率。倍率必须大于 `0`，并统一作用于输入、输出、缓存写入和缓存读取成本，避免只调整部分 token 项目。账号统计自定义规则不使用 Fast/Flex 服务层级倍率。

主渠道定价和账号统计自定义规则的上下文区间都可以使用 `input_multiplier`、`output_multiplier`、`cache_write_multiplier` 和 `cache_read_multiplier`，按命中的基础价格分别计算。区间内同一计费项目同时存在显式价格和倍率时，显式价格优先；没有命中区间或对应项目均未配置时回退基础价格。主渠道的区间倍率是否参与计费仍受分组或账号的长上下文计费开关控制，最终再叠加服务层级倍率、分时倍率及有效分组倍率。

账号统计自定义规则按 `input_tokens + cache_creation_tokens + cache_read_tokens` 选择上下文区间，不把输出 token 纳入选档；命中规则后仍保持既有的规则顺序优先级，不叠加 Fast/Flex 或分时倍率。管理接口 `POST /api/v1/admin/channels`、`PUT /api/v1/admin/channels/:id` 及渠道详情响应使用同名区间倍率字段。主渠道倍率保存于 `channel_model_pricing.fast_multiplier`、`channel_model_pricing.flex_multiplier` 和 `channel_pricing_intervals.*_multiplier`；账号统计区间倍率保存于 `channel_account_stats_pricing_intervals.*_multiplier`。分组模型定价不接受这些倍率字段；旧记录字段为空，保持原计费行为，不需要回填。

### 视频渠道定价与兼容边界

显式 `billing_mode=video` 的渠道定价按视频时长计费：`per_request_price` 是渠道默认美元每秒价（USD/s），`intervals[].per_request_price` 是对应 `480p / 720p / 1080p` 层级的美元每秒价。字段名为兼容现有渠道定价结构而保留，实际费用为 `每秒价 * 视频数量 * 规范化时长秒数 * 有效倍率`。

模型广场和模型测试台按以下顺序解析视频报价；运行时的分组覆盖、显式视频渠道价和系统默认价遵循相同优先级，历史模式和 token 模式的边界见下文：

1. 当前分组对应分辨率的 `video_price_*` 覆盖价，按秒计费。
2. 当前渠道定价中与分辨率匹配的层级价。
3. 当前渠道定价的默认价。
4. 已知 Grok 视频模型的系统默认每秒价。

显式数值 `0` 是有效价格，不会被当作缺失继续回退。倍率方面，`video_rate_independent=true` 时使用 `video_rate_multiplier`；否则优先使用用户专属分组倍率，没有专属倍率时使用分组通用倍率。

历史上以 `billing_mode=image` 或 `billing_mode=per_request` 保存的视频渠道定价保持美元每请求（USD/request）语义：分组没有当前分辨率覆盖价时，渠道层级价或默认价只乘视频数量，不乘视频时长；管理端加载和保存时保留原模式，不会自动迁移为 `video`。历史模式未命中当前分辨率且没有默认价时不回退系统每秒价，避免混用单位；管理员只有在确认并重新填写每秒价格后，才能显式改为 `video`。分组如果已配置当前分辨率的 `video_price_*`，仍优先使用该分组每秒覆盖价。

`billing_mode=token` 不提供视频报价，即使分组存在视频覆盖价也不会把 token 价格解释为视频价格。未携带渠道定价的已知 Grok 视频模型可以使用系统默认每秒价；未知视频模型没有任何价格来源时显示无价格。

`grok-imagine-video-1.5` 在没有起始图的文生视频请求中会按标准模型 `grok-imagine-video` 路由和计费；上传起始图执行图生视频时才按 `grok-imagine-video-1.5` 计费。测试台的 1.5 起始图入口最多支持 1 张并在超过 1MB 时提示、压缩后提交为 `image.url`；标准版的参考图入口最多支持 4 张并提交为 `reference_images[].url`，属于独立的参考图引导工作流。参考图不额外收费；渠道 `input_price` 历史字段不再显示、不再保存，也不参与计费。模型广场只展示每个模型支持的输出分辨率；测试台根据是否上传起始图切换实际计费模型，但价格预览只显示视频输出费。标准版只展示 `480p/720p`，不为官方未提供的 `1080p` 生成伪报价。

## 图片分组当前定价口径

`ChatGPT2API 图片` 分组按以下单张价格展示并计费：

- `1K`：`0.134` 灵石 / 张
- `2K`：`0.201` 灵石 / 张
- `4K`：`0.268` 灵石 / 张

模型广场和模型测试台都会直接展示这三档价格。模型测试台调用真实 `/v1/images/generations` 网关，不绕过用量记录和扣费。
