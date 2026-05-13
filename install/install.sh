#!/bin/sh
set -e

REPO="${MENTOR_REPO:-sunrf-renlab-ai/mentor}"
VERSION="${MENTOR_VERSION:-latest}"
INSTALL_DIR="${MENTOR_INSTALL_DIR:-/usr/local/bin}"

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

ASSET=$(curl -sSL "$REL_URL" | grep -o "https://[^\"]*mentor-${OS}-${ARCH}\\.tar\\.gz" | head -n1)
if [ -z "$ASSET" ]; then
  echo "no release asset for ${OS}-${ARCH}" >&2; exit 1
fi

TMP=$(mktemp -d)
trap "rm -rf $TMP" EXIT
curl -sSL "$ASSET" -o "$TMP/mentor.tar.gz"
tar xzf "$TMP/mentor.tar.gz" -C "$TMP"

if [ ! -w "$INSTALL_DIR" ]; then
  INSTALL_DIR="$HOME/.local/bin"
  mkdir -p "$INSTALL_DIR"
fi

mv "$TMP/mentor" "$INSTALL_DIR/mentor"
mv "$TMP/mentord" "$INSTALL_DIR/mentord"
chmod +x "$INSTALL_DIR/mentor" "$INSTALL_DIR/mentord"

if [ "$OS" = "darwin" ]; then
  PLIST="$HOME/Library/LaunchAgents/com.mentor.mentord.plist"
  cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.mentor.mentord</string>
  <key>ProgramArguments</key><array><string>${INSTALL_DIR}/mentord</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>${HOME}/.config/mentor/mentord.log</string>
  <key>StandardErrorPath</key><string>${HOME}/.config/mentor/mentord.log</string>
</dict>
</plist>
EOF
  mkdir -p "$HOME/.config/mentor"
  launchctl unload "$PLIST" 2>/dev/null || true
  launchctl load "$PLIST"
else
  UNIT="$HOME/.config/systemd/user/mentord.service"
  mkdir -p "$(dirname "$UNIT")"
  cat > "$UNIT" <<EOF
[Unit]
Description=Mentor daemon
After=default.target

[Service]
ExecStart=${INSTALL_DIR}/mentord
Restart=on-failure

[Install]
WantedBy=default.target
EOF
  systemctl --user daemon-reload
  systemctl --user enable --now mentord.service
fi

cat <<EOF

Mentor installed to $INSTALL_DIR/mentor
Daemon started.

Next:
  1. mentor init     # install hooks into ~/.claude/settings.json
  2. mentor          # open chat

EOF
