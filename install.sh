#!/usr/bin/env bash
# Install witc from GitHub releases
set -euf -o pipefail

REPO="jomolungmah/witc"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
FORCE="${FORCE:-0}"

# Detect OS and arch
os="$(uname -s)"
arch="$(uname -m)"

case "$os" in
  Linux)   os=linux ;;
  Darwin)  os=darwin ;;
  *)       echo "Unsupported OS: $os" >&2; exit 1 ;;
esac

case "$arch" in
  x86_64)  arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *)       echo "Unsupported arch: $arch" >&2; exit 1 ;;
esac

# Fetch latest release version from GitHub API
version="$(curl -sSfL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | head -1 | sed -E 's/.*"v([0-9]+\.[0-9]+\.[0-9]+)".*/\1/')"

if [ -z "$version" ]; then
  echo "Failed to fetch latest version" >&2
  exit 1
fi

filename="witc-${version}-${os}-${arch}"
if [ "$os" = "windows" ]; then
  archive="${filename}.zip"
else
  archive="${filename}.tar.gz"
fi

url="https://github.com/${REPO}/releases/download/v${version}/${archive}"

# Check if already installed
if [ "$FORCE" = "0" ] && [ -f "${INSTALL_DIR}/witc" ]; then
  current_version="$("${INSTALL_DIR}/witc" --version 2>/dev/null || echo unknown)"
  echo "witc ${current_version} is already installed at ${INSTALL_DIR}/witc"
  echo "Run with FORCE=1 to reinstall"
  exit 0
fi

echo "Downloading witc v${version} (${os}/${arch})..."

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

if curl -sSfL "$url" -o "${tmpdir}/${archive}"; then
  echo "Downloaded successfully"
else
  echo "Failed to download from: $url" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR"

if [ "$os" = "windows" ]; then
  unzip -o "${tmpdir}/${archive}" -d "$tmpdir"
  cp "${tmpdir}/witc.exe" "${INSTALL_DIR}/witc.exe"
else
  tar -xzf "${tmpdir}/${archive}" -C "$tmpdir"
  cp "${tmpdir}/witc" "${INSTALL_DIR}/witc"
  chmod +x "${INSTALL_DIR}/witc"
fi

echo "Installed witc v${version} to ${INSTALL_DIR}/witc"

# Check if INSTALL_DIR is in PATH
if ! echo "$PATH" | grep -q "$INSTALL_DIR"; then
  echo ""
  echo "Add ${INSTALL_DIR} to your PATH:"
  shell="$SHELL"
  case "$(basename "$shell")" in
    zsh)    rcfile="${ZDOTDIR:-$HOME}/.zshrc" ;;
    bash)   rcfile="$HOME/.bashrc" ;;
    fish)   rcfile="$HOME/.config/fish/config.fish" ;;
    *)      rcfile="$HOME/.profile" ;;
  esac
  echo "  echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> $rcfile"
  echo "  source $rcfile"
fi
