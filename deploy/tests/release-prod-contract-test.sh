#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
cd "$repo_root"

fail() {
  printf 'release prod contract test failed: %s\n' "$1" >&2
  exit 1
}

assert_contains() {
  local file="$1" text="$2"
  grep -Fq -- "$text" "$file" || fail "$file is missing: $text"
}

assert_not_contains() {
  local file="$1" text="$2"
  if grep -Fq -- "$text" "$file"; then
    fail "$file still contains: $text"
  fi
}

assert_not_contains deploy/release-prod 'validate-backup-receipt'
assert_contains deploy/release-prod 'version_has_capability "$target_version_output" account_stats_video_pricing_per_second'
assert_contains deploy/release-prod 'if ! version_supports_account_stats_video "$previous_version_output" "$previous_commit"; then'

# 执行实际能力判定函数，覆盖已审查旧镜像、显式能力和未知版本拒绝。
eval "$(sed -n '/^version_has_capability() {/,/^}/p' deploy/release-prod)"
eval "$(sed -n '/^version_supports_account_stats_video() {/,/^}/p' deploy/release-prod)"
version_supports_account_stats_video 'Sub2API 0.2.1 (capabilities: explicit_video_pricing_per_second)' \
  94cb824216f568f31404cca403c5b817074b91ab || fail '已核实的旧生产镜像应兼容视频账号成本'
version_supports_account_stats_video 'Sub2API 0.2.3 (capabilities: explicit_video_pricing_per_second,account_stats_video_pricing_per_second)' \
  unknown || fail '显式视频账号成本能力应被识别'
if version_supports_account_stats_video 'Sub2API 9.9.9 (capabilities: explicit_video_pricing_per_second)' unknown; then
  fail '主渠道视频能力不能代替账号成本能力'
fi
if version_supports_account_stats_video 'Sub2API 0.2.3 (capabilities: account_stats_video_pricing_per_second_extra)' unknown; then
  fail '能力名必须完整匹配'
fi
assert_contains deploy/release-prod '"$release_gates" wait-container-healthy "$rollback_container_id" 300 2'
assert_contains deploy/release-prod '"$release_gates" wait-container-healthy "$container_id" 300 2'
assert_contains deploy/release-prod 'build_resources="$("$release_gates" check-build-resources "$repo_dir")"'
assert_contains deploy/release-prod '        --build-arg GOMAXPROCS="$build_gomaxprocs" \'
assert_contains deploy/Dockerfile 'ARG GOMAXPROCS=2'
assert_contains deploy/Dockerfile 'GOMAXPROCS="${GOMAXPROCS}" CGO_ENABLED=0 GOOS=linux go build \'
assert_contains deploy/release-prod '"$scripts_dir/update-sub2api-image" "$env_file" "$previous_original_image" prod-abort'
assert_contains deploy/release-prod 'if [[ "$recovery_failed" -eq 0 ]]; then'
assert_not_contains deploy/release-prod 'database_backup='
assert_not_contains deploy/release-prod "'pg_dump -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -Fc'"
assert_contains deploy/release-prod 'schema_compat="$repo_dir/deploy/release-schema-compat"'
assert_contains deploy/release-prod '"$schema_compat" snapshot "$env_file" "$schema_snapshot"'
assert_contains deploy/release-prod '[[ "$schema_snapshot_ready" -eq 0 ]] || "$schema_compat" restore "$env_file" "$schema_snapshot"'
assert_contains deploy/release-prod "printf 'schema_241_snapshot=%s\\n'"

# 运行真实回滚函数，确认恢复列和清理缓存完成后才重启旧镜像。
compat_test_dir="$(mktemp -d)"
trap 'rm -rf -- "$compat_test_dir"' EXIT
mkdir "$compat_test_dir/scripts"
cat > "$compat_test_dir/action" <<'SH'
#!/usr/bin/env bash
printf '%s %s\n' "$(basename "$0")" "$1" >> "$COMPAT_TEST_LOG"
if [[ "$(basename "$0")" = schema-compat && "${COMPAT_TEST_RESTORE_FAIL:-0}" = 1 ]]; then
  exit 1
fi
SH
chmod +x "$compat_test_dir/action"
cp "$compat_test_dir/action" "$compat_test_dir/scripts/update-sub2api-image"
cp "$compat_test_dir/action" "$compat_test_dir/schema-compat"
cp "$compat_test_dir/action" "$compat_test_dir/release-gates"

