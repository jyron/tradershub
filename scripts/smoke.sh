#!/usr/bin/env bash
#
# scripts/smoke.sh — quick end-to-end sanity check against a running server.
#
# Requires .dev_bot.json (produced by seed_dev.sh). Exits non-zero on the
# first failure so it's safe to wire into `make smoke` and CI later.

set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:3000}"
STATE_FILE=".dev_bot.json"

green() { printf "\033[32m%s\033[0m\n" "$*"; }
red()   { printf "\033[31m%s\033[0m\n" "$*"; }

if [ ! -f "$STATE_FILE" ]; then
  red "no $STATE_FILE — run 'make seed' first"
  exit 1
fi

BOT_ID=$(jq -r '.bot_id' "$STATE_FILE")
API_KEY=$(jq -r '.api_key' "$STATE_FILE")
RIVAL_BOT_ID=$(jq -r '.rival_bot_id // empty' "$STATE_FILE")
SEASON_ID=$(jq -r '.season_id' "$STATE_FILE")

check() {
  local name="$1"; shift
  local expect="$1"; shift
  local code
  code=$(curl -s -o /tmp/smoke_body -w "%{http_code}" "$@")
  if [ "$code" = "$expect" ]; then
    green "✓ $name ($code)"
  else
    red "✗ $name — expected $expect, got $code: $(cat /tmp/smoke_body)"
    exit 1
  fi
}

check "bot detail"          200 "$BASE_URL/api/bots/$BOT_ID"
check "list seasons"        200 "$BASE_URL/api/seasons"
check "season detail"       200 "$BASE_URL/api/seasons/$SEASON_ID"
check "main portfolio"      200 -H "X-API-Key: $API_KEY" "$BASE_URL/api/portfolio"
check "season portfolio"    200 -H "X-API-Key: $API_KEY" "$BASE_URL/api/portfolio?season_id=$SEASON_ID"
check "season leaderboard"  200 "$BASE_URL/api/seasons/$SEASON_ID/leaderboard"
check "AAPL quote"          200 "$BASE_URL/api/market/quote/AAPL"

# vs endpoint: positive (two distinct bots, main account) + negative paths.
if [ -n "$RIVAL_BOT_ID" ]; then
  check "vs main"            200 "$BASE_URL/api/vs?a=$BOT_ID&b=$RIVAL_BOT_ID"
  check "vs self-vs-self"    400 "$BASE_URL/api/vs?a=$BOT_ID&b=$BOT_ID"
  check "vs missing param"   400 "$BASE_URL/api/vs?a=$BOT_ID"
  check "vs unknown bot"     404 "$BASE_URL/api/vs?a=00000000-0000-0000-0000-000000000000&b=$RIVAL_BOT_ID"
fi

green ""
green "All smoke checks passed."
