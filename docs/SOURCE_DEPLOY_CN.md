# Sub2API 源码定制上线说明

本文档记录当前源码仓库、VPS 源码目录和从源码完整编译上线的固定流程。正式 VPS `192.220.24.46` 上的 `sub2api` 当前使用自定义二进制挂载运行。

## 强制原则

- 新功能开发默认走 `dev -> 测试 VPS -> 用户口头确认 -> main -> 正式 VPS` 链路；不得跳过测试 VPS 直接上线正式 VPS。
- 测试 VPS 固定为 `192.220.36.75`，默认拉取 `origin/dev` 分支，并按正式 VPS 的二进制挂载方式做预发布验证。
- 测试 VPS 验证通过后，只能在用户明确口头命令后合并到 `main` 并执行正式上线。
- 生产构建前必须先提交并推送 Git，严禁使用未提交工作区构建线上产物。
- VPS `/opt/sub2api-src` 必须拉取到本次构建对应的同一 commit，确保线上运行产物有可追溯源码。
- 正式 VPS `192.220.24.46` 按当前二进制挂载流程维护。
- 正式 VPS 二进制挂载流程必须执行仓库根目录的 `make build-deploy`，该目标会先构建前端，再用 `embed` 标签构建后端二进制。
- 不允许只执行 `go build -tags embed` 就覆盖线上；必须确认前端资源、后端二进制、资源文件和源码 commit 属于同一次构建。
- 内嵌前端由后端直接提供时，`/assets/*` 会返回长期缓存头，HTML/JS/CSS/JSON 会按浏览器 `Accept-Encoding` 返回 gzip 压缩；外层 Nginx 或 Caddy 仍可继续做 HTTPS、HTTP/2 和代理层优化。
- 构建产物必须包含 Git commit 和提交时间；正式 VPS 必须核对 `/opt/sub2api-src/backend/bin/server --version`，输出 commit 必须与待上线 commit 一致。
- 正式 VPS 替换线上二进制前必须校验 SHA256 并备份当前文件；使用同目录临时文件原子替换，禁止用 `cp` 直接覆盖正在执行的挂载文件。
- 替换后必须验证容器状态、健康接口、管理端账号页面和日志。
- 验证通过后必须清理 Docker 构建缓存、无回滚价值的旧镜像和旧二进制备份，只保留当前运行二进制和最近一份 `.bak-*` 回滚文件。
- 服务器密码、Token、数据库密码、OAuth 密钥等敏感信息不得写入仓库文档或提交记录。

## 源码仓库

- GitHub：`git@github.com:a11995910/sub2api.git`
- 主分支：`main`
- 开发分支：`dev`
- 本地开发完成后，提交并推送到 GitHub：

```bash
git status
git add .
git commit -m "说明本次修改"
git push
```

## 本地提交与推送要求

本地开发完成后，必须先提交并推送。生产产物不在本地构建；正式 VPS 从已推送的 Git commit 构建二进制：

```bash
cd /Users/wangjun/Documents/GitHub/sub2api
git status --short
git add 本次相关文件
git commit -m "说明本次修改"
git push
git rev-parse HEAD
git log -1 --oneline
```

如 `git status --short` 仍有未提交改动，必须确认这些改动与本次上线无关；否则禁止继续上线。

## 测试 VPS 预发布流程

测试 VPS 用于承接 `dev` 分支的新功能验证。默认环境信息：

| 项目 | 值 |
| --- | --- |
| 测试 VPS | `192.220.36.75` |
| Git 分支 | `origin/dev` |
| 源码目录 | `/opt/sub2api-src` |
| 部署目录 | `/opt/sub2api-deploy` |
| 挂载二进制 | `/opt/sub2api-deploy/custom/sub2api-pool-overview` |

测试 VPS 部署前，本地必须先在 `dev` 分支提交并推送：

```bash
cd /Users/wangjun/Documents/GitHub/sub2api
git status --short
git branch --show-current
git add 本次相关文件
git commit -m "说明本次修改"
git push -u origin dev
git rev-parse HEAD
git log -1 --oneline
```

