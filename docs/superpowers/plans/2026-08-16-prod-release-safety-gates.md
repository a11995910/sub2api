# 生产发布安全门禁实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让生产发布使用异机备份凭证、条件式容器健康等待和可重建的原镜像回滚，消除 VPS 全库 dump 与启动竞态。

**Architecture:** 新增独立可测试的 `deploy/release-gates` 命令，负责校验异机备份凭证、等待 Docker health 和重试宿主机健康接口。`deploy/release-prod` 保留发布编排职责，调用门禁命令并在失败时恢复 `previous_original_image`；staging 工作流复用同一健康等待定义。

**Tech Stack:** Bash、Python 3 标准库、Docker Compose、GitHub Actions、shell contract tests。

---

### Task 1: 用失败测试固定门禁契约

**Files:**
- Create: `deploy/tests/release-gates-test.sh`
- Create: `deploy/tests/release-prod-contract-test.sh`
- Test: `deploy/tests/release-gates-test.sh`
- Test: `deploy/tests/release-prod-contract-test.sh`

- [ ] **Step 1: 创建凭证与健康等待失败测试**

`release-gates-test.sh` 使用临时目录和伪 `docker`/`curl` 命令，至少实现以下断言：

```bash
run_expect_fail "$gate" validate-backup-receipt "$missing" "$commit" "$run_id" "$now"

write_receipt "$receipt" "$commit" "$run_id" "$verified_at"
chmod 0644 "$receipt"
run_expect_fail env BACKUP_RECEIPT_OWNER_UID="$(id -u)" \
  "$gate" validate-backup-receipt "$receipt" "$commit" "$run_id" "$now"

chmod 0600 "$receipt"
BACKUP_RECEIPT_OWNER_UID="$(id -u)" \
  "$gate" validate-backup-receipt "$receipt" "$commit" "$run_id" "$now"

printf '%s\n' starting starting healthy > "$state_file"
PATH="$fake_bin:$PATH" RELEASE_DOCKER_STATE_FILE="$state_file" \
  "$gate" wait-container-healthy container-1 5 0
test "$(cat "$inspect_count_file")" -eq 3

printf '%s\n' starting unhealthy > "$state_file"
run_expect_fail env PATH="$fake_bin:$PATH" RELEASE_DOCKER_STATE_FILE="$state_file" \
  "$gate" wait-container-healthy container-1 5 0
```

- [ ] **Step 2: 创建 `release-prod` 静态契约失败测试**

`release-prod-contract-test.sh` 必须检查：

```bash
assert_contains 'backup_result=/opt/sub2api/state/prod-backup-result.json'
assert_contains 'release-gates validate-backup-receipt'
assert_contains 'release-gates wait-container-healthy'
assert_contains '"$scripts_dir/update-sub2api-image" "$env_file" "$previous_original_image" prod-abort'
assert_not_contains "pg_dump -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -Fc'"
assert_not_contains 'database_backup='
```

- [ ] **Step 3: 运行测试并确认 RED**

Run:

```bash
bash deploy/tests/release-gates-test.sh
bash deploy/tests/release-prod-contract-test.sh
```

Expected: 两个测试均失败；前者提示 `deploy/release-gates` 不存在，后者提示缺少备份凭证/健康等待契约。

- [ ] **Step 4: 提交测试**

```bash
git add deploy/tests/release-gates-test.sh deploy/tests/release-prod-contract-test.sh
git commit -m "test: 固定生产发布安全门禁"
```

### Task 2: 实现可测试的发布门禁命令

**Files:**
- Create: `deploy/release-gates`
- Test: `deploy/tests/release-gates-test.sh`

- [ ] **Step 1: 实现命令分发与凭证校验**

`deploy/release-gates` 使用 `set -Eeuo pipefail`，支持：

```bash
case "${1:-}" in
  validate-backup-receipt) validate_backup_receipt "${@:2}" ;;
  print-backup-receipt) print_backup_receipt "${@:2}" ;;
  wait-container-healthy) wait_container_healthy "${@:2}" ;;
  wait-http) wait_http "${@:2}" ;;
  *) usage >&2; exit 2 ;;
esac
```

