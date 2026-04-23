#!/usr/bin/env bash
# Sync Anthropic model prices into internal/metrics/pricing.json from LiteLLM.
#
# Usage:
#   scripts/pricing-sync.sh list      # print current prices
#   scripts/pricing-sync.sh check     # diff local vs LiteLLM; exit 1 if different
#   scripts/pricing-sync.sh sync      # write LiteLLM prices into pricing.json
#
# Source: https://github.com/BerriAI/litellm (model_prices_and_context_window.json)
# Each entry in pricing.json has a `litellm_key` pointing at its counterpart.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PRICING_FILE="$ROOT/internal/metrics/pricing.json"
LITELLM_URL="https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
TOLERANCE="0.0001"

cmd="${1:-check}"

require() { command -v "$1" >/dev/null || { echo "missing dependency: $1" >&2; exit 2; }; }
require jq
require curl

approx_equal() {
  awk -v a="$1" -v b="$2" -v t="$TOLERANCE" \
    'BEGIN { d = (a>b)?(a-b):(b-a); exit (d<t) ? 0 : 1 }'
}

list_prices() {
  jq -r '
    "last_updated: \(.last_updated)",
    "",
    (.models | to_entries[] | "\(.key)
  litellm_key: \(.value.litellm_key // "(none)")
  input:          $\(.value.input_per_mtok)/MTok
  output:         $\(.value.output_per_mtok)/MTok
  cache_read:     $\(.value.cache_read_per_mtok)/MTok
  cache_creation: $\(.value.cache_creation_per_mtok)/MTok
")
  ' "$PRICING_FILE"
}

fetch_litellm() {
  curl -fsSL "$LITELLM_URL"
}

# Given full LiteLLM JSON on stdin and a model key, emit 4 per-MTok numbers:
#   input output cache_read cache_creation
extract_litellm_prices() {
  jq -r --arg key "$1" '
    .[$key] // empty
    | [
        (.input_cost_per_token                // 0) * 1000000,
        (.output_cost_per_token               // 0) * 1000000,
        (.cache_read_input_token_cost         // 0) * 1000000,
        (.cache_creation_input_token_cost     // 0) * 1000000
      ] | @tsv
  '
}

diff_or_sync() {
  local write="$1"   # "1" = write changes; "0" = dry-run
  local litellm
  litellm=$(fetch_litellm)
  local diffs=0
  local tmpfile
  tmpfile=$(mktemp)
  cp "$PRICING_FILE" "$tmpfile"

  local models
  models=$(jq -r '.models | keys[]' "$PRICING_FILE")

  local field_pairs=(
    "input_per_mtok:input"
    "output_per_mtok:output"
    "cache_read_per_mtok:cache_read"
    "cache_creation_per_mtok:cache_creation"
  )

  while IFS= read -r model; do
    local key
    key=$(jq -r --arg m "$model" '.models[$m].litellm_key // empty' "$PRICING_FILE")
    if [[ -z "$key" ]]; then
      echo "[skip] $model: no litellm_key"
      continue
    fi

    local row
    row=$(echo "$litellm" | extract_litellm_prices "$key")
    if [[ -z "$row" ]]; then
      echo "[miss] $model: $key not found in LiteLLM"
      continue
    fi

    IFS=$'\t' read -r in_ out_ cr_ cc_ <<<"$row"
    local -a remote=("$in_" "$out_" "$cr_" "$cc_")

    local i=0
    for pair in "${field_pairs[@]}"; do
      local local_field="${pair%:*}"
      local local_val
      local_val=$(jq -r --arg m "$model" --arg f "$local_field" '.models[$m][$f]' "$PRICING_FILE")
      local remote_val="${remote[$i]}"
      if ! approx_equal "$local_val" "$remote_val"; then
        printf '[diff] %s.%s: local=%s  litellm=%s\n' "$model" "$local_field" "$local_val" "$remote_val"
        diffs=$((diffs + 1))
        if [[ "$write" == "1" ]]; then
          jq --arg m "$model" --arg f "$local_field" --argjson v "$remote_val" \
             '.models[$m][$f] = $v' "$tmpfile" > "$tmpfile.new" && mv "$tmpfile.new" "$tmpfile"
        fi
      fi
      i=$((i + 1))
    done
  done <<<"$models"

  if [[ "$write" == "1" ]] && [[ $diffs -gt 0 ]]; then
    local today
    today=$(date +%Y-%m-%d)
    jq --arg d "$today" '.last_updated = $d' "$tmpfile" > "$tmpfile.new" && mv "$tmpfile.new" "$tmpfile"
    mv "$tmpfile" "$PRICING_FILE"
    echo
    echo "✅ Updated $PRICING_FILE ($diffs change(s))."
    echo "   Review with: git diff -- $PRICING_FILE"
    echo "   Re-run tests: go test ./internal/metrics/..."
  else
    rm -f "$tmpfile"
  fi

  if [[ $diffs -eq 0 ]]; then
    echo "✓ All prices match LiteLLM."
  elif [[ "$write" == "0" ]]; then
    echo
    echo "⚠️  $diffs price(s) differ. Run 'make pricing-sync' to update."
    exit 1
  fi
}

case "$cmd" in
  list)  list_prices ;;
  check) diff_or_sync 0 ;;
  sync)  diff_or_sync 1 ;;
  *)
    echo "Usage: $0 {list|check|sync}" >&2
    exit 2
    ;;
esac
