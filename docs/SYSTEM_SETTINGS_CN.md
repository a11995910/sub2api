# 系统设置功能说明

## 通用设置

管理员在后台 `系统设置 > 通用设置` 中维护站点基础信息和顶部快捷入口。配置通过 `settings` 表的 key/value 记录保存，不需要独立数据表。

顶部快捷入口配置包含以下字段：

| 配置项 | settings key | 说明 |
| --- | --- | --- |
| 启用状态 | `quick_link_enabled` | 控制登录后顶部栏是否展示快捷入口，默认关闭。 |
| 入口文案 | `quick_link_text` | 顶部栏展示的链接文案，启用时不能为空，保存前会去除首尾空白。 |
| 入口链接 | `quick_link_url` | 点击后打开的外部链接，启用时必须是 `http(s)` 绝对地址，保存前会去除首尾空白。 |

配置读取与保存入口：

| 入口 | 说明 |
| --- | --- |
| `GET /api/v1/admin/settings` | 管理端读取完整系统设置，返回顶部快捷入口配置字段。 |
| `PUT /api/v1/admin/settings` | 管理端保存系统设置，支持更新 `quick_link_enabled`、`quick_link_text`、`quick_link_url`。 |
| `GET /api/v1/settings/public` | 公开设置返回顶部快捷入口配置，供用户端顶部栏渲染。 |

展示规则：

- 桌面端登录后在顶部栏右侧展示胶囊式外部链接。
- 移动端收进用户下拉菜单，避免挤压顶部栏。
- 未启用、文案为空或链接为空时，用户端不展示该入口。

## 每日签到配置

管理员在后台 `系统设置 > 功能开关` 中维护每日签到配置。配置通过 `settings` 表的 key/value 记录保存，不需要独立数据表。

每日签到配置包含以下字段：

| 配置项 | settings key | 说明 |
| --- | --- | --- |
| 启用状态 | `checkin_enabled` | 控制每日签到能力是否对用户端开放，默认关闭。 |
| 签到内容 | `checkin_content` | 用户端展示的签到说明文案，空值回退为 `每日签到`。 |
| 每日固定奖励 | `checkin_daily_reward` | 每次签到固定发放的灵石，启用签到时必须大于 `0`。 |
| 第 4 天额外奖励 | `checkin_extra_reward_4` | 用户当月第 4 次签到时额外发放的灵石，必须为非负数。 |
| 第 16 天额外奖励 | `checkin_extra_reward_16` | 用户当月第 16 次签到时额外发放的灵石，必须为非负数。 |

配置读取与保存入口：

| 入口 | 说明 |
| --- | --- |
| `GET /api/v1/admin/settings` | 管理端读取完整系统设置，返回每日签到配置字段。 |
| `PUT /api/v1/admin/settings` | 管理端保存系统设置，支持更新每日签到配置字段。 |
| `GET /api/v1/settings/public` | 公开设置返回每日签到展示配置，供用户端决定是否展示签到入口。 |

保存规则：

- 签到内容保存前会去除首尾空白；空内容按默认文案保存。
- 每日固定奖励和额外奖励必须是非负数。
- 启用每日签到时，每日固定奖励必须大于 `0`。
- 旧客户端未提交每日签到字段时，系统保留已有配置。
- 旧版 `checkin_reward_min` / `checkin_reward_max` 只保留兼容读取；当新字段 `checkin_daily_reward` 为 `0` 时，会使用旧版 `checkin_reward_max` 作为每日固定奖励。

用户签到接口、签到记录、重复签到限制、并发幂等控制和余额发放逻辑由签到业务模块实现。完整规则见 `docs/CHECKIN_CN.md`。

## 运维高级设置

管理员在后台 `运维监控 > 设置` 中维护运维高级设置。配置通过 `settings` 表的 `ops_advanced_settings` JSON 保存，读取和保存接口为：

| 入口 | 说明 |
| --- | --- |
| `GET /api/v1/admin/ops/advanced-settings` | 读取运维高级设置。 |
| `PUT /api/v1/admin/ops/advanced-settings` | 保存运维高级设置。 |

当前高级设置包含数据保留、聚合开关、错误过滤、展示开关、自动刷新和 OpenAI 账号配额自动暂停默认阈值。

OpenAI 账号配额自动暂停配置位于 `openai_account_quota_auto_pause`：

| 配置项 | JSON 字段 | 说明 |
| --- | --- | --- |
| 5 小时默认阈值 | `default_threshold_5h` | 按 `0~1` 保存，后台按百分比展示；`0` 表示不启用全局默认阈值。 |
| 7 天默认阈值 | `default_threshold_7d` | 按 `0~1` 保存，后台按百分比展示；`0` 表示不启用全局默认阈值。 |

保存规则：

- 两个阈值必须在 `0~1` 范围内。
- 单个 OpenAI 账号配置了 `extra.auto_pause_5h_threshold` 或 `extra.auto_pause_7d_threshold` 时，优先使用账号级阈值。
- 单个 OpenAI 账号配置了 `extra.auto_pause_5h_disabled=true` 或 `extra.auto_pause_7d_disabled=true` 时，对应窗口不会使用账号级阈值或全局默认阈值。
- 设置保存后会刷新服务内存缓存，OpenAI 调度热路径读取缓存值，不在每次请求中阻塞查询数据库。