test_restore_sequence() (
  export COMPAT_TEST_LOG="$compat_test_dir/events-$1"
  export COMPAT_TEST_RESTORE_FAIL="$2"
  schema_snapshot_ready="$3"
  release_started=1
  rollback_tag_created=1
  previous_original_image=original-image
  previous_image_id=image-id
  previous_image=rollback-image
  env_file=prod.env
  schema_snapshot=private-snapshot.json
  scripts_dir="$compat_test_dir/scripts"
  schema_compat="$compat_test_dir/schema-compat"
  release_gates="$compat_test_dir/release-gates"
  release_record="$compat_test_dir/record-$1"
  touch "$release_record"
  docker() { if [[ "$1 $2" = 'image inspect' ]]; then printf 'image-id\n'; fi; }
  compose_prod() {
    printf 'compose %s\n' "$1" >> "$COMPAT_TEST_LOG"
    if [[ "$1" = ps ]]; then printf 'old-container\n'; fi
  }
  eval "$(sed -n '/^restore_previous_release() {/,/^}/p' deploy/release-prod)"
  restore_previous_release 7
)
for scenario in normal failed no-snapshot; do
  restore_fail=0
  snapshot_ready=1
  [[ "$scenario" != failed ]] || restore_fail=1
  [[ "$scenario" != no-snapshot ]] || snapshot_ready=0
  if test_restore_sequence "$scenario" "$restore_fail" "$snapshot_ready"; then
    fail '回滚函数必须保留原始失败退出码'
  else
    test "$?" -eq 7 || fail '回滚函数丢失原始退出码'
  fi
done
python3 - "$compat_test_dir" <<'PY'
from pathlib import Path
import sys

directory = Path(sys.argv[1])
events = (directory / 'events-normal').read_text().splitlines()
assert events.index('compose stop') < events.index('schema-compat restore') < events.index('compose up')
assert events.index('compose up') < events.index('release-gates wait-container-healthy') < events.index('release-gates wait-http')
failed = (directory / 'events-failed').read_text().splitlines()
assert 'schema-compat restore' in failed and 'compose up' not in failed
assert (directory / 'record-failed').exists()
without_snapshot = (directory / 'events-no-snapshot').read_text().splitlines()
assert 'schema-compat restore' not in without_snapshot and 'compose up' in without_snapshot
assert not (directory / 'record-normal').exists()
PY

if [[ "${RELEASE_PROD_CONTRACT_SCOPE:-all}" == script ]]; then
  printf 'release prod script contract test passed\n'
  exit 0
fi

assert_contains deploy/release-staging 'result_status=failed'
assert_contains deploy/release-staging '"workflow": "manual"'
assert_contains deploy/release-staging 'staging_result="$state_dir/staging-result.json"'
assert_contains deploy/release-staging 'build_resources="$("$release_gates" check-build-resources "$repo_dir")"'
assert_contains deploy/release-staging '  --build-arg GOMAXPROCS="$build_gomaxprocs" \'
assert_contains deploy/release-staging '"$release_gates" wait-container-healthy "$container_id" 300 2'
assert_contains deploy/release-staging '--bootstrap-without-prod'
assert_contains deploy/release-staging '"bootstrap_without_prod": bootstrap_raw == "1"'
assert_contains deploy/release-staging '[[ ! -e "$prod_env_file" && ! -L "$prod_env_file" ]]'
assert_contains deploy/release-staging '[[ ! -e "$prod_override" && ! -L "$prod_override" ]]'
assert_contains deploy/release-staging 'test -z "$(docker ps -aq --filter label=com.docker.compose.project=sub2api-prod)"'
assert_contains deploy/release-staging 'test -z "$(docker network ls -q --filter label=com.docker.compose.project=sub2api-prod)"'
assert_contains deploy/release-staging 'test -z "$(docker volume ls -q --filter label=com.docker.compose.project=sub2api-prod)"'
assert_contains deploy/release-staging 'test ! -L "$prod_data_dir"'
assert_contains deploy/release-staging 'test -z "$(find "$prod_data_dir" -type f -print -quit)"'
assert_contains deploy/release-staging 'test "$(docker inspect --format '\''{{.State.Status}}'\'' "$prod_id")" = running'
assert_contains deploy/release-staging 'test "$(docker inspect --format '\''{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}'\'' "$prod_id")" = healthy'
assert_contains deploy/docker-compose.yml 'start_period: 180s'
assert_contains deploy/docker-compose.local.yml 'start_period: 180s'
assert_contains deploy/docker-compose.standalone.yml 'start_period: 180s'
assert_contains deploy/release-staging '"$release_gates" wait-http http://127.0.0.1:18080/health 10 1'

if find .github/workflows -type f -print -quit 2>/dev/null | grep -q .; then
  fail '.github/workflows must not contain automation files'
fi

printf 'release prod contract test passed\n'
