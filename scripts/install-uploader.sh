#!/usr/bin/env bash
set -euo pipefail

PLIST_NAME="com.claude-code.jsonl-upload"
PLIST_DIR="$HOME/Library/LaunchAgents"
PLIST_PATH="$PLIST_DIR/$PLIST_NAME.plist"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
UPLOAD_SCRIPT="$SCRIPT_DIR/upload-jsonl.sh"

if [ ! -f "$UPLOAD_SCRIPT" ]; then
  echo "Error: upload-jsonl.sh not found at $UPLOAD_SCRIPT"
  exit 1
fi

chmod +x "$UPLOAD_SCRIPT"

mkdir -p "$PLIST_DIR"

cat > "$PLIST_PATH" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>$PLIST_NAME</string>
    <key>ProgramArguments</key>
    <array>
        <string>/bin/bash</string>
        <string>$UPLOAD_SCRIPT</string>
    </array>
    <key>StartInterval</key>
    <integer>3600</integer>
    <key>StandardOutPath</key>
    <string>/tmp/claude-jsonl-upload.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/claude-jsonl-upload.log</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin</string>
    </dict>
</dict>
</plist>
EOF

# Unload first if already loaded
launchctl bootout "gui/$(id -u)/$PLIST_NAME" 2>/dev/null || true

launchctl bootstrap "gui/$(id -u)" "$PLIST_PATH"

echo "Installed and loaded $PLIST_NAME"
echo "  Script: $UPLOAD_SCRIPT"
echo "  Plist:  $PLIST_PATH"
echo "  Log:    /tmp/claude-jsonl-upload.log"
echo "  Interval: every 3600 seconds (1 hour)"
