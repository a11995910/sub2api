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
- 日常修改默认直接在 `main` 分支完成，项目不使用长期 `dev` 分支。
- 只有上游同步、隔离 worktree、风险验证或其他确有隔离必要的场景才允许在本地创建临时分支。临时分支必须先完成本地验证，再合并进 `main` 并推送；正式 VPS 不得拉取、检出或构建 `dev`、功能分支、同步分支或其他临时分支。任务结束前必须切回 `main`，删除对应的本地和远端临时分支，并核对当前分支和分支残留。生产环境切换仍需遵守独立的口头确认门禁。
- 提交说明使用中文，格式保持简洁，例如 `docs: 规范源码定制上线流程`。
- `AGENTS.md` 必须纳入版本控制，部署拓扑或发布门禁变化时同步更新。
- 任何生产构建前，本地相关改动必须先完成 Git 提交并推送到远端；严禁用未提交工作区直接构建生产产物。
- 正式 VPS 的 `/opt/sub2api/repo` 只能检出 `main`。构建前必须执行 `git fetch origin`、`git switch main`、`git pull --ff-only origin main`，并核对镜像构建 commit 与本次待发布的 `origin/main` commit 一致。

## 正式 VPS staging 与 prod 操作

- 项目只使用一台正式 VPS：`207.57.145.15`，登录账户 `root`，本机 SSH 别名 `sub2api-new-vps`；不存在独立测试 VPS。
- 预发布验证在正式 VPS 的隔离 staging 中完成。功能代码必须先在本地完成验证、合并并推送到 `main`，staging 只允许拉取和构建 `origin/main`，并使用独立 compose project、运行配置、数据库、Redis、数据目录和 `18080` 端口。
- staging 验证通过后必须报告验证结果、目标 `main` commit 和风险点，并等待用户明确口头命令；prod 只能切换到 staging 已验证的同一个 `main` commit，不得在 staging 验证后再合并代码或更换 commit。
- 正式 VPS 当前为 8GB 内存且无 Swap。Docker 冷缓存构建必须通过 `GOMAXPROCS=2` 限制 Go 编译并行度；仍需在构建前核对磁盘、内存、CPU 余量和当前运行服务，避免触发 OOM 或与线上请求争抢资源。
- 正式 VPS 的 root 密码不得写入本文件、仓库、文档、提交记录或日志；如需密码登录，应使用运行时凭据或本机 Keychain 凭据引用，例如 `sub2api-new-vps-root`，并优先使用 SSH Key 免密登录。
- 国内腾讯云服务器：`118.89.91.26`，账户为 `ubuntu`，仅在用户明确要求相关操作时使用。
- 服务器密码、SSH 私钥、Token、数据库密码、OAuth 密钥和 Cookie 等敏感信息不得写入仓库、文档、提交记录或日志；如需使用，只能通过运行时凭据或环境变量临时注入。

## 生产操作

- 生产操作前必须确认当前运行来源，包括源码目录、Git remote、当前 commit、容器挂载路径和运行中的二进制路径。
- 线上敏感信息不得写入仓库，包括服务器密码、Token、数据库密码、OAuth 密钥和 Cookie。
- 涉及数据库写入、迁移或批量数据修复时，必须先确认表结构、影响范围、备份方式和回滚方式。
- 不确定线上实际状态时，优先通过只读命令核实，例如 `git status --short`、`git remote -v`、`git rev-parse HEAD`、`docker compose ps`、`docker compose logs --tail=200`。

## Sub2API 正式 VPS Git 拉取与镜像化部署规范

正式 VPS `207.57.145.15` 采用“VPS 拉取 Git 源码 -> VPS 本机构建 Docker 镜像 -> staging 验证 -> prod 切换镜像”的部署方式。除非 Docker 构建链路不可用且用户明确同意应急 fallback，否则禁止直接覆盖挂载二进制。

推荐目录结构：

