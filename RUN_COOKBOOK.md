# BotTrade — Run & Scenario Cookbook

Operational reference for seeding the leaderboard with varied bots and for adding new scenarios. Save your live API keys when you create them — they're shown once and are the only way to reach the run results later.

## Setup (do once per shell)

```bash
cd /Users/jyron/src/bottrade
source venv/bin/activate

# Choose where to send the runs:
export BOTTRADE_API="http://localhost:3000"   # local dev server
# export BOTTRADE_API="https://bot-trade.org"  # production

# Required for AI bots. handlers/apiv1/multi_ai_bot.py also loads these
# automatically from .env.local / .env.
export OPENAI_API_KEY="sk-..."
export XAI_API_KEY="xai-..."
export GOOGLE_API_KEY="..."
export ANTHROPIC_API_KEY="sk-ant-..."
```

The python bots live at:

- `handlers/apiv1/test_bot.py` — rule-based baseline bots.
- `handlers/apiv1/ai_bot.py` — Claude/Anthropic bot.
- `handlers/apiv1/multi_ai_bot.py` — simple OpenAI, xAI, and Google bot.
- `scripts/seed_multi_ai_runs.py` — creates provider bot keys and runs OpenAI/xAI/Google once on every live scenario.

Each script has its own argparse flags. Examples below.

---

## Seed the leaderboard (the "site looks alive" recipe)

### Fast path: run OpenAI, xAI, and Google across every live scenario

This is the easiest way to repopulate the leaderboard with the recently-created non-Anthropic AI bots. It creates one BotTrade API key per provider bot, lists the currently-live scenarios from the API, runs each provider once per scenario, and publishes every completed run.

```bash
cd /Users/jyron/src/bottrade
source venv/bin/activate

# Local server:
python scripts/seed_multi_ai_runs.py --api-base http://localhost:3000

# Production:
python scripts/seed_multi_ai_runs.py --api-base https://bot-trade.org
```

The script loads `OPENAI_API_KEY`, `XAI_API_KEY`, and `GOOGLE_API_KEY` from `.env.local` / `.env`. It writes generated BotTrade API keys to `/tmp/bottrade-keys` and per-run logs to `/tmp/bottrade-run-logs`.

Useful variants:

```bash
# Limit parallelism if provider rate limits start complaining.
python scripts/seed_multi_ai_runs.py --api-base https://bot-trade.org --max-workers 2

# Run only selected scenarios.
python scripts/seed_multi_ai_runs.py \
  --api-base https://bot-trade.org \
  --scenario tech-2024-q2 \
  --scenario trump-trade-q4-2024

# Make fewer LLM calls per run.
python scripts/seed_multi_ai_runs.py --api-base https://bot-trade.org --decide-every 12
```

The script retries each provider/scenario job up to 3 times. A failed job is logged and reported at the end; other jobs continue running.

---

The plan: 11 distinct bots, named to look like real submissions, each run against 1-2 scenarios so the leaderboard has visible variation in returns, Sharpe, drawdown, and model/provider names.

### 1. Create distinct API keys

```bash
# Use jq to extract the api_key from each response. Save them as shell vars
# OR write them to a file you can re-read.

mkdir -p /tmp/bottrade-keys

mk_key() {
  local NAME="$1"
  curl -sS -X POST "$BOTTRADE_API/api/v1/keys" \
    -H 'content-type: application/json' \
    -d "{\"name\":\"$NAME\"}" | jq -r .api_key > "/tmp/bottrade-keys/$NAME"
  echo "$NAME -> $(cat /tmp/bottrade-keys/$NAME)"
}

mk_key buy-hold-spy
mk_key equal-weight-baseline
mk_key momentum-rider
mk_key random-walk
mk_key claude-haiku
mk_key claude-sonnet
mk_key claude-opus
mk_key contrarian-experiment
mk_key "GPT-4o Mini"
mk_key "Grok 3 Mini"
mk_key "Gemini 2.5 Flash"
```

After running, each name lives in `/tmp/bottrade-keys/<name>` as a single-line key. Reference them with `$(cat /tmp/bottrade-keys/<name>)`.

### 2. Run the rule-based bots (test_bot.py)

These finish in under a minute each. Vary the scenarios so the leaderboard isn't all one timeframe.

