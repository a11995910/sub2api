# Sub2API 源码定制上线说明

本文档记录当前源码仓库、VPS 源码目录和从源码完整编译上线的固定流程。老正式 VPS `192.220.24.46` 上的 `sub2api` 当前使用自定义二进制挂载运行；新正式 VPS `207.57.145.15` 使用 Git 拉取源码、本机构建 Docker 镜像、staging 验证后再切换 prod 的部署方式。

## 强制原则

- 新功能开发默认走 `dev -> 测试 VPS -> 用户口头确认 -> main -> 正式 VPS` 链路；不得跳过测试 VPS 直接上线正式 VPS。
- 测试 VPS 固定为 `192.220.36.75`，默认拉取 `origin/dev` 分支，并按正式 VPS 的二进制挂载方式做预发布验证。
- 测试 VPS 验证通过后，只能在用户明确口头命令后合并到 `main` 并执行正式上线。
- 生产构建前必须先提交并推送 Git，严禁使用未提交工作区构建线上产物。
- 老正式 VPS / 测试 VPS 的 `/opt/sub2api-src`，以及新正式 VPS 的 `/opt/sub2api/repo`，都必须拉取到本次构建对应的同一 commit，确保线上运行产物有可追溯源码。
- 正式 VPS `192.220.24.46` 按当前二进制挂载流程维护。
- 新正式 VPS 迁移目标为 `207.57.145.15`，登录账户 `root`。迁移完成并切换正式流量前，`192.220.24.46` 仍是当前正式环境。
- 新正式 VPS 资源空间充足，迁移完成后默认使用 `deploy/Dockerfile` 构建完整 Docker 镜像，不使用旧 VPS 的低资源保护参数；只有在旧 VPS、测试 VPS 或临时资源紧张时，才继续使用 `GOFLAGS='-p=1' GOMAXPROCS=1` 等限制。
- 老正式 VPS 二进制挂载流程必须执行仓库根目录的 `make build-deploy`，该目标会先构建前端，再用 `embed` 标签构建后端二进制。
- 新正式 VPS 镜像化流程必须执行 `docker buildx build -f deploy/Dockerfile ... --load .`，由 Dockerfile 先构建前端，再把前端资源嵌入 Go 后端镜像。
- 不允许只执行 `go build -tags embed` 就覆盖线上；必须确认前端资源、后端二进制、资源文件和源码 commit 属于同一次构建。
- 内嵌前端由后端直接提供时，`/assets/*` 会返回长期缓存头，HTML/JS/CSS/JSON 会按浏览器 `Accept-Encoding` 返回 gzip 压缩；外层 Nginx 或 Caddy 仍可继续做 HTTPS、HTTP/2 和代理层优化。
- 构建产物必须包含 Git commit 和提交时间；老正式 VPS 必须核对 `/opt/sub2api-src/backend/bin/server --version`，新正式 VPS 必须核对镜像内 `/app/sub2api --version`，输出 commit 必须与待上线 commit 一致。
- 正式 VPS 替换线上二进制前必须校验 SHA256 并备份当前文件；使用同目录临时文件原子替换，禁止用 `cp` 直接覆盖正在执行的挂载文件。
- 替换后必须验证容器状态、健康接口、管理端账号页面和日志。
- 验证通过后必须清理 Docker 构建缓存、无回滚价值的旧镜像和旧二进制备份，只保留当前运行二进制和最近一份 `.bak-*` 回滚文件。
- 服务器密码、Token、数据库密码、OAuth 密钥等敏感信息不得写入仓库文档或提交记录。

## 图片 URL 本地存储

分组选择 URL 图片响应后，服务会把最终图片写入 `IMAGE_STORAGE_PATH`。Docker 默认值为 `/app/data/generated-images`，该目录位于既有 `/app/data` 持久卷内；测试、staging 和 prod 必须使用彼此独立的数据卷或宿主目录。

部署前需要确认：

- 运行用户对目录具有创建、写入、重命名和删除权限。
- 磁盘空间能够承载最近 24 小时的图片量，单张图片上限为 64MB。
- 管理端“API 端点地址”应配置为客户实际访问的 HTTPS API 地址；未配置时返回同域相对路径，避免信任客户端可控的 Host 或转发头。
- 多实例部署时所有实例共享同一个 `IMAGE_STORAGE_PATH`；独立本地盘不受支持。
- Nginx/Caddy 必须继续把 `/generated-images/*` 转发给 Sub2API，不能被前端 SPA fallback 截获。

升级后验证：选择 URL 默认传输方式的测试分组发起一次 `/v1/images/generations` 请求，确认 `data[0].url` 使用当前 API 域名、无需 API Key 可访问，并检查容器日志中没有 `generated_image.cleanup_failed` 或图片存储错误。回滚前无需迁移数据库数据；旧版本会忽略本地图片文件，但应保留目录至少 24 小时，避免已发放链接提前失效。

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

## 老正式 VPS / 测试 VPS 源码目录

老正式 VPS 和当前测试 VPS 的源码目录固定为：

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

## 新正式 VPS 迁移目标

新正式 VPS 迁移目标信息如下：

| 项目 | 值 |
| --- | --- |
| 新正式 VPS | `207.57.145.15` |
| 登录账户 | `root` |
| 凭据管理 | 不在仓库或文档记录明文密码；建议使用本机 Keychain 服务 `sub2api-new-vps-root` 或 SSH Key |
| SSH 别名 | `sub2api-new-vps` |
| 构建策略 | 新 VPS 拉取 Git 源码，在 VPS 本机构建 Docker 镜像，staging 验证后再切换 prod |

