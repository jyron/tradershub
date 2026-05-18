#!/usr/bin/env bash
#
# scripts/seed_dev.sh — bootstrap a fresh local dev environment.
#
# Idempotent: re-running reuses the bot/season recorded in .dev_bot.json if
# they still exist on the server. Wipe .dev_bot.json (or run `make reset`)
# to force a clean start.
#
# Produces:
#   - A claimed bot with an API key, ready to trade on the main account
#   - A season ("Dev Season"), force-started so it accepts trades immediately
#   - The bot enrolled in that season
#
# Requires the server to be running on $BASE_URL with ADMIN_SECRET set.

set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:3000}"
ADMIN_SECRET="${ADMIN_SECRET:-dev}"
STATE_FILE=".dev_bot.json"

green() { printf "\033[32m%s\033[0m\n" "$*"; }
red()   { printf "\033[31m%s\033[0m\n" "$*"; }
info()  { printf "→ %s\n" "$*"; }

require_jq() {
  if ! command -v jq >/dev/null 2>&1; then
    red "jq is required (brew install jq)"; exit 1
  fi
}
require_jq

# curl_retry — retries on transient 5xx (e.g. AssetSync briefly locks the
# local SQLite DB during the first ~6s after startup). Forwards stdout, so
# callers can pipe to jq as usual.
curl_retry() {
  local out body code attempt
  out=$(mktemp)
  for attempt in 1 2 3 4 5 6 7 8; do
    code=$(curl -s -o "$out" -w "%{http_code}" "$@") || true
    if [ "${code:0:1}" = "2" ]; then
      cat "$out"; rm -f "$out"; return 0
    fi
    if [ "${code:0:1}" = "4" ]; then
      # Non-retryable client error: print body to stderr, propagate exit code
      cat "$out" >&2; rm -f "$out"; return 22
    fi
    sleep 0.75
  done
  cat "$out" >&2; rm -f "$out"; return 22
}

# --- 0. Wait for the server to be reachable -----------------------------------
info "checking server at $BASE_URL"
for i in 1 2 3 4 5 6 7 8 9 10; do
  if curl -fsS "$BASE_URL/api/seasons" -o /dev/null 2>/dev/null; then
    break
  fi
  if [ "$i" = 10 ]; then
    red "server not reachable at $BASE_URL — run 'make dev' in another shell first"
    exit 1
  fi
  sleep 0.5
done
green "✓ server reachable"

# --- 1. Bot ------------------------------------------------------------------
BOT_ID=""
API_KEY=""
if [ -f "$STATE_FILE" ]; then
  BOT_ID=$(jq -r '.bot_id // empty' "$STATE_FILE")
  API_KEY=$(jq -r '.api_key // empty' "$STATE_FILE")
  if [ -n "$BOT_ID" ] && curl -fsS "$BASE_URL/api/bots/$BOT_ID" -o /dev/null 2>/dev/null; then
    info "reusing bot $BOT_ID from $STATE_FILE"
  else
    BOT_ID=""; API_KEY=""
  fi
fi

register_bot() {
  # register_bot <name-prefix> -> echoes "bot_id|api_key"
  local prefix="$1"
  local reg
  reg=$(curl_retry -X POST "$BASE_URL/api/bots/register" \
    -H "Content-Type: application/json" \
    -d "{
      \"name\": \"${prefix}-$(date +%s%N | tail -c 7)\",
      \"description\": \"Local dev bot — seeded by scripts/seed_dev.sh\",
      \"creator_email\": \"dev@local\",
      \"is_test\": true
    }")
  local id key
  id=$(echo "$reg" | jq -r '.bot_id')
  key=$(echo "$reg" | jq -r '.api_key')
  if [ -z "$id" ] || [ "$id" = "null" ]; then
    red "failed to register bot: $reg" >&2; return 1
  fi
  curl -fsS -X POST "$BASE_URL/api/claim/$id" -H "Content-Type: application/json" -d '{}' >/dev/null
  echo "$id|$key"
}

