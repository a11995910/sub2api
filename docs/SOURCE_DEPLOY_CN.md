# Sub2API 源码定制上线说明

本文档记录当前源码仓库、VPS 源码目录和从源码完整编译上线的固定流程。线上 `sub2api` 当前使用自定义二进制挂载运行，默认不重建 Docker 镜像；每次上线应在本地完整构建 Linux amd64 二进制，再让 VPS 拉取同一 Git commit，最后替换挂载文件。

## 强制原则

- 生产构建前必须先提交并推送 Git，严禁使用未提交工作区构建线上产物。
- VPS `/opt/sub2api-src` 必须拉取到本次构建对应的同一 commit，确保线上运行产物有可追溯源码。
- 默认不重建 Docker 镜像。除非容器基础环境、入口脚本或系统依赖发生变化，否则只替换 `/opt/sub2api-deploy/custom/sub2api-pool-overview`。
- 每次上线必须执行仓库根目录的 `make build-deploy`，该目标会先构建前端，再用 `embed` 标签构建后端二进制。
- 不允许只执行 `go build -tags embed` 就覆盖线上，除非已经确认前端资源是同一次源码构建生成的最新产物。
- 构建产物必须包含 Git commit 和提交时间；本地交叉编译的 Linux amd64 产物上传到 VPS 后，`/tmp/sub2api-pool-overview.new --version` 的 commit 必须与待上线 commit 一致。
- 替换线上二进制前必须校验 SHA256 并备份当前文件；使用同目录临时文件原子替换，禁止用 `cp` 直接覆盖正在执行的挂载文件。
- 替换后必须验证容器状态、健康接口、管理端账号页面和日志。
- 验证通过后必须清理 Docker 构建缓存、未使用镜像和旧二进制备份；只保留当前运行二进制和最近一份 `.bak-*` 回滚文件。
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

## 本地构建与提交要求

本地开发完成后，必须先提交并推送。生产构建只能从干净工作区执行：

```bash
cd /Users/wangjun/Documents/GitHub/sub2api
git status --short
git add 本次相关文件
git commit -m "说明本次修改"
git push
git rev-parse HEAD
git log -1 --oneline
```

如 `git status --short` 仍有未提交改动，必须确认这些改动与本次构建无关；否则禁止继续上线。

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

部署产物必须包含前端静态资源，因此后端编译必须使用 `embed` 标签。当前仓库已经提供统一目标。

编译机器需要先准备：

- Go：以 `backend/go.mod` 中声明的版本为准
- Node.js / pnpm：用于构建 `frontend`
- make：用于执行仓库根目录的构建目标

默认在本地构建 Linux amd64 二进制：

```bash
cd /Users/wangjun/Documents/GitHub/sub2api
pnpm --dir frontend install --frozen-lockfile
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 make build-deploy
file backend/bin/server
shasum -a 256 backend/bin/server
```

构建产物位置：

```bash
backend/bin/server
```

如果本地构建环境不可用，才允许改用 VPS `/opt/sub2api-src` 构建：

```bash
/usr/local/bin/prebuild-cleanup
cd /opt/sub2api-src
pnpm --dir frontend install --frozen-lockfile
make build-deploy
./backend/bin/server --version
```

`prebuild-cleanup` 默认只清理 Go build cache、apt cache、systemd journal 和 Docker build cache。除非已经确认未使用镜像不再需要回滚，否则不要设置 `PRUNE_UNUSED_IMAGES=1`。

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
# 本地执行，并记录完整 commit 和 SHA256。
git rev-parse HEAD
shasum -a 256 backend/bin/server
scp backend/bin/server root@192.220.24.46:/tmp/sub2api-pool-overview.new

# VPS 执行。expected_commit 必须填写上一步记录的完整 commit。
timeout 5 /tmp/sub2api-pool-overview.new --version
cd /opt/sub2api-src
git status --short
git pull --ff-only
expected_commit='填写本地 git rev-parse HEAD 的输出'
test "$(git rev-parse HEAD)" = "$expected_commit"
git log -1 --oneline