新 VPS 初始化和迁移清单见 `docs/VPS_MIGRATION_CN.md`。DNS 切换前，旧正式 VPS `192.220.24.46` 仍是生产基准；任何迁移验证都必须先在新 VPS 本机完成健康检查，再进入域名切换。

## 新正式 VPS 镜像化部署流程

新正式 VPS `207.57.145.15` 默认目录结构如下：

```bash
/opt/sub2api/
  repo/
  env/
    staging/.env
    prod/.env
  compose/
    staging/docker-compose.yml
    prod/docker-compose.yml
  data/
    staging/
    prod/
  backups/
  scripts/
```

`repo/` 是干净 Git 工作区，只负责拉取、切分支和构建镜像；`.env`、证书、数据库目录和业务数据不得写入 `repo/` 或 Git。

新 VPS 的环境 compose 文件只保存 staging 或 prod 差异，不能脱离仓库基础文件单独运行。所有 `config`、`up`、`ps`、`logs` 操作都必须同时叠加 `/opt/sub2api/repo/deploy/docker-compose.yml` 和对应环境的 override 文件，并固定 `-p` 项目名与 `--env-file`，避免两个环境共享默认项目名或漏加载基础服务定义。

### 环境 override 与镜像变量门禁

仓库基础 compose 当前为本地快速启动保留了默认镜像和固定容器名。发布前必须实际检查新 VPS 上的 staging、prod override，不能假定线上文件已经覆盖这些值。两个 override 都必须：

- 用 `${SUB2API_IMAGE:?SUB2API_IMAGE is required}` 覆盖 `sub2api` 服务镜像，使缺少目标镜像变量时 compose 直接失败。
- 分别覆盖 `sub2api`、`postgres`、`redis` 的 `container_name`，保证 staging 与 prod 三个容器名互不冲突。
- 继续通过各自 `.env` 设置独立的 `BIND_HOST`、`SERVER_PORT`、数据库、Redis 和密钥；不得复用另一环境的 `.env`。

staging override `/opt/sub2api/compose/staging/docker-compose.yml` 至少包含：

```yaml
services:
  sub2api:
    image: ${SUB2API_IMAGE:?SUB2API_IMAGE is required}
    container_name: sub2api-staging
  postgres:
    container_name: sub2api-staging-postgres
  redis:
    container_name: sub2api-staging-redis
```

prod override `/opt/sub2api/compose/prod/docker-compose.yml` 至少包含：

```yaml
services:
  sub2api:
    image: ${SUB2API_IMAGE:?SUB2API_IMAGE is required}
    container_name: sub2api-prod
  postgres:
    container_name: sub2api-prod-postgres
  redis:
    container_name: sub2api-prod-redis
```

每个 `.env` 必须预先且仅有一行 `SUB2API_IMAGE=`。下面的 root-only 脚本先保留带权限和属主的备份，再在原文件同目录生成临时文件，核对唯一目标值后用 `mv` 原子替换。staging、prod 更新和回滚都必须调用该脚本；备份目录含敏感配置，只允许 root 读取，不得输出内容或写入 Git：

