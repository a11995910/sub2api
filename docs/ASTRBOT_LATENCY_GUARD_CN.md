# AstrBot 首字节延迟调度守卫

## 作用

`sub2api_latency_guard` 是独立 AstrBot 插件，不修改 Sub2API 网关调度代码，也不修改现有 `sub2api_version_monitor`。它定时读取管理员接口 `/api/v1/admin/accounts` 和 `/api/v1/admin/usage`，使用真实请求记录中的 `first_token_ms` 评估账号健康度。

评分使用最近窗口的 p50、p95、EWMA、慢请求比例和 `p95/p50` 波动系数。少于 10 条只观察；持续慢请求产生 5/10 分惩罚，尾部波动产生 2 分惩罚；单次尖峰不触发切换。惩罚加到管理员原始 priority 上，数字越大越靠后。恢复需要连续三个正常周期。

## 同级切换

账号按平台、套餐/账号类型和共享分组判断同级。Plus 变慢时先推荐健康 Plus；只有同级没有可用账号时，后续调度层才按项目现有规则考虑 Pro 或上游账号。插件本身不会主动探测上游，也不会替管理员改变套餐等级。

## 配置与运行模式

复制 `astrbot/sub2api_latency_guard` 到 AstrBot 插件目录，在插件配置中设置：

- `sub2api_base_url`：Sub2API 地址。
- `sub2api_admin_api_key` 或 `sub2api_jwt`：只从 AstrBot 运行时密钥配置注入，不写入仓库。
- `mode`：`observe` 只记录，`confirm`（默认）发送飞书卡片等待管理员，`auto` 自动执行但仍会做 priority 乐观锁校验。
- `notify_platform`、`notify_target_id`：复用 AstrBot 已登录的飞书平台和会话。
- `admin_ids`：允许确认调整的管理员用户 ID。
- `ai_enabled`、`ai_provider_id`：可选的 AI 中文解释；AI 只处理脱敏后的聚合指标，不能决定 priority 或执行写入。

插件状态保存在 AstrBot 数据目录的 `sub2api_latency_guard_state.json`，其中包括滚动惩罚、最近提案、确认码和过期时间。确认码默认 10 分钟有效。

## 安全边界与回滚

每次写入前插件都会重新读取账号；如果管理员期间手动修改了 priority，则拒绝覆盖并要求重新扫描。自动模式只适合已经观察稳定的环境，建议先运行 `observe`，再使用默认 `confirm`。如需回滚，直接在 Sub2API 管理页面把账号 priority 改回原值，下一轮扫描会重新记录手工值，不会强制覆盖。

手工操作命令：`/latency_guard scan` 触发扫描，`/latency_guard confirm <确认码>` 执行单条提案。
