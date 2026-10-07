# VPS 运行架构说明

本文档描述 Sub2API 当前正式 VPS 的运行拓扑、目录、发布顺序和回滚边界。项目只有一台正式 VPS，不存在独立测试 VPS；旧主机不再作为 Sub2API 正式线上环境。

## 主机角色与迁移边界

当前正式线上主机为 `185.61.210.32`，附加地址为 `185.61.210.33` 至 `185.61.210.36`。prod、隔离 staging、各自的 PostgreSQL 和 Redis 均位于该主机。本机通过 `ssh sub2api-migration-vps` 使用专用密钥 `~/.ssh/sub2api_185_61_210_32_ed25519` 登录 root；服务器密码与私钥不进入 Git。

`205.185.113.15`（别名 `sub2api-new-vps`）为旧正式数据源和回滚参考，不执行新版本发布。新 shop 跳板为 `166.88.36.223`（别名 `sub2api-shop-vps`），已完成 Caddy 反代和证书迁入，等待用户切换 DNS；旧跳板 `207.57.145.15`（别名 `sub2api-jump-vps`）保留用于过渡和回滚。跳板角色与主应用发布主机分开管理，DNS 记录、证书续期、验收和回滚见 [shop 跳板运行说明](SHOP_JUMP_CN.md)。

当前正式机已承接生产，升级必须走普通 staging 验证和经用户确认的 prod 发布，不得通过 bootstrap 参数绕过健康门禁。迁移隔离数据库副本不得另行启动会刷新 OAuth、处理支付或执行后台任务的第二套生产应用。

后续如需迁移或恢复，必须单独确认数据真值来源、停写窗口和回滚方式。一致性快照记录实际采集时间、源版本、校验值与恢复结果，只保存于目标主机 root-only 目录；不得直接复制运行中的 PostgreSQL 数据目录。追平必须覆盖新增、修改、删除、序列及余额、订单等状态，不能仅按 `created_at` 追加。Redis 与文件快照分别记录采集时间，不视为跨服务原子快照。

## 正式 VPS

| 项目 | 当前值 |
| --- | --- |
| 地址 | `185.61.210.32` |
| 登录账户 | `root` |
| 本机 SSH 别名 | `sub2api-migration-vps` |
| 源码目录 | `/opt/sub2api/repo` |
| 源码分支 | 只允许 `main` |
| 部署方式 | VPS 拉取 Git、VPS 本机构建 Docker 镜像 |
| 预发布入口 | `http://185.61.210.32:18080`，上游 `127.0.0.1:18080` |
| 正式入口 | prod，宿主机端口 `8080` |

当前正式 VPS 实测资源为 40 vCPU、约 62GiB 内存、1.8TiB 根磁盘。staging 和 prod 构建共用 `deploy/release-gates check-build-resources` 门禁：至少保留 20GiB 磁盘、12GiB 总内存、4GiB 可用内存；通过 `/proc/stat` 间隔 1 秒采样的整机 CPU 使用率必须不超过 50%，idle 和 iowait 不计入占用，采样失败拒绝发布，不检查 load average。`GOMAXPROCS` 根据在线 CPU、可用内存和默认上限 8 动态计算，按每个编译并行槽 2GiB 可用内存估算；在该主机资源充足时为 8。门禁失败时禁止继续 Docker 构建。

服务器密码、SSH 私钥、Token、数据库密码、OAuth 密钥和 Cookie 不得写入仓库、文档、镜像 tag 或日志。登录优先使用 SSH Key；运行配置只保存在服务器 root-only 文件中。

## 主 IP HTTP API 入口

`http://185.61.210.32` 可直接调用正式 API；要求 OpenAI 兼容 Base URL 的客户端使用 `http://185.61.210.32/v1`。该入口不跳转 HTTPS，API Key 鉴权由正式应用处理。HTTP 会明文传输密钥与请求内容；支持域名的客户端优先使用 `https://fast.youkeduo.xyz`，现有 HTTPS 证书不覆盖主 IP。

