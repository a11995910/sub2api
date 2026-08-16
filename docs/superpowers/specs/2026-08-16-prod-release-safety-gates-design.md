# Sub2API 生产发布安全门禁设计

## 结论

生产发布改为“异机备份凭证 + 条件式健康等待 + 可重建的原镜像回滚”流程。
`release-prod` 不再在生产 VPS 创建全库归档；发布前必须存在一份绑定目标 commit、
staging run 和本次异机备份校验结果的 root-only 凭证。容器切换后必须等待 Docker
health 明确变为 `healthy`，再执行宿主机 HTTP 验证。任何失败都恢复发布前正式镜像
tag，确保后续 Compose 重建仍能找到镜像。

## 背景

2026-08-16 首次发布 `a6064326812539536c8ceb9d9b48f3551565cf9e` 时，目标容器在
14:10:48 启动，发布脚本在 14:10:49、容器仍为 `health: starting` 时立即请求
`/health`。请求收到一次连接重置，脚本将启动窗口误判为发布失败并自动回滚。

回滚成功恢复了旧二进制，但同时暴露两个发布链路问题：

- 失败恢复把 `.env` 指向临时 rollback tag，随后又删除该 tag；当前容器可继续运行，
  但未来重建无法解析该镜像引用。
- `release-prod` 在生产 VPS 重复创建全库 dump。正式备份已在隔离备份机完成并验证，
  该重复归档增加发布时间、CPU/IO 开销和 VPS 存储占用，与当前备份策略不一致。

## 目标

- 只有已验证的同一 `main` commit 才能从 staging 进入 prod。
- prod 切换前必须证明最新生产业务库已备份到指定备份机。
- 生产 VPS 不长期或临时创建全库发布归档。
- 应用启动较慢时不误判失败；真实启动失败必须在有限时间内触发回滚。
- 失败回滚后 `.env`、实际容器和可用镜像 tag 保持一致。
- 发布记录不包含密码、Token、数据库凭据或归档内容。

## 非目标

- 本次不改变 PostgreSQL schema、业务数据和应用计费逻辑。
- 本次不自动恢复数据库；数据库恢复仍是独立人工灾备操作。
- 本次不重新启用每小时异机自动备份。
- 本次不改变 staging/prod 的 Compose project、数据目录或端口隔离。

## 异机备份凭证

异机归档完成后，操作员在生产 VPS 写入
`/opt/sub2api/state/prod-backup-result.json`。该文件只保存非敏感校验结果，权限必须为
`root:root`、`0600`，字段固定为：

```json
{
  "environment": "prod-backup",
  "status": "verified",
  "target_commit": "40 位 Git commit",
  "staging_run_id": "数字 run ID",
  "backup_host": "非敏感主机标识",
  "archive": "归档文件名，不含凭据",
  "sha256": "64 位十六进制摘要",
  "size_bytes": 1,
  "toc_entries": 100,
  "zstd_verified": true,
  "pg_restore_list_verified": true,
  "verified_at": "UTC RFC3339 时间"
}
```

`release-prod` 在任何镜像或 `.env` 修改前验证：

- 文件所有者、权限和 JSON 结构符合要求；
- `status=verified`，两项归档校验均为 `true`；
- `target_commit`、`staging_run_id` 与本次发布参数完全一致；
- SHA-256、文件名、大小和 TOC 数量格式有效；
- `verified_at` 不晚于当前时间，且距当前时间不超过两小时。

凭证是人工发布流程的结构化证明，不包含备份机 SSH 密码，也不让生产 VPS 反向连接
备份机。发布失败时凭证在有效期内可用于同一 commit 重试；发布成功后保留在发布记录
中用于审计，下一次发布必须生成新凭证。

## 容器健康等待

`release-prod` 在 `compose up -d` 后按以下条件等待，最长 90 秒：

1. 解析 prod 应用容器 ID，并确认实际镜像为目标 tag。
2. 每两秒读取容器 `State.Status` 和 `State.Health.Status`。
3. 只有 `running + healthy` 才进入宿主机 HTTP 验证。
4. `exited`、`dead`、`removing` 或 `unhealthy` 立即失败。
5. 超时仍为 `starting` 时失败，并输出最近 200 行应用日志。
6. 容器 healthy 后，宿主机 `/health` 最多重试 10 秒，以覆盖端口发布的短暂竞态。

不得用固定 sleep 代替状态轮询。staging 和 prod 使用同一健康等待实现，避免两个环境
对“验证通过”的定义不同。

## 失败恢复

发布前同时记录：

- `previous_original_image`：发布前 `.env` 和容器使用的正式 tag；
- `previous_image_id`：旧镜像不可变 ID；
- `previous_rollback_image`：为本次发布创建的专用 rollback tag。

失败恢复必须按以下顺序执行：

1. 将 prod `.env` 恢复为 `previous_original_image`，不能写入临时 rollback tag。
2. 确认该 tag 仍解析到 `previous_image_id`。
3. 使用原 tag 重建应用容器，不重启 PostgreSQL 或 Redis。
4. 按与发布相同的条件等待旧容器 healthy，并验证宿主机 `/health`。
5. 只有恢复成功后才能删除未记录的临时 rollback tag。
6. 若恢复任一步失败，保留所有镜像和非敏感现场并返回明确错误，不继续清理。

发布成功时保留发布前 rollback tag，继续执行现有“最多保留两份专用回滚 tag”策略。

## 发布记录

`prod-release-before-*.txt` 继续记录旧镜像、旧 commit、目标 commit、staging run、定价
快照和快速策略快照。原 `database_backup` 字段替换为：

- `external_backup_receipt`
- `external_backup_archive`
- `external_backup_sha256`
- `external_backup_verified_at`

VPS 仍可保留小型定价和快速策略快照，用于精确回滚配置；它们不属于全库归档。

## 测试策略

新增部署脚本测试，至少覆盖：

- 备份凭证缺失、权限错误、commit/run 不匹配、过期和校验标记为 false 时拒绝发布；
- `starting -> healthy` 时等待后成功，不提前执行宿主机 curl；
- `unhealthy`、容器退出和超时时失败；
- 发布失败时恢复 `previous_original_image`，不会让 `.env` 指向已删除 tag；
- 发布脚本不再调用全库 `pg_dump`，仍保留定价与快速策略快照；
- staging/prod 两条工作流调用同一健康等待实现。

测试按红绿循环执行：先用当前脚本确认新增测试因缺少门禁而失败，再实施最小修改并运行
全部部署脚本测试、`bash -n`、`git diff --check`。

## 发布顺序

1. 提交并推送脚本、工作流、测试和当前状态文档。
2. VPS 拉取新的 `origin/main`，安装同 commit 的 root-only 发布脚本及辅助脚本。
3. 使用新 commit 构建并部署 staging，等待 healthy，验证关键页面和日志。
4. 对最新生产业务库执行异机备份，生成与新 commit/staging run 绑定的凭证。
5. 用户再次明确确认后，通过 `release-prod` 切换 prod。
6. 验证实际镜像、版本、内外网健康、反代、关键页面/API、数据库连接和错误日志。
7. 成功后确认 VPS 没有 `prod-database-before-*.dump` 全库归档。

## 回滚与停止条件

- 异机备份凭证无效或超过两小时：停止，不切容器。
- staging commit 与目标 commit 不一致：停止，重新 staging。
- 新容器 90 秒内未 healthy：自动恢复原正式 tag。
- 旧容器恢复失败：停止自动清理并立即人工处理。
- 生产健康异常、数据库连接异常或错误日志持续增长：回滚镜像，保留异机归档。
