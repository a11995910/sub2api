# Sub2API 源码定制上线说明

本文档记录当前源码仓库、VPS 源码目录和从源码完整编译上线的固定流程。线上 `sub2api` 当前使用自定义二进制挂载运行，必须从服务器上的同源源码目录完整构建前端和后端，再替换挂载文件。

## 强制原则

- 线上定制二进制只能从 `/opt/sub2api-src` 构建，不允许使用本地临时打包的 `backend` 目录直接覆盖线上。
- 每次上线必须执行仓库根目录的 `make build-deploy`，该目标会先构建前端，再用 `embed` 标签构建后端二进制。
- 不允许只执行 `go build -tags embed` 就覆盖线上，除非已经确认前端资源是同一次源码构建生成的最新产物。
- 替换线上二进制前必须备份当前文件，替换后必须验证容器状态、健康接口、管理端账号页面和日志。
- 服务器密码、Token、数据库密码、OAuth 密钥等敏感信息不得写入仓库文档或提交记录。

## 源码仓库

- GitHub：`git@github.com:a11995910/sub2api.git`
- 主分支：`main`
- 本地开发完成后，提交并推送到 GitHub：

```bash
git status
git add .
git commit -m "说明本次修改"
git push
```

## VPS 源码目录

当前 VPS 源码目录固定为：

```bash
/opt/sub2api-src
```

首次拉取：

```bash
git clone git@github.com:a11995910/sub2api.git /opt/sub2api-src
```

如果服务器没有配置 GitHub SSH Key，也可以使用 HTTPS：

```bash
git clone https://github.com/a11995910/sub2api.git /opt/sub2api-src
```

后续更新：

```bash
cd /opt/sub2api-src
git status --short
git pull --ff-only
git rev-parse --short HEAD
```

执行 `git pull --ff-only` 前，`git status --short` 应为空。如果有未提交改动，必须先确认来源，不能直接覆盖或回滚。

## 线上域名与 Nginx

当前 VPS 的 Sub2API 通过系统 Nginx 对外提供 HTTPS 访问，后端容器入口为本机 `127.0.0.1:8080`。

线上域名配置如下：

| 域名 | Nginx 配置 | 证书来源 | 证书路径 | 说明 |
| --- | --- | --- | --- | --- |
| `fast.youkeduo.site` | `/etc/nginx/sites-available/fast.youkeduo.site` | Certbot 自动管理 | `/etc/letsencrypt/live/fast.youkeduo.site/` | 主访问域名 |
| `img.hctoken.top` | `/etc/nginx/sites-available/img.hctoken.top` | `*.hctoken.top` 通配符证书手动部署 | `/etc/nginx/ssl/img.hctoken.top/` | 新增访问域名，HTTP 自动跳转 HTTPS |

`img.hctoken.top` 当前证书文件固定为：

```bash
/etc/nginx/ssl/img.hctoken.top/fullchain.pem
/etc/nginx/ssl/img.hctoken.top/privkey.pem
```

该证书不是 Certbot 自动续期证书。证书到期前需要替换新的 `fullchain.pem` 和 `privkey.pem`，替换后执行：

```bash
nginx -t
systemctl reload nginx
```

替换证书前应先校验证书是否覆盖 `img.hctoken.top`，并确认私钥匹配：

```bash
openssl x509 -in fullchain.pem -noout -subject -issuer -dates
openssl x509 -in fullchain.pem -pubkey -noout | openssl pkey -pubin -outform DER | openssl dgst -sha256
openssl pkey -in privkey.pem -pubout -outform DER | openssl dgst -sha256
```

## 编译要求

部署产物必须包含前端静态资源，因此后端编译必须使用 `embed` 标签。当前仓库已经提供统一目标：

编译机器需要先准备：

- Go：以 `backend/go.mod` 中声明的版本为准
- Node.js / pnpm：用于构建 `frontend`
- make：用于执行仓库根目录的构建目标

```bash
cd /opt/sub2api-src
pnpm --dir frontend install --frozen-lockfile
make build-deploy
./backend/bin/server --version
```

构建产物位置：

```bash
backend/bin/server
```

上线前应记录本次构建对应的 Git commit：

```bash
git rev-parse HEAD
git log -1 --oneline
```

## 替换当前运行版本

当前容器挂载的可执行文件路径：

```bash
/opt/sub2api-deploy/custom/sub2api-pool-overview
```

替换流程：

```bash
ts=$(date +%Y%m%d-%H%M%S)
cp /opt/sub2api-deploy/custom/sub2api-pool-overview \
  /opt/sub2api-deploy/custom/sub2api-pool-overview.bak-$ts
cp /opt/sub2api-src/backend/bin/server \
  /opt/sub2api-deploy/custom/sub2api-pool-overview
chmod +x /opt/sub2api-deploy/custom/sub2api-pool-overview
cd /opt/sub2api-deploy
docker compose restart sub2api
```

如果 Docker Compose 服务名变化，先用 `docker compose ps` 确认实际服务名，再重启对应服务。

## 验证

```bash
cd /opt/sub2api-deploy
docker compose ps
curl -I https://fast.youkeduo.site/health
curl -I https://fast.youkeduo.site/purchase
curl -I https://fast.youkeduo.site/models
docker compose logs --tail=200 sub2api
```

期望结果：

- 容器状态为 `healthy`
- `/health`、`/purchase`、`/models` 返回成功状态码
- 前端页面刷新不出现 404
- 管理端 `/admin/accounts` 能正常打开，账号列表接口 `/api/v1/admin/accounts` 不出现 5xx
- 日志中没有启动失败、前端资源缺失、数据库迁移失败或账号列表序列化异常

## 回滚

如果新版本异常，使用最近一次备份恢复：

```bash
cp /opt/sub2api-deploy/custom/sub2api-pool-overview.bak-时间戳 \
  /opt/sub2api-deploy/custom/sub2api-pool-overview
chmod +x /opt/sub2api-deploy/custom/sub2api-pool-overview
cd /opt/sub2api-deploy
docker compose restart sub2api
docker compose ps
docker compose logs --tail=200 sub2api
```

## OAuth 凭证说明

源码仓库不内置第三方 OAuth `client_id` / `client_secret`。如需使用相关登录流，请在运行环境中通过 `.env` 或服务环境变量注入：

- `GEMINI_CLI_OAUTH_CLIENT_ID`
- `GEMINI_CLI_OAUTH_CLIENT_SECRET`
- `ANTIGRAVITY_OAUTH_CLIENT_ID`
- `ANTIGRAVITY_OAUTH_CLIENT_SECRET`
