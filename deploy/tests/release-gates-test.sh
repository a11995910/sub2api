#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
gate="$repo_root/deploy/release-gates"

fail() {
  printf 'release gates test failed: %s\n' "$1" >&2
  exit 1
}

run_expect_fail() {
  if "$@"; then
    fail "command unexpectedly succeeded: $*"
  fi
}

test -x "$gate" || fail 'deploy/release-gates is missing or not executable'

tmp_dir="$(mktemp -d)"
trap 'rm -rf -- "$tmp_dir"' EXIT

commit=0123456789abcdef0123456789abcdef01234567
other_commit=abcdef0123456789abcdef0123456789abcdef01
run_id=20260816132652
now_epoch=1786888800
verified_at=2026-08-16T14:00:00+00:00
expired_at=2026-08-16T11:00:00+00:00
receipt="$tmp_dir/prod-backup-result.json"

write_receipt() {
  local path="$1" target_commit="$2" target_run_id="$3" timestamp="$4" zstd_verified="${5:-true}"
  python3 - "$path" "$target_commit" "$target_run_id" "$timestamp" "$zstd_verified" <<'PY'
import json
import sys

path, commit, run_id, verified_at, zstd_verified = sys.argv[1:]
payload = {
    "environment": "prod-backup",
    "status": "verified",
    "target_commit": commit,
    "staging_run_id": run_id,
    "backup_host": "sub2api-backup",
    "archive": "sub2api-prod-20260816T134952Z.dump.zst",
    "sha256": "a" * 64,
    "size_bytes": 685423367,
    "toc_entries": 1203,
    "zstd_verified": zstd_verified == "true",
    "pg_restore_list_verified": True,
    "verified_at": verified_at,
}
with open(path, "w", encoding="utf-8") as handle:
    json.dump(payload, handle)
    handle.write("\n")
PY
}

run_expect_fail "$gate" validate-backup-receipt "$tmp_dir/missing.json" "$commit" "$run_id" "$now_epoch"

write_receipt "$receipt" "$commit" "$run_id" "$verified_at"
chmod 0644 "$receipt"
run_expect_fail env BACKUP_RECEIPT_OWNER_UID="$(id -u)" \
  "$gate" validate-backup-receipt "$receipt" "$commit" "$run_id" "$now_epoch"

chmod 0600 "$receipt"
BACKUP_RECEIPT_OWNER_UID="$(id -u)" \
  "$gate" validate-backup-receipt "$receipt" "$commit" "$run_id" "$now_epoch"

run_expect_fail env BACKUP_RECEIPT_OWNER_UID="$(id -u)" \
  "$gate" validate-backup-receipt "$receipt" "$other_commit" "$run_id" "$now_epoch"

write_receipt "$receipt" "$commit" "$run_id" "$expired_at"
chmod 0600 "$receipt"
run_expect_fail env BACKUP_RECEIPT_OWNER_UID="$(id -u)" \
  "$gate" validate-backup-receipt "$receipt" "$commit" "$run_id" "$now_epoch"

write_receipt "$receipt" "$commit" "$run_id" "$verified_at" false
chmod 0600 "$receipt"
run_expect_fail env BACKUP_RECEIPT_OWNER_UID="$(id -u)" \
  "$gate" validate-backup-receipt "$receipt" "$commit" "$run_id" "$now_epoch"

write_receipt "$receipt" "$commit" "$run_id" "$verified_at"
chmod 0600 "$receipt"
receipt_output="$tmp_dir/receipt-output.txt"
BACKUP_RECEIPT_OWNER_UID="$(id -u)" \
  "$gate" print-backup-receipt "$receipt" > "$receipt_output"
grep -Fqx 'external_backup_archive=sub2api-prod-20260816T134952Z.dump.zst' "$receipt_output"
grep -Fqx "external_backup_sha256=$(printf 'a%.0s' {1..64})" "$receipt_output"
grep -Fqx 'external_backup_verified_at=2026-08-16T14:00:00+00:00' "$receipt_output"

resource_dir="$tmp_dir/resource"
mkdir -p "$resource_dir"
cat > "$tmp_dir/meminfo" <<'EOF'
MemTotal:       16777216 kB
MemAvailable:   12582912 kB
EOF
printf '0.50 0.40 0.30 2/100 1234\n' > "$tmp_dir/loadavg"
resource_output="$(env \
  SUB2API_BUILD_MIN_DISK_GIB=1 \
  SUB2API_BUILD_MEMINFO_PATH="$tmp_dir/meminfo" \
  SUB2API_BUILD_LOADAVG_PATH="$tmp_dir/loadavg" \
  SUB2API_BUILD_CPU_COUNT=4 \
  "$gate" check-build-resources "$resource_dir")"
