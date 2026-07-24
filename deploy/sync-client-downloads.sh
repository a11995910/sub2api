#!/usr/bin/env bash

set -Eeuo pipefail

CC_SWITCH_VERSION="v3.18.0"
CODEX_PRODUCT_ID="9PLM9XGG6VKS"
CODEX_SFS_PRODUCT_ID="fdf7dba1-a7bc-4592-ad8e-04aa3b974675"
CODEX_MAC_URL="https://persistent.oaistatic.com/codex-app-prod/Codex.dmg"
MICROSOFT_UPDATE_CA_URL="http://www.microsoft.com/pkiops/certs/Microsoft%20Update%20Secure%20Server%20CA%202.1.crt"
MICROSOFT_UPDATE_CA_SHA256="6139e2df97dc93bf7e90a303f75b3968fd06c57316b45e94dcff773707cf2754"
MICROSOFT_ROOT_CA_URL="http://www.microsoft.com/pki/certs/MicRooCerAut2011_2011_03_22.crt"
MICROSOFT_ROOT_CA_SHA256="847df6a78497943f27fc72eb93f9a637320a02b561d0a91b09e87a7807ed7c61"

mode="sync"
output_dir="${SUB2API_CLIENT_DOWNLOAD_DIR:-./data/public/downloads/clients}"

usage() {
  cat <<'EOF'
用法：sync-client-downloads.sh [--check] [--output-dir DIR]

  --check           只核实上游安装包，不下载大文件
  --output-dir DIR  写入目录，默认 ./data/public/downloads/clients
EOF
}

