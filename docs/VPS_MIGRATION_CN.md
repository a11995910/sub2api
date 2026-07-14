# VPS 运行架构说明

本文档描述 Sub2API 当前正式 VPS 的运行拓扑、目录、发布顺序和回滚边界。项目只有一台正式 VPS，不存在独立测试 VPS 或旧正式 VPS。

## 正式 VPS

| 项目 | 当前值 |
| --- | --- |
| 地址 | `207.57.145.15` |
| 登录账户 | `root` |
| 本机 SSH 别名 | `sub2api-new-vps` |
| 源码目录 | `/opt/sub2api/repo` |
| 部署方式 | VPS 拉取 Git、VPS 本机构建 Docker 镜像 |
| 预发布入口 | staging，宿主机端口 `18080` |
| 正式入口 | prod，宿主机端口 `8080` |

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

1. 本地在 `dev` 完成修改、自动化测试、提交和推送。
2. 正式 VPS 的 `/opt/sub2api/repo` 拉取已推送 commit，并使用 `deploy/Dockerfile` 构建 `sub2api:staging-<commit>`。
3. 备份 staging 数据后，在隔离 staging 启动镜像并验证版本、健康接口、关键页面、API、数据库迁移和日志。
4. staging 验证通过后报告结果，等待用户明确口头确认。
5. 将同一代码合并到 `main` 并推送；正式 VPS 切换到 `main`，核对 commit 与 staging 已验证 commit 完全一致。
6. 记录 prod 当前镜像，备份 prod PostgreSQL、Redis 关键状态和 prod `.env`，再把已验证镜像标记为 `sub2api:prod-<commit>`。
7. 原子更新 prod 的 `SUB2API_IMAGE`，只重建 Sub2API 应用容器；PostgreSQL 和 Redis 不得因应用发布被重建或清空。
8. 完成容器、健康接口、HTTPS、管理端账号页、`/api/v1/admin/accounts`、`/purchase`、`/models`、数据库连接和日志回归。

## 构建与版本追溯

镜像构建必须传入：

- `COMMIT=$(git rev-parse --short=12 HEAD)`
- `DATE=$(git show -s --format=%cI HEAD)`

构建后执行镜像内 `/app/sub2api --version`，输出 commit 必须与待发布 Git commit 一致。prod 只能运行 `main` 上已推送且经过 staging 验证的 commit。

## 备份与回滚

prod 切换前必须：

- 记录当前运行镜像 tag、镜像 ID、容器健康状态和目标 commit。
- 使用 `pg_dump -Fc` 生成 prod PostgreSQL 备份并校验文件非空。
- 通过 root-only 原子更新脚本备份并修改 prod `.env`。
- 保留当前 prod 镜像和至少一个最近的可回滚镜像。

应用异常时优先把 prod `SUB2API_IMAGE` 切回发布前镜像，再通过 compose 只重建应用容器。数据库迁移为前向迁移，默认保留新增列、索引和约束；只有确认旧镜像不兼容且已有经过验证的反向迁移时，才允许修改数据库结构。

## 资源与其他服务

构建前必须检查磁盘、内存、CPU 和当前容器负载。正式 VPS 同时运行的其他服务不得因 Sub2API 构建或清理被停止、重建或删除。Docker 清理必须保护所有运行中镜像、Sub2API 当前/回滚镜像以及全部业务数据卷。
