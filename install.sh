#!/usr/bin/env bash
# SafeCut CLI installer.
#
# Downloads the latest (or pinned) GoReleaser archive from GitHub, verifies
# the checksum, and installs the `safecut` binary to $INSTALL_DIR.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Rafaelhdsg/safecut/main/install.sh | bash
#
# Environment variables:
#   INSTALL_DIR     install prefix (default: /usr/local/bin)
#   SAFECUT_VERSION   pin a specific version (default: latest release)
#   SAFECUT_SKIP_VERIFY  set to 1 to skip checksum verification (not recommended)
set -euo pipefail

REPO="Rafaelhdsg/safecut"
BINARY="safecut"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
PIN_VERSION="${SAFECUT_VERSION:-}"
SKIP_VERIFY="${SAFECUT_SKIP_VERIFY:-0}"

# TMP_DIR must be declared at the top-level so the EXIT trap (registered
# after mktemp) can reference it without tripping `set -u`. Bash traps
# fire on shell exit, outside any function scope, so a `local` inside
# main() would be unset by the time the trap runs.
TMP_DIR=""
cleanup() {
  [[ -n "${TMP_DIR:-}" && -d "${TMP_DIR}" ]] && rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

log()  { printf '  %s\n' "$*"; }
err()  { printf '  ERROR: %s\n' "$*" >&2; }
die()  { err "$*"; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

detect_os() {
  case "$(uname -s)" in
    Linux*)  echo "linux" ;;
    Darwin*) echo "darwin" ;;
    *)       die "unsupported OS: $(uname -s). Supported: Linux, Darwin." ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)  echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)             die "unsupported arch: $(uname -m). Supported: amd64, arm64." ;;
  esac
}

resolve_version() {
  if [[ -n "$PIN_VERSION" ]]; then
    printf '%s' "${PIN_VERSION#v}"
    return
  fi
  local api_url tag
  api_url="https://api.github.com/repos/${REPO}/releases/latest"
  tag=$(curl -fsSL "$api_url" | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/' | head -n1)
  [[ -n "$tag" ]] || die "could not determine latest release (is the repo public?)"
  printf '%s' "$tag"
}

verify_checksum() {
  local tmp archive version os arch
  tmp="$1"; archive="$2"; version="$3"; os="$4"; arch="$5"
  if [[ "$SKIP_VERIFY" == "1" ]]; then
    log "WARNING: checksum verification skipped via SAFECUT_SKIP_VERIFY=1"
    return 0
  fi
  require_cmd sha256sum
  local cksum_url cksum_file expected got
  cksum_url="https://github.com/${REPO}/releases/download/v${version}/checksums.txt"
  cksum_file="${tmp}/checksums.txt"
  if ! curl -fsSL "$cksum_url" -o "$cksum_file"; then
    die "could not download checksums.txt from ${cksum_url}"
  fi
  expected=$(grep " ${BINARY}_${version}_${os}_${arch}.tar.gz\$" "$cksum_file" | awk '{print $1}')
  [[ -n "$expected" ]] || die "checksum entry for ${BINARY}_${version}_${os}_${arch}.tar.gz not found in checksums.txt"
  got=$(sha256sum "$archive" | awk '{print $1}')
  [[ "$expected" == "$got" ]] || die "checksum mismatch: expected ${expected}, got ${got}"
  log "✓ checksum verified"
}

main() {
  require_cmd curl
  require_cmd tar

  local os arch version url archive
  os=$(detect_os)
  arch=$(detect_arch)

  log "Detected platform: ${os}/${arch}"
  version=$(resolve_version)
  log "Installing SafeCut v${version}…"

  url="https://github.com/${REPO}/releases/download/v${version}/${BINARY}_${version}_${os}_${arch}.tar.gz"
  TMP_DIR=$(mktemp -d)
  archive="${TMP_DIR}/${BINARY}.tar.gz"

  if ! curl -fsSL "$url" -o "$archive"; then
    err "failed to download ${url}"
    err "check https://github.com/${REPO}/releases for the correct asset name for your OS/arch."
    exit 1
  fi

  verify_checksum "$TMP_DIR" "$archive" "$version" "$os" "$arch"

  tar -xzf "$archive" -C "$TMP_DIR"
  [[ -f "${TMP_DIR}/${BINARY}" ]] || die "archive did not contain the expected binary at /${BINARY}"

  if [[ -w "$INSTALL_DIR" ]]; then
    install -m 0755 "${TMP_DIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
  else
    log "Elevating with sudo to write to ${INSTALL_DIR}"
    sudo install -m 0755 "${TMP_DIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
  fi

  log ""
  log "✓ Installed SafeCut v${version} to ${INSTALL_DIR}/${BINARY}"
  if command -v "${BINARY}" >/dev/null 2>&1; then
    "${BINARY}" version 2>/dev/null | sed 's/^/    /' || true
  else
    log "    (note: ${INSTALL_DIR} is not on your PATH — add it to run \`${BINARY}\`)"
  fi
  log ""
  log "Get started:"
  log "    ${BINARY} quick-scan"
  log ""
}

main "$@"