live=/opt/sub2api-deploy/custom/sub2api-pool-overview
candidate=${live}.new
install -m 0755 /tmp/sub2api-pool-overview.new "$candidate"
sha256sum /tmp/sub2api-pool-overview.new "$candidate"

ts=$(date +%Y%m%d-%H%M%S)
cp -a "$live" "$live.bak-$ts"
mv -f "$candidate" "$live"
rm -f /tmp/sub2api-pool-overview.new

cd /opt/sub2api-deploy
docker compose up -d --force-recreate --no-deps sub2api
```

两行 `sha256sum` 的输出必须一致。原子 `mv` 会让现有容器继续使用旧 inode，直到 `force-recreate` 后重新挂载新文件，从而避免直接覆盖运行中二进制导致 `Text file busy`。

如果 Docker Compose 服务名变化，先用 `docker compose ps` 确认实际服务名，再重启对应服务。

## 验证

```bash
cd /opt/sub2api-deploy
docker compose ps
/opt/sub2api-deploy/custom/sub2api-pool-overview --version
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

## 构建后清理

上线验证通过后必须清理一次 VPS 构建缓存和旧备份，避免磁盘持续膨胀。

```bash
docker builder prune -af
keep_images="$(docker ps --format '{{.Image}}' | sort -u)"
latest_backup="$(docker images --format '{{.Repository}}:{{.Tag}}' 'weishaw/sub2api' \
  | awk '/^weishaw\/sub2api:backup-/ {print}' | sort -r | head -n1)"
docker images --format '{{.Repository}}:{{.Tag}} {{.ID}}' | while read -r ref image_id; do
  printf '%s\n' "$keep_images" | grep -qx "$ref" && continue
  [ -n "$latest_backup" ] && [ "$ref" = "$latest_backup" ] && continue
  case "$ref" in
    weishaw/sub2api:backup-*|golang:*|node:*|alpine:*|'<none>:<none>')
      docker rmi "$ref" 2>/dev/null || docker rmi "$image_id" 2>/dev/null || true
      ;;
  esac
done
docker image prune -f
find /opt/sub2api-deploy/custom -maxdepth 1 -type f \
  -name 'sub2api-pool-overview.bak-*' -printf '%T@ %p\n' \
  | sort -nr | awk 'NR>1 {print $2}' | xargs -r rm -f
docker system df
ls -lt /opt/sub2api-deploy/custom/sub2api-pool-overview*
```

清理要求：

- 保留当前运行中的 `/opt/sub2api-deploy/custom/sub2api-pool-overview`。
- 只保留最近一份 `sub2api-pool-overview.bak-*` 回滚备份。
- `docker builder prune -af` 清理构建缓存。
- 镜像清理必须先保护正在运行的容器镜像；如果存在多个 `weishaw/sub2api:backup-*`，只保留最近一份确有回滚价值的备份镜像。
- 不删除 `data`、`postgres_data`、`redis_data`、数据库卷或任何业务数据目录。

## 回滚

如果新版本异常，使用最近一次备份恢复：

```bash
live=/opt/sub2api-deploy/custom/sub2api-pool-overview
install -m 0755 "$live.bak-时间戳" "$live.rollback"
mv -f "$live.rollback" "$live"
cd /opt/sub2api-deploy
docker compose up -d --force-recreate --no-deps sub2api
docker compose ps
docker compose logs --tail=200 sub2api
```

## OAuth 凭证说明

源码仓库不内置第三方 OAuth `client_id` / `client_secret`。如需使用相关登录流，请在运行环境中通过 `.env` 或服务环境变量注入：

- `GEMINI_CLI_OAUTH_CLIENT_ID`
- `GEMINI_CLI_OAUTH_CLIENT_SECRET`
- `ANTIGRAVITY_OAUTH_CLIENT_ID`
- `ANTIGRAVITY_OAUTH_CLIENT_SECRET`
