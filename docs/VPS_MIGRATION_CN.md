# VPS 迁移说明

本文档描述当前从老正式 VPS `192.220.24.46` 迁移到新正式 VPS `207.57.145.15` 的项目盘点、迁移顺序、前置条件和验证要求。文档只记录服务器、目录、服务和凭据引用方式，不记录任何明文密码、Token、私钥或 Cookie。

## 迁移目标

| 环境 | 地址 | 登录账户 | 当前定位 | 凭据要求 |
| --- | --- | --- | --- | --- |
| 老正式 VPS | `192.220.24.46` | `root` | 当前正式生产环境，DNS 切换前继续承载线上流量 | 已有 SSH Key，可使用 `canvas-vps` |
| 新正式 VPS | `207.57.145.15` | `root` | 迁移目标机器，资源空间充足，适合承接完整构建和运行 | 不记录明文密码；建议保存到本机 Keychain 服务 `sub2api-new-vps-root`，并尽快配置 SSH Key |

新正式 VPS 资源空间充足，迁移完成后不默认使用旧 VPS 的低资源构建参数。`sub2api` 采用 Git 拉取源码、本机构建 Docker 镜像、staging 验证后切换 prod 的部署方式。构建前仍需核对 `df -h`、`free -h`、CPU 负载、Docker 状态和当前运行服务。

## 老 VPS 当前项目盘点

以下盘点来自 `2026-07-08` 对老正式 VPS 的只读核实。

| 项目 | 当前运行形态 | 关键路径或数据目录 | 当前入口 | 迁移优先级 |
| --- | --- | --- | --- | --- |
| `sub2api` | Docker 容器 `sub2api`，镜像 `weishaw/sub2api:latest`，容器健康 | 源码 `/opt/sub2api-src`；部署 `/opt/sub2api-deploy`；应用数据 `/opt/sub2api-deploy/data`；PostgreSQL `/opt/sub2api-deploy/postgres_data`；Redis `/opt/sub2api-deploy/redis_data`；挂载二进制 `/opt/sub2api-deploy/custom/sub2api-pool-overview` | Nginx `fast.youkeduo.xyz`、`fast.youkeduo.shop`、`img.hctoken.top` 代理到 `127.0.0.1:8080`，部分路径代理到 `127.0.0.1:8091` | 最高 |
| `chatgpt2api` | Docker 容器 `chatgpt2api`，镜像 `chatgpt2api:account-tags-3582047`，容器健康 | 运行配置 `/opt/chatgpt2api/.env`；数据 `/opt/chatgpt2api/data`；发布目录 `/opt/chatgpt2api-release-3582047` | 端口 `3000` 对外暴露 | 高 |
| `CLIProxyAPI` | Docker 容器 `cli-proxy-api`，镜像 `cli-proxy-api:v7.2.50-image-quota-local-20260706` | 配置 `/opt/CLIProxyAPI/config.yaml`；认证 `/opt/CLIProxyAPI/auths`；插件 `/opt/CLIProxyAPI/plugins`；日志 `/opt/CLIProxyAPI/logs` | 端口 `8317` 对外暴露 | 高 |
| `infinite-canvas` | Docker 容器 `infinite-canvas`，镜像 `infinite-canvas:local` | 源码与数据 `/opt/infinite-canvas-src`，数据挂载 `/opt/infinite-canvas-src/data` | Nginx `canvas.youkeduo.xyz`、`canvas.youkeduo.shop` 代理到 `127.0.0.1:13000` | 中 |
| 图像相关服务目录 | 当前未在 `docker ps` 中看到直接运行容器 | `/opt/fsrcnn-api`、`/opt/image2_source_20260608` | 是否仍被 `127.0.0.1:8091` 入口依赖需要继续核实 | 待确认 |
| VPN / Shadowsocks | `shadowsocks-libev.service` 当前为 disabled | systemd 模板服务仍存在 | 未监听 `8388` | 低，默认不迁移，除非用户确认还需要 |

老 VPS 根分区约 `39G`，已用约 `28G`，剩余约 `12G`。主要数据体量包括：`/opt/sub2api-deploy/postgres_data` 约 `5.5G`、`/opt/chatgpt2api/data` 约 `5.7G`、`/opt/sub2api-deploy/data` 约 `751M`。老 VPS 空余空间不适合在本机一次性打全量压缩包，迁移应优先采用数据库流式备份、`rsync` 到新 VPS 或先分项目备份。