凭证校验先用 `stat -c '%u %a'` 验证 owner UID 和 `0600`，再用 Python 标准库验证固定字段、commit/run、两小时有效期、SHA-256、正整数大小与 TOC 数量：

```python
required = {
    "environment", "status", "target_commit", "staging_run_id",
    "backup_host", "archive", "sha256", "size_bytes", "toc_entries",
    "zstd_verified", "pg_restore_list_verified", "verified_at",
}
if set(payload) != required:
    raise SystemExit("backup receipt fields mismatch")
if payload["target_commit"] != target_commit:
    raise SystemExit("backup receipt commit mismatch")
if str(payload["staging_run_id"]) != staging_run_id:
    raise SystemExit("backup receipt staging run mismatch")
if not 0 <= now_epoch - verified_epoch <= 7200:
    raise SystemExit("backup receipt is expired or from the future")
```

- [ ] **Step 2: 实现 Docker health 条件等待**

每轮通过一次 `docker inspect` 输出 `status|health`，`running|healthy` 成功，
`unhealthy/exited/dead/removing` 立即失败，其他状态在 deadline 前继续：

```bash
state="$(docker inspect --format '{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$container_id")"
case "$state" in
  running\|healthy) return 0 ;;
  *\|unhealthy|exited\|*|dead\|*|removing\|*) return 1 ;;
esac
```

- [ ] **Step 3: 实现 HTTP 条件等待**

在 deadline 前每秒执行一次：

```bash
curl --fail --silent --show-error --max-time 5 -o /dev/null "$url"
```

成功立即返回；超时返回非零。

- [ ] **Step 4: 运行测试并确认 GREEN**

Run:

```bash
bash -n deploy/release-gates
bash deploy/tests/release-gates-test.sh
```

Expected: `release gates test passed`。

- [ ] **Step 5: 提交门禁命令**

```bash
git add deploy/release-gates
git commit -m "feat: 增加生产发布运行门禁"
```

### Task 3: 修复 `release-prod` 编排与失败恢复

**Files:**
- Modify: `deploy/release-prod`
- Test: `deploy/tests/release-prod-contract-test.sh`

- [ ] **Step 1: 接入异机备份凭证**

增加：

```bash
backup_result=/opt/sub2api/state/prod-backup-result.json
release_gates="$repo_dir/deploy/release-gates"
test -x "$release_gates"
"$release_gates" validate-backup-receipt \
  "$backup_result" "$target_commit" "$staging_run_id"
```

移除全库 `database_backup` 和对应 `pg_dump`；保留定价与快速策略小型快照。发布记录改写为：

```bash
"$release_gates" print-backup-receipt "$backup_result" >> "$release_record"
```

- [ ] **Step 2: 修复失败恢复引用**

失败恢复必须使用原正式 tag：

```bash
test "$(docker image inspect --format '{{.Id}}' "$previous_original_image")" = "$previous_image_id"
"$scripts_dir/update-sub2api-image" "$env_file" "$previous_original_image" prod-abort
compose_prod up -d --no-deps sub2api
rollback_container_id="$(compose_prod ps -q sub2api)"
"$release_gates" wait-container-healthy "$rollback_container_id" 90 2
"$release_gates" wait-http http://127.0.0.1:8080/health 10 1
```

仅当上述步骤全部成功后删除未记录 rollback tag；恢复失败则保留镜像与现场并返回失败。

- [ ] **Step 3: 在目标切换后等待健康**

将立即 curl 替换为：

```bash
"$release_gates" wait-container-healthy "$container_id" 90 2 || {
  compose_prod logs --tail=200 sub2api >&2
  return 1
}
"$release_gates" wait-http http://127.0.0.1:8080/health 10 1
```

实际实现需保持在顶层脚本可触发 `ERR` trap 的调用上下文，不能在无法 `return` 的顶层使用上述示意块。

- [ ] **Step 4: 运行契约测试并确认 GREEN**

Run:

```bash
bash -n deploy/release-prod
bash deploy/tests/release-prod-contract-test.sh
bash deploy/tests/release-gates-test.sh
```

Expected: 三项全部通过。

- [ ] **Step 5: 提交发布脚本修复**

```bash
git add deploy/release-prod
git commit -m "fix: 修复生产发布健康等待与回滚"
```

### Task 4: 统一 staging/prod 工作流健康定义

