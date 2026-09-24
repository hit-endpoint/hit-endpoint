#!/bin/sh
# Hit Endpoint installer script
# Usage: curl -fsSL https://raw.githubusercontent.com/hit-endpoint/hit-endpoint/main/install.sh | sh

set -e

REPO="hit-endpoint/hit-endpoint"
GITHUB_URL="https://github.com/${REPO}"

# Detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "${OS}" in
  darwin) OS="darwin" ;;
  linux) OS="linux" ;;
  *)
    echo "Error: Unsupported operating system '${OS}'. Please download pre-built binaries from:"
    echo "  ${GITHUB_URL}/releases"
    exit 1
    ;;
esac

# Detect Architecture
ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "Error: Unsupported architecture '${ARCH}'. Please download pre-built binaries from:"
    echo "  ${GITHUB_URL}/releases"
    exit 1
    ;;
esac

# Determine Version
VERSION="${HIT_VERSION:-}"
if [ -z "${VERSION}" ]; then
  echo "🔍 Looking up latest release for ${REPO}..."
  LATEST_JSON="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null || true)"
  if [ -n "${LATEST_JSON}" ]; then
    VERSION="$(echo "${LATEST_JSON}" | grep '"tag_name":' | head -n 1 | cut -d '"' -f 4)"
  fi
  # Fallback if API rate-limited or no releases yet
  if [ -z "${VERSION}" ]; then
    VERSION="v0.1.0"
  fi
fi

# Ensure version has 'v' prefix for release download
TAG="${VERSION}"
if [ "${TAG#v}" = "${TAG}" ]; then
  TAG="v${TAG}"
fi

FILE_NAME="hit-${OS}-${ARCH}.tar.gz"
DOWNLOAD_URL="${GITHUB_URL}/releases/download/${TAG}/${FILE_NAME}"

echo "📦 Downloading Hit Endpoint ${TAG} for ${OS}/${ARCH}..."
TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t 'hit-install')"
trap 'rm -rf "${TMP_DIR}"' EXIT INT TERM

if ! curl -fsSL "${DOWNLOAD_URL}" -o "${TMP_DIR}/${FILE_NAME}"; then
  echo "Error: Failed to download release from ${DOWNLOAD_URL}."
  echo "Check available releases at: ${GITHUB_URL}/releases"
  exit 1
fi

tar -xzf "${TMP_DIR}/${FILE_NAME}" -C "${TMP_DIR}"

if [ ! -f "${TMP_DIR}/hit" ]; then
  echo "Error: Binary 'hit' not found in downloaded archive."
  exit 1
fi

chmod +x "${TMP_DIR}/hit"

# Determine Install Directory
INSTALL_DIR="/usr/local/bin"
USE_SUDO=0

if [ ! -w "${INSTALL_DIR}" ]; then
  if command -v sudo >/dev/null 2>&1 && [ -t 0 ]; then
    USE_SUDO=1
  else
    INSTALL_DIR="${HOME}/.local/bin"
    mkdir -p "${INSTALL_DIR}"
  fi
fi

echo "🚀 Installing binary to ${INSTALL_DIR}/hit..."
if [ "${USE_SUDO}" -eq 1 ]; then
  sudo cp "${TMP_DIR}/hit" "${INSTALL_DIR}/hit"
  sudo chmod +x "${INSTALL_DIR}/hit"
else
  cp "${TMP_DIR}/hit" "${INSTALL_DIR}/hit"
  chmod +x "${INSTALL_DIR}/hit"
fi

# On macOS, codesign ad-hoc if codesign command exists
if [ "${OS}" = "darwin" ] && command -v codesign >/dev/null 2>&1; then
  codesign -s - -f "${INSTALL_DIR}/hit" 2>/dev/null || true
fi

echo "✅ Hit Endpoint installed successfully!"

if [ ":${PATH}:" != *":${INSTALL_DIR}:"* ]; then
  echo ""
  echo "⚠️  Note: '${INSTALL_DIR}' is not in your PATH."
  echo "Add it to your shell profile (~/.zshrc or ~/.bashrc):"
  echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
  echo ""
fi

# Verification
if command -v hit >/dev/null 2>&1; then
  hit --version
elif [ -x "${INSTALL_DIR}/hit" ]; then
  "${INSTALL_DIR}/hit" --version
fi

echo ""
echo "Try running:"
echo "  hit https://httpbin.org/get"
echo "  hit --help"