## 新 VPS 前置准备

新 VPS 目前应先完成以下准备，完成前不切换 DNS：

- 配置本机 SSH Key 到 `root@207.57.145.15`，并在本机 `~/.ssh/config` 中增加稳定别名，例如 `sub2api-new-vps`。
- 安装基础运行环境：Docker、Docker Compose 插件、Nginx 或 Caddy、Certbot、Git、rsync、tar、curl、jq、make。
- 安装 `sub2api` 构建环境：Go 版本以 `backend/go.mod` 为准，Node.js / pnpm 版本以当前项目构建要求为准。
- 准备 `sub2api` 新目录：`/opt/sub2api/repo`、`/opt/sub2api/env/{staging,prod}`、`/opt/sub2api/compose/{staging,prod}`、`/opt/sub2api/data/{staging,prod}`、`/opt/sub2api/backups`、`/opt/sub2api/scripts`。
- 其他项目按需准备目录：`/opt/chatgpt2api`、`/opt/CLIProxyAPI`、`/opt/infinite-canvas-src` 和备份目录。
- 迁移前先在新 VPS 上跑只读环境核对：`hostnamectl`、`df -hT /`、`free -h`、`docker version`、`docker compose version`、`nginx -v`。

## 推荐迁移顺序

1. 先迁移静态配置和可回滚材料：Nginx 站点配置、证书目录清单、Docker compose 文件、项目 `.env` 或 `config.yaml` 的安全备份。
2. 迁移 `sub2api`：先在 `/opt/sub2api/repo` 克隆源码，使用 `deploy/Dockerfile` 构建带 commit tag 的镜像，先部署 staging 并验证；再迁移 PostgreSQL、Redis、`/opt/sub2api-deploy/data` 到新 VPS 的 prod 数据目录，最后切换 prod 并在新 VPS 本机验证 `127.0.0.1:8080`。
3. 迁移 `chatgpt2api`：同步 `/opt/chatgpt2api/data`、`.env`、发布目录和镜像构建方式，验证 `127.0.0.1:3000/health`。
4. 迁移 `CLIProxyAPI`：同步配置、认证目录、插件目录和当前镜像或源码构建链路，验证 `127.0.0.1:8317`。
5. 迁移 `infinite-canvas`：同步源码、数据和 Nginx 域名入口，验证 `127.0.0.1:13000`。
6. 复核 `127.0.0.1:8091` 的实际服务来源；若对应图像服务仍在业务链路中，再迁移 `/opt/fsrcnn-api` 或 `/opt/image2_source_20260608`。
7. 新 VPS 全部本机健康检查通过后，再逐个域名切 DNS；DNS 切换完成并观察稳定后，老 VPS 保留一段回滚窗口。

## 可以先行处理的事项

- 给新 VPS 配置 SSH Key 免密登录，并固定 SSH 别名。
- 在新 VPS 安装 Docker、Nginx/Caddy、Certbot、Git、rsync、Go、Node.js、pnpm 和 make。
- 从老 VPS 拉取 Nginx 站点文件、Docker compose 文件和项目目录清单到本地安全备份；备份文件不得包含明文密钥输出。
- 为 `sub2api` 准备新 VPS 的 `/opt/sub2api/repo`，拉取 GitHub 仓库并构建一次当前 `dev` 或 `main` commit 镜像，先在 staging 验证。
- 设计数据库迁移窗口：`sub2api` PostgreSQL 和 Redis 迁移应安排短暂停写或维护窗口，避免数据不一致。
- 梳理 DNS：`fast.youkeduo.xyz`、`fast.youkeduo.shop`、`img.hctoken.top`、`canvas.youkeduo.xyz`、`canvas.youkeduo.shop` 切换前必须确认新 VPS 证书和反代都已正常。

## 验证与回滚

每个项目迁移后必须至少完成：

- 新 VPS 本机容器状态检查：`docker compose ps` 或 `docker ps`。
- 本机端口健康检查：`curl -I http://127.0.0.1:端口/health`，无健康接口时检查首页、关键 API 或日志。
- Nginx/Caddy 配置检查：`nginx -t` 或对应反代配置校验。
- 业务入口检查：域名切换前使用 Host 头或临时域名验证；域名切换后再验证 HTTPS、页面和关键接口。
- 回滚准备：DNS TTL、老 VPS 容器、老 VPS 数据目录和旧证书在观察窗口内保持不删除。
