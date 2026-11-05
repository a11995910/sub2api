# 注册邮箱域名黑名单

注册邮箱黑名单通过 `settings.registration_email_suffix_blocklist` 运行时配置维护，值为 JSON 字符串数组。它独立于注册白名单和非白名单域名限量开关，命中黑名单时优先拒绝，不因白名单为空或域名被列入白名单而放行。

- `@example.com` 或 `example.com`：匹配该域名。
- `*.example.com`：匹配该主域及所有子域。
- 域名大小写和末尾根点会归一化；空数组表示不限制。
- 配置缺失时兼容旧行为；配置读取失败、JSON 或域名格式无效时拒绝注册，返回服务不可用。

黑名单覆盖普通邮箱注册、注册验证码发送、OAuth 邮箱注册及旧 OAuth 自动建号路径。邮箱绑定也复用邮箱策略校验。命中时返回 `403 / EMAIL_DOMAIN_BLOCKED`，提示使用其他邮箱；人机验证码等前置校验仍按既有流程执行。

黑名单不自动改变已有账号状态。处置已注册账号时，先保存受影响账号的 ID 和原状态到正式机 root-only 目录，再通过 `PUT /api/v1/admin/users/:id` 设置 `{"status":"disabled"}`。管理接口会同步清除关联 API Key 认证缓存；不要只直接修改数据库状态。

该运维配置不在公开设置接口中返回，现有管理页面保存其他设置不会覆盖它。维护时先只读确认旧值并定向备份，再在事务中写入目标 JSON 数组；禁止复制全部系统设置或记录密钥。示例：

```sql
BEGIN;
INSERT INTO settings (key, value, updated_at)
VALUES ('registration_email_suffix_blocklist', '["*.example.com"]', now())
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at;
COMMIT;
```

有已有条目时应合并保留，不能直接用示例覆盖。修改后即时读取生效，无需重启；验证目标域名被拒绝、非目标域名保持原策略。回滚时恢复保存的旧配置（原先不存在则删除该键）；账号只恢复本次由 active 改成 disabled 的 ID，原本禁用的账号保持禁用。
