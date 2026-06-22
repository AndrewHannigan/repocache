#!/usr/bin/env sh
# shed install script
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/AndrewHannigan/shed/main/install.sh | sh
#
# Environment variables:
#   SHED_INSTALL_DIR — install dir (default: /usr/local/bin)
#   SHED_VERSION     — release tag without leading 'v' (default: latest)
#
# Examples:
#   SHED_VERSION=0.0.4 sh install.sh
#   SHED_INSTALL_DIR=$HOME/.local/bin sh install.sh

set -eu

REPO="AndrewHannigan/shed"
BINARY="shed"
INSTALL_DIR="${SHED_INSTALL_DIR:-/usr/local/bin}"
VERSION="${SHED_VERSION:-latest}"

# ── Detect OS ─────────────────────────────────────────────────────────────────
case "$(uname -s)" in
  Linux*)  OS="linux" ;;
  Darwin*) OS="darwin" ;;
  *)
    printf 'error: unsupported OS: %s\n' "$(uname -s)" >&2
    printf '       shed is for Linux and macOS.\n' >&2
    exit 1
    ;;
esac

# ── Detect arch ───────────────────────────────────────────────────────────────
case "$(uname -m)" in
  x86_64|amd64)  ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    printf 'error: unsupported arch: %s\n' "$(uname -m)" >&2
    exit 1
    ;;
esac

# ── Suggest Homebrew on macOS ─────────────────────────────────────────────────
if [ "$OS" = "darwin" ] && command -v brew >/dev/null 2>&1; then
  printf 'note: on macOS, the recommended install is:\n'
  printf '      brew install AndrewHannigan/tap/shed\n'
  printf '      (signed + notarized, auto-updates via `brew upgrade`)\n\n'
  printf 'Continuing with manual install in 3 seconds... (Ctrl-C to abort)\n\n'
  sleep 3
fi

# ── Resolve version ───────────────────────────────────────────────────────────
if [ "$VERSION" = "latest" ]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name":' | head -1 | cut -d '"' -f 4 | sed 's/^v//')
  if [ -z "$VERSION" ]; then
    printf 'error: could not resolve latest version (rate-limited? offline?)\n' >&2
    exit 1
  fi
fi

printf 'Installing %s v%s (%s_%s)...\n' "$BINARY" "$VERSION" "$OS" "$ARCH"

TARBALL="${BINARY}_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/v${VERSION}/${TARBALL}"
CHECKSUMS_URL="https://github.com/${REPO}/releases/download/v${VERSION}/checksums.txt"

# ── Download to temp ──────────────────────────────────────────────────────────
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

printf '  download: %s\n' "$URL"
if ! curl -fsSL "$URL" -o "${TMP_DIR}/${TARBALL}"; then
  printf 'error: download failed. Check that v%s exists at https://github.com/%s/releases\n' "$VERSION" "$REPO" >&2
  exit 1
fi

# ── Verify checksum ───────────────────────────────────────────────────────────
printf '  verify checksum...\n'
curl -fsSL "$CHECKSUMS_URL" -o "${TMP_DIR}/checksums.txt"
EXPECTED=$(grep " ${TARBALL}\$" "${TMP_DIR}/checksums.txt" | cut -d ' ' -f 1)
if [ -z "$EXPECTED" ]; then
  printf 'error: %s not listed in checksums.txt\n' "$TARBALL" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL=$(sha256sum "${TMP_DIR}/${TARBALL}" | cut -d ' ' -f 1)
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL=$(shasum -a 256 "${TMP_DIR}/${TARBALL}" | cut -d ' ' -f 1)
else
  printf 'warning: no sha256 tool available, skipping checksum verification\n' >&2
  ACTUAL="$EXPECTED"
fi
if [ "$ACTUAL" != "$EXPECTED" ]; then
  printf 'error: checksum mismatch\n  expected: %s\n  actual:   %s\n' "$EXPECTED" "$ACTUAL" >&2
  exit 1
fi

# ── Extract + install ─────────────────────────────────────────────────────────
tar -xzf "${TMP_DIR}/${TARBALL}" -C "$TMP_DIR"
if [ ! -f "${TMP_DIR}/${BINARY}" ]; then
  printf 'error: %s not found inside tarball\n' "$BINARY" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR" 2>/dev/null || true
if [ -w "$INSTALL_DIR" ]; then
  install -m 0755 "${TMP_DIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
else
  printf '  install to %s (needs sudo)...\n' "$INSTALL_DIR"
  sudo install -m 0755 "${TMP_DIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
fi

# ── Done ──────────────────────────────────────────────────────────────────────
printf '\n'
printf '✓ %s v%s installed at %s/%s\n' "$BINARY" "$VERSION" "$INSTALL_DIR" "$BINARY"

# Warn if INSTALL_DIR isn't on PATH
case ":$PATH:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    printf '\n'
    printf 'warning: %s is not on your PATH.\n' "$INSTALL_DIR"
    printf '         add it to your shell rc, e.g.:\n'
    printf '           echo '\''export PATH="%s:$PATH"'\'' >> ~/.zshrc\n' "$INSTALL_DIR"
    ;;
esac

printf '\nRun '\''%s init'\'' to get started.\n' "$BINARY"
