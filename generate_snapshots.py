#!/usr/bin/env python3
"""
Generate portfolio snapshots for BotTrade platform using REAL historical stock prices.
Fetches actual historical daily bars from Alpaca Market Data API (batched by date).
Rate limit: 200 Alpaca API calls/minute.

Requirements:
    pip install alpaca-py psycopg2-binary python-dotenv

Usage:
    python3 generate_snapshots.py
"""

import psycopg2
from datetime import datetime, timedelta
import os
import time
from dotenv import load_dotenv

from alpaca.data.historical.stock import StockHistoricalDataClient
from alpaca.data.requests import StockBarsRequest
from alpaca.data.timeframe import TimeFrame

load_dotenv()

DB_HOST = os.getenv("DB_HOST", "localhost")
DB_NAME = os.getenv("DB_NAME", "bottrade")
DB_USER = os.getenv("DB_USER", "postgres")
DB_PASSWORD = os.getenv("DB_PASSWORD", "")
ALPACA_API_KEY = os.getenv("ALPACA_API_KEY")
ALPACA_SECRET_KEY = os.getenv("ALPACA_SECRET_KEY")

if not ALPACA_API_KEY or not ALPACA_SECRET_KEY:
    print("Error: ALPACA_API_KEY and ALPACA_SECRET_KEY must be set in .env")
    print("Get credentials from: https://alpaca.markets")
    exit(1)

ALPACA_RATE_DELAY = 60 / 200  # 200 calls per minute

_client = None

def _get_client():
    global _client
    if _client is None:
        _client = StockHistoricalDataClient(ALPACA_API_KEY, ALPACA_SECRET_KEY)
    return _client

# Cache: (symbol, date_str) -> price. Populated by batch fetch per date.
price_cache = {}


def get_holdings_at_date(conn, bot_id, target_date):
    """Return dict symbol -> quantity held at end of target_date (from trades only)."""
    cur = conn.cursor()
    cur.execute("""
        SELECT symbol, side, quantity, total_value
        FROM trades
        WHERE bot_id = %s AND executed_at <= %s
        ORDER BY executed_at ASC
    """, (bot_id, target_date))
    rows = cur.fetchall()
    cur.close()
    cash = 100000.0
    holdings = {}
    for symbol, side, quantity, total_value in rows:
        total_value = float(total_value)
        if side == "buy":
            cash -= total_value
            holdings[symbol] = holdings.get(symbol, 0) + quantity
        else:
            cash += total_value
            holdings[symbol] = holdings.get(symbol, 0) - quantity
            if holdings[symbol] <= 0:
                del holdings[symbol]
    return {s: q for s, q in holdings.items() if q > 0}, cash


def get_all_symbols_needed_for_date(conn, target_date):
    """Return set of symbols held by any bot at end of target_date."""
    cur = conn.cursor()
    cur.execute("""
        SELECT DISTINCT b.id
        FROM bots b
        INNER JOIN trades t ON b.id = t.bot_id
        WHERE t.executed_at <= %s
    """, (target_date,))
    bot_ids = [r[0] for r in cur.fetchall()]
    cur.close()
    symbols = set()
    for bot_id in bot_ids:
        holdings, _ = get_holdings_at_date(conn, bot_id, target_date)
        symbols.update(holdings.keys())
    return symbols


def fetch_prices_for_date(symbols, date_obj):
    """One Alpaca call for all symbols on date_obj; populate price_cache. Throttle after call."""
    if not symbols:
        return
    date_str = date_obj.strftime("%Y-%m-%d")
    start = datetime(date_obj.year, date_obj.month, date_obj.day, 0, 0, 0)
    end = datetime(date_obj.year, date_obj.month, date_obj.day, 23, 59, 59)
    req = StockBarsRequest(
        symbol_or_symbols=list(symbols),
        start=start,
        end=end,
        timeframe=TimeFrame.Day,
    )
    client = _get_client()
    bars = client.get_stock_bars(req)
    if bars.data:
        for symbol, symbol_bars in bars.data.items():
            if symbol_bars and len(symbol_bars) > 0:
                price_cache[f"{symbol}_{date_str}"] = float(symbol_bars[0].close)
    time.sleep(ALPACA_RATE_DELAY)


def portfolio_value_at_date_from_cache(conn, bot_id, target_date):
    """
    Compute portfolio value at target_date using only price_cache.
    Returns (total_value, cash_balance, positions_value, holdings, current_prices).
    Raises if any needed symbol/date is missing from cache.
    """
    holdings, cash_balance = get_holdings_at_date(conn, bot_id, target_date)
    date_str = target_date.strftime("%Y-%m-%d")
    positions_value = 0.0
    current_prices = {}
    for symbol, qty in holdings.items():
        key = f"{symbol}_{date_str}"
        if key not in price_cache:
            raise Exception(f"No daily bar from Alpaca for {symbol} on {date_str}")
        price = price_cache[key]
        current_prices[symbol] = price
        positions_value += qty * price
    total_value = cash_balance + positions_value
    return total_value, cash_balance, positions_value, holdings, current_prices