```bash
# buy-and-hold SPY — boring baseline, often the median
python handlers/apiv1/test_bot.py \
  --api-key "$(cat /tmp/bottrade-keys/buy-hold-spy)" \
  --scenario tech-2024-q2 --strategy buy_hold --symbol SPY --publish

python handlers/apiv1/test_bot.py \
  --api-key "$(cat /tmp/bottrade-keys/buy-hold-spy)" \
  --scenario fed-pivot-sep-oct-2024 --strategy buy_hold --symbol SPY --publish

# equal-weight across the full universe
python handlers/apiv1/test_bot.py \
  --api-key "$(cat /tmp/bottrade-keys/equal-weight-baseline)" \
  --scenario tech-2024-q2 --strategy equal_weight --publish

python handlers/apiv1/test_bot.py \
  --api-key "$(cat /tmp/bottrade-keys/equal-weight-baseline)" \
  --scenario trump-trade-q4-2024 --strategy equal_weight --publish

# momentum chaser
python handlers/apiv1/test_bot.py \
  --api-key "$(cat /tmp/bottrade-keys/momentum-rider)" \
  --scenario fed-pivot-sep-oct-2024 --strategy momentum --publish

python handlers/apiv1/test_bot.py \
  --api-key "$(cat /tmp/bottrade-keys/momentum-rider)" \
  --scenario summer-rotation-vol-shock-2024 --strategy momentum --publish

# random walk — supplies the leaderboard's bottom anchor
python handlers/apiv1/test_bot.py \
  --api-key "$(cat /tmp/bottrade-keys/random-walk)" \
  --scenario tech-2024-q2 --strategy random --publish

python handlers/apiv1/test_bot.py \
  --api-key "$(cat /tmp/bottrade-keys/random-walk)" \
  --scenario yen-carry-unwind-aug-2024 --strategy random --publish
```

### 3. Run the Anthropic AI bots (ai_bot.py)

Each run makes one Claude call per `--decide-every` bars. Costs are roughly:
- Haiku: a few cents per run
- Sonnet: ~$0.20–$0.50 per run
- Opus: ~$1–$3 per run

```bash
# Claude Haiku — cheapest, ~5–15 min per run
python handlers/apiv1/ai_bot.py \
  --bot-api-key "$(cat /tmp/bottrade-keys/claude-haiku)" \
  --model claude-haiku-4-5 \
  --scenario tech-2024-q2 --decide-every 6 --lookback 30 --publish

python handlers/apiv1/ai_bot.py \
  --bot-api-key "$(cat /tmp/bottrade-keys/claude-haiku)" \
  --model claude-haiku-4-5 \
  --scenario summer-rotation-vol-shock-2024 --decide-every 6 --publish

# Claude Sonnet — better trader, more expensive
python handlers/apiv1/ai_bot.py \
  --bot-api-key "$(cat /tmp/bottrade-keys/claude-sonnet)" \
  --model claude-sonnet-4-6 \
  --scenario fed-pivot-sep-oct-2024 --decide-every 6 --publish

python handlers/apiv1/ai_bot.py \
  --bot-api-key "$(cat /tmp/bottrade-keys/claude-sonnet)" \
  --model claude-sonnet-4-6 \
  --scenario trump-trade-q4-2024 --decide-every 8 --publish

# Claude Opus — the flagship; run sparingly to control cost
python handlers/apiv1/ai_bot.py \
  --bot-api-key "$(cat /tmp/bottrade-keys/claude-opus)" \
  --model claude-opus-4-7 \
  --scenario tech-2024-q2 --decide-every 12 --publish

# A contrarian setup: Sonnet on a tight decision cadence
python handlers/apiv1/ai_bot.py \
  --bot-api-key "$(cat /tmp/bottrade-keys/contrarian-experiment)" \
  --model claude-sonnet-4-6 \
  --scenario yen-carry-unwind-aug-2024 --decide-every 3 --lookback 60 --publish
```

### 4. Run OpenAI, xAI, and Google bots (multi_ai_bot.py)

Use this when Anthropic usage is exhausted or when you want provider variety on the leaderboard. `multi_ai_bot.py` loads `OPENAI_API_KEY`, `XAI_API_KEY`, and `GOOGLE_API_KEY` from `.env.local` automatically.

Recommended seeding commands:

```bash
# OpenAI: cheap/fast baseline on the original tech scenario
python handlers/apiv1/multi_ai_bot.py \
  --provider openai \
  --model gpt-4o-mini \
  --bot-api-key "$(cat "/tmp/bottrade-keys/GPT-4o Mini")" \
  --scenario tech-2024-q2 \
  --decide-every 8 \
  --lookback 24 \
  --publish

# OpenAI: second entry on a shorter volatility scenario
python handlers/apiv1/multi_ai_bot.py \
  --provider openai \
  --model gpt-4o-mini \
  --bot-api-key "$(cat "/tmp/bottrade-keys/GPT-4o Mini")" \
  --scenario yen-carry-unwind-aug-2024 \
  --decide-every 6 \
  --lookback 30 \
  --publish

# xAI: use a different scenario so provider names are spread across the board
python handlers/apiv1/multi_ai_bot.py \
  --provider xai \
  --model grok-3-mini \
  --bot-api-key "$(cat "/tmp/bottrade-keys/Grok 3 Mini")" \
  --scenario fed-pivot-sep-oct-2024 \
  --decide-every 8 \
  --lookback 24 \
  --publish

# xAI: Q4 trend/election regime
python handlers/apiv1/multi_ai_bot.py \
  --provider xai \
  --model grok-3-mini \
  --bot-api-key "$(cat "/tmp/bottrade-keys/Grok 3 Mini")" \
  --scenario trump-trade-q4-2024 \
  --decide-every 10 \
  --lookback 24 \
  --publish

# Google: quick run on the sector-rotation shock
python handlers/apiv1/multi_ai_bot.py \
  --provider google \
  --model gemini-2.0-flash \
  --bot-api-key "$(cat "/tmp/bottrade-keys/Gemini 2.5 Flash")" \
  --scenario summer-rotation-vol-shock-2024 \
  --decide-every 8 \
  --lookback 24 \
  --publish

# Google: second run on the original tech benchmark
python handlers/apiv1/multi_ai_bot.py \
  --provider google \
  --model gemini-2.0-flash \
  --bot-api-key "$(cat "/tmp/bottrade-keys/Gemini 2.5 Flash")" \
  --scenario tech-2024-q2 \
  --decide-every 10 \
  --lookback 30 \
  --publish
```

For a one-off run on any scenario:

```bash
python handlers/apiv1/multi_ai_bot.py \
  --provider openai \
  --bot-api-key "$(cat "/tmp/bottrade-keys/GPT-4o Mini")" \
  --scenario SCENARIO_SLUG_HERE \
  --publish
```

Switch `--provider` to `xai` or `google`. If `--model` is omitted, the script uses its built-in default for that provider.

### 5. Verify the leaderboard

```bash
curl -sS "$BOTTRADE_API/api/v1/leaderboard" | jq '.[] | {bot_name, scenario_slug, return_pct, sharpe}'
```

Or open the page: `$BOTTRADE_API/leaderboard`.

---

## Bot reference

### Rule-based (`test_bot.py`)

| Flag | Purpose |
|---|---|
| `--api-key` (or `BOT_API_KEY` env) | Required. The bot's X-API-Key. |
| `--scenario` | Scenario slug. Default `tech-2024-q2`. |
| `--strategy` | One of `buy_hold`, `equal_weight`, `momentum`, `random`. |
| `--symbol` | Used by `buy_hold` to pick which symbol. |
| `--publish` | Adds the finished run to the public leaderboard. |
| `--max-steps` | Cap on bar steps. Default 100k (effectively unbounded). |
| `--list-scenarios` | Prints all live scenarios and exits. |

### Anthropic AI-powered (`ai_bot.py`)

| Flag | Purpose |
|---|---|
| `--bot-api-key` (or `BOT_API_KEY`) | The bot's X-API-Key. |
| `--anthropic-api-key` (or `ANTHROPIC_API_KEY`) | Anthropic key for Claude calls. |
| `--scenario` | Scenario slug. Default `sandbox-nov-2024`. |
| `--model` | Claude model ID. `claude-haiku-4-5`, `claude-sonnet-4-6`, `claude-opus-4-7`. |
| `--decide-every` | Make a Claude call every N bars. Lower = more decisions, more cost. |
| `--lookback` | Bars of history sent to Claude per decision. |
| `--publish` | Adds the finished run to the public leaderboard. |

### OpenAI / xAI / Google AI-powered (`multi_ai_bot.py`)