resource_gomaxprocs="$(printf '%s\n' "$resource_output" | cut -d '|' -f 1)"
test "$resource_gomaxprocs" -eq 4
run_expect_fail env \
  SUB2API_BUILD_MIN_DISK_GIB=1 \
  SUB2API_BUILD_MIN_AVAILABLE_MEM_GIB=13 \
  SUB2API_BUILD_MEMINFO_PATH="$tmp_dir/meminfo" \
  SUB2API_BUILD_LOADAVG_PATH="$tmp_dir/loadavg" \
  SUB2API_BUILD_CPU_COUNT=4 \
  "$gate" check-build-resources "$resource_dir"
printf '3.50 0.40 0.30 2/100 1234\n' > "$tmp_dir/loadavg-high"
run_expect_fail env \
  SUB2API_BUILD_MIN_DISK_GIB=1 \
  SUB2API_BUILD_MEMINFO_PATH="$tmp_dir/meminfo" \
  SUB2API_BUILD_LOADAVG_PATH="$tmp_dir/loadavg-high" \
  SUB2API_BUILD_CPU_COUNT=4 \
  "$gate" check-build-resources "$resource_dir"
run_expect_fail env \
  SUB2API_BUILD_MIN_DISK_GIB=1 \
  SUB2API_BUILD_MEMINFO_PATH="$tmp_dir/meminfo" \
  SUB2API_BUILD_LOADAVG_PATH="$tmp_dir/loadavg" \
  SUB2API_BUILD_CPU_COUNT=4 \
  SUB2API_BUILD_GOMAXPROCS=5 \
  "$gate" check-build-resources "$resource_dir"
printf 'MemTotal:       8388608 kB\nMemAvailable:   6291456 kB\n' > "$tmp_dir/meminfo-low-total"
run_expect_fail env \
  SUB2API_BUILD_MIN_DISK_GIB=1 \
  SUB2API_BUILD_MEMINFO_PATH="$tmp_dir/meminfo-low-total" \
  SUB2API_BUILD_LOADAVG_PATH="$tmp_dir/loadavg" \
  SUB2API_BUILD_CPU_COUNT=4 \
  "$gate" check-build-resources "$resource_dir"

fake_bin="$tmp_dir/bin"
mkdir -p "$fake_bin"
state_file="$tmp_dir/docker-states"
inspect_count_file="$tmp_dir/docker-count"

cat > "$fake_bin/docker" <<'SH'
#!/bin/sh
set -eu
count=0
if [ -f "$RELEASE_DOCKER_COUNT_FILE" ]; then
  count=$(cat "$RELEASE_DOCKER_COUNT_FILE")
fi
count=$((count + 1))
printf '%s\n' "$count" > "$RELEASE_DOCKER_COUNT_FILE"
state=$(sed -n "${count}p" "$RELEASE_DOCKER_STATE_FILE")
if [ -z "$state" ]; then
  state=$(tail -n 1 "$RELEASE_DOCKER_STATE_FILE")
fi
printf '%s\n' "$state"
SH
chmod +x "$fake_bin/docker"

printf '%s\n' 'running|starting' 'running|starting' 'running|healthy' > "$state_file"
printf '0\n' > "$inspect_count_file"
PATH="$fake_bin:$PATH" \
RELEASE_DOCKER_STATE_FILE="$state_file" \
RELEASE_DOCKER_COUNT_FILE="$inspect_count_file" \
  "$gate" wait-container-healthy container-1 5 0
test "$(cat "$inspect_count_file")" -eq 3

printf '%s\n' 'running|starting' 'running|unhealthy' > "$state_file"
printf '0\n' > "$inspect_count_file"
run_expect_fail env \
  PATH="$fake_bin:$PATH" \
  RELEASE_DOCKER_STATE_FILE="$state_file" \
  RELEASE_DOCKER_COUNT_FILE="$inspect_count_file" \
  "$gate" wait-container-healthy container-1 5 0

curl_count_file="$tmp_dir/curl-count"
cat > "$fake_bin/curl" <<'SH'
#!/bin/sh
set -eu
count=0
if [ -f "$RELEASE_CURL_COUNT_FILE" ]; then
  count=$(cat "$RELEASE_CURL_COUNT_FILE")
fi
count=$((count + 1))
printf '%s\n' "$count" > "$RELEASE_CURL_COUNT_FILE"
[ "$count" -ge "$RELEASE_CURL_SUCCEED_ON" ]
SH
chmod +x "$fake_bin/curl"

printf '0\n' > "$curl_count_file"
PATH="$fake_bin:$PATH" \
RELEASE_CURL_COUNT_FILE="$curl_count_file" \
RELEASE_CURL_SUCCEED_ON=3 \
  "$gate" wait-http http://127.0.0.1:18080/health 5 0
test "$(cat "$curl_count_file")" -eq 3

printf 'release gates test passed\n'
