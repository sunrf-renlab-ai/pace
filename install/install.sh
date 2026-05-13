#!/bin/sh
set -e

REPO="${PACE_REPO:-sunrf-renlab-ai/pace}"
VERSION="${PACE_VERSION:-latest}"
INSTALL_DIR="${PACE_INSTALL_DIR:-/usr/local/bin}"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  arm64|aarch64) ARCH=arm64 ;;
  x86_64|amd64)  ARCH=amd64 ;;
  *) echo "unsupported arch: $ARCH" >&2; exit 1 ;;
esac

if [ "$OS" != "darwin" ] && [ "$OS" != "linux" ]; then
  echo "unsupported OS: $OS" >&2; exit 1
fi

if [ "$VERSION" = "latest" ]; then
  REL_URL="https://api.github.com/repos/${REPO}/releases/latest"
else
  REL_URL="https://api.github.com/repos/${REPO}/releases/tags/${VERSION}"
fi

ASSET=$(curl -sSL "$REL_URL" | grep -o "https://[^\"]*pace-${OS}-${ARCH}\\.tar\\.gz" | head -n1)
if [ -z "$ASSET" ]; then
  echo "no release asset for ${OS}-${ARCH}" >&2; exit 1
fi

TMP=$(mktemp -d)
trap "rm -rf $TMP" EXIT
curl -sSL "$ASSET" -o "$TMP/pace.tar.gz"
tar xzf "$TMP/pace.tar.gz" -C "$TMP"

if [ ! -w "$INSTALL_DIR" ]; then
  INSTALL_DIR="$HOME/.local/bin"
  mkdir -p "$INSTALL_DIR"
fi

mv "$TMP/pace" "$INSTALL_DIR/pace"
mv "$TMP/paced" "$INSTALL_DIR/paced"
chmod +x "$INSTALL_DIR/pace" "$INSTALL_DIR/paced"

if [ "$OS" = "darwin" ]; then
  PLIST="$HOME/Library/LaunchAgents/com.pace.paced.plist"
  cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.pace.paced</string>
  <key>ProgramArguments</key><array><string>${INSTALL_DIR}/paced</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>${HOME}/.config/pace/paced.log</string>
  <key>StandardErrorPath</key><string>${HOME}/.config/pace/paced.log</string>
</dict>
</plist>
EOF
  mkdir -p "$HOME/.config/pace"
  launchctl unload "$PLIST" 2>/dev/null || true
  launchctl load "$PLIST"
else
  UNIT="$HOME/.config/systemd/user/paced.service"
  mkdir -p "$(dirname "$UNIT")"
  cat > "$UNIT" <<EOF
[Unit]
Description=Pace daemon
After=default.target

[Service]
ExecStart=${INSTALL_DIR}/paced
Restart=on-failure

[Install]
WantedBy=default.target
EOF
  systemctl --user daemon-reload
  systemctl --user enable --now paced.service
fi

cat <<EOF

Pace installed to $INSTALL_DIR/pace
Daemon started.

Next:
  1. pace init     # install hooks into ~/.claude/settings.json
  2. pace          # open chat

EOF
