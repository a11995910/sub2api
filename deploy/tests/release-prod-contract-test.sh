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

assert_contains deploy/release-prod 'backup_result=/opt/sub2api/state/prod-backup-result.json'
assert_contains deploy/release-prod 'validate-backup-receipt'
assert_contains deploy/release-prod 'wait-container-healthy'
assert_contains deploy/release-prod '        --build-arg GOMAXPROCS=2 \'
assert_contains deploy/Dockerfile 'ARG GOMAXPROCS'
assert_contains deploy/release-prod '"$scripts_dir/update-sub2api-image" "$env_file" "$previous_original_image" prod-abort'
assert_contains deploy/release-prod 'if [[ "$recovery_failed" -eq 0 ]]; then'
assert_not_contains deploy/release-prod 'database_backup='
assert_not_contains deploy/release-prod "'pg_dump -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -Fc'"

if [[ "${RELEASE_PROD_CONTRACT_SCOPE:-all}" == script ]]; then
  printf 'release prod script contract test passed\n'
  exit 0
fi

assert_contains .github/workflows/staging-verify.yml 'deploy/release-gates wait-container-healthy "$container_id" 90 2'
assert_contains .github/workflows/staging-verify.yml 'deploy/release-gates wait-http http://127.0.0.1:18080/health 10 1'
assert_contains .github/workflows/staging-verify.yml '              --build-arg GOMAXPROCS=2 \'
assert_contains .github/workflows/prod-release.yml 'BACKUP_RESULT: /opt/sub2api/state/prod-backup-result.json'
assert_contains .github/workflows/prod-release.yml 'test -f "$BACKUP_RESULT"'

printf 'release prod contract test passed\n'
