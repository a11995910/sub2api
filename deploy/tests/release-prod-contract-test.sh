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
