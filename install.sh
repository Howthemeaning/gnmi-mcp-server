#!/bin/sh
# Install gnmi-mcp-server (macOS / Linux).
#
#   curl -fsSL https://raw.githubusercontent.com/Howthemeaning/gnmi-mcp-server/main/install.sh | sh
#
# Install to a different dir (no sudo):
#   INSTALL_DIR="$HOME/.local/bin" curl -fsSL .../install.sh | sh
set -eu

REPO="Howthemeaning/gnmi-mcp-server"
BIN="gnmi-mcp-server"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# --- detect OS / arch -> release asset name ---
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux | darwin) ;;
  *) echo "unsupported OS: $os (only macOS and Linux are supported)" >&2; exit 1 ;;
esac
arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac
asset="${BIN}_${os}_${arch}.tar.gz"

# --- resolve the latest release tag ---
echo "Finding the latest release of $REPO ..."
tag=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' | head -1 \
  | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')
if [ -z "${tag:-}" ]; then
  echo "could not find a release. Is the repo public and does it have a release yet?" >&2
  exit 1
fi
echo "Latest release: $tag"

base="https://github.com/${REPO}/releases/download/${tag}"

# --- download to a temp dir (auto-removed) ---
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
echo "Downloading ${asset} ..."
curl -fsSL "${base}/${asset}" -o "${tmp}/${asset}"

# --- verify checksum if tools + checksums.txt are available ---
if curl -fsSL "${base}/checksums.txt" -o "${tmp}/checksums.txt" 2>/dev/null; then
  sumcmd=""
  if command -v sha256sum >/dev/null 2>&1; then sumcmd="sha256sum"
  elif command -v shasum >/dev/null 2>&1; then sumcmd="shasum -a 256"; fi
  if [ -n "$sumcmd" ]; then
    expected=$(awk -v f="$asset" '$2==f {print $1}' "${tmp}/checksums.txt")
    actual=$( (cd "$tmp" && $sumcmd "$asset") | awk '{print $1}')
    if [ -n "$expected" ] && [ "$expected" != "$actual" ]; then
      echo "checksum mismatch for $asset" >&2; exit 1
    fi
    [ -n "$expected" ] && echo "Checksum verified."
  fi
fi

# --- extract ---
tar -xzf "${tmp}/${asset}" -C "$tmp"
[ -f "${tmp}/${BIN}" ] || { echo "binary '$BIN' not found in archive" >&2; exit 1; }
chmod +x "${tmp}/${BIN}"

# --- install (sudo only when the target dir isn't writable) ---
echo "Installing to ${INSTALL_DIR} ..."
if [ -d "$INSTALL_DIR" ] && [ -w "$INSTALL_DIR" ]; then
  mv "${tmp}/${BIN}" "${INSTALL_DIR}/${BIN}"
elif command -v sudo >/dev/null 2>&1; then
  sudo mkdir -p "$INSTALL_DIR"
  sudo mv "${tmp}/${BIN}" "${INSTALL_DIR}/${BIN}"
else
  mkdir -p "$INSTALL_DIR"
  mv "${tmp}/${BIN}" "${INSTALL_DIR}/${BIN}"
fi

echo ""
echo "Installed: ${INSTALL_DIR}/${BIN}"
"${INSTALL_DIR}/${BIN}" --version 2>/dev/null || true
echo ""
echo "Next steps:"
echo "  1. Create your config:"
echo "       mkdir -p \"\$HOME/.gnmi-mcp-server\""
echo "       curl -fsSL https://raw.githubusercontent.com/${REPO}/main/gnmi-mcp.example.yaml \\"
echo "         -o \"\$HOME/.gnmi-mcp-server/config.yaml\"    # then edit it for your devices"
echo "  2. Register it with your MCP client (e.g. opencode.json):"
echo '       "gnmi": { "type": "local", "command": ["gnmi-mcp-server"], "enabled": true }'

# warn if the install dir isn't on PATH
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *) echo ""; echo "NOTE: ${INSTALL_DIR} is not on your PATH. Add it, e.g.:"; \
     echo "  export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
esac
