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
| 第 4 天额外奖励 | `checkin_extra_reward_4` | 用户每个连续签到奖励周期第 4 天额外发放的灵石，必须为非负数。 |
| 第 16 天额外奖励 | `checkin_extra_reward_16` | 用户每个连续签到奖励周期第 16 天额外发放的灵石，必须为非负数。 |

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

## 邮件与 SMTP 配置

管理员在后台 `系统设置 > 邮件` 中维护 SMTP 配置，SMTP 配置入口不依赖邮箱验证开关；即使未启用邮箱验证，也可以提前配置、测试和保存邮件服务。邮箱验证和密码重置是否实际对用户开放仍由 `系统设置 > 安全与认证` 中的对应开关控制。配置通过 `settings` 表的 key/value 记录保存，不需要独立数据表。

SMTP 配置包含以下字段：

| 配置项 | settings key | 说明 |
| --- | --- | --- |
| SMTP 主机 | `smtp_host` | SMTP 服务器地址，未配置时邮件服务不可用。 |
| SMTP 端口 | `smtp_port` | SMTP 服务端口，默认 `587`。 |
| SMTP 用户名 | `smtp_username` | SMTP 登录用户名。 |
| SMTP 密码 | `smtp_password` | SMTP 登录密码，后台保存时允许留空以保留原密码。 |
| 发件人邮箱 | `smtp_from` | SMTP envelope sender 和邮件 From 地址。 |
| 发件人名称 | `smtp_from_name` | 邮件 From 展示名称。 |
| 使用 TLS | `smtp_use_tls` | 为 `true` 时使用 TLS 直连；为 `false` 时连接后如服务端支持则尝试 STARTTLS。 |
| 备用 SMTP 服务 | `smtp_fallbacks` | JSON 数组，按顺序保存备用 SMTP 配置；每项包含 `host`、`port`、`username`、`password`、`from_email`、`from_name`、`use_tls`。 |

发送策略：

- 系统发送注册验证码或密码重置邮件时，优先使用主 SMTP 配置。
- 主 SMTP 连接、认证或投递失败时，系统会按 `smtp_fallbacks` 中的顺序尝试备用 SMTP 服务，直到某个服务发送成功或所有配置都失败。
- 后台读取设置时不会返回主 SMTP 密码或备用 SMTP 密码明文，只返回是否已配置密码；保存时主 SMTP 密码留空会保留原密码，备用 SMTP 密码留空会按同序号保留原密码。

邮件发送范围：

- 系统只允许注册邮箱验证码和密码重置邮件实际投递。
- 注册邮箱验证码通过 `auth.verify_code` 事件或内置验证码模板发送，验证码写入缓存并受 1 分钟冷却和 15 分钟有效期限制。
- 密码重置邮件通过 `auth.password_reset` 事件或内置密码重置模板发送，重置令牌写入缓存并受 30 秒冷却和 30 分钟有效期限制。
- 订阅到期提醒、余额不足提醒、账号限额通知、支付成功通知、风控通知、运营告警、定时报表、额外通知邮箱验证等邮件事件不会投递到 SMTP，用于避免普通 SMTP 账号被非必要通知耗尽每日发信额度。
- 后台 SMTP 测试入口只验证 SMTP 连接和登录认证，不发送真实测试邮件。

相关接口：

| 入口 | 说明 |
| --- | --- |
| `GET /api/v1/admin/settings` | 管理端读取 SMTP 配置展示字段，密码仅返回是否已配置。 |
| `PUT /api/v1/admin/settings` | 管理端保存主 SMTP 与备用 SMTP 配置，密码为空时保留已有密码。 |
| `POST /api/v1/admin/settings/send-test-email` | 使用提交的 SMTP 配置测试连接和认证，不发送真实邮件。 |
| 注册验证码发送入口 | 根据注册流程提交邮箱后进入邮件队列，队列任务类型为 `verify_code`。 |
| 密码重置发送入口 | 根据忘记密码流程提交邮箱后进入邮件队列，队列任务类型为 `password_reset`。 |

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