Nginx 配置位于 `/etc/nginx/conf.d/sub2api-ip-http.conf`，只监听 `185.61.210.32:80`，上游固定为 `http://127.0.0.1:8080`。配置关闭响应缓冲，保留 WebSocket Upgrade，读写超时为 3600 秒，请求体上限为 200MiB；`X-Forwarded-Proto` 使用实际请求协议。连接升级使用现有 `/etc/nginx/conf.d/youkeduo-ssl-local.conf` 中的 `$sub2api_connection_upgrade` 映射。既有域名仍由各自 HTTPS 入口处理。

维护前将 Nginx 配置备份到服务器 root-only 目录；执行 `nginx -t` 后平滑 reload，并等待新监听生效。验证公网 `/health` 返回 200，未带密钥的 `/v1/models` 和 POST `/v1/responses` 返回 `API_KEY_REQUIRED`（401），同时回归两个 fast 域名和两个画布域名的 HTTPS。401 仅证明入口及鉴权链路可达，真实模型响应仍需客户端携带有效密钥测试。

首次启用前的配置备份位于 `/root/sub2api-ip-http-20261002-045701/nginx`，同目录 `prod-before.txt` 记录正式镜像和启动时间。撤销此 HTTP 入口时，仅将 `sub2api-ip-http.conf` 移出 Nginx 加载目录，通过 `nginx -t` 后平滑 reload；不回退其他站点配置，也不重建应用或数据库。

## 固定 staging 测试站

预发布站点为 `http://185.61.210.32:18080`，智能轮候页面为 `/admin/intelligent-ops/time-rotation`。该地址通过主 IP 直达新正式 VPS，不依赖域名解析或用户电脑上的 SSH 隧道。公网端口 18080 只承接 staging；默认 443 仍承接 prod。使用 staging 独立账号登录，浏览器按不同端口隔离本地登录存储。

配置来源为仓库 `deploy/nginx-staging.conf`，部署到 `/etc/nginx/conf.d/sub2api-staging.conf`，只监听主 IP 的 18080 端口，全部页面及 API 固定转发到 `127.0.0.1:18080`，支持流式响应和 WebSocket。不能引用指向正式 8080 端口的 Responses 配置片段。公网 Nginx 绑定 `185.61.210.32:18080`，容器只绑定 `127.0.0.1:18080`，两者地址不同且不冲突；响应包含 `X-Sub2API-Environment: staging` 和禁止索引标识。

安装或更新前，在 root-only 目录备份现有 Nginx 配置，并记录 prod/staging 镜像。只从已推送的 `origin/main` 安装配置：

```bash
install -o root -g root -m 0644 /opt/sub2api/repo/deploy/nginx-staging.conf /etc/nginx/conf.d/sub2api-staging.conf
nginx -t
systemctl reload nginx
```

等待新监听生效后，从公网验证 HTTP、登录页、轮候页、静态资源与 staging 一致，以及未认证管理接口和模型接口返回 401；同时回归现有 fast、canvas 域名和正式镜像健康状态。`release-staging` 将公网健康、环境标识、版本和页面入口资源一致性作为成功条件，回执记录 `public_url`。若维护失败，恢复该配置的备份（首次安装则移出新增文件），通过 `nginx -t` 后 reload；不回退应用数据或其他站点。

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

以下首次启动规则仅适用于尚未接流的新迁移目标，不适用于当前已有 prod 的正式机。迁移到新正式 VPS 时，staging 必须先于 prod 验证。仅在用户已明确授权、且目标主机完全没有 prod 配置、compose 容器和数据文件时，可以执行：

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

Infinite Canvas 和 Team Manage 也运行于该新正式机，分别使用独立 SQLite 数据和运行配置。画布由 Nginx 转发至 `127.0.0.1:13000`，Team Manage 使用 `8008` 端口；旧机对应应用已停止，仅保留入口转发和恢复数据。启动、验收、最终快照边界及回滚见 [画布与账号管理运行说明](AUXILIARY_APPS_CN.md)。

构建前必须检查磁盘、内存、CPU 和当前容器负载。正式 VPS 同时运行的其他服务不得因 Sub2API 构建或清理被停止、重建或删除。Docker 清理必须保护所有运行中镜像、Sub2API 当前/回滚镜像以及全部业务数据卷。
