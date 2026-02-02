#!/usr/bin/env python3
"""
Generate historical test data for BotTrade platform.
Creates backdated trades over the past 14 days to populate charts with realistic data.

Requirements:
    pip install requests psycopg2-binary python-dotenv

Usage:
    python3 generate_historical_data.py
"""

import requests
import time
import random
import sys
import os
from datetime import datetime, timedelta
import psycopg2
from dotenv import load_dotenv

load_dotenv()

BASE_URL = os.getenv("BASE_URL", "http://localhost:3000/api")
DB_HOST = os.getenv("DB_HOST", "localhost")
DB_NAME = os.getenv("DB_NAME", "bottrade")
DB_USER = os.getenv("DB_USER", "postgres")
DB_PASSWORD = os.getenv("DB_PASSWORD", "")

# Test bot configurations
TEST_BOTS = [
    {
        "name": "MomentumMaster",
        "description": "High-frequency momentum trader with aggressive rebalancing",
        "strategy": "aggressive_growth",
        "email": "momentum@test.com",
        "trade_frequency": 0.8  # Higher frequency
    },
    {
        "name": "ValueVulture",
        "description": "Long-term value investor focusing on fundamentals",
        "strategy": "conservative",
        "email": "value@test.com",
        "trade_frequency": 0.3  # Lower frequency
    },
    {
        "name": "TechTitan",
        "description": "Tech stock specialist riding innovation waves",
        "strategy": "tech_focused",
        "email": "tech@test.com",
        "trade_frequency": 0.5
    },
    {
        "name": "DipBuyer",
        "description": "Contrarian strategy buying weakness in quality names",
        "strategy": "buy_dips",
        "email": "dip@test.com",
        "trade_frequency": 0.6
    },
    {
        "name": "RandomWalker",
        "description": "Random algorithm baseline for performance comparison",
        "strategy": "random",
        "email": "random@test.com",
        "trade_frequency": 0.5
    },
    {
        "name": "SwingTrader",
        "description": "Captures multi-day price swings in volatile stocks",
        "strategy": "swing_trade",
        "email": "swing@test.com",
        "trade_frequency": 0.4
    },
    {
        "name": "AlgoScalper",
        "description": "High-frequency algorithmic scalping strategy",
        "strategy": "aggressive_growth",
        "email": "scalper@test.com",
        "trade_frequency": 0.9
    }
]

# Popular stock symbols
SYMBOLS = ["AAPL", "GOOGL", "MSFT", "TSLA", "AMZN", "NVDA", "META", "NFLX", "AMD", "INTC"]

def cleanup_test_bots():
    """Delete all existing test bots and their data."""
    try:
        conn = psycopg2.connect(
            host=DB_HOST,
            database=DB_NAME,
            user=DB_USER,
            password=DB_PASSWORD
        )
        cur = conn.cursor()

        # Delete test bots (CASCADE will delete related trades and positions)
        cur.execute("DELETE FROM bots WHERE is_test = true")
        conn.commit()

        count = cur.rowcount
        cur.close()
        conn.close()

        print(f"✓ Cleaned up {count} existing test bots")
        return True
    except Exception as e:
        print(f"⚠️  Could not clean up test bots: {e}")
        return False

def register_bot(bot_config):
    """Register a new bot and return API key and bot ID."""
    response = requests.post(
        f"{BASE_URL}/bots/register",
        json={
            "name": bot_config["name"],
            "description": bot_config["description"],
            "creator_email": bot_config["email"],
            "is_test": True
        }
    )

    if response.status_code == 201:
        data = response.json()
        print(f"✓ Registered {bot_config['name']}")
        return data["api_key"], data["bot_id"]
    else:
        print(f"✗ Failed to register {bot_config['name']}: {response.text}")
        return None, None

def claim_bot(bot_id):
    """Claim a bot."""
    response = requests.post(f"{BASE_URL}/claim/{bot_id}")
    if response.status_code == 200:
        print(f"✓ Claimed bot {bot_id}")
        return True
    else:
        print(f"✗ Failed to claim bot {bot_id}")
        return False

def backdate_trades_in_db(bot_id, days_ago_start, days_ago_end):
    """
    Backdate trades in the database for a specific bot.
    This spreads trades over a time range to create historical data.
    """
    try:
        conn = psycopg2.connect(
            host=DB_HOST,
            database=DB_NAME,
            user=DB_USER,
            password=DB_PASSWORD
        )
        cur = conn.cursor()

        # Get all trades for this bot ordered by executed_at
        cur.execute("""
            SELECT id, executed_at
            FROM trades
            WHERE bot_id = %s
            ORDER BY executed_at ASC
        """, (bot_id,))

        trades = cur.fetchall()

        if not trades:
            cur.close()
            conn.close()
            return 0

        # Spread trades evenly over the date range
        total_trades = len(trades)
        time_span_hours = (days_ago_start - days_ago_end) * 24

        for i, (trade_id, original_time) in enumerate(trades):
            # Calculate backdated timestamp
            hours_offset = (i / total_trades) * time_span_hours
            new_timestamp = datetime.now() - timedelta(days=days_ago_start) + timedelta(hours=hours_offset)

            # Add some randomness (±30 minutes)
            random_offset = random.randint(-30, 30)
            new_timestamp += timedelta(minutes=random_offset)

            # Update the trade timestamp
            cur.execute("""
                UPDATE trades
                SET executed_at = %s
                WHERE id = %s
            """, (new_timestamp, trade_id))

        conn.commit()
        cur.close()
        conn.close()

        return total_trades
    except Exception as e:
        print(f"  ⚠️  Error backdating trades: {e}")
        return 0

