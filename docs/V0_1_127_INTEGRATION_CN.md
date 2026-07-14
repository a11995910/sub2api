# Sub2API v0.1.127 官方增量整合说明

本文档记录当前 fork 整合官方 `v0.1.127` 增量后的功能、数据库和上线注意点。当前 fork 与官方仓库没有共同 Git 历史，本次整合采用官方 `v0.1.126..v0.1.127` 差异补丁应用到定制分支的方式完成。

## 整合范围

- 来源仓库：`https://github.com/Wei-Shaw/sub2api.git`
- 目标仓库：`https://github.com/a11995910/sub2api.git`
- 官方标签区间：`v0.1.126..v0.1.127`
- 当前整合分支：`codex/integrate-v0.1.127`

官方标签中的 `backend/cmd/server/VERSION` 文件仍显示 `0.1.126`。核查相邻标签后可见该文件长期比 Git tag 滞后一版，因此本次以 Git tag `v0.1.127` 作为整合来源，不单独改写版本文件。

## 主要功能变化

- 新增钉钉 OAuth 登录、绑定和待注册补全流程，管理端设置增加钉钉连接配置项。
- 新增不同登录来源的默认权益配置，覆盖 GitHub、Google、钉钉等来源。
- 用户和管理端用量展示增加按平台拆分、图片尺寸统计和成本展示。
- OpenAI / Responses / Chat Completions 兼容逻辑增强，包括静默拒绝识别、Responses 路由、reasoning 内容保留和缺失 refresh token 的账号保护。
- 上游模型同步能力增加后端服务、管理端 API 和前端入口。
- 兑换码支持设置自身过期时间；订阅兑换过期后续期会重置用量窗口。
- 账号编辑接口合并敏感凭证字段，避免前端脱敏数据在全量编辑时清空已有 token。
- Docker 前端构建阶段跟随官方固定到 `pnpm@9`，同时保留本 fork 的 Node 内存限制。

## 数据库迁移

本次新增以下迁移文件：

- `backend/migrations/136_add_dingtalk_provider_type.sql`
- `backend/migrations/136_usage_log_image_size_metadata.sql`
- `backend/migrations/137_redeem_code_expires_at.sql`

迁移影响：

- 扩展 `users`、`auth_identities`、`auth_identity_channels`、`pending_auth_sessions` 中登录来源或 provider 类型约束，加入 `dingtalk`。
- `usage_logs` 新增图片输入尺寸、输出尺寸、尺寸来源和尺寸拆分 JSON 字段，并补充图片计费尺寸约束。
- `redeem_codes` 新增 `expires_at` 字段和索引，用于兑换码自身有效期。

上线前必须先确认生产数据库迁移链路正常；上线后重点检查登录、用量统计、兑换码和账号列表接口。

## 冲突处理口径

本次冲突都已按“保留官方修复，同时保留本 fork 定制能力”的原则解决，未发现需要业务取舍后才能继续的功能冲突。

- `Dockerfile`：采用官方 `pnpm@9`，保留本 fork 的 `NODE_OPTIONS=--max-old-space-size=1536`。
- `backend/internal/service/admin_service.go`：账号编辑同时保留官方敏感凭证合并逻辑和本 fork 的 OpenAI 必备模型映射兜底。
- `backend/internal/service/subscription_service.go`：保留官方过期订阅续期重置用量窗口逻辑，并保留本 fork 的外层事务复用语义。
- `frontend/src/views/admin/UsersView.vue` 与用量组件：保留官方按平台拆分和列设置能力，兼容本 fork 既有页面结构。
- 前端测试兼容：对新增登录来源默认值、本地分页配置签名、用量金额格式化、账号用量刷新参数和 OAuth 注册断言做了兼容修正。

## 验证记录

本地已完成以下验证：

- `pnpm --dir frontend exec vue-tsc --noEmit`
- `pnpm --dir frontend test:run`
- `go test ./...`
- `make build-deploy`
- `./backend/bin/server --version`
- `git diff --cached --check`

`make build-deploy` 构建过程中出现 Vite chunk size 和动态/静态重复导入警告，这是当前构建配置下的体积提示，不影响构建结果。

## 上线注意事项

线上按 `docs/SOURCE_DEPLOY_CN.md` 的单正式 VPS 镜像化流程执行：VPS 从 GitHub 拉取已推送 commit，在隔离 staging 构建并验证镜像，用户确认后再将同一 commit 切换到 prod；不能把本地构建产物或临时二进制直接上传覆盖生产。

上线后必须重点回归：

- `/health`、`/purchase`、`/models`
- 管理端 `/admin/accounts` 和 `/api/v1/admin/accounts`
- 管理端设置页的钉钉、OAuth 默认权益、上游模型同步配置
- 管理端和用户端用量页面
- 兑换码创建、兑换、过期兑换码提示
- OpenAI OAuth 账号编辑、用量刷新和 Responses / Chat Completions 请求链路
