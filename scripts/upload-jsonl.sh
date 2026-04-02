#!/usr/bin/env bash
set -euo pipefail

SOURCE_DIR="$HOME/.claude/projects"
BUCKET="s3://claude-code-jsonl/projects/"
PROFILE="oci"
ENDPOINT="https://axhmpfnlwpld.compat.objectstorage.ap-singapore-1.oraclecloud.com"

echo "[$(date -Iseconds)] Starting JSONL sync to S3..."

aws s3 sync "$SOURCE_DIR" "$BUCKET" \
  --profile "$PROFILE" \
  --endpoint-url "$ENDPOINT" \
  --exclude "*" \
  --include "*.jsonl" \
  --exclude "*/memory/*"

echo "[$(date -Iseconds)] Sync complete."
