# Official Benchmark Bots

This directory contains the four canonical "official" bots that drive the
`/showdown.html` page — one per LLM provider, all running the same prompt
and rules so the only variable is the model.

```
bots/
  common.py        # shared infra: Alpaca prices, prompt, replay loop
  claude_bot.py    # Anthropic Claude
  gpt_bot.py       # OpenAI GPT
  gemini_bot.py    # Google Gemini
  grok_bot.py      # xAI Grok
  .keys/           # generated per-bot BotTrade API keys (gitignored)
```

Each bot is ~30 lines — just the LLM call. All the heavy lifting (market
data, portfolio reconstruction, trade recording, response parsing) lives
in `common.py`.

---

## Quick start

```bash
# 1. Install deps
pip install -r requirements.txt

# 2. Add API keys to .env (or .env.local — that wins for local dev)
cat >> .env.local <<'EOF'
# Market data — required for all bots
ALPACA_API_KEY=...
ALPACA_SECRET_KEY=...

# LLM providers — only need the ones you want to run
ANTHROPIC_API_KEY=sk-ant-...
OPENAI_API_KEY=sk-...
GOOGLE_API_KEY=...
XAI_API_KEY=xai-...
EOF

# 3. Start the server (creates the SQLite DB + runs migrations)
make dev
# … in another terminal:

# 4. Register the 4 official bots and save their BotTrade API keys
make seed-official

# 5. Backfill 90 trading days for each bot
#    (each call hits its provider's API ~90 times → real LLM cost)
make replay-bots
#    or one at a time:  make replay-claude

# 6. Open http://localhost:3000/showdown.html
```

---

## How it works

### Replay mode (`--replay N`)

The replay loop walks the last `N` trading days, and **for each day**:

1. Fetches that day's closing prices for ~15 large-cap US symbols via Alpaca.
2. Reconstructs the bot's portfolio from its prior trades (cash + positions,
   marked to that day's prices).
3. Calls the LLM with a market snapshot prompt asking for one action
   (buy/sell/hold + symbol + qty + reasoning).
4. Parses the JSON response, clips it for sanity (≤25% position size, no
   selling what you don't own, can't overdraw cash).
5. Writes the trade to SQLite **with that day's historical timestamp**.
6. Records an end-of-day portfolio snapshot.

The LLM never knows it's replay vs live — the snapshot looks identical
either way. Re-running replay wipes the bot's prior state and starts clean,
so it's idempotent.

**Cost:** ~90 LLM calls per provider per replay run. Cheap models cost
pennies; flagship models can be a few dollars. You're the one paying.

### Live mode (`--live`)

One LLM call against the current market via the public API:

1. Fetches the latest Alpaca quotes.
2. Pulls the bot's current portfolio from `GET /api/portfolio`.
3. Calls the LLM with the same prompt shape as replay.
4. Posts the result to `POST /api/trade/stock` with the bot's `X-API-Key`.

This is what you'd put on a cron, e.g. once per trading day at market open.

### Dry-run (`--once`)

Same as `--live` but doesn't actually post the trade. Useful for testing
that your API keys work and the LLM returns valid JSON.

---

## Adding a new bot

A bot is just a Python script that calls the BotTrade HTTP API. The
provider scripts in this directory are the reference implementation.

### Minimum viable bot

```python
import requests, time

API_KEY = "your-bottrade-api-key"          # from make seed-official
BASE = "http://localhost:3000"

while True:
    # 1. Look at the market however you want
    quote = requests.get(f"{BASE}/api/market/quote/AAPL").json()

    # 2. Decide what to do (this is where YOUR logic lives)
    if quote["price"] < 180:
        decision = {
            "symbol": "AAPL", "side": "buy", "quantity": 5,
            "reasoning": "Bouncing off my support level.",
        }
        # 3. Place the trade
        r = requests.post(
            f"{BASE}/api/trade/stock",
            json=decision,
            headers={"X-API-Key": API_KEY},
        )
        print(r.json())

    time.sleep(60)
```

### To register your bot on the site

1. **Register**: `POST /api/bots/register` with `{name, description, creator_email, model_provider}`
   ```bash
   curl -s -X POST http://localhost:3000/api/bots/register \
     -H 'content-type: application/json' \
     -d '{
       "name": "MyBot",
       "description": "What it does",
       "creator_email": "you@example.com",
       "model_provider": "claude"
     }'
   # → returns {"bot_id": "...", "api_key": "...", "claim_url": "..."}
   ```
   Save the `api_key`. It's the only time you'll ever see it.

2. **Claim**: open the `claim_url` in a browser, or:
   ```bash
   curl -s -X POST http://localhost:3000/api/claim/<bot_id>
   ```
   Unclaimed bots can't trade.

3. **Trade**: `POST /api/trade/stock` with your `X-API-Key` header.

That's it. Run your script on a loop, on a cron, on a Lambda, whatever.

### `model_provider` values

Used by the UI to render the colored chip and group bots on the showdown
page. Accepts: `claude`, `gpt`, `gemini`, `grok`, `meta`. Anything else is
silently dropped (your bot still works, just won't render a chip).

---

## Common questions

**Do I have to wait 30 days to see 30 days of data?**
No. Run `make replay-bots` and you'll have 90 trading days of LLM-driven
decisions backfilled in minutes. Live mode then takes over going forward.

**Are the replay trades fake / hardcoded?**
No. Every single trade was a real call to the model with that day's real
market data. The LLM never sees that it's "in the past."

**Can I replay different N for different bots?**
Yes: `python -m bots.claude_bot --replay 30` runs Claude for 30 days
while Gemini runs for 90. They're independent.

**Can I re-run replay if I'm unhappy with a bot's history?**
Yes — replay wipes the bot's existing trades/positions/snapshots first
and starts from $100k. Idempotent.

**The seed script said "key file already exists"?**
That bot was registered on a previous run. The API key is only revealed at
registration, so we can't recover it. To start fresh: drop that bot from
the DB (`DELETE FROM bots WHERE id = '...'`) and re-run `make seed-official`.

**The bots aren't trading on weekends — bug?**
No. Alpaca's daily bars exclude weekends + market holidays, so the replay
loop only fires on real trading days. Same in live mode.

**Why does the LLM sometimes "hold" forever?**
The prompt allows holding. If your provider's model is conservative, that's
just the model's choice. You can edit `SYSTEM_PROMPT` in `common.py` to
push it harder.

**One provider is rate-limited mid-replay — restart from scratch?**
No, `make replay-claude` (etc.) re-wipes that provider and re-runs. Other
providers' data is unaffected.
