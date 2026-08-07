#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

check_application_security_opt() {
  file=$1
  service=$2
  count=$(
    awk -v service="$service" '
      $0 == "  " service ":" {
        in_application = 1
        next
      }
      in_application && $0 ~ /^  [A-Za-z0-9_-]+:$/ {
        in_application = 0
      }
      in_application && $0 == "    security_opt:" {
        in_security_opt = 1
        next
      }
      in_application && in_security_opt && $0 == "      - no-new-privileges:true" {
        count++
      }
      END { print count + 0 }
    ' "$file"
  )

  if [ "$count" -ne 1 ]; then
    printf '%s must enable no-new-privileges exactly once for the sub2api service\n' "$file" >&2
    exit 1
  fi
}

check_application_security_opt deploy/docker-compose.yml sub2api
check_application_security_opt deploy/docker-compose.local.yml sub2api
check_application_security_opt deploy/docker-compose.standalone.yml sub2api
check_application_security_opt deploy/docker-compose.dev.yml backend

printf 'docker compose security test passed\n'