def get_global_date_range(conn):
    """Return (first_date, last_date) across all bots' trades."""
    cur = conn.cursor()
    cur.execute("""
        SELECT MIN(executed_at)::date, MAX(executed_at)::date
        FROM trades
    """)
    row = cur.fetchone()
    cur.close()
    if not row or not row[0]:
        return None, None
    return row[0], row[1]


def get_bots_with_trades(conn):
    """Return list of (bot_id, bot_name)."""
    cur = conn.cursor()
    cur.execute("""
        SELECT DISTINCT b.id, b.name
        FROM bots b
        INNER JOIN trades t ON b.id = t.bot_id
        ORDER BY b.name
    """)
    rows = cur.fetchall()
    cur.close()
    return rows


def generate_snapshots_batched(conn):
    """
    One pass over all trading days in global range. Per day: one Alpaca call for all
    symbols needed, then for each bot compute value from cache and insert snapshot.
    """
    first_date, last_date = get_global_date_range(conn)
    if not first_date or not last_date:
        return 0

    bots = get_bots_with_trades(conn)
    if not bots:
        return 0

    # Delete existing snapshots for these bots
    cur = conn.cursor()
    for bot_id, _ in bots:
        cur.execute("DELETE FROM portfolio_snapshots WHERE bot_id = %s", (bot_id,))
    conn.commit()

    # Collect all trading days in range
    trading_days = []
    d = first_date
    while d <= last_date:
        if d.weekday() < 5:
            trading_days.append(d)
        d += timedelta(days=1)

    print(f"  Date range: {first_date} to {last_date} ({len(trading_days)} trading days)")
    print(f"  Rate limit: 200/min (throttled after each day)")

    total_snapshots = 0
    previous_value_by_bot = {bot_id: 100000.0 for bot_id, _ in bots}

    for i, day in enumerate(trading_days):
        symbols = get_all_symbols_needed_for_date(conn, datetime.combine(day, datetime.max.time()))
        if symbols:
            try:
                fetch_prices_for_date(symbols, day)
            except Exception as e:
                print(f"  Skipping {day} (fetch error): {e}")
                continue

        snapshot_time = datetime.combine(day, datetime.max.time())
        day_created = 0

        for bot_id, bot_name in bots:
            try:
                total_value, cash_balance, positions_value, holdings, _ = portfolio_value_at_date_from_cache(
                    conn, bot_id, snapshot_time
                )
            except Exception as e:
                if "No daily bar" in str(e):
                    continue
                raise

            previous_value = previous_value_by_bot[bot_id]
            daily_pnl = total_value - previous_value
            total_pnl = total_value - 100000.0
            previous_value_by_bot[bot_id] = total_value

            cur.execute("""
                INSERT INTO portfolio_snapshots
                (bot_id, total_value, cash_balance, positions_value, daily_pnl, total_pnl, snapshot_at)
                VALUES (%s, %s, %s, %s, %s, %s, %s)
            """, (bot_id, total_value, cash_balance, positions_value, daily_pnl, total_pnl, snapshot_time))
            day_created += 1
            total_snapshots += 1

        if (i + 1) % 50 == 0:
            print(f"  Processed {i + 1}/{len(trading_days)} days, {total_snapshots} snapshots so far")

    conn.commit()
    cur.close()
    return total_snapshots


def main():
    print("=" * 70)
    print("BotTrade Portfolio Snapshot Generator (Alpaca Market Data)")
    print("=" * 70)
    print("Using Alpaca Market Data API, batched by date (200/min)")

    try:
        conn = psycopg2.connect(
            host=DB_HOST,
            database=DB_NAME,
            user=DB_USER,
            password=DB_PASSWORD,
        )

        bots = get_bots_with_trades(conn)
        if not bots:
            print("\nNo bots with trades found")
            conn.close()
            return

        print(f"\nFound {len(bots)} bots with trading history")
        print("-" * 70)

        for bot_id, bot_name in bots:
            print(f"  {bot_name}")
        print()

        total_snapshots = generate_snapshots_batched(conn)
        conn.close()

        print("\n" + "=" * 70)
        print("Snapshot generation complete!")
        print("=" * 70)
        print(f"\nTotal snapshots created: {total_snapshots}")
        print(f"Bots processed: {len(bots)}")
        print(f"Prices cached: {len(price_cache)} symbol/date pairs")
        print("\nAll snapshots use Alpaca Market Data API historical daily bars")
        print("Visit http://localhost:3000 to see historical charts")

    except Exception as e:
        print(f"\nError: {e}")
        import traceback
        traceback.print_exc()


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("\n\nInterrupted by user")