```bash
ssh sub2api-new-vps
install -d -m 0700 /opt/sub2api/backups /opt/sub2api/scripts
umask 077
tee /opt/sub2api/scripts/update-sub2api-image >/dev/null <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail

test "$#" -eq 3
env_file="$1"
target_image="$2"
backup_prefix="$3"
timestamp="$(date +%Y%m%d-%H%M%S)"

test -f "$env_file"
test "$(grep -c '^SUB2API_IMAGE=' "$env_file")" -eq 1
cp -a -- "$env_file" "/opt/sub2api/backups/${backup_prefix}.env.${timestamp}"

umask 077
tmp="$(mktemp "${env_file}.new.XXXXXX")"
trap 'rm -f -- "$tmp"' EXIT
sed "s|^SUB2API_IMAGE=.*$|SUB2API_IMAGE=${target_image}|" "$env_file" > "$tmp"
test "$(grep -c '^SUB2API_IMAGE=' "$tmp")" -eq 1
test "$(grep -Fxc "SUB2API_IMAGE=${target_image}" "$tmp")" -eq 1
chmod --reference="$env_file" "$tmp"
chown --reference="$env_file" "$tmp"
mv -f -- "$tmp" "$env_file"
trap - EXIT
SCRIPT
chmod 0700 /opt/sub2api/scripts/update-sub2api-image

tee /opt/sub2api/scripts/assert-no-explicit-video-pricing >/dev/null <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail

test "$#" -eq 1
env_file="$1"
test -f "$env_file"

video_counts="$(
  docker compose -p sub2api-prod \
    --env-file "$env_file" \
    -f /opt/sub2api/repo/deploy/docker-compose.yml \
    -f /opt/sub2api/compose/prod/docker-compose.yml \
    exec -T postgres sh -c \
      'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -F ":"' <<'SQL'
SELECT 'channel_model_pricing', COUNT(*)
FROM channel_model_pricing
WHERE billing_mode = 'video'
UNION ALL
SELECT 'channel_account_stats_model_pricing', COUNT(*)
FROM channel_account_stats_model_pricing
WHERE billing_mode = 'video';
SQL
)"

test "$(printf '%s\n' "$video_counts" | grep -Fxc 'channel_model_pricing:0' || true)" -eq 1
test "$(printf '%s\n' "$video_counts" | grep -Fxc 'channel_account_stats_model_pricing:0' || true)" -eq 1
test "$(printf '%s\n' "$video_counts" | wc -l)" -eq 2
SCRIPT
chmod 0700 /opt/sub2api/scripts/assert-no-explicit-video-pricing

tee /opt/sub2api/scripts/assert-no-account-stats-video-pricing >/dev/null <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail

test "$#" -eq 1
env_file="$1"
test -f "$env_file"

video_count="$(
  docker compose -p sub2api-prod \
    --env-file "$env_file" \
    -f /opt/sub2api/repo/deploy/docker-compose.yml \
    -f /opt/sub2api/compose/prod/docker-compose.yml \
    exec -T postgres sh -c \
      'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At' <<'SQL'
SELECT COUNT(*)
FROM channel_account_stats_model_pricing
WHERE billing_mode = 'video';
SQL
)"

test "$video_count" = "0"
SCRIPT
chmod 0700 /opt/sub2api/scripts/assert-no-account-stats-video-pricing

tee /opt/sub2api/scripts/assert-no-user-scoped-openai-fast-policy >/dev/null <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail

test "$#" -eq 1
env_file="$1"
test -f "$env_file"

if ! fast_policy_state="$(
  docker compose -p sub2api-prod \
    --env-file "$env_file" \
    -f /opt/sub2api/repo/deploy/docker-compose.yml \
    -f /opt/sub2api/compose/prod/docker-compose.yml \
    exec -T postgres sh -c \
      'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At' <<'SQL'
WITH rules AS (
  SELECT jsonb_array_elements(
    CASE
      WHEN jsonb_typeof(value::jsonb -> 'rules') = 'array' THEN value::jsonb -> 'rules'
      ELSE '[]'::jsonb
    END
  ) AS rule
  FROM settings
  WHERE key = 'openai_fast_policy_settings'
)
SELECT CASE WHEN EXISTS (
  SELECT 1
  FROM rules
  WHERE CASE
    WHEN NOT (rule ? 'user_ids') THEN false
    WHEN rule -> 'user_ids' = 'null'::jsonb THEN false
    WHEN jsonb_typeof(rule -> 'user_ids') = 'array'
      THEN jsonb_array_length(rule -> 'user_ids') > 0
    ELSE true
  END
) THEN 'unsafe' ELSE 'safe' END;
SQL
)"; then
  exit 1
fi

case "$fast_policy_state" in
  safe) exit 0 ;;
  unsafe) exit 10 ;;
  *) exit 1 ;;
esac
SCRIPT
chmod 0700 /opt/sub2api/scripts/assert-no-user-scoped-openai-fast-policy

tee /opt/sub2api/scripts/snapshot-openai-fast-policy >/dev/null <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail

test "$#" -eq 2
env_file="$1"
output_file="$2"
test -f "$env_file"
test -d "$(dirname "$output_file")"

snapshot="$(
  docker compose -p sub2api-prod \
    --env-file "$env_file" \
    -f /opt/sub2api/repo/deploy/docker-compose.yml \
    -f /opt/sub2api/compose/prod/docker-compose.yml \
    exec -T postgres sh -c \
      'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At' <<'SQL'
SELECT CASE
  WHEN value IS NULL THEN 'absent'
  ELSE 'present:' || encode(convert_to(value, 'UTF8'), 'hex')
END
FROM (
  SELECT (SELECT value FROM settings WHERE key = 'openai_fast_policy_settings') AS value
) AS snapshot;
SQL
)"

if [[ "$snapshot" != "absent" && ! "$snapshot" =~ ^present:([0-9a-f]{2})+$ ]]; then
  exit 1
fi
umask 077
tmp="$(mktemp "${output_file}.new.XXXXXX")"
trap 'rm -f -- "$tmp"' EXIT
printf '%s\n' "$snapshot" > "$tmp"
test "$(wc -l < "$tmp")" -eq 1
chmod 0600 "$tmp"
mv -f -- "$tmp" "$output_file"
trap - EXIT
SCRIPT
chmod 0700 /opt/sub2api/scripts/snapshot-openai-fast-policy

tee /opt/sub2api/scripts/restore-openai-fast-policy >/dev/null <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail

test "$#" -eq 2
env_file="$1"
snapshot_file="$2"
test -f "$env_file"
test -s "$snapshot_file"
test "$(wc -l < "$snapshot_file")" -eq 1
IFS= read -r snapshot < "$snapshot_file"

compose_prod() {
  docker compose -p sub2api-prod \
    --env-file "$env_file" \
    -f /opt/sub2api/repo/deploy/docker-compose.yml \
    -f /opt/sub2api/compose/prod/docker-compose.yml "$@"
}

case "$snapshot" in
  absent)
    compose_prod exec -T postgres sh -c \
      'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<'SQL'
DELETE FROM settings WHERE key = 'openai_fast_policy_settings';
SQL
    ;;
  present:*)
    [[ "$snapshot" =~ ^present:([0-9a-f]{2})+$ ]]
    fast_policy_hex="${snapshot#present:}"
    compose_prod exec -T postgres sh -c \
      'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v fast_policy_hex="$1"' sh "$fast_policy_hex" <<'SQL'
INSERT INTO settings (key, value, updated_at)
VALUES (
  'openai_fast_policy_settings',
  convert_from(decode(:'fast_policy_hex', 'hex'), 'UTF8'),
  NOW()
)
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at;
SQL
    ;;
  *) exit 1 ;;
esac
SCRIPT
chmod 0700 /opt/sub2api/scripts/restore-openai-fast-policy
```

