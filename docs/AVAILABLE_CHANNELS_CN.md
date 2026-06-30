# 可用渠道展示说明

## 目标

用户在创建 API 密钥前，需要能清楚判断自己可以访问哪些分组、哪些分组支持图片生成，以及模型价格在对应分组倍率下的最终展示价格。

## 分组展示

`GET /api/v1/channels/available` 会按渠道和平台返回用户可访问的分组。用户侧分组字段包含：

- `rate_multiplier`：分组默认文本倍率。
- `allow_image_generation`：该分组是否允许图片生成。
- `image_super_resolution_enabled`：该分组的图片生成结果是否会在返回前自动执行 4K 超分。
- `image_2k_enhancement_enabled`：该分组命中显式 2K 生图时，是否优先调用另一个图片分组做二段提升。
- `image_2k_enhancement_group_id`：二段 2K 提升使用的目标图片分组 ID；仅管理端配置和内部调度使用，用户侧无需手动传参。
- `image_4k_enhancement_enabled`：该分组命中 4K 生图时，是否优先调用另一个图片分组做二段提升。
- `image_4k_enhancement_group_id`：二段 4K 提升使用的目标图片分组 ID；仅管理端配置和内部调度使用，用户侧无需手动传参。
- `image_rate_independent`：图片生成是否使用独立倍率。
- `cache_hit_quarter_to_input_enabled`：缓存命中重新计费开关。开启后，本次请求有缓存读取 token 时，会把缓存读取 token 的四分之一按整数向下取整划入输入 token，再用调整后的 token 分类写入用量记录并扣除余额、订阅额度、API Key 配额和账号配额；历史用量不回填。
- `image_rate_multiplier`：图片独立倍率，仅 `image_rate_independent=true` 时生效。
- `image_price_1k`、`image_price_2k`、`image_price_4k`：图片生成 1K、2K、4K 单张基础价；为空时后端真实计费会回退默认图片价格。

前端“可用渠道”页会将“我可访问的分组”作为独立区域展示。支持图片生成的分组会显示“图片可用”标签，用户在创建 API 密钥前即可识别图片分组。

图片分组存在以下后处理方式：

- `image_2k_enhancement_enabled=true` 时，非流式且显式声明 2K `size` 的图片请求会先生成基础图片，再调用 `image_2k_enhancement_group_id` 指向的 OpenAI 图片分组做二段提升。网关会把原请求 `size` 原样传给二段请求，例如 `2048x2048` 或 `2048x1152`，并在目标分组返回 PNG/JPEG 内联图片时按该 `size` 校正最终图片像素；目标分组失败最多尝试 3 次，仍失败时保留基础图片返回。未传 `size` 的默认 2K 请求不会触发二段提升，避免默认生图流量被误放大。
- `image_4k_enhancement_enabled=true` 时，非流式 4K 图片请求会先生成基础图片，再调用 `image_4k_enhancement_group_id` 指向的 OpenAI 图片分组做二段提升。网关会把原请求 `size` 原样传给二段请求，例如 `3840x2160`，并在目标分组返回 PNG/JPEG 内联图片时按该 `size` 校正最终图片像素，避免元数据与实际尺寸不一致；目标分组失败最多尝试 3 次，仍失败时保留基础图片返回。
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
- 启用 `cache_hit_quarter_to_input_enabled` 的分组，缓存读取 token 会先按四分之一划入输入 token 后再计算费用和写入用量日志；统计、账单、余额消耗都读取调整后的用量日志，保持同一展示口径。

如果同一平台下用户可访问多个分组，价格卡会按分组分别展示最终价格，避免用户误以为所有分组价格相同。

## 图片分组当前定价口径

`ChatGPT2API 图片` 分组按以下单张价格展示并计费：

- `1K`：`0.134` 灵石 / 张
- `2K`：`0.201` 灵石 / 张
- `4K`：`0.268` 灵石 / 张

模型广场和模型测试台都会直接展示这三档价格。模型测试台调用真实 `/v1/images/generations` 网关，不绕过用量记录和扣费。
