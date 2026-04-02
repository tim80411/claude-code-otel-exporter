#!/usr/bin/env bash
set -euo pipefail

PLIST_NAME="com.claude-code.jsonl-upload"
PLIST_PATH="$HOME/Library/LaunchAgents/$PLIST_NAME.plist"

launchctl bootout "gui/$(id -u)/$PLIST_NAME" 2>/dev/null && \
  echo "Unloaded $PLIST_NAME" || \
  echo "Service was not loaded"

if [ -f "$PLIST_PATH" ]; then
  rm "$PLIST_PATH"
  echo "Removed $PLIST_PATH"
else
  echo "Plist not found at $PLIST_PATH"
fi