账号统计定价链路不消费视频时长，因此 `assert-no-account-stats-video-pricing` 是发布与回滚都必须执行的 fail-closed 门禁。主渠道显式视频定价只有在回滚镜像不包含每秒计费能力时才执行 `assert-no-explicit-video-pricing`；已证明兼容的镜像允许保留主表中的合法 `video` 记录。Fast/Flex 断言只在回滚目标不支持 `user_ids` 时执行。数据库连接、JSON 解析、SQL、输出内容或计数任一不符合预期都会返回非零。Fast/Flex 断言只用退出码 `10` 表示明确存在无法由旧版安全解释的 `user_ids`；其他非零值均表示检查失败，不得自动覆盖数据库。Fast/Flex 断言不得把非法 JSON 或非法 `user_ids` 类型当成安全配置。

### staging 构建与发布

staging 用于承接 `dev` 或功能分支。每次构建都必须核对本地已推送 commit 与新 VPS 工作区 commit 一致：

```bash
ssh sub2api-new-vps
set -Eeuo pipefail
cd /opt/sub2api/repo
git status --short
git fetch origin
git switch dev
git pull --ff-only origin dev
expected_commit='填写本地 dev 的 git rev-parse HEAD 输出'
test "$(git rev-parse HEAD)" = "$expected_commit"
git log -1 --oneline

commit="$(git rev-parse --short=12 HEAD)"
date="$(git show -s --format=%cI HEAD)"
docker buildx build \
  -f deploy/Dockerfile \
  --build-arg COMMIT="$commit" \
  --build-arg DATE="$date" \
  -t "sub2api:staging-$commit" \
  --load .
docker run --rm "sub2api:staging-$commit" --version

env_file=/opt/sub2api/env/staging/.env
target_image="sub2api:staging-$commit"
/opt/sub2api/scripts/update-sub2api-image "$env_file" "$target_image" staging

cd /opt/sub2api/repo/deploy
compose_staging() {
  docker compose -p sub2api-staging \
    --env-file "$env_file" \
    -f /opt/sub2api/repo/deploy/docker-compose.yml \
    -f /opt/sub2api/compose/staging/docker-compose.yml "$@"
}

compose_staging config -q
resolved_images="$(compose_staging config --images)"
test "$(printf '%s\n' "$resolved_images" | grep -Fxc "$target_image" || true)" -eq 1
compose_staging up -d
container_id="$(compose_staging ps -q sub2api)"
test -n "$container_id"
test "$(docker inspect --format '{{.Config.Image}}' "$container_id")" = "$target_image"
compose_staging ps
curl -I http://127.0.0.1:18080/health
compose_staging logs --tail=200 sub2api
```

`config -q` 只验证 compose 结构；随后对 `config --images` 的精确计数断言用于证明最终合并配置只引用一次目标应用镜像。`up` 后还必须通过 `compose ps -q sub2api` 定位真实容器，再由 `docker inspect` 证明实际运行 tag 与目标 tag 相同，三项均通过才算发布成功。

staging 功能验收必须使用隔离测试账号、渠道、分组、API Key 和唯一请求 ID，开始前记录所有测试对象 ID 及余额基线。以下快照命令在上述 staging 发布的同一 SSH 会话执行；若已开启新会话，必须先按 staging 发布段重新定义 `env_file` 和 `compose_staging()`。测试前先确认 PostgreSQL 容器确实属于 `sub2api-staging` compose project，并生成可读的完整数据库快照：

```bash
postgres_id="$(compose_staging ps -q postgres)"
test -n "$postgres_id"
test "$(docker inspect --format '{{ index .Config.Labels "com.docker.compose.project" }}' "$postgres_id")" = "sub2api-staging"

timestamp="$(date +%Y%m%d-%H%M%S)"
staging_snapshot="/opt/sub2api/backups/staging-before-video-test-${timestamp}.dump"
umask 077
compose_staging exec -T postgres sh -c \
  'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > "$staging_snapshot"
test -s "$staging_snapshot"
chmod 0600 "$staging_snapshot"
```

验收后优先通过对应管理接口删除测试对象，并核对测试请求、余额和定价记录已清理；不能只删除渠道而保留用量或余额副作用。若接口无法完整清理，只能在确认无人并行使用 staging 后恢复上述快照：

```bash
ssh sub2api-new-vps
cd /opt/sub2api/repo/deploy
env_file=/opt/sub2api/env/staging/.env
compose_staging() {
  docker compose -p sub2api-staging \
    --env-file "$env_file" \
    -f /opt/sub2api/repo/deploy/docker-compose.yml \
    -f /opt/sub2api/compose/staging/docker-compose.yml "$@"
}

staging_snapshot='填写已验证可读的 staging-before-video-test-*.dump 绝对路径'
test -s "$staging_snapshot"
postgres_id="$(compose_staging ps -q postgres)"
test -n "$postgres_id"
test "$(docker inspect --format '{{ index .Config.Labels "com.docker.compose.project" }}' "$postgres_id")" = "sub2api-staging"

compose_staging stop sub2api
compose_staging exec -T postgres sh -c \
  'pg_restore --exit-on-error --clean --if-exists --no-owner -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
  < "$staging_snapshot"
compose_staging exec -T redis redis-cli FLUSHDB
compose_staging up -d sub2api
compose_staging ps
curl -I http://127.0.0.1:18080/health
```

