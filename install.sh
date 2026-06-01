#!/bin/sh
set -e

REPO="germanamz/tusk"
INSTALL_DIR="${INSTALL_DIR:-${HOME}/.local/bin}"

# Man pages go next to the binary by convention (.../bin -> .../share/man).
# Override with MAN_DIR; fall back to ~/.local/share/man for non-standard dirs.
case "$INSTALL_DIR" in
  */bin) MAN_DIR="${MAN_DIR:-${INSTALL_DIR%/bin}/share/man}" ;;
  *)     MAN_DIR="${MAN_DIR:-${HOME}/.local/share/man}" ;;
esac

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  linux)  OS="linux" ;;
  darwin) OS="darwin" ;;
  *)      echo "Unsupported OS: $OS" >&2; exit 1 ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)             echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

# Get latest version (override with TUSK_VERSION=vX.Y.Z)
VERSION="${TUSK_VERSION:-}"
if [ -z "$VERSION" ]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
fi
if [ -z "$VERSION" ]; then
  echo "Failed to fetch latest version" >&2
  exit 1
fi

ARCHIVE="tusk_${VERSION#v}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"

echo "Installing tusk ${VERSION} (${OS}/${ARCH})..."

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

curl -fsSL "$URL" -o "${TMPDIR}/${ARCHIVE}"
tar -xzf "${TMPDIR}/${ARCHIVE}" -C "$TMPDIR"

mkdir -p "$INSTALL_DIR"
mv "${TMPDIR}/tusk" "${INSTALL_DIR}/tusk"

echo "tusk ${VERSION} installed to ${INSTALL_DIR}/tusk"

# Man pages are best-effort: a read-only MAN_DIR (e.g. /usr/local without sudo)
# must not fail the binary install that already succeeded.
if ls "${TMPDIR}/man/"*.1 >/dev/null 2>&1; then
  if mkdir -p "${MAN_DIR}/man1" 2>/dev/null && cp "${TMPDIR}/man/"*.1 "${MAN_DIR}/man1/" 2>/dev/null; then
    echo "man pages installed to ${MAN_DIR}/man1"
  else
    echo "Note: could not write man pages to ${MAN_DIR}/man1 — skipped (set MAN_DIR to override)." >&2
  fi
fi
echo
case ":$PATH:" in
  *:"$INSTALL_DIR":*) ;;
  *) echo "Note: ${INSTALL_DIR} is not on your PATH. Add it to your shell profile to run 'tusk' directly." ;;
esac
