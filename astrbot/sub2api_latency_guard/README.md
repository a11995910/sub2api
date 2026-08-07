# sub2api_latency_guard

独立的 AstrBot 插件。它读取 Sub2API `/api/v1/admin/usage` 中的 `first_token_ms`，按账号真实使用记录计算 p50、p95、EWMA、慢请求比例和尾延迟波动，并提出同级账号的 priority 调整建议。

默认 `mode=confirm`：只生成建议，交由飞书人工确认后执行。`observe` 只记录不通知，`auto` 才会在通过乐观锁校验后写入 priority。插件不会修改 `sub2api_version_monitor`，也不会主动探测上游。

API Key/JWT 只能通过 AstrBot 运行时配置注入。状态保存在 AstrBot 数据目录的 `sub2api_latency_guard_state.json`，提案默认十分钟过期；管理员手工改变 priority 后，插件拒绝覆盖。