测试 VPS 首次准备源码目录：

```bash
git clone -b dev git@github.com:a11995910/sub2api.git /opt/sub2api-src
```

如果服务器未配置 GitHub SSH Key，可临时使用 HTTPS：

```bash
git clone -b dev https://github.com/a11995910/sub2api.git /opt/sub2api-src
```

测试 VPS 每次构建必须拉取 `dev` 并核对 commit：

```bash
cd /opt/sub2api-src
git status --short
git fetch origin
git switch dev
git pull --ff-only origin dev
expected_commit='填写本地 dev 的 git rev-parse HEAD 输出'
test "$(git rev-parse HEAD)" = "$expected_commit"
git log -1 --oneline
/usr/local/bin/prebuild-cleanup || true
(cd frontend && pnpm install --frozen-lockfile)
GOFLAGS='-p=1' GOMAXPROCS=1 make build-deploy
file backend/bin/server
sha256sum backend/bin/server
timeout 5 backend/bin/server --version
```

测试 VPS 替换运行二进制和验证流程与正式 VPS 的二进制流程保持一致。测试通过后，必须等待用户明确口头命令，才能把 `dev` 合并到 `main` 并按正式 VPS 二进制挂载流程发布。

### 测试 VPS 源码运行边界

`sub2api` 后端是 Go 编译型服务，前端是 Vite/Vue 应用。后端不能像解释型脚本一样在 Git 提交后不经过编译直接生效；`go run ./cmd/server` 本质上也会先编译，再启动临时二进制。生产形态还要求前端 `dist` 通过 `embed` 标签打入后端二进制，因此完整功能验证仍应使用 `make build-deploy` 生成可追溯产物。

测试 VPS `192.220.36.75` 当前内存较小，不适合作为常驻源码热编译机器。实测后端首次编译会卡在 `backend/ent` 大包编译阶段并触发明显 swap，容易影响 PostgreSQL、Redis 和现有测试服务稳定性。因此测试 VPS 不默认启用“提交后自动源码编译并接管 8080”的模式。

测试环境需要更快自动生效时，优先采用以下低风险方案：

- 前端页面调试可单独使用 Vite 开发服务，并让 `/api`、`/v1` 代理到当前测试后端。
- 后端改动仍以 `dev` 分支提交为准，由测试 VPS 拉取同一 commit 后低资源构建二进制，再原子替换挂载文件。
- 如需实现“提交后自动更新测试环境”，应使用外部构建产物或 CI 构建 Linux amd64 二进制，再由测试 VPS 只负责拉取产物、校验 commit/SHA256、备份旧二进制、替换并重启容器；测试 VPS 不应承担完整 Go/Vite 构建压力。

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
| `fast.youkeduo.xyz` | `/etc/nginx/sites-available/fast.youkeduo.xyz` | Certbot 自动管理 | `/etc/letsencrypt/live/fast.youkeduo.xyz/` | 主访问域名，A 记录直接指向正式 VPS |
| `fast.youkeduo.shop` | `/etc/nginx/sites-available/fast.youkeduo.shop` | Certbot 自动管理 | `/etc/letsencrypt/live/fast.youkeduo.shop/` | 备用访问域名，当前经 Cloudflare 解析 |
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

## 正式 VPS 二进制编译要求

本节适用于正式 VPS `192.220.24.46` 和当前测试 VPS `192.220.36.75` 的二进制挂载流程。

部署产物必须包含前端静态资源，因此后端编译必须使用 `embed` 标签。当前仓库已经提供统一目标。

线上编译机器为 VPS `/opt/sub2api-src`。VPS 需要先准备：

- Go：以 `backend/go.mod` 中声明的版本为准
- Node.js / pnpm：用于构建 `frontend`
- make：用于执行仓库根目录的构建目标

正式 VPS 和当前测试 VPS 默认在 `/opt/sub2api-src` 构建 Linux amd64 二进制。构建前必须先清理可再生成缓存，避免系统盘被 Go / Node / Docker 构建缓存打满：