def generate_strategy_trades(strategy, trade_count):
    """Generate trade patterns based on strategy type."""
    trades = []

    if strategy == "aggressive_growth":
        # High frequency, lots of buys and sells
        for i in range(trade_count):
            if i % 3 == 0:  # Every 3rd trade is a sell
                symbol = random.choice(["TSLA", "NVDA", "AMD"])
                trades.append((symbol, "sell", random.randint(10, 30), "Taking profits on momentum"))
            else:
                symbol = random.choice(["TSLA", "NVDA", "AMD", "META"])
                trades.append((symbol, "buy", random.randint(10, 50), "Momentum breakout detected"))

    elif strategy == "conservative":
        # Fewer trades, mostly buys, blue chips
        for i in range(trade_count):
            if i % 5 == 0:  # Rarely sell
                symbol = random.choice(["AAPL", "MSFT"])
                trades.append((symbol, "sell", random.randint(5, 15), "Rebalancing position"))
            else:
                symbol = random.choice(["AAPL", "MSFT", "GOOGL"])
                trades.append((symbol, "buy", random.randint(15, 40), "Accumulating quality"))

    elif strategy == "tech_focused":
        # Tech stocks only
        tech_symbols = ["GOOGL", "MSFT", "NVDA", "META", "AMZN"]
        for i in range(trade_count):
            symbol = random.choice(tech_symbols)
            side = "sell" if i % 4 == 0 else "buy"
            trades.append((symbol, side, random.randint(15, 35), f"Tech sector {'rotation' if side == 'sell' else 'strength'}"))

    elif strategy == "buy_dips":
        # Buy on weakness, sell on strength
        volatile = ["TSLA", "NFLX", "META"]
        for i in range(trade_count):
            symbol = random.choice(volatile)
            if i % 4 == 0:
                trades.append((symbol, "sell", random.randint(10, 25), "Exit on recovery"))
            else:
                trades.append((symbol, "buy", random.randint(15, 30), "Buying the dip"))

    elif strategy == "swing_trade":
        # Hold for several days, then exit
        for i in range(trade_count):
            symbol = random.choice(SYMBOLS)
            side = "buy" if i % 2 == 0 else "sell"
            trades.append((symbol, side, random.randint(20, 40), f"Swing {'entry' if side == 'buy' else 'exit'}"))

    else:  # random
        for i in range(trade_count):
            symbol = random.choice(SYMBOLS)
            side = random.choice(["buy", "sell"])
            trades.append((symbol, side, random.randint(5, 30), "Algorithmic signal"))

    return trades

def execute_trade(api_key, symbol, side, quantity, reasoning):
    """Execute a trade."""
    response = requests.post(
        f"{BASE_URL}/trade/stock",
        headers={"X-API-Key": api_key},
        json={
            "symbol": symbol,
            "side": side,
            "quantity": quantity,
            "reasoning": reasoning
        }
    )

    if response.status_code == 200:
        return True
    else:
        return False

def main():
    """Generate historical test data."""
    print("=" * 70)
    print("BotTrade Historical Data Generator")
    print("=" * 70)
    print(f"This will create {len(TEST_BOTS)} bots with trades over the past 14 days")

    # Clean up existing test bots
    print("\n🧹 CLEANING UP OLD TEST DATA")
    print("-" * 70)
    cleanup_test_bots()

    bots_data = []

    # Register all bots
    print("\n📝 REGISTERING BOTS")
    print("-" * 70)
    for bot_config in TEST_BOTS:
        api_key, bot_id = register_bot(bot_config)
        if api_key:
            bots_data.append({
                "config": bot_config,
                "api_key": api_key,
                "bot_id": bot_id
            })
        time.sleep(0.1)

    # Claim all bots
    print("\n✋ CLAIMING BOTS")
    print("-" * 70)
    for bot in bots_data:
        claim_bot(bot["bot_id"])
        time.sleep(0.1)

    # Execute trades for each bot
    print("\n📈 GENERATING TRADES")
    print("-" * 70)

    for bot in bots_data:
        strategy = bot["config"]["strategy"]
        trade_frequency = bot["config"]["trade_frequency"]

        # Calculate trade count based on frequency (5-50 trades over 14 days)
        base_trades = int(20 * trade_frequency)
        trade_count = random.randint(base_trades, base_trades + 15)

        print(f"\n{bot['config']['name']} ({strategy})")
        print(f"  Generating {trade_count} trades...")

        trades = generate_strategy_trades(strategy, trade_count)

        # Execute all trades
        executed = 0
        for symbol, side, qty, reason in trades:
            if execute_trade(bot["api_key"], symbol, side, qty, reason):
                executed += 1
            time.sleep(0.05)  # Small delay to avoid rate limits

        print(f"  ✓ Executed {executed}/{trade_count} trades")

    # Backdate all trades
    print("\n⏰ BACKDATING TRADES")
    print("-" * 70)

    for bot in bots_data:
        print(f"{bot['config']['name']}...")
        # Spread trades over past 14 days
        count = backdate_trades_in_db(bot["bot_id"], days_ago_start=14, days_ago_end=0)
        print(f"  ✓ Backdated {count} trades over 14 days")

    # Display summary
    print("\n" + "=" * 70)
    print("✅ Historical data generation complete!")
    print("=" * 70)
    print(f"\n{len(bots_data)} bots created with trades spread over 14 days")
    print("\nNext steps:")
    print("  1. Run: python3 generate_snapshots.py")
    print("  2. Visit http://localhost:3000 to see populated charts")
    print("\nBot URLs:")
    for bot in bots_data:
        print(f"  • {bot['config']['name']}: http://localhost:3000/bot.html?id={bot['bot_id']}")

if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("\n\n⚠️  Interrupted by user")
    except Exception as e:
        print(f"\n\n❌ Error: {e}")
        import traceback
        traceback.print_exc()
