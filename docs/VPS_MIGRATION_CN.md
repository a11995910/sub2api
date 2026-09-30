# VPS 运行架构说明

本文档描述 Sub2API 当前正式 VPS 的运行拓扑、目录、发布顺序和回滚边界。项目只有一台正式 VPS，不存在独立测试 VPS；旧主机不再作为 Sub2API 正式线上环境。

## 迁移准备目标

当前生产入口仍在 `205.185.113.15`。准备迁往 `185.61.210.32`，附加地址为 `185.61.210.33` 至 `185.61.210.36`；本机通过 `ssh sub2api-migration-vps` 使用独立 Ed25519 密钥登录 root。新主机为 Ubuntu 24.04、40 个逻辑 CPU、约 62GiB 内存、1.8TiB 根磁盘。服务器密码与私钥不进入 Git。

迁移准备包含 Docker、Compose、Buildx、Nginx、隔离 staging、运行配置及业务数据恢复验证。正式入口切换需用户另行明确授权；准备阶段不修改 DNS、不停止旧站。目标主机上的生产数据副本不得启动会刷新 OAuth、处理支付或执行后台任务的第二套生产应用。

原定北京时间 2026-09-27 20:30 已由用户调整为本次实际一致性快照时间。PostgreSQL 使用一致性逻辑快照，经加密 SSH 传输直接写入目标主机 `/opt/sub2api/migration/`，记录快照时间、源版本、校验值及恢复结果。原服务器不落地全库 dump；不得直接复制运行中的 PostgreSQL 数据目录。目标恢复使用与源端一致的 PostgreSQL 主版本和扩展。

正式切换采用短暂停写后的最终一致性快照替换目标副本，同时最终同步应用文件及 Redis 状态；这样覆盖基线后全部新增、更新、删除和序列变化。此方式属于最终全量追平，不是按时间戳筛选的增量导入。切换前先测量导出、传输和恢复耗时并报告维护窗口；旧站保持唯一写入源，直到最终同步完成。若维护窗口不可接受，应先单独验证物理复制方案，不能临场跳过一致性检查。

准备阶段的 staging 使用全新独立数据库及密钥。生产基线保存在隔离、无应用写入的恢复数据库中；恢复核对表数量、关键表行数、迁移账本及数据库完整性。Redis 与文件快照单独记录采集时间，不能声称它们和数据库构成跨服务原子快照。正式切换前需在暂停写入后再同步这些状态。

## 正式 VPS

| 项目 | 当前值 |
| --- | --- |
| 地址 | `205.185.113.15` |
| 登录账户 | `root` |
| 本机 SSH 别名 | `sub2api-new-vps` |
| 源码目录 | `/opt/sub2api/repo` |
| 源码分支 | 只允许 `main` |
| 部署方式 | VPS 拉取 Git、VPS 本机构建 Docker 镜像 |
| 预发布入口 | staging，宿主机端口 `18080` |
| 正式入口 | prod，宿主机端口 `8080` |

当前正式 VPS 实测资源为 4 vCPU、约 16GiB 内存、4GiB Swap，可用磁盘约 276GiB。staging 和 prod 构建共用 `deploy/release-gates check-build-resources` 门禁：至少保留 20GiB 磁盘、12GiB 总内存、4GiB 可用内存；通过 `/proc/stat` 间隔 1 秒采样的整机 CPU 使用率必须不超过 50%，idle 和 iowait 不计入占用，采样失败拒绝发布，不检查 load average。`GOMAXPROCS` 根据在线 CPU、可用内存和默认上限 8 动态计算，按每个编译并行槽 2GiB 可用内存估算；在该主机基线下通常为 4。门禁失败时禁止继续 Docker 构建。

服务器密码、SSH 私钥、Token、数据库密码、OAuth 密钥和 Cookie 不得写入仓库、文档、镜像 tag 或日志。登录优先使用 SSH Key；运行配置只保存在服务器 root-only 文件中。

## 环境隔离

staging 和 prod 位于同一台服务器，但必须保持以下隔离：

- compose project 分别为 `sub2api-staging` 和 `sub2api-prod`。
- 环境文件分别为 `/opt/sub2api/env/staging/.env` 和 `/opt/sub2api/env/prod/.env`。
- compose override 分别为 `/opt/sub2api/compose/staging/docker-compose.yml` 和 `/opt/sub2api/compose/prod/docker-compose.yml`。
- 数据目录分别位于 `/opt/sub2api/data/staging` 和 `/opt/sub2api/data/prod`。
- PostgreSQL、Redis、应用容器、宿主机端口和业务测试数据不得跨环境复用。

仓库基础 compose `/opt/sub2api/repo/deploy/docker-compose.yml` 必须与环境 override 同时加载，不能单独执行 override。两个环境都通过各自 `.env` 中唯一的 `SUB2API_IMAGE` 选择镜像。

## 发布顺序