**Files:**
- Modify: `.github/workflows/staging-verify.yml`
- Modify: `.github/workflows/prod-release.yml`
- Modify: `deploy/tests/release-prod-contract-test.sh`

- [ ] **Step 1: 扩展失败测试**

断言 staging 工作流调用：

```text
deploy/release-gates wait-container-healthy "$container_id" 90 2
deploy/release-gates wait-http http://127.0.0.1:18080/health 10 1
```

断言 prod 工作流检查 `/opt/sub2api/state/prod-backup-result.json` 存在。

- [ ] **Step 2: 运行测试并确认 RED**

Run: `bash deploy/tests/release-prod-contract-test.sh`

Expected: FAIL，提示工作流尚未使用统一门禁。

- [ ] **Step 3: 修改两个工作流**

staging 的 `compose up -d` 后调用仓库门禁；prod prerequisites 增加备份凭证文件和仓库门禁可执行检查。生产 workflow 仍只调用 root-only `release-prod`，不复制发布逻辑。

- [ ] **Step 4: 运行测试并确认 GREEN**

Run:

```bash
bash deploy/tests/release-prod-contract-test.sh
bash -n deploy/release-gates deploy/release-prod
```

Expected: 全部通过。

- [ ] **Step 5: 提交工作流修改**

```bash
git add .github/workflows/staging-verify.yml .github/workflows/prod-release.yml deploy/tests/release-prod-contract-test.sh
git commit -m "ci: 统一发布健康检查门禁"
```

### Task 5: 同步部署规范与运行文档

**Files:**
- Modify: `AGENTS.md`
- Modify: `docs/SOURCE_DEPLOY_CN.md`

- [ ] **Step 1: 更新硬性约束**

`AGENTS.md` 明确：

```text
prod 发布前必须存在 root:root 0600 的异机备份凭证；凭证绑定目标 commit 与 staging run，且不超过两小时。release-prod 不得在生产 VPS 创建全库 dump。staging/prod 切换后必须等待容器 healthy，再执行宿主机健康检查。失败回滚必须恢复原正式镜像 tag，不得让 .env 指向已删除的临时 tag。
```

- [ ] **Step 2: 更新安装与发布示例**

`docs/SOURCE_DEPLOY_CN.md` 增加 `deploy/release-gates` 安装/执行说明、备份凭证 schema、权限、有效期、健康等待和失败恢复流程；删除“release-prod 创建全库备份”的旧描述和示例字段。

- [ ] **Step 3: 检查文档一致性**

Run:

```bash
rg -n "database_backup|prod-database-before|立即.*health|release-gates|prod-backup-result" AGENTS.md docs/SOURCE_DEPLOY_CN.md deploy/release-prod .github/workflows
```

Expected: 不再把 VPS 全库 dump 描述为正式流程；新门禁在代码、工作流和文档中一致。

- [ ] **Step 4: 提交文档**

```bash
git add AGENTS.md docs/SOURCE_DEPLOY_CN.md
git commit -m "docs: 规范异机备份与发布恢复流程"
```

### Task 6: 完成本地验证、审查并推送

**Files:**
- Verify: all files modified in Tasks 1-5

- [ ] **Step 1: 运行部署脚本测试**

```bash
for test_file in deploy/tests/*-test.sh; do bash "$test_file"; done
```

Expected: 每个脚本退出 0；新测试输出通过信息。

- [ ] **Step 2: 运行语法与 diff 检查**

```bash
bash -n deploy/release-gates deploy/release-prod deploy/tests/release-gates-test.sh deploy/tests/release-prod-contract-test.sh
git diff --check a6064326812539536c8ceb9d9b48f3551565cf9e..HEAD
git status --short
```

Expected: 无语法错误、无空白错误、工作区干净。

- [ ] **Step 3: 审查提交范围并推送**

```bash
git log --oneline a6064326812539536c8ceb9d9b48f3551565cf9e..HEAD
git diff --stat a6064326812539536c8ceb9d9b48f3551565cf9e..HEAD
git push origin main
test "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)"
```

Expected: 仅包含设计、计划、测试、发布脚本、工作流和文档修改。

### Task 7: 重新部署并验证 staging

