# VPS 运行架构说明

本文档描述 Sub2API 当前正式 VPS 的运行拓扑、目录、发布顺序和回滚边界。项目只有一台正式 VPS，不存在独立测试 VPS；旧主机不再作为 Sub2API 正式线上环境。

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

当前正式 VPS 实测资源为 4 vCPU、约 16GiB 内存、4GiB Swap，可用磁盘约 276GiB。staging 和 prod 构建共用同一套资源门禁：至少保留 20GiB 磁盘、12GiB 总内存、4GiB 可用内存，一分钟负载低于 CPU 容量的 75%。`GOMAXPROCS` 根据在线 CPU、可用内存和默认上限 8 动态计算，按每个编译并行槽 2GiB 可用内存估算；在该主机基线下通常为 4。门禁失败时禁止继续 Docker 构建。

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
6. 记录 prod 当前镜像，备份 prod PostgreSQL、Redis 关键状态和 prod `.env`，再把已验证镜像标记为 `sub2api:prod-<commit>`，原子更新 prod 的 `SUB2API_IMAGE`，只重建 Sub2API 应用容器；PostgreSQL 和 Redis 不得因应用发布被重建或清空。
7. 完成容器、健康接口、HTTPS、管理端账号页、`/api/v1/admin/accounts`、`/purchase`、`/model-market`、数据库连接和日志回归。

### 新主机首次 staging bootstrap

迁移到新正式 VPS 时，staging 必须先于 prod 验证。仅在用户已明确授权、且目标主机完全没有 prod 配置、compose 容器和数据文件时，可以执行：

```bash
/opt/sub2api/scripts/release-staging "$expected_commit" --bootstrap-without-prod
```

该参数只取代“同机 prod 已健康”这一项前置条件；磁盘、内存、负载、干净 `main`、精确 commit、镜像版本、Docker health、HTTP 和验证回执门禁保持不变。普通 staging 发布不传该参数，仍必须先确认 prod 健康。

## 构建与版本追溯

镜像构建必须传入：

- `COMMIT=$(git rev-parse --short=12 HEAD)`
- `DATE=$(git show -s --format=%cI HEAD)`

构建后执行镜像内 `/app/sub2api --version`，输出 commit 必须与待发布 Git commit 一致。staging 和 prod 都只能运行 `main` 上已推送的 commit，prod 只能使用 staging 已验证的同一个 commit。

## 备份与回滚

prod 切换前必须：

- 记录当前运行镜像 tag、镜像 ID、容器健康状态和目标 commit。
- 使用 `pg_dump -Fc` 生成 prod PostgreSQL 备份并校验文件非空。
- 通过 root-only 原子更新脚本备份并修改 prod `.env`。
- 保留当前 prod 镜像和至少一个最近的可回滚镜像。

应用异常时优先把 prod `SUB2API_IMAGE` 切回发布前镜像，再通过 compose 只重建应用容器。数据库迁移为前向迁移，默认保留新增列、索引和约束；只有确认旧镜像不兼容且已有经过验证的反向迁移时，才允许修改数据库结构。

## 资源与其他服务

构建前必须检查磁盘、内存、CPU 和当前容器负载。正式 VPS 同时运行的其他服务不得因 Sub2API 构建或清理被停止、重建或删除。Docker 清理必须保护所有运行中镜像、Sub2API 当前/回滚镜像以及全部业务数据卷。
