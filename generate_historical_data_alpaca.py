#!/usr/bin/env python3
"""
Generate 1 year of historical test data using Alpaca Market Data only.
Creates test bots with intentional winner/loser/mixed portfolios by inserting
trades at real historical prices (no live execution). Rate limit: 200 Alpaca calls/min.

Requirements: pip install alpaca-py psycopg2-binary python-dotenv requests

Usage: python3 generate_historical_data_alpaca.py
"""

import os
import time
import random
from datetime import datetime, timedelta
from collections import defaultdict

import psycopg2
import requests
from dotenv import load_dotenv

from alpaca.data.historical.stock import StockHistoricalDataClient
from alpaca.data.requests import StockBarsRequest
from alpaca.data.timeframe import TimeFrame

load_dotenv()

BASE_URL = os.getenv("BASE_URL", "http://localhost:3000/api")
DB_HOST = os.getenv("DB_HOST", "localhost")
DB_NAME = os.getenv("DB_NAME", "bottrade")
DB_USER = os.getenv("DB_USER", "postgres")
DB_PASSWORD = os.getenv("DB_PASSWORD", "")
ALPACA_API_KEY = os.getenv("ALPACA_API_KEY")
ALPACA_SECRET_KEY = os.getenv("ALPACA_SECRET_KEY")

if not ALPACA_API_KEY or not ALPACA_SECRET_KEY:
    print("Error: ALPACA_API_KEY and ALPACA_SECRET_KEY must be set in .env")
    exit(1)

# 25–40 US symbols for 1y bars and trade generation
SYMBOLS = [
    "AAPL", "GOOGL", "MSFT", "TSLA", "AMZN", "NVDA", "META", "NFLX", "AMD", "INTC",
    "COIN", "DIS", "JPM", "V", "JNJ", "SPY", "QQQ", "PYPL", "ADBE", "CRM",
    "NKE", "HD", "MCD", "WMT", "PFE", "ABBV", "XOM", "CVX", "BA", "UNH",
]

# Bot personas: winner (buy low/sell high), loser (buy high/sell low), mixed
TEST_BOTS = [
    {"name": "MomentumMaster", "description": "High-frequency momentum trader", "email": "momentum@test.com", "persona": "winner"},
    {"name": "SwingTrader", "description": "Multi-day swing strategy", "email": "swing@test.com", "persona": "winner"},
    {"name": "ValueVulture", "description": "Long-term value investor", "email": "value@test.com", "persona": "winner"},
    {"name": "DipBuyer", "description": "Contrarian buy-the-dip (inverted for demo)", "email": "dip@test.com", "persona": "loser"},
    {"name": "RandomWalker", "description": "Random baseline (losing)", "email": "random@test.com", "persona": "loser"},
    {"name": "TechTitan", "description": "Tech specialist mixed outcomes", "email": "tech@test.com", "persona": "mixed"},
    {"name": "AlgoScalper", "description": "High-frequency mixed P&L", "email": "scalper@test.com", "persona": "mixed"},
]

ALPACA_RATE_DELAY = 60 / 200  # 200 calls per minute


def get_trading_days(days_back=365, max_trading_days=252):
    """Return list of trading days (weekdays) from today going back, up to max_trading_days."""
    out = []
    d = datetime.now().date()
    while len(out) < max_trading_days and (datetime.now().date() - d).days < days_back + 30:
        if d.weekday() < 5:
            out.append(d)
        d -= timedelta(days=1)
    out.reverse()
    return out[:max_trading_days]


def fetch_one_year_bars(client, trading_days):
    """Fetch daily bars for all SYMBOLS for each trading day. Returns bars[symbol][date_str] = close."""
    bars_cache = defaultdict(dict)
    for i, day in enumerate(trading_days):
        start = datetime(day.year, day.month, day.day, 0, 0, 0)
        end = datetime(day.year, day.month, day.day, 23, 59, 59)
        req = StockBarsRequest(
            symbol_or_symbols=SYMBOLS,
            start=start,
            end=end,
            timeframe=TimeFrame.Day,
        )
        try:
            resp = client.get_stock_bars(req)
            if resp.data:
                for symbol, symbol_bars in resp.data.items():
                    if symbol_bars and len(symbol_bars) > 0:
                        bars_cache[symbol][day.strftime("%Y-%m-%d")] = float(symbol_bars[0].close)
        except Exception as e:
            print(f"    Warning: {day} {e}")
        time.sleep(ALPACA_RATE_DELAY)
        if (i + 1) % 50 == 0:
            print(f"    Fetched {i + 1}/{len(trading_days)} days")
    return bars_cache


