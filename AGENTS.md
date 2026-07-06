# AGENTS.md

本文件是本项目的协作与上线约束。所有 AI 助手、自动化脚本和人工协作在本仓库内工作时都必须遵守。

## 语言与沟通

- 所有回复、分析、计划、注释、文档和提交说明必须使用中文。
- 输出先给结论，再给依据；存在不确定信息时必须明确说明，并给出核实方式。
- 复杂问题必须先拆解任务，再逐步执行，不能跳过上下文直接修改。

## 修改代码前的要求

- 必须先阅读相关文件、调用链、类型定义、配置项、上下游逻辑和已有实现。
- 严禁猜测接口格式、字段含义、数据结构、配置语义和业务规则。
- 修复问题必须先定位根因，再实施修改，不能只修复表面现象。
- 修改后必须检查影响范围，包括页面、接口、数据表、权限、状态流转、兼容逻辑和异常场景。
- 不允许回滚或覆盖用户已有改动，除非用户明确要求。

## Git 与提交

- 修改前后都要查看 `git status --short`，确认工作区状态。
- 提交前必须检查 diff，确保只包含本次任务相关内容。
- 日常新功能开发默认在 `dev` 分支完成并推送到 `origin/dev`，测试 VPS 验证通过后，必须等待用户口头确认再合并到 `main` 并上线正式 VPS。
- 提交说明使用中文，格式保持简洁，例如 `docs: 规范源码定制上线流程`。
- `AGENTS.md` 被 `.gitignore` 忽略，首次加入仓库时必须使用 `git add -f AGENTS.md`。
- 任何生产构建前，本地相关改动必须先完成 Git 提交并推送到远端；严禁用未提交工作区直接构建生产产物。
- VPS 上线前必须在 `/opt/sub2api-src` 执行 `git pull --ff-only`，并核对线上源码 commit 与本次构建 commit 一致。

## 测试 VPS 与正式 VPS 操作

- 测试 VPS：`192.220.36.75`，用途为新功能预发布验证，默认源码目录、部署目录和构建方式应尽量与正式 VPS 保持一致。
- 测试 VPS 默认拉取 `origin/dev` 分支；新功能完成后必须先部署到测试 VPS 验证。
- 测试 VPS 验证通过后，只能报告验证结果和风险点；必须等用户明确口头命令后，才允许合并 `dev` 到 `main` 并上线正式 VPS。
- 正式 VPS：`192.220.24.46`，内部包含 VPN、`sub2api`、`chatgpt2api`，正式上线仍按生产上线规范执行。
- 国内腾讯云服务器：`118.89.91.26`，账户为 `ubuntu`，仅在用户明确要求相关操作时使用。
- 服务器密码、SSH 私钥、Token、数据库密码、OAuth 密钥和 Cookie 等敏感信息不得写入仓库、文档、提交记录或日志；如需使用，只能通过运行时凭据或环境变量临时注入。

## 生产操作

- 生产操作前必须确认当前运行来源，包括源码目录、Git remote、当前 commit、容器挂载路径和运行中的二进制路径。
- 线上敏感信息不得写入仓库，包括服务器密码、Token、数据库密码、OAuth 密钥和 Cookie。
- 涉及数据库写入、迁移或批量数据修复时，必须先确认表结构、影响范围、备份方式和回滚方式。
- 不确定线上实际状态时，优先通过只读命令核实，例如 `git status --short`、`git remote -v`、`git rev-parse HEAD`、`docker compose ps`、`docker compose logs --tail=200`。

## Sub2API 正式 VPS 定制二进制上线规范

正式 VPS `192.220.24.46` 当前 `sub2api` 使用自定义二进制挂载运行，挂载文件为：

```bash
/opt/sub2api-deploy/custom/sub2api-pool-overview
```

正确源码目录为：

```bash
/opt/sub2api-src
```

正式 VPS 生产上线默认不重建 Docker 镜像。除非容器基础环境、入口脚本或系统依赖发生变化，否则只允许构建并替换 Linux amd64 的定制二进制。

默认构建方式为 VPS 线上同源构建：本地只负责提交并推送代码，生产二进制必须在 VPS `/opt/sub2api-src` 拉取同一 Git commit 后构建。构建前必须保证本地改动已提交并推送：

```bash
cd /Users/wangjun/Documents/GitHub/sub2api
git status --short
git log -1 --oneline
git push
git rev-parse HEAD
```

线上构建前必须先清理可再生成缓存，避免系统盘被 Go / Node / Docker 构建缓存打满；默认不设置 `PRUNE_UNUSED_IMAGES=1`，避免误删仍有回滚价值的镜像：

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

线上源码 commit 必须与本次本地推送的 commit 一致，用于保证运行产物有可追溯源码：

```bash
cd /opt/sub2api-src
git status --short
git rev-parse HEAD
```

严禁使用以下方式覆盖线上：

- 从本地电脑打包 `backend` 目录上传后临时编译。
- 只执行 `go build -tags embed`，但没有重新构建同一次源码对应的前端资源。
- 从未知源码目录或工作区有未确认改动的目录编译线上二进制。
- 把本地构建产物作为默认生产产物上传上线；除非 VPS 构建链路不可用且已明确确认应急 fallback。
- 不备份当前二进制就直接替换挂载文件。
- 使用 `cp` 直接覆盖仍被容器执行的挂载二进制。
- 重建 Docker 镜像后直接上线，除非已经确认本次改动确实涉及容器环境。

替换必须使用 VPS `/opt/sub2api-src/backend/bin/server` 的新二进制并核对 SHA256，再通过同目录临时文件原子替换，避免 `Text file busy`：

```bash
live=/opt/sub2api-deploy/custom/sub2api-pool-overview
candidate=${live}.new
src=/opt/sub2api-src/backend/bin/server
install -m 0755 "$src" "$candidate"
sha256sum "$src" "$candidate"
timeout 5 "$candidate" --version

ts=$(date +%Y%m%d-%H%M%S)
cp -a "$live" "$live.bak-$ts"
mv -f "$candidate" "$live"

cd /opt/sub2api-deploy
docker compose up -d --force-recreate --no-deps sub2api
```

上线后必须验证：

```bash
cd /opt/sub2api-deploy
docker compose ps
curl -I https://fast.youkeduo.site/health
curl -I https://fast.youkeduo.site/purchase
curl -I https://fast.youkeduo.site/models
docker compose logs --tail=200 sub2api
```

上线验证通过后必须执行清理，避免 VPS 构建缓存、旧镜像和二进制备份持续膨胀：

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
```

清理规则：保留当前运行二进制和最近一次 `.bak-*` 回滚备份；Docker 镜像只保留正在使用的镜像，以及确有回滚价值的一份 `weishaw/sub2api:backup-*` 备份镜像。

还必须人工或接口回归管理端账号页：

- `/admin/accounts` 页面能正常打开。
- `/api/v1/admin/accounts` 不返回 5xx。
- 账号列表字段显示正常，特别是模型能力、状态、可调度状态和错误信息。

更完整的部署说明见 `docs/SOURCE_DEPLOY_CN.md`。

## 文档同步

- 涉及 API、部署流程、运行方式、配置项、数据库结构、业务流程或异常处理策略变化时，必须同步更新 `docs` 或 README 中的对应说明。
- 文档要描述当前系统实际状态，不能写成临时变更记录。
- 如果代码、线上状态和旧文档不一致，先核实真实逻辑，再更新文档。
