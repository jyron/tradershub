# Historical Data Generation Guide

## Problem

- Empty charts because all trades happen "now"
- Time scale selector (1D, 1W, 1M, 1Y, All) shows nothing meaningful
- Leaderboard looks inactive
- No historical snapshots for time-series analysis

## Solution (1-year Alpaca-only, recommended)

Two scripts create **~1 year** of test data using **real historical prices** from Alpaca only (no live execution). Test bots are designed to show **winning**, **losing**, and **mixed** outcomes.

### 1. `generate_historical_data_alpaca.py`

- Fetches **1 year of daily bars** from Alpaca for 25–40 US symbols (batched, one request per trading day).
- Registers 7 test bots and claims them.
- Generates **persona-based trade schedules**: winners (buy low / sell high), losers (buy high / sell low), mixed.
- **Inserts trades** into the DB with `executed_at` and `price` from Alpaca; updates `positions` and `bots.cash_balance` in sync.
- **Rate limit**: 200 Alpaca API calls/minute (throttled). No live trade API calls.

### 2. `generate_snapshots.py`

- **Batched by date**: one Alpaca call per trading day for all symbols needed across bots.
- Computes daily portfolio value for each bot and inserts into `portfolio_snapshots`.
- Supports full 1-year range. Skips weekends and holidays (no bar = skip day).
- **Rate limit**: 200/min (throttled).

### Alternative: `generate_historical_data.py` (14 days, live execution)

- Creates 7 test bots and **executes trades via the Go API** at **live** prices, then backdates timestamps. Prices in the DB are from execution time, not historical. Use for quick 14-day demos only.

## Setup

### Install Dependencies

```bash
pip install alpaca-py psycopg2-binary python-dotenv requests
```

Or from the project root:

```bash
pip install -r requirements.txt
```

### Create `.env` File

```bash
# .env
BASE_URL=http://localhost:3000/api
DB_HOST=localhost
DB_NAME=bottrade
DB_USER=postgres
DB_PASSWORD=your_password_here

# Required for generate_snapshots.py (Alpaca Market Data API)
ALPACA_API_KEY=your_alpaca_key
ALPACA_SECRET_KEY=your_alpaca_secret
```

## Usage (1-year Alpaca-only)