```bash
cd /opt/sub2api-src
git status --short
git pull --ff-only
expected_commit='填写本地 git rev-parse HEAD 的输出'
test "$(git rev-parse HEAD)" = "$expected_commit"
git log -1 --oneline
/usr/local/bin/prebuild-cleanup
(cd frontend && pnpm install --frozen-lockfile)
GOFLAGS='-p=1' GOMAXPROCS=1 make build-deploy
file backend/bin/server
sha256sum backend/bin/server
timeout 5 backend/bin/server --version
```

构建产物位置：

```bash
/opt/sub2api-src/backend/bin/server
```

`prebuild-cleanup` 默认只清理 Go build cache、apt cache、systemd journal 和 Docker build cache。除非已经确认未使用镜像不再需要回滚，否则不要设置 `PRUNE_UNUSED_IMAGES=1`。

本地构建上传不再作为默认生产路径。只有在 VPS 构建链路不可用、已经明确确认应急 fallback，并且本地构建也严格使用同一 Git commit、完整重建前端嵌入产物、补齐 SHA256 / `--version` / 健康检查时，才允许临时采用本地构建上传。

上线前应记录本次构建对应的 Git commit：

```bash
git rev-parse HEAD
git log -1 --oneline
```

## 正式 VPS 替换当前运行版本

当前容器挂载的可执行文件路径：

```bash
/opt/sub2api-deploy/custom/sub2api-pool-overview
```

替换流程：

```bash
# VPS 执行。expected_commit 必须填写本地 git rev-parse HEAD 的输出。
cd /opt/sub2api-src
git status --short
expected_commit='填写本地 git rev-parse HEAD 的输出'
test "$(git rev-parse HEAD)" = "$expected_commit"
git log -1 --oneline
src=/opt/sub2api-src/backend/bin/server
timeout 5 "$src" --version
sha256sum "$src"

live=/opt/sub2api-deploy/custom/sub2api-pool-overview
candidate=${live}.new
install -m 0755 "$src" "$candidate"
sha256sum "$src" "$candidate"

ts=$(date +%Y%m%d-%H%M%S)
cp -a "$live" "$live.bak-$ts"
mv -f "$candidate" "$live"

cd /opt/sub2api-deploy
docker compose up -d --force-recreate --no-deps sub2api
```

两行 `sha256sum` 的输出必须一致。原子 `mv` 会让现有容器继续使用旧 inode，直到 `force-recreate` 后重新挂载新文件，从而避免直接覆盖运行中二进制导致 `Text file busy`。

如果 Docker Compose 服务名变化，先用 `docker compose ps` 确认实际服务名，再重启对应服务。

## 正式 VPS 二进制流程验证

```bash
cd /opt/sub2api-deploy
docker compose ps
/opt/sub2api-deploy/custom/sub2api-pool-overview --version
curl -I https://fast.youkeduo.xyz/health
curl -I https://fast.youkeduo.xyz/redeem
curl -I https://fast.youkeduo.xyz/purchase
curl -I https://fast.youkeduo.xyz/models
curl -I -H 'Accept-Encoding: gzip' https://fast.youkeduo.xyz/dashboard
curl -I https://fast.youkeduo.xyz/assets/实际构建出的任一-js-文件名.js
docker compose logs --tail=200 sub2api
```

期望结果：

- 容器状态为 `healthy`
- `/health`、`/redeem`、`/models` 返回成功状态码
- `/purchase` 返回重定向或前端兼容页面，浏览器访问后进入 `/redeem`
- 前端页面刷新不出现 404
- `/dashboard` 在 gzip 请求下返回 `Content-Encoding: gzip`
- `/assets/*` 返回 `Cache-Control: public, max-age=31536000, immutable`
- 管理端 `/admin/accounts` 能正常打开，账号列表接口 `/api/v1/admin/accounts` 不出现 5xx
- 日志中没有启动失败、前端资源缺失、数据库迁移失败或账号列表序列化异常

## 正式 VPS 二进制流程构建后清理

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

## 正式 VPS 二进制流程回滚

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
