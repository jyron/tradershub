#!/usr/bin/env python3
"""
Generate portfolio snapshots for BotTrade platform using REAL historical stock prices.
Fetches actual historical prices from Finnhub API and calculates daily portfolio values.

Requirements:
    pip install psycopg2-binary python-dotenv requests

Usage:
    python3 generate_snapshots.py
"""

import psycopg2
from datetime import datetime, timedelta
import os
from dotenv import load_dotenv
import requests
import time

load_dotenv()

DB_HOST = os.getenv("DB_HOST", "localhost")
DB_NAME = os.getenv("DB_NAME", "bottrade")
DB_USER = os.getenv("DB_USER", "postgres")
DB_PASSWORD = os.getenv("DB_PASSWORD", "")
MARKET_API_KEY = os.getenv("MARKET_API_KEY")

if not MARKET_API_KEY or MARKET_API_KEY == "your_api_key_here":
    print("❌ Error: MARKET_API_KEY not set in .env file")
    print("Get your free API key from: https://finnhub.io/register")
    exit(1)

# Cache for historical prices to avoid repeated API calls
price_cache = {}

def get_historical_price(symbol, date):
    """
    Get the closing price for a symbol on a specific date using Finnhub API.
    Uses daily candles and returns the closing price for that day.
    """
    # Create cache key
    date_str = date.strftime('%Y-%m-%d')
    cache_key = f"{symbol}_{date_str}"

    if cache_key in price_cache:
        return price_cache[cache_key]

    # Convert date to unix timestamps (start and end of day)
    start_of_day = int(datetime.combine(date, datetime.min.time()).timestamp())
    end_of_day = int(datetime.combine(date, datetime.max.time()).timestamp())

    # Finnhub API endpoint for historical candles
    url = f"https://finnhub.io/api/v1/stock/candle"
    params = {
        'symbol': symbol,
        'resolution': 'D',  # Daily resolution
        'from': start_of_day,
        'to': end_of_day,
        'token': MARKET_API_KEY
    }

    try:
        response = requests.get(url, params=params, timeout=10)

        if response.status_code == 200:
            data = response.json()

            # Check if we got valid data
            if data.get('s') == 'ok' and data.get('c') and len(data['c']) > 0:
                # Get closing price (last element in array)
                price = float(data['c'][-1])
                price_cache[cache_key] = price
                print(f"    ✓ {symbol} on {date_str}: ${price:.2f}")
                return price
            else:
                print(f"    ⚠️  No data for {symbol} on {date_str}, trying fallback...")
                # Try to get the most recent price before this date
                return get_most_recent_price(symbol, date)
        else:
            print(f"    ⚠️  API error for {symbol}: {response.status_code}")
            return get_most_recent_price(symbol, date)

    except Exception as e:
        print(f"    ⚠️  Error fetching {symbol}: {e}")
        return get_most_recent_price(symbol, date)

    finally:
        # Rate limiting - Finnhub free tier: 60 calls/minute
        time.sleep(1.1)  # ~55 calls per minute to be safe

def get_most_recent_price(symbol, target_date):
    """
    If no price available for exact date (weekend/holiday), get most recent price.
    Goes back up to 7 days to find valid trading day.
    """
    for days_back in range(1, 8):
        check_date = target_date - timedelta(days=days_back)
        date_str = check_date.strftime('%Y-%m-%d')
        cache_key = f"{symbol}_{date_str}"

        if cache_key in price_cache:
            print(f"    → Using cached price from {date_str}")
            return price_cache[cache_key]

        start_ts = int(datetime.combine(check_date, datetime.min.time()).timestamp())
        end_ts = int(datetime.combine(check_date, datetime.max.time()).timestamp())

        url = f"https://finnhub.io/api/v1/stock/candle"
        params = {
            'symbol': symbol,
            'resolution': 'D',
            'from': start_ts,
            'to': end_ts,
            'token': MARKET_API_KEY
        }

        try:
            response = requests.get(url, params=params, timeout=10)
            if response.status_code == 200:
                data = response.json()
                if data.get('s') == 'ok' and data.get('c') and len(data['c']) > 0:
                    price = float(data['c'][-1])
                    price_cache[cache_key] = price
                    print(f"    → Found price from {date_str}: ${price:.2f}")
                    return price
        except:
            pass

        time.sleep(1.1)

    # If we still can't find a price, fail loudly
    raise Exception(f"Could not find any historical price for {symbol} near {target_date}")