快照恢复只能针对 `sub2api-staging` 项目及 staging 数据卷；不得对 prod 执行 staging 清理命令，也不得把 staging 数据复制到 prod。恢复前若 project 标签、快照文件或独占窗口任一项无法确认，停止清理并保留测试对象 ID 供人工处理。

### prod 切换与回滚

如果当前版本已保存包含 `user_ids` 的 `openai_fast_policy_settings`，旧镜像回滚前必须先恢复发布前的该设置快照，或删除所有带 `user_ids` 的规则。旧版本忽略未知字段后会把用户专属规则视为全局规则，可能导致全局 block、filter 或 force_priority；只切换镜像不构成安全回滚。

prod 只允许使用 `main`，并且必须在 staging 验证通过、用户明确确认后执行。截至 2026-07-11，生产主渠道定价表已有 2 条合法的显式 `video` 每秒定价，账号统计定价表为 0 条；发布前仍必须用真实表查询再次确认，不能把盘点结论当作永久事实：

```sql
SELECT 'channel_model_pricing' AS source, COUNT(*) AS video_count
FROM channel_model_pricing
WHERE billing_mode = 'video'
UNION ALL
SELECT 'channel_account_stats_model_pricing' AS source, COUNT(*) AS video_count
FROM channel_account_stats_model_pricing
WHERE billing_mode = 'video';
```

`channel_account_stats_model_pricing` 必须始终为 `0`，因为账号统计链路不按视频时长计费。`channel_model_pricing` 是否必须为 `0` 取决于回滚镜像能力：新镜像必须在 `--version` 中显式声明 `explicit_video_pricing_per_second`；历史镜像只有精确 commit `a08a958be9a29594692ab87f74c9227504c09d27` 和 `7d5b9bc6bb6d854e00d97bf185ed131e69bfbcd6` 经过代码审查确认兼容。其他没有能力标识的镜像一律按不支持处理，不能只看版本号或祖先关系。prod 更新前还必须记录当前运行镜像 tag、镜像 ID、容器完整 `--version` 输出、真实 commit、回滚能力位和目标 Git commit，并确认数据库已有可恢复备份；同时备份当前 prod `.env`。定价恢复材料至少应覆盖 `channel_model_pricing`、`channel_pricing_intervals`、`channel_account_stats_model_pricing` 和 `channel_account_stats_pricing_intervals`，不得只保存页面截图。