```bash
/opt/sub2api/
  repo/                 # 干净 Git 源码仓库，只用于 fetch / checkout / build
  env/
    staging/.env         # 预发布配置，不进 Git
    prod/.env            # 正式配置，不进 Git
  compose/
    staging/docker-compose.yml
    prod/docker-compose.yml
  data/
    staging/
    prod/
  backups/
  state/
  scripts/
```

正式 VPS 部署硬性要求：

- 本地开发完成后必须先提交并推送到 GitHub；正式 VPS 只从 GitHub 拉取已推送 commit，不接收本地未提交源码或本地构建产物。
- 正式 VPS 源码目录必须保持干净：每次构建前执行 `git status --short`，若存在未确认改动，必须先核实来源，不得直接覆盖。
- `/opt/sub2api/repo` 必须始终检出 `main`，staging 和 prod 都只允许使用 `origin/main`。任何功能分支或同步分支都必须在本地验证并合并、推送到 `main` 后，才允许进入 VPS staging；VPS 上禁止检出或构建其他分支。prod 切换仍必须等待用户明确口头确认。
- 每次构建必须使用 `deploy/Dockerfile` 在正式 VPS 本机构建完整镜像，镜像 tag 必须包含 Git commit，例如 `sub2api:<commit>` 或 `sub2api:staging-<commit>`。
- Docker 构建必须传入可追溯版本信息，至少包含 `COMMIT=$(git rev-parse --short=12 HEAD)` 和 `DATE=$(git show -s --format=%cI HEAD)`。
- 仓库 `deploy/release-staging` 和 `deploy/release-prod` 分别是 `/opt/sub2api/scripts/release-staging` 和 `/opt/sub2api/scripts/release-prod` 的唯一受版本控制来源；安装或升级时必须从已推送的 `origin/main` 复制并设置为 `root:root`、`0700`，不得在 VPS 上直接手写简化发布脚本。
- 异机备份机只承担 prod 数据库归档，归档目录为 `/opt/sub2api-prod-backup/archives`；正式 VPS 不保存 prod 全库 dump，`deploy/release-prod` 也不得在正式 VPS 创建全库 dump。
- prod 发布前必须存在 `/opt/sub2api/state/prod-backup-result.json`，并保持 `root:root`、`0600`。凭证必须由已完成 SHA-256、zstd 和 `pg_restore --list` 校验的异机归档生成，绑定目标完整 commit 与 staging run ID，且校验时不得超过两小时。
- staging 与 prod 切换应用容器后必须先使用 `deploy/release-gates wait-container-healthy` 等待 Docker health 为 `healthy`，再使用 `wait-http` 检查宿主机健康接口；禁止在 `compose up -d` 后立即以单次 `curl` 判定失败。
- prod 发布失败时必须把 `.env` 恢复为发布前记录的原正式镜像 tag，并确认恢复后的容器与 HTTP 均健康；临时 `sub2api:rollback-*` tag 只能在恢复成功后删除，不得让 `.env` 指向已删除的临时 tag。
- staging 和 prod 必须使用独立 compose project、独立 `.env`、独立数据目录和独立端口；不得让测试数据污染正式数据。
- `.env`、数据库密码、JWT、TOTP、OAuth、支付密钥和 Cookie 只允许保存在正式 VPS 的运行时配置目录或凭据管理工具中，不得写入 Git、文档、镜像 tag 或日志。
- 发布前必须记录当前运行镜像 tag，发布后保留至少一个可回滚镜像；回滚优先通过 compose 切回旧镜像 tag 完成。
- 上游同步、staging 和 prod 均由用户人工发起。禁止 AstrBot、飞书机器人、GitHub Actions、自托管 Runner、Webhook、定时任务或其他自动化执行上游合并、VPS 部署或生产切换。
- 仓库不使用 GitHub Actions、PR 或 GitHub Environment 做代码验证和发布；所有检查必须由用户或协作助手在本地显式执行并报告结果。
- 上游同步必须先在本地执行 `git fetch upstream --prune` 并记录目标上游完整 SHA；在本地 `main` 或隔离 worktree 完成 Git 三方合并，逐项处理冲突并运行必要检查。合并结果必须保留双方父提交，禁止 force push。
- 合并完成后必须重新 fetch，确认目标上游 SHA 未变化且 `origin/main` 仍等于合并基线；再由用户以普通 push 推送 `main`。推送后由用户手工 SSH 到正式 VPS 执行 staging，禁止 GitHub 验证、PR、临时同步分支或自动部署。