**Files:**
- Runtime: `/opt/sub2api/repo`
- Runtime: `/opt/sub2api/env/staging/.env`
- Runtime: `/opt/sub2api/state/staging-result.json`

- [ ] **Step 1: 拉取并锁定新 commit**

```bash
cd /opt/sub2api/repo
git status --short
git fetch origin
target_commit="$(git rev-parse origin/main)"
git switch main
git pull --ff-only origin main
test "$(git rev-parse HEAD)" = "$target_commit"
```

- [ ] **Step 2: 构建并部署新 staging 镜像**

使用 `deploy/Dockerfile`、`target_commit` 和 `git show -s --format=%cI "$target_commit"`
构建 `sub2api:staging-$(git rev-parse --short=12 "$target_commit")`，更新 staging `.env`，
叠加基础 Compose 与 staging override 执行 `up -d`。

- [ ] **Step 3: 用新门禁验证 staging**

```bash
container_id="$(compose_staging ps -q sub2api)"
/opt/sub2api/repo/deploy/release-gates wait-container-healthy "$container_id" 90 2
/opt/sub2api/repo/deploy/release-gates wait-http http://127.0.0.1:18080/health 10 1
test "$(docker inspect --format '{{.Config.Image}}' "$container_id")" = "$target_image"
```

同时验证首页、`/purchase`、管理端账号页、错误日志和 prod 健康未受影响。

- [ ] **Step 4: 写入人工 staging 结果**

写入 root-owned `staging-result.json`，字段明确标记 `workflow=manual-staging-verify`，run ID 使用容器创建时间数字，并复核 commit/image/run 一致。

### Task 8: 生成新备份凭证并再次发布 prod

**Files:**
- Runtime: backup host `/opt/sub2api-prod-backup/archives`
- Runtime: prod `/opt/sub2api/state/prod-backup-result.json`
- Runtime: prod `/opt/sub2api/scripts/release-prod`

- [ ] **Step 1: 手工执行最新异机备份**

在备份机启动一次 `sub2api-prod-backup.service`，持续监测生产 `/health` 和负载。完成后独立执行：

```bash
archive="$(find /opt/sub2api-prod-backup/archives -maxdepth 1 -type f \
  -name 'sub2api-prod-*.dump.zst' -printf '%T@|%p\n' \
  | sort -t '|' -k1,1nr | head -n 1 | cut -d '|' -f2-)"
toc="$(mktemp /tmp/sub2api-prod-backup-toc.XXXXXX)"
cd /opt/sub2api-prod-backup/archives
sha256sum -c -- "$(basename "$archive").sha256"
zstd -q -t "$archive"
zstd -q -dc "$archive" \
  | docker run --rm -i postgres:18-alpine pg_restore --list > "$toc"
test "$(awk '!/^;/ && NF {count++} END {print count + 0}' "$toc")" -ge 100
rm -f -- "$toc"
```

要求 SHA-256、zstd 和 TOC（至少 100 项）通过。

- [ ] **Step 2: 写入并校验备份凭证**

凭证绑定最终 target commit 和本次 staging run，权限为 `root:root 0600`。执行：

```bash
/opt/sub2api/repo/deploy/release-gates validate-backup-receipt \
  /opt/sub2api/state/prod-backup-result.json "$target_commit" "$staging_run_id"
```

- [ ] **Step 3: 安装同 commit 发布脚本**

```bash
install -o root -g root -m 0700 deploy/release-prod /opt/sub2api/scripts/release-prod
sha256sum deploy/release-prod /opt/sub2api/scripts/release-prod
```

- [ ] **Step 4: 执行 prod 发布**

```bash
/opt/sub2api/scripts/release-prod \
  /opt/sub2api/env/prod/.env \
  "sub2api:prod-$(git rev-parse --short=12 HEAD)" \
  "$(git rev-parse HEAD)" \
  "$staging_run_id"
```

Expected: 等待新容器 healthy 后输出 `prod 发布成功`，不创建 `prod-database-before-*.dump`。

- [ ] **Step 5: 完成生产验收**

核对实际镜像/version、内外网 `/health`、HTTPS 反代、首页、`/purchase`、管理端账号页、未授权 API 返回、数据库与 Redis 健康、错误日志、旧镜像回滚 tag，并确认生产 VPS 没有新增全库归档。