```bash
ssh sub2api-new-vps
set -Eeuo pipefail
cd /opt/sub2api/repo
git status --short
git fetch origin
git switch main
git pull --ff-only origin main
expected_commit='填写已确认上线的 main commit'
test "$(git rev-parse HEAD)" = "$expected_commit"
git log -1 --oneline

commit="$(git rev-parse --short=12 HEAD)"
date="$(git show -s --format=%cI HEAD)"
target_image="sub2api:prod-$commit"
docker tag "sub2api:staging-$commit" "$target_image" 2>/dev/null || \
  docker buildx build \
    -f deploy/Dockerfile \
    --build-arg COMMIT="$commit" \
    --build-arg DATE="$date" \
    -t "$target_image" \
    --load .
target_version_output="$(docker run --rm "$target_image" --version)"
test "$(printf '%s\n' "$target_version_output" | wc -l)" -eq 1
target_commit_short="$(printf '%s\n' "$target_version_output" | sed -nE 's/.*commit: ([0-9a-f]{12}).*/\1/p')"
test -n "$target_commit_short"
target_commit="$(git rev-parse "$target_commit_short^{commit}")"
test "$target_commit" = "$expected_commit"

cd /opt/sub2api/repo/deploy
env_file=/opt/sub2api/env/prod/.env
compose_prod() {
  docker compose -p sub2api-prod \
    --env-file "$env_file" \
    -f /opt/sub2api/repo/deploy/docker-compose.yml \
    -f /opt/sub2api/compose/prod/docker-compose.yml "$@"
}

version_at_least() {
  local current="$1" required="$2"
  local current_major current_minor current_patch
  local required_major required_minor required_patch
  IFS=. read -r current_major current_minor current_patch <<< "$current"
  IFS=. read -r required_major required_minor required_patch <<< "$required"
  (( current_major > required_major )) ||
    (( current_major == required_major && current_minor > required_minor )) ||
    (( current_major == required_major && current_minor == required_minor && current_patch >= required_patch ))
}

version_has_capability() {
  local version_output="$1" capability="$2" capabilities
  capabilities="$(printf '%s\n' "$version_output" | sed -nE 's/.*capabilities: ([^)]+)\).*/\1/p')"
  test -n "$capabilities" || return 1
  printf '%s\n' "$capabilities" | tr ',' '\n' | \
    sed 's/^[[:space:]]*//; s/[[:space:]]*$//' | grep -Fqx "$capability"
}

explicit_video_pricing_capability='explicit_video_pricing_per_second'
version_has_capability "$target_version_output" "$explicit_video_pricing_capability"

compose_prod config -q
current_container_id="$(compose_prod ps -q sub2api)"
test -n "$current_container_id"
previous_original_image="$(docker inspect --format '{{.Config.Image}}' "$current_container_id")"
test -n "$previous_original_image"
previous_image_id="$(docker inspect --format '{{.Image}}' "$current_container_id")"
test -n "$previous_image_id"
previous_version_output="$(docker exec "$current_container_id" /app/sub2api --version)"
test "$(printf '%s\n' "$previous_version_output" | wc -l)" -eq 1
previous_version="$(printf '%s\n' "$previous_version_output" | sed -nE 's/.*Sub2API ([0-9]+\.[0-9]+\.[0-9]+).*/\1/p')"
previous_commit_short="$(printf '%s\n' "$previous_version_output" | sed -nE 's/.*commit: ([0-9a-f]{12}).*/\1/p')"
test -n "$previous_version"
test "$(printf '%s\n' "$previous_version" | wc -l)" -eq 1
test -n "$previous_commit_short"
test "$(printf '%s\n' "$previous_commit_short" | wc -l)" -eq 1
previous_commit="$(git rev-parse "$previous_commit_short^{commit}")"
test "${previous_commit:0:12}" = "$previous_commit_short"
previous_supports_fast_policy_user_ids=0
if version_at_least "$previous_version" "0.1.151"; then
  previous_supports_fast_policy_user_ids=1
fi
previous_supports_explicit_video_pricing=0
case "$previous_commit" in
  a08a958be9a29594692ab87f74c9227504c09d27|7d5b9bc6bb6d854e00d97bf185ed131e69bfbcd6)
    previous_supports_explicit_video_pricing=1
    ;;
  *)
    if version_has_capability "$previous_version_output" "$explicit_video_pricing_capability"; then
      previous_supports_explicit_video_pricing=1
    fi
    ;;
esac

timestamp="$(date +%Y%m%d-%H%M%S)"
previous_image="sub2api:rollback-${timestamp}-${previous_commit_short}"
release_record="/opt/sub2api/backups/prod-release-before-${timestamp}.txt"
pricing_backup="/opt/sub2api/backups/prod-pricing-before-${timestamp}.dump"
fast_policy_backup="/opt/sub2api/backups/prod-fast-policy-before-${timestamp}.txt"
umask 077

/opt/sub2api/scripts/assert-no-account-stats-video-pricing "$env_file"
if [ "$previous_supports_explicit_video_pricing" -eq 0 ]; then
  /opt/sub2api/scripts/assert-no-explicit-video-pricing "$env_file"
fi

compose_prod exec -T postgres sh -c \
  'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc --data-only --table=channel_model_pricing --table=channel_pricing_intervals --table=channel_account_stats_model_pricing --table=channel_account_stats_pricing_intervals' \
  </dev/null > "$pricing_backup"
test -s "$pricing_backup"
chmod 0600 "$pricing_backup"

/opt/sub2api/scripts/snapshot-openai-fast-policy "$env_file" "$fast_policy_backup"

rollback_tag_created=0
cleanup_unrecorded_rollback_tag() {
  original_rc="$?"
  rm -f "$release_record"
  if [ "$rollback_tag_created" -eq 1 ]; then
    docker image rm "$previous_image" >/dev/null 2>&1 || true
  fi
  exit "$original_rc"
}
trap cleanup_unrecorded_rollback_tag EXIT
docker tag "$previous_image_id" "$previous_image"
rollback_tag_created=1
test "$(docker image inspect --format '{{.Id}}' "$previous_image")" = "$previous_image_id"

{
  printf 'previous_image=%s\n' "$previous_image"
  printf 'previous_original_image=%s\n' "$previous_original_image"
  printf 'previous_image_id=%s\n' "$previous_image_id"
  printf 'previous_version_output=%s\n' "$previous_version_output"
  printf 'previous_version=%s\n' "$previous_version"
  printf 'previous_commit=%s\n' "$previous_commit"
  printf 'previous_supports_fast_policy_user_ids=%s\n' "$previous_supports_fast_policy_user_ids"
  printf 'previous_supports_explicit_video_pricing=%s\n' "$previous_supports_explicit_video_pricing"
  printf 'target_commit=%s\n' "$expected_commit"
  printf 'fast_policy_backup=%s\n' "$fast_policy_backup"
  printf 'pricing_backup=%s\n' "$pricing_backup"
} > "$release_record"
chmod 0600 "$release_record"
trap - EXIT

/opt/sub2api/scripts/update-sub2api-image "$env_file" "$target_image" prod
compose_prod config -q
resolved_images="$(compose_prod config --images)"
test "$(printf '%s\n' "$resolved_images" | grep -Fxc "$target_image" || true)" -eq 1
compose_prod up -d
container_id="$(compose_prod ps -q sub2api)"
test -n "$container_id"
test "$(docker inspect --format '{{.Config.Image}}' "$container_id")" = "$target_image"
compose_prod ps
curl -I http://127.0.0.1:8080/health
compose_prod logs --tail=200 sub2api

# 专用回滚 tag 只保留最近两份；原有 prod tag 不受此清理影响。
mapfile -t rollback_tags < <(
  docker images --format '{{.Repository}}:{{.Tag}}' \
    --filter 'reference=sub2api:rollback-*' | sort -r
)
test "$(printf '%s\n' "${rollback_tags[@]}" | grep -Fxc "$previous_image" || true)" -eq 1
for ((i = 2; i < ${#rollback_tags[@]}; i++)); do
  docker image rm "${rollback_tags[$i]}" >/dev/null 2>&1 || \
    printf '警告：旧回滚 tag 清理失败：%s\n' "${rollback_tags[$i]}" >&2
done
```

