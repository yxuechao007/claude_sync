#!/usr/bin/env sh
set -euf

(set -o pipefail) 2>/dev/null && set -o pipefail

REPO="yxuechao007/claude_sync"
BIN_NAME="claude_sync"
VERSION="latest"
INSTALL_DIR="/usr/local/bin"

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required" >&2
  exit 1
fi

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux*) os="linux" ;;
  darwin*) os="darwin" ;;
  msys*|mingw*|cygwin*) os="windows" ;;
  *)
    echo "unsupported OS: $os" >&2
    exit 1
    ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  armv7l|armv7) arch="armv7" ;;
  i386|i686) arch="386" ;;
  *)
    echo "unsupported arch: $arch" >&2
    exit 1
    ;;
esac

if [ "$VERSION" = "latest" ]; then
  api_url="https://api.github.com/repos/${REPO}/releases/latest"
else
  api_url="https://api.github.com/repos/${REPO}/releases/tags/${VERSION}"
fi

release_json="$(curl -fsSL "$api_url")"

pattern1="${BIN_NAME}_${os}_${arch}"
pattern2="${BIN_NAME}-${os}-${arch}"

urls="$(printf '%s\n' "$release_json" | grep -E '"browser_download_url":' | sed -E 's/.*"browser_download_url": "([^"]+)".*/\1/')"

asset_url="$(printf '%s\n' "$urls" | grep -E "$pattern1|$pattern2" | grep -E '\.tar\.gz$' | head -n1 || true)"
if [ -z "${asset_url:-}" ]; then
  asset_url="$(printf '%s\n' "$urls" | grep -E "$pattern1|$pattern2" | head -n1 || true)"
fi

if [ -z "${asset_url:-}" ]; then
  echo "no release asset found for ${os}/${arch}" >&2
  exit 1
fi

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmpdir"
}
trap cleanup EXIT INT TERM

archive_path="${tmpdir}/asset"
curl -fsSL "$asset_url" -o "$archive_path"

bin_path=""
case "$asset_url" in
  *.tar.gz)
    tar -xzf "$archive_path" -C "$tmpdir"
    if [ -f "${tmpdir}/${BIN_NAME}" ]; then
      bin_path="${tmpdir}/${BIN_NAME}"
    else
      bin_path="$(find "$tmpdir" -type f -name "$BIN_NAME" | head -n1 || true)"
    fi
    ;;
  *)
    bin_path="$archive_path"
    ;;
esac

if [ -z "${bin_path:-}" ] || [ ! -f "$bin_path" ]; then
  echo "downloaded asset does not contain ${BIN_NAME}" >&2
  exit 1
fi

if [ ! -d "$INSTALL_DIR" ]; then
  if [ -w "$(dirname "$INSTALL_DIR")" ]; then
    mkdir -p "$INSTALL_DIR"
  elif command -v sudo >/dev/null 2>&1; then
    sudo mkdir -p "$INSTALL_DIR"
  else
    echo "cannot create ${INSTALL_DIR} (no sudo)" >&2
    exit 1
  fi
fi

if [ -w "$INSTALL_DIR" ]; then
  install -m 0755 "$bin_path" "${INSTALL_DIR}/${BIN_NAME}"
else
  if command -v sudo >/dev/null 2>&1; then
    sudo install -m 0755 "$bin_path" "${INSTALL_DIR}/${BIN_NAME}"
  else
    echo "no write permission for ${INSTALL_DIR} (and sudo not available)" >&2
    exit 1
  fi
fi

echo "installed ${BIN_NAME} to ${INSTALL_DIR}/${BIN_NAME}"