if [ -z "$BOT_ID" ]; then
  info "registering new dev bot"
  pair=$(register_bot "DevBot") || exit 1
  BOT_ID=${pair%|*}
  API_KEY=${pair#*|}
  green "✓ bot registered & claimed: $BOT_ID"
fi

# A second "rival" bot lets the smoke test exercise /api/vs and gives the
# Head-to-Head page two real bots to play with out of the box. Not enrolled
# in the season — vs covers main accounts.
RIVAL_BOT_ID=""
RIVAL_API_KEY=""
if [ -f "$STATE_FILE" ]; then
  RIVAL_BOT_ID=$(jq -r '.rival_bot_id // empty' "$STATE_FILE")
  RIVAL_API_KEY=$(jq -r '.rival_api_key // empty' "$STATE_FILE")
  if [ -n "$RIVAL_BOT_ID" ] && curl -fsS "$BASE_URL/api/bots/$RIVAL_BOT_ID" -o /dev/null 2>/dev/null; then
    info "reusing rival bot $RIVAL_BOT_ID"
  else
    RIVAL_BOT_ID=""; RIVAL_API_KEY=""
  fi
fi
if [ -z "$RIVAL_BOT_ID" ]; then
  info "registering rival dev bot"
  pair=$(register_bot "RivalBot") || exit 1
  RIVAL_BOT_ID=${pair%|*}
  RIVAL_API_KEY=${pair#*|}
  green "✓ rival registered & claimed: $RIVAL_BOT_ID"
fi

# --- 2. Season ---------------------------------------------------------------
# Order matters: enroll only accepts pending seasons, so create → enroll →
# start. If we're reusing a pre-existing active season we skip the start
# step but still attempt enrollment (the server will 409 if it's too late,
# which we surface clearly).
SEASON_ID=""
SEASON_STATUS=""
if [ -f "$STATE_FILE" ]; then
  SEASON_ID=$(jq -r '.season_id // empty' "$STATE_FILE")
  if [ -n "$SEASON_ID" ]; then
    if curl -fsS "$BASE_URL/api/seasons/$SEASON_ID" -o /tmp/seed_season.json 2>/dev/null; then
      SEASON_STATUS=$(jq -r '.status // empty' /tmp/seed_season.json)
      info "reusing season $SEASON_ID (status=$SEASON_STATUS)"
    else
      SEASON_ID=""
    fi
  fi
fi

if [ -z "$SEASON_ID" ]; then
  info "creating dev season"
  STARTS_AT=$(date -u -v-1H '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date -u -d '1 hour ago' '+%Y-%m-%dT%H:%M:%SZ')
  ENDS_AT=$(date -u -v+30d '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date -u -d '+30 days' '+%Y-%m-%dT%H:%M:%SZ')
  SLUG="dev-$(date +%s)"
  CREATE=$(curl_retry -X POST "$BASE_URL/api/admin/seasons" \
    -H "Content-Type: application/json" \
    -H "X-Admin-Secret: $ADMIN_SECRET" \
    -d "{
      \"name\": \"Dev Season\",
      \"slug\": \"$SLUG\",
      \"starts_at\": \"$STARTS_AT\",
      \"ends_at\": \"$ENDS_AT\",
      \"starting_balance\": 100000,
      \"auto_enroll\": false
    }")
  SEASON_ID=$(echo "$CREATE" | jq -r '.id // .ID // empty')
  if [ -z "$SEASON_ID" ] || [ "$SEASON_ID" = "null" ]; then
    red "failed to create season (is ADMIN_SECRET set on the server?): $CREATE"; exit 1
  fi
  SEASON_STATUS="pending"
  green "✓ season created: $SEASON_ID"
fi

# --- 3. Enrollment (must happen while season is pending) ---------------------
if [ "$SEASON_STATUS" = "pending" ]; then
  info "enrolling bot in season"
  ENROLL_HTTP=$(curl -s -o /tmp/seed_enroll.json -w "%{http_code}" \
    -X POST "$BASE_URL/api/seasons/$SEASON_ID/enroll" \
    -H "Content-Type: application/json" \
    -H "X-API-Key: $API_KEY" \
    -d '{}')
  case "$ENROLL_HTTP" in
    20*) green "✓ enrolled" ;;
    409) info "already enrolled" ;;
    *)   red "enroll failed ($ENROLL_HTTP): $(cat /tmp/seed_enroll.json)"; exit 1 ;;
  esac
else
  info "season is $SEASON_STATUS, skipping enroll step"
fi

# --- 4. Force-start (after enrollment, so this dev bot is in the roster) -----
if [ "$SEASON_STATUS" = "pending" ]; then
  info "force-starting season"
  curl -fsS -X POST "$BASE_URL/api/admin/seasons/$SEASON_ID/start" \
    -H "X-Admin-Secret: $ADMIN_SECRET" >/dev/null
  green "✓ season active"
fi

# --- 5. Persist state --------------------------------------------------------
jq -n \
  --arg bot_id "$BOT_ID" \
  --arg api_key "$API_KEY" \
  --arg rival_bot_id "$RIVAL_BOT_ID" \
  --arg rival_api_key "$RIVAL_API_KEY" \
  --arg season_id "$SEASON_ID" \
  --arg base_url "$BASE_URL" \
  '{bot_id: $bot_id, api_key: $api_key, rival_bot_id: $rival_bot_id, rival_api_key: $rival_api_key, season_id: $season_id, base_url: $base_url}' \
  > "$STATE_FILE"

echo
green "Dev environment ready."
echo "  BOT_ID       = $BOT_ID"
echo "  API_KEY      = $API_KEY"
echo "  RIVAL_BOT_ID = $RIVAL_BOT_ID"
echo "  SEASON_ID    = $SEASON_ID"
echo "  state        = $STATE_FILE"
echo
echo "Try:"
echo "  curl -H \"X-API-Key: \$API_KEY\" $BASE_URL/api/portfolio"
echo "  curl -H \"X-API-Key: \$API_KEY\" \"$BASE_URL/api/portfolio?season_id=$SEASON_ID\""
echo "  open \"$BASE_URL/vs.html?a=$BOT_ID&b=$RIVAL_BOT_ID\""