当前源码远端与上游关系必须保持清晰：

- `origin` 是当前定制仓库 `https://github.com/a11995910/sub2api.git`，本地 `main` 和正式 VPS 都以它的 `main` 为发布来源。
- `upstream` 是官方上游仓库 `https://github.com/Wei-Shaw/sub2api.git`，只用于本地版本对照和人工三方合并，不得让正式 VPS 直接拉取或构建 `upstream/main`。
- 上游同步顺序固定为：本地 fetch -> 固定上游 SHA 和 `origin/main` 基线 -> 人工三方合并 -> 冲突与差异检查 -> 必要测试 -> 生成可追溯 merge commit -> 重新核对远端基线 -> 普通 push `main` -> 手工 SSH staging -> 用户明确确认 -> 手工 prod。
- 本仓库历史中的 `e666c87dc`（同步官方上游主线）和 `13fc3cbf2`（同步上游 v0.1.169）证明历史同步采用合并提交并保留定制；后续人工同步仍必须保留可追溯合并记录、远端基线核对和 staging 门禁。
- `deploy/install.sh`、`deploy/docker-deploy.sh` 和默认 `weishaw/sub2api:latest` 属于官方通用部署链路，不得当作当前定制 VPS 的生产发布入口。

正式 VPS staging 必须手工执行受版本控制的脚本：

```bash
ssh sub2api-new-vps
cd /opt/sub2api/repo
git status --short
git fetch origin
git switch main
git pull --ff-only origin main
expected_commit='填写本地 main 的 git rev-parse HEAD 输出'
test "$(git rev-parse HEAD)" = "$expected_commit"
install -o root -g root -m 0700 deploy/release-staging /opt/sub2api/scripts/release-staging
/opt/sub2api/scripts/release-staging "$expected_commit"
# 记录脚本输出的数字 run_id，后续异机备份凭证和 prod 必须使用同一值。
```

prod 发布必须在用户明确确认后执行，并使用同一 commit、staging run 和两小时内的异机备份凭证。正式 VPS 只调用受版本控制的 root-only 发布脚本，不复制其内部切换逻辑：

```bash
cd /opt/sub2api/repo
git status --short
git fetch origin
git switch main
git pull --ff-only origin main
expected_commit='填写已确认上线的 main commit'
test "$(git rev-parse HEAD)" = "$expected_commit"
commit="$(git rev-parse --short=12 HEAD)"
staging_run_id='填写对应 staging 验证 run ID'
test "$(stat -c '%U:%G %a' /opt/sub2api/state/prod-backup-result.json)" = 'root:root 600'
deploy/release-gates validate-backup-receipt \
  /opt/sub2api/state/prod-backup-result.json "$expected_commit" "$staging_run_id"
install -o root -g root -m 0700 deploy/release-prod /opt/sub2api/scripts/release-prod
/opt/sub2api/scripts/release-prod \
  /opt/sub2api/env/prod/.env \
  "sub2api:prod-$commit" \
  "$expected_commit" \
  "$staging_run_id"
```

正式 VPS `sub2api` 验证通过后，还必须检查 Nginx/Caddy 反代、HTTPS、管理端账号页、`/api/v1/admin/accounts`、`/purchase`、`/models`、容器日志和数据库连接。

## 文档同步

- 涉及 API、部署流程、运行方式、配置项、数据库结构、业务流程或异常处理策略变化时，必须同步更新 `docs` 或 README 中的对应说明。
- 文档要描述当前系统实际状态，不能写成临时变更记录。
- 如果代码、线上状态和旧文档不一致，先核实真实逻辑，再更新文档。