def find_local_extrema(bars_cache, symbol, window=5):
    """For one symbol, return (min_dates, max_dates) from bars_cache. min_dates/max_dates are sorted by date."""
    if symbol not in bars_cache or not bars_cache[symbol]:
        return [], []
    dates = sorted(bars_cache[symbol].keys())
    if len(dates) < window:
        return [], []
    mins = []
    maxs = []
    for i in range(window // 2, len(dates) - window // 2):
        left = max(0, i - window // 2)
        right = min(len(dates), i + window // 2 + 1)
        window_dates = dates[left:right]
        prices = [bars_cache[symbol][d] for d in window_dates]
        if bars_cache[symbol][dates[i]] == min(prices):
            mins.append(dates[i])
        if bars_cache[symbol][dates[i]] == max(prices):
            maxs.append(dates[i])
    return mins, maxs


def _spread_across(n, want):
    """Return up to `want` evenly spaced indices into a list of length n (so picks span the full range)."""
    if n <= want:
        return list(range(n))
    step = (n - 1) / max(1, want - 1)
    return [int(round(i * step)) for i in range(want)]


def generate_trade_schedule(bars_cache, trading_days, persona, num_trades_target=50):
    """
    Generate list of (date_str, symbol, side, qty, reason) in chronological order.
    Winner: buy at local min, sell at later local max. Loser: buy at max, sell at min. Mixed: combine both.
    Pick mins/maxs SPREAD across the year (not just the first few) so we get trades all 12 months.
    """
    schedule = []
    date_list = sorted(set().union(*(set(bars_cache[s].keys()) for s in bars_cache if bars_cache[s])))[:252]
    if not date_list:
        return schedule

    symbols_with_data = [s for s in SYMBOLS if s in bars_cache and len(bars_cache[s]) >= 20]
    if not symbols_with_data:
        return schedule

    for symbol in random.sample(symbols_with_data, min(15, len(symbols_with_data))):
        mins, maxs = find_local_extrema(bars_cache, symbol)
        if not mins or not maxs:
            continue
        # Use extrema spread across the full year, not just the first 6 (which are all early dates)
        n_mins = min(10, len(mins))
        n_maxs = min(10, len(maxs))
        mins_pick = [mins[i] for i in _spread_across(len(mins), n_mins)]
        maxs_pick = [maxs[i] for i in _spread_across(len(maxs), n_maxs)]
        if persona == "winner":
            for min_d in mins_pick:
                for max_d in maxs_pick:
                    if max_d > min_d and (datetime.strptime(max_d, "%Y-%m-%d") - datetime.strptime(min_d, "%Y-%m-%d")).days >= 5:
                        qty = random.randint(10, 40)
                        schedule.append((min_d, symbol, "buy", qty, "Momentum entry"))
                        schedule.append((max_d, symbol, "sell", qty, "Take profit"))
                        break
        elif persona == "loser":
            for max_d in maxs_pick:
                for min_d in mins_pick:
                    if min_d > max_d and (datetime.strptime(min_d, "%Y-%m-%d") - datetime.strptime(max_d, "%Y-%m-%d")).days >= 5:
                        qty = random.randint(10, 35)
                        schedule.append((max_d, symbol, "buy", qty, "Entry"))
                        schedule.append((min_d, symbol, "sell", qty, "Exit"))
                        break
        else:
            # Mixed: use full spread (not [:6]) so trades span the whole year
            for min_d in mins_pick:
                for max_d in maxs_pick:
                    if max_d > min_d and (datetime.strptime(max_d, "%Y-%m-%d") - datetime.strptime(min_d, "%Y-%m-%d")).days >= 5:
                        qty = random.randint(8, 30)
                        schedule.append((min_d, symbol, "buy", qty, "Entry"))
                        schedule.append((max_d, symbol, "sell", qty, "Exit"))
                        break
            for max_d in maxs_pick:
                for min_d in mins_pick:
                    if min_d > max_d and (datetime.strptime(min_d, "%Y-%m-%d") - datetime.strptime(max_d, "%Y-%m-%d")).days >= 5:
                        qty = random.randint(8, 25)
                        schedule.append((max_d, symbol, "buy", qty, "Entry"))
                        schedule.append((min_d, symbol, "sell", qty, "Exit"))
                        break

    schedule.sort(key=lambda x: (x[0], x[1], x[2]))

    # Keep buy/sell pairs together; spread selection across the year (by month)
    if len(schedule) <= num_trades_target:
        return schedule

    # Build pairs: match each buy to its corresponding sell (same symbol, qty; sell date > buy date).
    # Buys and sells are not adjacent after date-sort, so we must match explicitly.
    buys = [(i, t) for i, t in enumerate(schedule) if t[2] == "buy"]
    sells = [(i, t) for i, t in enumerate(schedule) if t[2] == "sell"]
    pairs = []
    used_sell_idx = set()
    for _bi, (date_str, symbol, _side, qty, reason) in buys:
        for sell_i, (s_date, s_sym, _s_side, s_qty, _r) in sells:
            if sell_i in used_sell_idx:
                continue
            if s_sym != symbol or s_qty != qty or s_date <= date_str:
                continue
            pairs.append([
                (date_str, symbol, "buy", qty, reason),
                (s_date, s_sym, "sell", s_qty, _r),
            ])
            used_sell_idx.add(sell_i)
            break

    if not pairs:
        # Fallback: spread trades by date (take evenly spaced indices into schedule)
        step = (len(schedule) - 1) / max(1, num_trades_target - 1)
        indices = [int(round(i * step)) for i in range(num_trades_target)]
        return [schedule[j] for j in indices]

    # How many pairs we want (each pair = 2 trades)
    num_pairs_want = min(len(pairs), num_trades_target // 2)
    if num_pairs_want <= 0:
        return schedule[:num_trades_target]

    # Sort pairs by buy date so we can spread across the year
    pairs.sort(key=lambda p: p[0][0])
    # Take evenly spaced pairs so activity spans the full year (not just first month)
    if num_pairs_want == 1:
        indices = [0]
    else:
        step = (len(pairs) - 1) / (num_pairs_want - 1)
        indices = [int(round(i * step)) for i in range(num_pairs_want)]
    selected = [pairs[j] for j in indices]
    out = []
    for p in selected:
        out.append(p[0])
        out.append(p[1])
    out.sort(key=lambda x: (x[0], x[1], x[2]))
    return out


def cleanup_test_bots(conn):
    cur = conn.cursor()
    cur.execute("DELETE FROM bots WHERE is_test = true")
    conn.commit()
    n = cur.rowcount
    cur.close()
    return n


def register_bot(bot_config):
    resp = requests.post(
        f"{BASE_URL}/bots/register",
        json={
            "name": bot_config["name"],
            "description": bot_config["description"],
            "creator_email": bot_config["email"],
            "is_test": True,
        },
        timeout=10,
    )
    if resp.status_code == 201:
        data = resp.json()
        return data["api_key"], data["bot_id"]
    return None, None


def claim_bot(bot_id):
    resp = requests.post(f"{BASE_URL}/claim/{bot_id}", timeout=10)
    return resp.status_code == 200


def insert_trade_and_update_position(conn, bot_id, date_str, symbol, side, quantity, price, reasoning):
    """Insert one trade and update positions + bots.cash_balance. Replicates TradingEngine logic."""
    total_value = round(price * quantity, 2)
    executed_at = datetime.strptime(date_str, "%Y-%m-%d").replace(hour=16, minute=0, second=0, microsecond=0)

    cur = conn.cursor()

    cur.execute("SELECT cash_balance FROM bots WHERE id = %s", (bot_id,))
    row = cur.fetchone()
    if not row:
        cur.close()
        raise ValueError("Bot not found")
    cash = float(row[0])

    if side == "buy":
        if cash < total_value:
            cur.close()
            raise ValueError(f"Insufficient funds: need {total_value}, have {cash}")
        cur.execute(
            "UPDATE bots SET cash_balance = cash_balance - %s WHERE id = %s",
            (total_value, bot_id),
        )
    else:
        cur.execute(
            "SELECT id, quantity, avg_cost FROM positions WHERE bot_id = %s AND symbol = %s AND position_type = 'stock'",
            (bot_id, symbol),
        )
        pos = cur.fetchone()
        if not pos or pos[1] < quantity:
            cur.close()
            raise ValueError(f"Insufficient shares for {symbol}")
        cur.execute(
            "UPDATE bots SET cash_balance = cash_balance + %s WHERE id = %s",
            (total_value, bot_id),
        )

    cur.execute(
        """INSERT INTO trades (bot_id, symbol, trade_type, side, quantity, price, total_value, reasoning, executed_at)
           VALUES (%s, %s, 'stock', %s, %s, %s, %s, %s, %s)""",
        (bot_id, symbol, side, quantity, price, total_value, reasoning, executed_at),
    )

    if side == "buy":
        cur.execute(
            "SELECT id, quantity, avg_cost FROM positions WHERE bot_id = %s AND symbol = %s AND position_type = 'stock'",
            (bot_id, symbol),
        )
        pos = cur.fetchone()
        if not pos:
            cur.execute(
                "INSERT INTO positions (bot_id, symbol, position_type, quantity, avg_cost) VALUES (%s, %s, 'stock', %s, %s)",
                (bot_id, symbol, quantity, price),
            )
        else:
            new_qty = pos[1] + quantity
            new_avg = (float(pos[2]) * pos[1] + price * quantity) / new_qty
            cur.execute(
                "UPDATE positions SET quantity = %s, avg_cost = %s, updated_at = NOW() WHERE id = %s",
                (new_qty, new_avg, pos[0]),
            )
    else:
        cur.execute(
            "SELECT id, quantity FROM positions WHERE bot_id = %s AND symbol = %s AND position_type = 'stock'",
            (bot_id, symbol),
        )
        pos = cur.fetchone()
        new_qty = pos[1] - quantity
        if new_qty == 0:
            cur.execute("DELETE FROM positions WHERE id = %s", (pos[0],))
        else:
            cur.execute("UPDATE positions SET quantity = %s, updated_at = NOW() WHERE id = %s", (new_qty, pos[0]))

    conn.commit()
    cur.close()


def main():
    print("=" * 70)
    print("BotTrade 1-Year Historical Data (Alpaca Only)")
    print("=" * 70)

    conn = psycopg2.connect(
        host=DB_HOST,
        database=DB_NAME,
        user=DB_USER,
        password=DB_PASSWORD,
    )

    print("\nCleaning up existing test bots...")
    n = cleanup_test_bots(conn)
    print(f"  Removed {n} test bots")

    print("\nRegistering bots...")
    bots_data = []
    for cfg in TEST_BOTS:
        api_key, bot_id = register_bot(cfg)
        if bot_id:
            bots_data.append({"config": cfg, "api_key": api_key, "bot_id": bot_id})
            print(f"  Registered {cfg['name']}")
        time.sleep(0.1)

    print("\nClaiming bots...")
    for bot in bots_data:
        claim_bot(bot["bot_id"])
        time.sleep(0.05)

    trading_days = get_trading_days(days_back=365, max_trading_days=252)
    print(f"\nFetching 1 year of daily bars ({len(trading_days)} trading days, {len(SYMBOLS)} symbols)...")
    print("  Rate limit: 200/min (throttled)")

    client = StockHistoricalDataClient(ALPACA_API_KEY, ALPACA_SECRET_KEY)
    bars_cache = fetch_one_year_bars(client, trading_days)
    total_bars = sum(len(bars_cache[s]) for s in bars_cache)
    print(f"  Cached {total_bars} symbol-day bars")

    print("\nGenerating trade schedules (winner/loser/mixed)...")
    for bot in bots_data:
        persona = bot["config"]["persona"]
        schedule = generate_trade_schedule(bars_cache, trading_days, persona, num_trades_target=100)
        bot["schedule"] = schedule
        print(f"  {bot['config']['name']} ({persona}): {len(schedule)} trades")

    print("\nInserting trades and updating positions/cash...")
    for bot in bots_data:
        bot_id = bot["bot_id"]
        name = bot["config"]["name"]
        inserted = 0
        for date_str, symbol, side, qty, reason in bot["schedule"]:
            if symbol not in bars_cache or date_str not in bars_cache[symbol]:
                continue
            price = bars_cache[symbol][date_str]
            try:
                insert_trade_and_update_position(conn, bot_id, date_str, symbol, side, qty, price, reason)
                inserted += 1
            except Exception as e:
                print(f"    Skip {name} {date_str} {symbol} {side}: {e}")
        print(f"  {name}: {inserted} trades inserted")

    conn.close()

    print("\n" + "=" * 70)
    print("Done. Next: python3 generate_snapshots.py")
    print("=" * 70)
    for bot in bots_data:
        print(f"  {bot['config']['name']}: http://localhost:3000/bot.html?id={bot['bot_id']}")


if __name__ == "__main__":
    main()