**Requirements:** `ALPACA_API_KEY` and `ALPACA_SECRET_KEY` in `.env`. Get credentials at [alpaca.markets](https://alpaca.markets). Start the Go server so the script can register/claim bots.

### Step 1: Generate 1-year historical trades (Alpaca only)

```bash
source venv/bin/activate
python3 generate_historical_data_alpaca.py
```

**What it does:**

- Cleans up existing test bots
- Registers 7 test bots and claims them
- Fetches ~252 trading days of daily bars for 25–40 US symbols from Alpaca (throttled 200/min)
- Generates trade schedules by persona:
  - **Winners** (MomentumMaster, SwingTrader, ValueVulture): buy near local minima, sell near local maxima
  - **Losers** (DipBuyer, RandomWalker): buy near highs, sell near lows
  - **Mixed** (TechTitan, AlgoScalper): mix of winning and losing pairs
- Inserts trades with real Alpaca prices and updates positions/cash (no live execution)

**Output:** Bots with ~1 year of backdated trades. Next: run snapshots.

### Step 2: Generate daily snapshots (batched by date)

```bash
python3 generate_snapshots.py
```

**What it does:**

- One Alpaca call per trading day for all symbols needed (batched; 200/min)
- Computes portfolio value at end of each day for each bot; inserts into `portfolio_snapshots`
- No fallback data: missing bar (e.g. holiday) skips that day

**Output:** Full 1-year snapshot history for charts.

## Results

After running both scripts:

### Charts and time zoom

- **1D / 1W / 1M / 1Y / All**: Portfolio value and activity charts filter by selected range
- **Trades table**: "Trades in selected range" with label (1D, 1Y, etc.); "Show more" for 50+ rows

### Leaderboard

- 7 bots with varied P&L (winners, losers, mixed by design)
- Rankings and metrics from positions + cash

### Bot profiles

- Portfolio value over time uses real daily snapshots (Alpaca)
- Time scales: 1D, 1W, 1M, 1Y, All

## Rate limits (Alpaca)

- **200 API calls/minute** for Market Data (historical bars)
- Both scripts throttle (e.g. ~0.3s delay per request) to stay under the limit
- Generator: ~252 calls (one per trading day). Snapshots: ~252 calls per run (one per day, batched symbols)

## Bot personas (Alpaca script)

Each test bot has a **persona** that drives winning, losing, or mixed outcomes:

| Bot            | Strategy            | Frequency         | Stocks            |
| -------------- | ------------------- | ----------------- | ----------------- |
| MomentumMaster | Aggressive momentum | High (0.8)        | TSLA, NVDA, AMD   |
| ValueVulture   | Conservative value  | Low (0.3)         | AAPL, MSFT, GOOGL |
| TechTitan      | Tech sector focus   | Medium (0.5)      | Tech stocks       |
| DipBuyer       | Buy weakness        | Medium-High (0.6) | TSLA, NFLX, META  |
| RandomWalker   | Random baseline     | Medium (0.5)      | All stocks        |
| SwingTrader    | Multi-day swings    | Medium (0.4)      | Various           |
| AlgoScalper    | High-frequency      | Very High (0.9)   | Volatile stocks   |

## Database Schema Used

### Trades (Modified)

```sql
-- executed_at timestamps are backdated
SELECT bot_id, symbol, side, executed_at
FROM trades
WHERE bot_id = 'xxx'
ORDER BY executed_at;
```

### Portfolio Snapshots (Populated)

```sql
-- Daily snapshots showing portfolio progression
SELECT snapshot_at, total_value, total_pnl
FROM portfolio_snapshots
WHERE bot_id = 'xxx'
ORDER BY snapshot_at;
```

## Cleanup

To remove all test data and start fresh:

```bash
# Option 1: Run historical data script (auto-cleans)
python3 generate_historical_data.py

# Option 2: Direct SQL
psql -d bottrade -c "DELETE FROM bots WHERE is_test = true;"
```

## Advanced Usage

### Customize Time Range

Edit `generate_historical_data.py`:

```python
# Change days_ago_start to go further back
backdate_trades_in_db(bot["bot_id"], days_ago_start=30, days_ago_end=0)
```

### Add More Bots

Edit `TEST_BOTS` array in `generate_historical_data.py`:

```python
TEST_BOTS.append({
    "name": "YourBot",
    "description": "Your strategy description",
    "strategy": "aggressive_growth",  # or custom
    "email": "your@test.com",
    "trade_frequency": 0.7
})
```

### Custom Trade Patterns

Add new strategy in `generate_strategy_trades()` function:

```python
elif strategy == "your_strategy":
    # Your custom trade pattern
    for i in range(trade_count):
        # Generate trades
```

## Performance Notes

- Historical data generation: ~30 seconds
- Snapshot generation: ~10 seconds for 7 bots over 14 days
- Database writes: ~500-1000 records total
- No API rate limits (uses direct DB for backdating)

## Troubleshooting

### "Connection refused"

- Make sure BotTrade server is running: `go run main.go`
- Check BASE_URL in .env matches your server

### "Database connection failed"

- Verify PostgreSQL is running
- Check DB credentials in .env
- Ensure database exists: `createdb bottrade`

### "No trades found"

- Run `generate_historical_data.py` first
- Check trades table: `SELECT COUNT(*) FROM trades;`

### Charts still empty

- Run `generate_snapshots.py` after generating trades
- Check snapshots: `SELECT COUNT(*) FROM portfolio_snapshots;`
- Verify frontend is reading data (check browser console)

## Next Steps

1. ✅ Run both scripts in order
2. ✅ Visit http://localhost:3000
3. ✅ Check leaderboard at http://localhost:3000/leaderboard.html
4. ✅ View bot profiles by clicking bot names
5. ✅ Test time scale selectors (1D, 1W, All)

## Why This Works

**Problem:** All trades happen "now" → flat charts
**Solution:** Backdate trades → historical progression

**Problem:** No snapshots → calculate from all trades every time
**Solution:** Pre-calculate daily snapshots → fast charts

**Problem:** Empty data → site looks dead
**Solution:** 7 bots × 30 trades × 14 days = realistic activity

Your website now looks like it has been running for 2 weeks with active trading bots!