1. 本地在 `main` 直接完成修改，或在临时分支完成修改和自动化测试后合并回 `main`；推送 `main` 前必须完成本地验证并清理临时分支。
2. 正式 VPS 的 `/opt/sub2api/repo` 只能检出 `main`，拉取已推送的目标 `origin/main` commit，并使用 `deploy/Dockerfile` 构建 `sub2api:staging-<commit>`。
3. 备份 staging 数据后，在隔离 staging 启动镜像并验证版本、健康接口、关键页面、API、数据库迁移和日志。
4. staging 验证通过后报告结果，等待用户明确口头确认。
5. 核对 VPS 仍位于 `main`，且当前 commit 与 staging 已验证 commit 完全一致；不得在 staging 验证后再合并代码或更换 commit。
6. 使用受版本控制的 `deploy/release-prod` 完成同一 commit 和 staging run、资源、版本能力与定价策略校验，记录 prod 当前镜像并保留原 `.env`，再把已验证镜像标记为 `sub2api:prod-<commit>`，原子更新 prod 的 `SUB2API_IMAGE`，只重建 Sub2API 应用容器。发布不执行异机备份、不校验备份凭证，也不创建 prod 全库 dump；PostgreSQL 和 Redis 不得因应用发布被重建或清空。
7. 完成容器、健康接口、HTTPS、管理端账号页、`/api/v1/admin/accounts`、`/purchase`、`/model-market`、数据库连接和日志回归。

### 新主机首次 staging bootstrap

迁移到新正式 VPS 时，staging 必须先于 prod 验证。仅在用户已明确授权、且目标主机完全没有 prod 配置、compose 容器和数据文件时，可以执行：

```bash
/opt/sub2api/scripts/release-staging "$expected_commit" --bootstrap-without-prod
```

该参数只取代“同机 prod 已健康”这一项前置条件；磁盘、内存、CPU 使用率、干净 `main`、精确 commit、镜像版本、Docker health、HTTP 和验证回执门禁保持不变。普通 staging 发布不传该参数，仍必须先确认 prod 健康。

## 构建与版本追溯

### 新机 prod 数据副本验收

新机尚无 prod 应用时，先把一致性快照恢复到独立的 prod PostgreSQL、Redis 和应用目录，再执行 `deploy/release-prod <prod.env> <target-image> <target-commit> <staging-run-id> --bootstrap-isolated`。该入口要求数据库与 Redis 健康、无现存 prod 应用、应用所用网络全部为 `internal: true`，并确认目标镜像与同提交 staging 镜像 ID 一致。保留资源、版本能力和迁移结构快照检查；应用健康验证通过后写入 root-only 首次启动回执。失败会停止应用并保留数据供检查，不覆盖旧站。

隔离验收支持页面、登录及管理数据查看。外部模型、支付、邮件、OAuth 刷新等调用不可用；副本内发生的编辑不会合回旧站，正式同步会覆盖这些测试编辑。Nginx 可代理到隔离容器供人工查看，内部数据库与 Redis 不向公网开放。正式接流必须另行完成最终一致性同步、解除验收隔离和域名切换。

如果新机已恢复 prod 数据库但尚未创建 prod 应用，发布工具修订可使用 `release-staging <commit> --bootstrap-isolated-prod` 重新验证。该模式要求 prod 所有服务网络均为 internal 且不存在 prod 应用容器，不允许用它绕过已接流生产环境的健康门禁；回执单独记录该模式。

镜像构建必须传入：

- `COMMIT=$(git rev-parse --short=12 HEAD)`
- `DATE=$(git show -s --format=%cI HEAD)`

构建后执行镜像内 `/app/sub2api --version`，输出 commit 必须与待发布 Git commit 一致。staging 和 prod 都只能运行 `main` 上已推送的 commit，prod 只能使用 staging 已验证的同一个 commit。

## 备份与回滚

prod 切换前必须：

- 记录当前运行镜像 tag、镜像 ID、容器健康状态和目标 commit。
- 发布不执行独立备份；仅在用户另行要求时执行备份操作，不因历史备份凭证缺失或过期阻止发布。
- 通过 root-only 原子更新脚本备份并修改 prod `.env`。
- 保留当前 prod 镜像和至少一个最近的可回滚镜像。

应用异常时由发布脚本把 prod `SUB2API_IMAGE` 恢复为发布前原镜像 tag，再通过 compose 只重建应用容器，依次等待 Docker health 和宿主机 HTTP 健康检查通过。临时回滚 tag 只能在恢复成功后删除。数据库迁移为前向迁移，默认保留新增列、索引和约束；只有确认旧镜像不兼容且已有经过验证的反向迁移时，才允许修改数据库结构。staging 涉及数据清理的升级先保存其独立数据库备份，并核对恢复方式。

## 资源与其他服务

构建前必须检查磁盘、内存、CPU 和当前容器负载。正式 VPS 同时运行的其他服务不得因 Sub2API 构建或清理被停止、重建或删除。Docker 清理必须保护所有运行中镜像、Sub2API 当前/回滚镜像以及全部业务数据卷。