def calculate_portfolio_value_at_date(conn, bot_id, target_date):
    """
    Calculate portfolio value for a bot at end of a specific date using REAL historical prices.
    Returns (total_value, cash_balance, positions_value, holdings, prices)
    """
    cur = conn.cursor()

    # Get all trades up to and including the target date
    cur.execute("""
        SELECT symbol, side, quantity, price, total_value, executed_at
        FROM trades
        WHERE bot_id = %s AND executed_at <= %s
        ORDER BY executed_at ASC
    """, (bot_id, target_date))

    trades = cur.fetchall()
    cur.close()

    # Calculate portfolio state
    cash_balance = 100000.0  # Starting balance
    holdings = {}  # symbol -> {quantity, total_cost}

    for symbol, side, quantity, price, total_value, executed_at in trades:
        # Convert Decimal to float
        total_value = float(total_value)
        price = float(price)

        if side == 'buy':
            cash_balance -= total_value
            if symbol not in holdings:
                holdings[symbol] = {'quantity': 0, 'total_cost': 0}
            holdings[symbol]['quantity'] += quantity
            holdings[symbol]['total_cost'] += total_value
        else:  # sell
            cash_balance += total_value
            if symbol in holdings:
                holdings[symbol]['quantity'] -= quantity
                # Reduce cost proportionally
                if holdings[symbol]['quantity'] > 0:
                    cost_per_share = holdings[symbol]['total_cost'] / (holdings[symbol]['quantity'] + quantity)
                    holdings[symbol]['total_cost'] = cost_per_share * holdings[symbol]['quantity']
                else:
                    del holdings[symbol]

    # Calculate positions value using REAL historical prices from Finnhub
    positions_value = 0.0
    current_prices = {}

    print(f"  Fetching historical prices for {target_date.date()}...")

    for symbol, holding in holdings.items():
        if holding['quantity'] > 0:
            # Get REAL historical price from Finnhub API
            price = get_historical_price(symbol, target_date.date())
            current_prices[symbol] = price
            positions_value += holding['quantity'] * price

    total_value = cash_balance + positions_value

    return total_value, cash_balance, positions_value, holdings, current_prices

def generate_snapshots_for_bot(conn, bot_id, bot_name):
    """Generate daily snapshots for a single bot using real historical prices."""
    cur = conn.cursor()

    # Get date range of trades for this bot
    cur.execute("""
        SELECT MIN(executed_at)::date, MAX(executed_at)::date
        FROM trades
        WHERE bot_id = %s
    """, (bot_id,))

    result = cur.fetchone()
    if not result or not result[0]:
        cur.close()
        print(f"  ⚠️  No trades found for {bot_name}")
        return 0

    first_trade_date, last_trade_date = result

    # Delete existing snapshots for this bot
    cur.execute("DELETE FROM portfolio_snapshots WHERE bot_id = %s", (bot_id,))
    conn.commit()

    print(f"  Creating snapshots from {first_trade_date} to {last_trade_date}...")

    # Generate snapshot for each day
    current_date = first_trade_date
    snapshots_created = 0
    previous_value = 100000.0  # Starting value

    while current_date <= last_trade_date:
        # Set snapshot time to end of day (23:59:59)
        snapshot_time = datetime.combine(current_date, datetime.max.time())

        # Calculate portfolio value at end of this day using REAL prices
        total_value, cash_balance, positions_value, holdings, prices = calculate_portfolio_value_at_date(
            conn, bot_id, snapshot_time
        )

        # Calculate daily P&L
        daily_pnl = total_value - previous_value
        total_pnl = total_value - 100000.0  # Total P&L from start

        # Insert snapshot
        cur.execute("""
            INSERT INTO portfolio_snapshots
            (bot_id, total_value, cash_balance, positions_value, daily_pnl, total_pnl, snapshot_at)
            VALUES (%s, %s, %s, %s, %s, %s, %s)
        """, (bot_id, total_value, cash_balance, positions_value, daily_pnl, total_pnl, snapshot_time))

        snapshots_created += 1
        previous_value = total_value

        # Move to next day
        current_date += timedelta(days=1)

    conn.commit()
    cur.close()

    return snapshots_created

def main():
    """Generate snapshots for all bots using REAL historical stock prices."""
    print("=" * 70)
    print("BotTrade Portfolio Snapshot Generator (Using Real Historical Prices)")
    print("=" * 70)
    print(f"API Key: {MARKET_API_KEY[:10]}...{MARKET_API_KEY[-4:]}")

    try:
        # Connect to database
        conn = psycopg2.connect(
            host=DB_HOST,
            database=DB_NAME,
            user=DB_USER,
            password=DB_PASSWORD
        )

        # Get all bots with trades
        cur = conn.cursor()
        cur.execute("""
            SELECT DISTINCT b.id, b.name
            FROM bots b
            INNER JOIN trades t ON b.id = t.bot_id
            ORDER BY b.name
        """)

        bots = cur.fetchall()
        cur.close()

        if not bots:
            print("\n⚠️  No bots with trades found")
            conn.close()
            return

        print(f"\n📊 Found {len(bots)} bots with trading history")
        print("-" * 70)
        print("\n⚠️  This will make API calls to Finnhub for historical prices")
        print("⚠️  Rate limit: ~55 calls per minute (free tier)")

        total_snapshots = 0

        for bot_id, bot_name in bots:
            print(f"\n{bot_name}")
            count = generate_snapshots_for_bot(conn, bot_id, bot_name)
            print(f"  ✓ Created {count} daily snapshots")
            total_snapshots += count

        conn.close()

        # Summary
        print("\n" + "=" * 70)
        print("✅ Snapshot generation complete!")
        print("=" * 70)
        print(f"\nTotal snapshots created: {total_snapshots}")
        print(f"Bots processed: {len(bots)}")
        print(f"Prices cached: {len(price_cache)} symbols/dates")
        print("\nAll snapshots use REAL historical stock prices from Finnhub API")
        print("Visit http://localhost:3000 to see accurate historical charts")

    except Exception as e:
        print(f"\n❌ Error: {e}")
        import traceback
        traceback.print_exc()

if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("\n\n⚠️  Interrupted by user")