while (($# > 0)); do
  case "$1" in
    --check)
      mode="check"
      shift
      ;;
    --output-dir)
      [[ $# -ge 2 ]] || { echo "错误：--output-dir 缺少目录" >&2; exit 2; }
      output_dir="$2"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "错误：未知参数 $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

for command_name in curl jq awk sha256sum openssl od tr head; do
  command -v "$command_name" >/dev/null 2>&1 || {
    echo "错误：缺少命令 $command_name" >&2
    exit 1
  }
done

curl_common=(
  --ipv4
  --fail
  --silent
  --show-error
  --location
  --retry 4
  --retry-delay 2
  --retry-all-errors
  --connect-timeout 20
)

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/sub2api-client-downloads.XXXXXX")"
stage_dir=""
cleanup() {
  rm -rf -- "$work_dir"
  if [[ -n "$stage_dir" && -d "$stage_dir" ]]; then
    rm -rf -- "$stage_dir"
  fi
}
trap cleanup EXIT

head_url() {
  local url="$1"
  curl "${curl_common[@]}" --head --max-time 120 "$url" >/dev/null
}

download_file() {
  local url="$1"
  local destination="$2"
  local expected_sha256="${3:-}"

  echo "下载：$(basename "$destination")"
  curl "${curl_common[@]}" --max-time 7200 --output "$destination" "$url"
  [[ -s "$destination" ]] || { echo "错误：下载文件为空 $destination" >&2; exit 1; }

  if [[ -n "$expected_sha256" ]]; then
    local actual_sha256
    actual_sha256="$(sha256sum "$destination" | awk '{print $1}')"
    [[ "$actual_sha256" == "$expected_sha256" ]] || {
      echo "错误：SHA256 校验失败 $(basename "$destination")" >&2
      exit 1
    }
  fi
}

validate_microsoft_package_url() {
  local url="$1"
  case "$url" in
    http://*.dl.delivery.mp.microsoft.com/filestreamingservice/files/*) ;;
    *)
      echo "错误：Windows Codex 安装包不是 Microsoft Delivery 官方地址" >&2
      exit 1
      ;;
  esac
}

prepare_microsoft_sfs_ca_bundle() {
  local update_ca_der="$work_dir/microsoft-update-ca.crt"
  local update_ca_pem="$work_dir/microsoft-update-ca.pem"
  local root_ca_der="$work_dir/microsoft-root-ca.crt"
  local root_ca_pem="$work_dir/microsoft-root-ca.pem"
  microsoft_ca_bundle="$work_dir/microsoft-sfs-ca-bundle.pem"

  download_file "$MICROSOFT_UPDATE_CA_URL" "$update_ca_der" "$MICROSOFT_UPDATE_CA_SHA256"
  download_file "$MICROSOFT_ROOT_CA_URL" "$root_ca_der" "$MICROSOFT_ROOT_CA_SHA256"
  openssl x509 -inform DER -in "$update_ca_der" -out "$update_ca_pem"
  openssl x509 -inform DER -in "$root_ca_der" -out "$root_ca_pem"
  cat "$root_ca_pem" "$update_ca_pem" > "$microsoft_ca_bundle"
}

base64_sha256_to_hex() {
  printf '%s' "$1" | openssl base64 -d -A | od -An -tx1 | tr -d ' \n'
}

resolve_codex_windows_packages() {
  local version_metadata="$work_dir/codex-sfs-version.json"
  local download_metadata="$work_dir/codex-sfs-downloads.json"
  local sfs_base="https://storeapps.api.cdp.microsoft.com/api/v2/contents/storeapps/namespaces/default/names/${CODEX_SFS_PRODUCT_ID}/versions"

  curl "${curl_common[@]}" --cacert "$microsoft_ca_bundle" --max-time 120 \
    --request POST \
    --header 'Content-Type: application/json' \
    --data '{"TargetingAttributes":{}}' \
    --output "$version_metadata" \
    "${sfs_base}/latest?action=select"

  codex_sfs_version="$(jq -r '.ContentId.Version // empty' "$version_metadata")"
  [[ "$codex_sfs_version" =~ ^[0-9]+(\.[0-9]+)+$ ]] || {
    echo "错误：Microsoft SFS 返回了无效的 Codex 版本" >&2
    exit 1
  }
  [[ "$(jq -r '.ContentId.Name // empty' "$version_metadata")" == "$CODEX_SFS_PRODUCT_ID" ]] || {
    echo "错误：Microsoft SFS 返回了错误的产品" >&2
    exit 1
  }

  curl "${curl_common[@]}" --cacert "$microsoft_ca_bundle" --max-time 120 \
    --request POST \
    --output "$download_metadata" \
    "${sfs_base}/${codex_sfs_version}/files?action=GenerateDownloadInfo"

  codex_windows_x64_url="$(jq -r '
    .[]
    | select(.FileMoniker | test("^OpenAI\\.Codex_[0-9.]+_x64__2p2nqsd0c76g0$"))
    | select(.FileId | endswith(".msix"))
    | .Url
  ' "$download_metadata" | head -n 1)"
  codex_windows_arm64_url="$(jq -r '
    .[]
    | select(.FileMoniker | test("^OpenAI\\.Codex_[0-9.]+_arm64__2p2nqsd0c76g0$"))
    | select(.FileId | endswith(".msix"))
    | .Url
  ' "$download_metadata" | head -n 1)"
  codex_windows_x64_sha256="$(base64_sha256_to_hex "$(jq -r '
    .[] | select(.FileMoniker | test("_x64__2p2nqsd0c76g0$")) | .Hashes.Sha256
  ' "$download_metadata" | head -n 1)")"
  codex_windows_arm64_sha256="$(base64_sha256_to_hex "$(jq -r '
    .[] | select(.FileMoniker | test("_arm64__2p2nqsd0c76g0$")) | .Hashes.Sha256
  ' "$download_metadata" | head -n 1)")"

  validate_microsoft_package_url "$codex_windows_x64_url"
  validate_microsoft_package_url "$codex_windows_arm64_url"
  [[ "$codex_windows_x64_sha256" =~ ^[0-9a-f]{64}$ ]] || { echo "错误：x64 MSIX 缺少有效 SHA256" >&2; exit 1; }
  [[ "$codex_windows_arm64_sha256" =~ ^[0-9a-f]{64}$ ]] || { echo "错误：ARM64 MSIX 缺少有效 SHA256" >&2; exit 1; }
}

verify_msix_layout() {
  local package_path="$1"
  command -v unzip >/dev/null 2>&1 || return 0

  local entries
  entries="$(unzip -Z1 "$package_path")"
  grep -qx 'AppxManifest.xml' <<<"$entries" || { echo "错误：MSIX 缺少 AppxManifest.xml" >&2; exit 1; }
  grep -qx 'AppxSignature.p7x' <<<"$entries" || { echo "错误：MSIX 缺少 AppxSignature.p7x" >&2; exit 1; }
}

cc_release_json="$work_dir/cc-switch-release.json"
curl "${curl_common[@]}" --max-time 120 \
  --output "$cc_release_json" \
  "https://api.github.com/repos/farion1231/cc-switch/releases/tags/${CC_SWITCH_VERSION}"

declare -a cc_assets=(
  "CC-Switch-${CC_SWITCH_VERSION}-Windows.msi|CC-Switch-Windows-x64.msi|c4a6eaf763269396f90a81377381e91c8341538b51376912c81bab73e844612d"
  "CC-Switch-${CC_SWITCH_VERSION}-Windows-Portable.zip|CC-Switch-Windows-x64-Portable.zip|9b180af9817d60b2be2a69f5a71ec7383fce67404521a38046c58d53aa3bbc01"
  "CC-Switch-${CC_SWITCH_VERSION}-Windows-arm64.msi|CC-Switch-Windows-arm64.msi|c8abd0b39cd6fe0d14637c1d1c66f39b79066aef3edf76bb7a5b3b5b57241e09"
  "CC-Switch-${CC_SWITCH_VERSION}-macOS.dmg|CC-Switch-macOS.dmg|26f0b81c4d0d4bc39ace97359e753f323a66f3e5c2a6bf2f341d5a053fb93881"
  "CC-Switch-${CC_SWITCH_VERSION}-Linux-x86_64.AppImage|CC-Switch-Linux-x86_64.AppImage|ba1a4009eec156ecb6b2a7b6ce638bc56682953e32a58211a3f08f37edec9634"
  "CC-Switch-${CC_SWITCH_VERSION}-Linux-arm64.AppImage|CC-Switch-Linux-arm64.AppImage|dd8bec9d99233ce9250b79ed1bd6b0420dcbe0b8157e8a01447a324602b957ec"
)

prepare_microsoft_sfs_ca_bundle
resolve_codex_windows_packages

echo "已解析 Codex Windows x64 / ARM64 官方 MSIX。"
echo "已锁定 CC-Switch ${CC_SWITCH_VERSION} 官方发布资产。"

if [[ "$mode" == "check" ]]; then
  head_url "$codex_windows_x64_url"
  head_url "$codex_windows_arm64_url"
  head_url "$CODEX_MAC_URL"

  for asset_spec in "${cc_assets[@]}"; do
    IFS='|' read -r source_name _ expected_sha256 <<<"$asset_spec"
    api_digest="$(jq -r --arg name "$source_name" '.assets[] | select(.name == $name) | .digest' "$cc_release_json")"
    asset_url="$(jq -r --arg name "$source_name" '.assets[] | select(.name == $name) | .browser_download_url' "$cc_release_json")"
    [[ "$api_digest" == "sha256:${expected_sha256}" ]] || { echo "错误：发布摘要不匹配 $source_name" >&2; exit 1; }
    [[ "$asset_url" == https://github.com/farion1231/cc-switch/releases/download/* ]] || { echo "错误：发布地址异常 $source_name" >&2; exit 1; }
  done

  echo "检查通过：Codex 下载地址可用，CC-Switch 发布资产与 SHA256 摘要完整。"
  exit 0
fi

mkdir -p "$output_dir"
stage_dir="$(mktemp -d "${output_dir%/}/.sync-client-downloads.XXXXXX")"
mkdir -p "$stage_dir/codex" "$stage_dir/cc-switch"

download_file "$codex_windows_x64_url" "$stage_dir/codex/Codex-Windows-x64.msix" "$codex_windows_x64_sha256"
verify_msix_layout "$stage_dir/codex/Codex-Windows-x64.msix"
download_file "$codex_windows_arm64_url" "$stage_dir/codex/Codex-Windows-arm64.msix" "$codex_windows_arm64_sha256"
verify_msix_layout "$stage_dir/codex/Codex-Windows-arm64.msix"
download_file "$CODEX_MAC_URL" "$stage_dir/codex/Codex-macOS.dmg"

for asset_spec in "${cc_assets[@]}"; do
  IFS='|' read -r source_name target_name expected_sha256 <<<"$asset_spec"
  api_digest="$(jq -r --arg name "$source_name" '.assets[] | select(.name == $name) | .digest' "$cc_release_json")"
  asset_url="$(jq -r --arg name "$source_name" '.assets[] | select(.name == $name) | .browser_download_url' "$cc_release_json")"
  [[ "$api_digest" == "sha256:${expected_sha256}" ]] || { echo "错误：发布摘要不匹配 $source_name" >&2; exit 1; }
  [[ "$asset_url" == https://github.com/farion1231/cc-switch/releases/download/* ]] || { echo "错误：发布地址异常 $source_name" >&2; exit 1; }
  download_file "$asset_url" "$stage_dir/cc-switch/$target_name" "$expected_sha256"
done

(
  cd "$stage_dir"
  sha256sum codex/* cc-switch/* > SHA256SUMS.txt
)

jq -n \
  --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg codex_product_id "$CODEX_PRODUCT_ID" \
  --arg codex_sfs_version "$codex_sfs_version" \
  --arg cc_switch_version "$CC_SWITCH_VERSION" \
  '{generated_at: $generated_at, codex_product_id: $codex_product_id, codex_sfs_version: $codex_sfs_version, cc_switch_version: $cc_switch_version}' \
  > "$stage_dir/manifest.json"

mkdir -p "$output_dir/codex" "$output_dir/cc-switch"
for package_path in "$stage_dir"/codex/*; do
  mv -f -- "$package_path" "$output_dir/codex/"
done
for package_path in "$stage_dir"/cc-switch/*; do
  mv -f -- "$package_path" "$output_dir/cc-switch/"
done
mv -f -- "$stage_dir/SHA256SUMS.txt" "$output_dir/SHA256SUMS.txt"
mv -f -- "$stage_dir/manifest.json" "$output_dir/manifest.json"

echo "同步完成：$output_dir"