prod 更新完成后进入观察窗口。回滚时必须先保持账号统计定价表无 `video`；如果发布记录证明 `previous_image` 支持显式视频每秒计费，主渠道表中的合法 `video` 记录可以原样保留，否则主渠道表也必须通过零计数门禁。满足对应能力门禁后，才可以把发布前记录的 `previous_image` 原子写回 `.env`：

```bash
ssh sub2api-new-vps
set -Eeuo pipefail
cd /opt/sub2api/repo/deploy
env_file=/opt/sub2api/env/prod/.env
compose_prod() {
  docker compose -p sub2api-prod \
    --env-file "$env_file" \
    -f /opt/sub2api/repo/deploy/docker-compose.yml \
    -f /opt/sub2api/compose/prod/docker-compose.yml "$@"
}

version_at_least() {
  local current="$1" required="$2"
  local current_major current_minor current_patch
  local required_major required_minor required_patch
  IFS=. read -r current_major current_minor current_patch <<< "$current"
  IFS=. read -r required_major required_minor required_patch <<< "$required"
  (( current_major > required_major )) ||
    (( current_major == required_major && current_minor > required_minor )) ||
    (( current_major == required_major && current_minor == required_minor && current_patch >= required_patch ))
}

version_has_capability() {
  local version_output="$1" capability="$2" capabilities
  capabilities="$(printf '%s\n' "$version_output" | sed -nE 's/.*capabilities: ([^)]+)\).*/\1/p')"
  test -n "$capabilities" || return 1
  printf '%s\n' "$capabilities" | tr ',' '\n' | \
    sed 's/^[[:space:]]*//; s/[[:space:]]*$//' | grep -Fqx "$capability"
}

# 以下值必须填写同一份 prod-release-before-*.txt 的记录值。
rollback_image='填写发布前记录的旧镜像 tag'
rollback_image_id='填写 previous_image_id'
rollback_version='填写 previous_version'
rollback_commit='填写 previous_commit'
# fast_policy_backup 必须填写同一发布记录中的快照路径。
fast_policy_backup='填写发布前记录的 prod-fast-policy-before-*.txt 绝对路径'
actual_rollback_image_id="$(docker image inspect --format '{{.Id}}' "$rollback_image")"
test "$actual_rollback_image_id" = "$rollback_image_id"
rollback_version_output="$(docker run --rm "$rollback_image" --version)"
test "$(printf '%s\n' "$rollback_version_output" | wc -l)" -eq 1
actual_rollback_version="$(printf '%s\n' "$rollback_version_output" | sed -nE 's/.*Sub2API ([0-9]+\.[0-9]+\.[0-9]+).*/\1/p')"
actual_rollback_commit_short="$(printf '%s\n' "$rollback_version_output" | sed -nE 's/.*commit: ([0-9a-f]{12}).*/\1/p')"
test "$actual_rollback_version" = "$rollback_version"
test -n "$actual_rollback_commit_short"
actual_rollback_commit="$(git rev-parse "$actual_rollback_commit_short^{commit}")"
test "$actual_rollback_commit" = "$rollback_commit"

rollback_supports_fast_policy_user_ids=0
if version_at_least "$actual_rollback_version" "0.1.151"; then
  rollback_supports_fast_policy_user_ids=1
fi
explicit_video_pricing_capability='explicit_video_pricing_per_second'
rollback_supports_explicit_video_pricing=0
case "$actual_rollback_commit" in
  a08a958be9a29594692ab87f74c9227504c09d27|7d5b9bc6bb6d854e00d97bf185ed131e69bfbcd6)
    rollback_supports_explicit_video_pricing=1
    ;;
  *)
    if version_has_capability "$rollback_version_output" "$explicit_video_pricing_capability"; then
      rollback_supports_explicit_video_pricing=1
    fi
    ;;
esac
test -s "$fast_policy_backup"
test "$(wc -l < "$fast_policy_backup")" -eq 1

current_container_id="$(compose_prod ps -q sub2api)"
test -n "$current_container_id"
current_image="$(docker inspect --format '{{.Config.Image}}' "$current_container_id")"
test -n "$current_image"
timestamp="$(date +%Y%m%d-%H%M%S)"
rollback_policy_backup="/opt/sub2api/backups/prod-fast-policy-before-rollback-${timestamp}.txt"
rollback_policy_snapshot_ready=0

restore_current_release_state() {
	original_rc="${1:-$?}"
	trap - ERR
	set +e
	recovery_failed=0
	if [ "$rollback_policy_snapshot_ready" -eq 1 ]; then
		/opt/sub2api/scripts/restore-openai-fast-policy "$env_file" "$rollback_policy_backup" || recovery_failed=1
	fi
	/opt/sub2api/scripts/update-sub2api-image "$env_file" "$current_image" prod-rollback-abort || recovery_failed=1
	compose_prod up -d --no-deps sub2api || recovery_failed=1
	if [ "$recovery_failed" -ne 0 ]; then
		printf '回滚失败，且恢复当前发布状态未完全成功，请立即人工处理。\n' >&2
	fi
	exit "$original_rc"
}
trap restore_current_release_state ERR

# 先停止应用写入并保存回滚即时设置，再执行门禁；任一步失败都会同时恢复
# 即时设置快照和当前镜像，避免只恢复镜像却丢失观察窗口内的用户规则。
compose_prod stop sub2api
/opt/sub2api/scripts/snapshot-openai-fast-policy "$env_file" "$rollback_policy_backup"
rollback_policy_snapshot_ready=1
/opt/sub2api/scripts/assert-no-account-stats-video-pricing "$env_file"
if [ "$rollback_supports_explicit_video_pricing" -eq 0 ]; then
  /opt/sub2api/scripts/assert-no-explicit-video-pricing "$env_file"
fi

# 只有旧版回滚目标不认识 user_ids 时才执行兼容门禁。v0.1.151 及后续
# 兼容镜像之间回滚保留当前设置，不要求管理员删除合法的用户专属规则。
if [ "$rollback_supports_fast_policy_user_ids" -eq 0 ]; then
  if /opt/sub2api/scripts/assert-no-user-scoped-openai-fast-policy "$env_file"; then
    fast_policy_rc=0
  else
    fast_policy_rc="$?"
  fi
  case "$fast_policy_rc" in
    0) ;;
    10)
      /opt/sub2api/scripts/restore-openai-fast-policy "$env_file" "$fast_policy_backup"
      /opt/sub2api/scripts/assert-no-user-scoped-openai-fast-policy "$env_file"
      ;;
    *) restore_current_release_state "$fast_policy_rc" ;;
  esac
fi

/opt/sub2api/scripts/update-sub2api-image "$env_file" "$rollback_image" prod-rollback
compose_prod config -q
resolved_images="$(compose_prod config --images)"
test "$(printf '%s\n' "$resolved_images" | grep -Fxc "$rollback_image" || true)" -eq 1
compose_prod up -d --no-deps sub2api
container_id="$(compose_prod ps -q sub2api)"
test -n "$container_id"
test "$(docker inspect --format '{{.Config.Image}}' "$container_id")" = "$rollback_image"
compose_prod ps
curl -I http://127.0.0.1:8080/health
compose_prod logs --tail=200 sub2api
trap - ERR
```

