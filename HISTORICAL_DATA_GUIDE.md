# Historical Data Generation Guide

## Problem
- Empty charts because all trades happen "now"
- Time scale selector (1D, 1W, All) shows nothing meaningful
- Leaderboard looks inactive
- No historical snapshots for time-series analysis

## Solution
Two scripts that work together to create realistic historical data:

### 1. `generate_historical_data.py`
- Creates 7 test bots with different strategies
- Generates 20-50 trades per bot
- **Backdates trades** over the past 14 days
- Spreads trades realistically across time

### 2. `generate_snapshots.py`
- Reads backdated trades
- Calculates portfolio value at end of each day
- Populates `portfolio_snapshots` table
- Creates time-series data for charts

## Setup

### Install Dependencies
```bash
pip install requests psycopg2-binary python-dotenv
```

### Create `.env` File
```bash
# .env
BASE_URL=http://localhost:3000/api
DB_HOST=localhost
DB_NAME=bottrade
DB_USER=postgres
DB_PASSWORD=your_password_here
```

## Usage

### Step 1: Generate Historical Trades
```bash
python3 generate_historical_data.py
```

**What it does:**
- Cleans up old test bots
- Registers 7 new bots with different strategies:
  - MomentumMaster (aggressive, high frequency)
  - ValueVulture (conservative, blue chips)
  - TechTitan (tech-focused)
  - DipBuyer (contrarian)
  - RandomWalker (baseline)
  - SwingTrader (multi-day holds)
  - AlgoScalper (very high frequency)
- Executes 20-50 trades per bot
- Backdates all trades over past 14 days
- Spreads trades evenly with randomization

**Output:**
```
✅ Historical data generation complete!
7 bots created with trades spread over 14 days
```

### Step 2: Generate Daily Snapshots
```bash
python3 generate_snapshots.py
```

**What it does:**
- Reads all backdated trades
- For each bot, for each day:
  - Calculates portfolio value at end of day
  - Calculates cash balance
  - Calculates positions value
  - Calculates daily P&L
  - Inserts snapshot into database
- Creates complete historical timeline

**Output:**
```
✅ Snapshot generation complete!
Total snapshots created: 98
Bots processed: 7
```

## Results

After running both scripts:

### Charts Show Real Data
- **1D view**: Shows trades from last 24 hours
- **1W view**: Shows trades from last 7 days
- **All view**: Shows full 14-day history with gradients

### Leaderboard Populated
- 7 bots with different performance
- Gradient bars showing rankings
- Rich tooltips with full metrics

### Bot Profiles Active
- Portfolio value chart shows progression over time
- Trading activity chart shows buy/sell patterns
- Time scales work properly

## Bot Strategies

Each bot has a unique trading personality:

| Bot | Strategy | Frequency | Stocks |
|-----|----------|-----------|--------|
| MomentumMaster | Aggressive momentum | High (0.8) | TSLA, NVDA, AMD |
| ValueVulture | Conservative value | Low (0.3) | AAPL, MSFT, GOOGL |
| TechTitan | Tech sector focus | Medium (0.5) | Tech stocks |
| DipBuyer | Buy weakness | Medium-High (0.6) | TSLA, NFLX, META |
| RandomWalker | Random baseline | Medium (0.5) | All stocks |
| SwingTrader | Multi-day swings | Medium (0.4) | Various |
| AlgoScalper | High-frequency | Very High (0.9) | Volatile stocks |

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
