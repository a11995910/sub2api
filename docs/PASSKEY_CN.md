# Passkey 登录功能说明

## 功能入口

Passkey 是基于 WebAuthn 的无密码登录方式，使用发现式凭据完成登录。用户登录页在服务端启用并完成 RP 配置、且当前浏览器支持 WebAuthn 时显示 Passkey 登录入口；用户资料页提供凭据注册、重命名和删除入口。后台模式仍只允许管理员登录。

## 配置与权限

部署配置位于 `webauthn`：

- `webauthn.enabled`：是否完成 WebAuthn 部署配置，默认 `false`。
- `webauthn.rp_display_name`：凭据注册时显示的信赖方名称。
- `webauthn.rp_id`：信赖方 ID，只填写域名，不包含协议、端口或路径。
- `webauthn.rp_origins`：允许的完整浏览器 Origin。生产环境使用 HTTPS；HTTP 仅允许 localhost 场景。

配置校验通过后，管理员可以在“系统设置 → 安全”维护 `passkey_enabled`。数据库开关不能替代或放宽无效的 WebAuthn 部署配置；部署配置关闭或不完整时，公开设置和 Passkey 接口均按未启用处理。生产环境必须使用 HTTPS，并确保 RP ID、Origin 与用户实际访问地址一致。

## 用户流程

1. 用户登录页请求公开设置。Passkey 已启用且浏览器支持时，用户点击 Passkey 登录。
2. 前端调用登录开始接口取得一次性会话令牌和 WebAuthn assertion options，浏览器完成用户验证后提交 credential。
3. 后端校验会话、凭据、用户 handle、计数器和用户状态，成功后创建普通 Sub2API token 会话；Passkey 登录不会再进入独立的 TOTP 挑战流程。
4. 已登录用户在个人资料页输入当前账户密码，调用注册开始接口并完成浏览器注册；密码用于防止被窃会话静默添加凭据。
5. 用户可以重命名凭据。删除凭据时必须再次输入当前账户密码；删除不存在或不属于当前用户的凭据不会泄露其他用户数据。

## 接口

认证接口位于 `/api/v1/auth`，受认证入口限流保护：

- `POST /passkey/login/begin`：开始发现式登录。
- `POST /passkey/login/finish`：提交浏览器 assertion 并换取普通登录 token。

用户接口位于 `/api/v1/user/passkeys`，均要求当前用户 JWT：

- `GET /api/v1/user/passkeys`：列出当前用户凭据摘要，不返回原始 credential 数据。
- `POST /api/v1/user/passkeys/register/begin`：提交账户密码并开始注册。
- `POST /api/v1/user/passkeys/register/finish`：提交一次性会话令牌、名称和浏览器 credential。
- `PATCH /api/v1/user/passkeys/:id`：重命名当前用户的凭据。
- `DELETE /api/v1/user/passkeys/:id`：提交账户密码并删除当前用户的凭据。

公开设置接口 `GET /api/v1/settings/public` 返回 `passkey_enabled`；管理端设置接口返回 `passkey_enabled`、`passkey_configured`、`passkey_rp_id` 和 `passkey_rp_origins`，不返回秘密材料。

## 数据与安全边界

迁移 `backend/migrations/191_passkey_credentials.sql` 创建 `passkey_user_handles` 和 `passkey_credentials`。凭据原始数据保存在 `credential_data` JSONB 中，用户删除时通过外键级联清理；凭据 ID 唯一，用户列表按创建时间倒序返回，并记录最近使用时间。

注册和删除使用账户密码校验，登录要求 WebAuthn 用户验证；登录会话令牌存入 Redis 并设置过期时间，完成接口消费后不能重复使用。凭据列表、重命名和删除始终按当前 JWT 用户 ID 与凭据 ID 联合校验。Passkey 功能关闭、会话无效、验证失败、凭据不存在或重复注册时，后端分别返回对应错误，不降级为匿名操作。

## 验证范围

后端覆盖配置校验、Passkey 服务、认证处理器、用户接口和接口契约测试；前端覆盖 API 序列化、登录入口、资料页凭据管理和设置页开关。常规验证入口为 `go test ./...`、`pnpm typecheck` 和 `pnpm test:run`。浏览器端完整验收还应使用 HTTPS 测试域名验证注册、登录、重命名和删除流程，并检查不同设备的同步凭据行为。