主渠道定价表写入显式 `billing_mode='video'` 后，其 `per_request_price` 和分辨率层级价格表示每秒价格。只有发布记录明确证明回滚镜像包含相同每秒语义时，才允许原样回滚；能力未知或不支持时禁止直接切换，否则会错误计费且旧管理端可能无法保存该配置。账号统计定价表不支持视频时长语义，任何版本发布或回滚前都必须保持其显式 `video` 记录为零。

如果必须回滚到不支持显式视频每秒语义的镜像，先停止应用写入及相关视频流量或禁用受影响渠道，再根据发布前数据库备份精确恢复原始定价记录，并调用 `assert-no-explicit-video-pricing`。只有脚本确认两张真实表均严格为零后才能切旧镜像；脚本失败时必须保持或恢复当前新镜像。不得把每秒价格直接改成 `image/per_request`，不得按固定时长猜测换算，也不得用全库回档覆盖观察窗口内的其他生产写入。无法证明原定价已准确恢复时，继续停用相关渠道并采用滚前修复。

回滚后仍需验证容器状态、实际镜像 tag、健康接口、管理端账号页、关键 API 和日志。发布与回滚期间都要保留当前运行镜像和 `previous_image`；成功发布后 `sub2api:rollback-*` 只保留最近两份，清理构建缓存时不得删除这两份回滚 tag。原有带业务意义的 `sub2api:prod-*` tag 另行按发布记录管理，不属于该自动清理范围。

Docker 镜像构建的运行模式为 `docker`。管理端只提供版本检查，不允许在容器内执行在线更新、在线回退或覆盖 `/app/sub2api`；镜像化环境必须通过本节的 Git commit 镜像 tag 完成升级与回滚，避免覆盖定制代码或在容器重建后丢失回退结果。

### 上游同步

上游同步不直接进入 prod。同步流程必须先建立独立分支，例如：

```bash
git switch -c sync/upstream-YYYYMMDD
```

解决冲突并完成本地验证后，先部署到新 VPS staging。只有 staging 验证通过后，才允许把同步结果并入 `dev`，再按正常流程进入 `main` 和 prod。

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

本节仅适用于老正式 VPS `192.220.24.46` 和当前测试 VPS `192.220.36.75` 的二进制挂载流程。新正式 VPS `207.57.145.15` 使用上文的镜像化部署流程。

部署产物必须包含前端静态资源，因此后端编译必须使用 `embed` 标签。当前仓库已经提供统一目标。

老正式 VPS / 测试 VPS 的线上编译机器为 `/opt/sub2api-src`。VPS 需要先准备：

- Go：以 `backend/go.mod` 中声明的版本为准
- Node.js / pnpm：用于构建 `frontend`
- make：用于执行仓库根目录的构建目标

旧正式 VPS 和当前测试 VPS 默认在 `/opt/sub2api-src` 构建 Linux amd64 二进制。构建前必须先清理可再生成缓存，避免系统盘被 Go / Node / Docker 构建缓存打满：

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
