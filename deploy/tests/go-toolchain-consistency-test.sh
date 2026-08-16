#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

fail() {
  printf 'go toolchain consistency test failed: %s\n' "$1" >&2
  exit 1
}

assert_line() {
  file=$1
  line=$2
  grep -Fqx "$line" "$file" || fail "$file is missing: $line"
}

assert_count() {
  file=$1
  text=$2
  expected=$3
  actual=$(grep -Fc "$text" "$file" || true)
  [ "$actual" -eq "$expected" ] || fail "$file has $actual occurrences of '$text', expected $expected"
}

go_version=$(awk '$1 == "go" { print $2; exit }' backend/go.mod)
[ -n "$go_version" ] || fail 'backend/go.mod does not declare a Go version'

assert_line Dockerfile "ARG GOLANG_IMAGE=golang:${go_version}-alpine"
assert_line backend/Dockerfile "FROM golang:${go_version}-alpine"
assert_line deploy/Dockerfile "ARG GOLANG_IMAGE=golang:${go_version}-alpine"
assert_line deploy/Dockerfile.dev "ARG GOLANG_IMAGE=golang:${go_version}-alpine"
assert_line deploy/docker-compose.dev.yml "        GOLANG_IMAGE: \${SUB2API_GO_IMAGE:-golang:${go_version}-alpine}"
assert_count .github/workflows/backend-ci.yml "go version | grep -q 'go${go_version}'" 2
assert_count .github/workflows/release.yml "go version | grep -q 'go${go_version}'" 1
assert_count .github/workflows/security-scan.yml "go version | grep -q 'go${go_version}'" 1
assert_count .github/workflows/ai-merge-verify.yml "go version | grep -q 'go${go_version}'" 1
assert_line README.md "[![Go](https://img.shields.io/badge/Go-${go_version}-00ADD8.svg)](https://golang.org/)"
assert_line README_CN.md "[![Go](https://img.shields.io/badge/Go-${go_version}-00ADD8.svg)](https://golang.org/)"
assert_line README_JA.md "[![Go](https://img.shields.io/badge/Go-${go_version}-00ADD8.svg)](https://golang.org/)"
assert_count DEV_GUIDE.md "当前为 **${go_version}**" 1

printf 'go toolchain consistency test passed: %s\n' "$go_version"