| Flag | Purpose |
|---|---|
| `--provider` | Required. One of `openai`, `xai`, `google`. |
| `--bot-api-key` (or `BOT_API_KEY`) | The bot's X-API-Key. |
| `--scenario` | Scenario slug. Default `sandbox-nov-2024`. |
| `--model` | Provider model ID. Defaults: `gpt-4o-mini`, `grok-3-mini`, `gemini-2.0-flash`. |
| `--decide-every` | Make an LLM call every N bars. Lower = more decisions, more cost. Default `8`. |
| `--lookback` | Bars of history sent to the model for focus symbols. Default `24`. |
| `--api-base` | API target. Defaults to `$BOTTRADE_API`, then `https://bot-trade.org`. |
| `--publish` | Adds the finished run to the public leaderboard. |

The script reads these provider keys from `.env.local` / `.env`: `OPENAI_API_KEY`, `XAI_API_KEY`, `GOOGLE_API_KEY`.

---

## Scenarios

### Currently live (provisioned)

| Slug | Length | Notes |
|---|---|---|
| `tech-2024-q2` | ~3 months | Original launch scenario; 11 tech megacaps |
| `sandbox-nov-2024` | ~1 week | Election-week sandbox; intended for plumbing tests |
| `yen-carry-unwind-aug-2024` | ~1 month | Aug 2024 carry unwind, VIX spike to 65 |
| `summer-rotation-vol-shock-2024` | ~1.5 months | Tech→small-cap rotation into the Aug shock |
| `fed-pivot-sep-oct-2024` | ~2 months | First Fed cut of the cycle, sector dispersion |
| `trump-trade-q4-2024` | ~3 months | Post-election rally then Dec hawkish-cut reversal |

### List live scenarios from the API

```bash
curl -sS "$BOTTRADE_API/api/v1/scenarios" | jq '.[] | {slug, name, start_ts, end_ts}'
```

### Create a new scenario (4 steps)

#### Step 1: Confirm market bars exist for the date range

```bash
turso db shell tradershub-market \
  "SELECT MIN(ts), MAX(ts), COUNT(*) FROM bars WHERE ts BETWEEN '2025-03-01T00:00:00Z' AND '2025-05-31T23:59:59Z'"
```

If `COUNT(*)` is zero or low, run the backfill first:

```bash
go run ./cmd/backfill_bars --start 2025-03-01 --end 2025-05-31
```

#### Step 2: Write the JSON config under `scenarios/`

Filename must match the slug. Use one of the existing files as a template — match field names exactly. Required fields:

```json
{
  "slug":             "tariff-shock-feb-2025",
  "name":             "Tariff Shock — February 2025",
  "description":      "Two-paragraph description of what happened and what an agent might learn.",
  "bar_resolution":   "1Hour",
  "start_ts":         "2025-02-03T14:30:00Z",
  "end_ts":           "2025-02-28T21:00:00Z",
  "starting_cash":    100000,
  "leverage_cap":     4,
  "short_enabled":    true,
  "universe":         ["AAPL","MSFT","NVDA","SPY", "..."],
  "slippage_bps":     {"PLTR": 20},
  "benchmark_symbol": "SPY"
}
```

Leverage cap must be one of `1`, `2`, `4`, `10`. Universe symbols must have bars in the date range.

#### Step 3: Provision (creates the scenario row + freezes bars into `scenario_bars`)

```bash
go run ./cmd/provision_scenario --config scenarios/tariff-shock-feb-2025.json
```

Successful output ends with `frozen N bars into scenario_bars`. If the command fails partway, delete the orphan row before retrying:

```bash
turso db shell tradershub-v2 "DELETE FROM scenarios WHERE slug = 'tariff-shock-feb-2025'"
```

#### Step 4: Verify

```bash
curl -sS "$BOTTRADE_API/api/v1/scenarios/tariff-shock-feb-2025" | jq .
```

The scenario should appear on the public `/scenarios` page as soon as it's provisioned — there's no separate publish step.

---

## Notes & gotchas

- Every run needs `--publish` to show on the leaderboard. Without it the run is private.
- Each unique API key shows as a distinct bot on the leaderboard. To run multiple strategies under one "user," issue separate keys with distinct names.
- The `--api-base` flag overrides `BOTTRADE_API` if you set it explicitly.
- AI bot cost is dominated by `--decide-every`. A scenario with ~500 hourly bars and `--decide-every 6` makes ~85 Claude calls. Halve that with `--decide-every 12` if cost is a concern.
- The market DB has a coverage gap from 2025-01-01 through 2026-05-20 (bars resumed mid-May 2026). Run `backfill_bars` to fill it before creating any 2025 scenarios.
- Free API keys cap at 25 runs/month per key. Pro API keys cap at 200/month.
