#!/usr/bin/env python3
"""
Generate test data for BotTrade platform.
Creates multiple bots with realistic trading patterns to test charts, leaderboards, and dashboards.
"""

import requests
import time
import random
from datetime import datetime

BASE_URL = "http://localhost:3000/api"

# Test bot configurations
TEST_BOTS = [
    {
        "name": "MomentumMaster",
        "description": "High-frequency momentum trader",
        "strategy": "aggressive_growth",
        "email": "momentum@test.com"
    },
    {
        "name": "ValueVulture",
        "description": "Long-term value investor",
        "strategy": "conservative",
        "email": "value@test.com"
    },
    {
        "name": "TechTitan",
        "description": "Tech stock specialist",
        "strategy": "tech_focused",
        "email": "tech@test.com"
    },
    {
        "name": "DipBuyer",
        "description": "Contrarian dip buyer",
        "strategy": "buy_dips",
        "email": "dip@test.com"
    },
    {
        "name": "RandomWalker",
        "description": "Random trading algorithm (for comparison)",
        "strategy": "random",
        "email": "random@test.com"
    }
]

# Popular stock symbols for testing
SYMBOLS = ["AAPL", "GOOGL", "MSFT", "TSLA", "AMZN", "NVDA", "META", "NFLX"]

def register_bot(bot_config):
    """Register a new bot and return API key and claim URL."""
    response = requests.post(
        f"{BASE_URL}/bots/register",
        json={
            "name": bot_config["name"],
            "description": bot_config["description"],
            "creator_email": bot_config["email"]
        }
    )

    if response.status_code == 201:
        data = response.json()
        print(f"✓ Registered {bot_config['name']}")
        print(f"  Bot ID: {data['bot_id']}")
        print(f"  Claim URL: {data['claim_url']}")
        return data["api_key"], data["bot_id"], data["claim_url"]
    else:
        print(f"✗ Failed to register {bot_config['name']}: {response.text}")
        return None, None, None

def claim_bot(bot_id):
    """Claim a bot."""
    response = requests.post(f"{BASE_URL}/claim/{bot_id}")
    if response.status_code == 200:
        print(f"✓ Claimed bot {bot_id}")
        return True
    else:
        print(f"✗ Failed to claim bot {bot_id}: {response.text}")
        return False

def get_quote(symbol):
    """Get current market quote for a symbol."""
    response = requests.get(f"{BASE_URL}/market/quote/{symbol}")
    if response.status_code == 200:
        return response.json()
    return None

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
        data = response.json()
        print(f"  ✓ {side.upper()} {quantity} {symbol} @ ${data['price']:.2f}")
        return True
    else:
        print(f"  ✗ Trade failed: {response.text}")
        return False

def get_portfolio(api_key):
    """Get current portfolio."""
    response = requests.get(
        f"{BASE_URL}/portfolio",
        headers={"X-API-Key": api_key}
    )
    if response.status_code == 200:
        return response.json()
    return None

def aggressive_growth_strategy(api_key, bot_name):
    """Execute aggressive growth trading pattern."""
    print(f"\n{bot_name} - Aggressive Growth Strategy")
    trades = [
        ("TSLA", "buy", 50, "High growth potential in EV market"),
        ("NVDA", "buy", 30, "AI chip demand accelerating"),
        ("TSLA", "sell", 25, "Taking partial profits on rally"),
        ("META", "buy", 20, "Metaverse opportunity undervalued"),
        ("NVDA", "buy", 20, "Doubling down on AI thesis"),
        ("TSLA", "sell", 25, "Exiting position on weakness"),
        ("AAPL", "buy", 40, "Safe haven rebalancing"),
    ]

    for symbol, side, qty, reason in trades:
        execute_trade(api_key, symbol, side, qty, reason)
        time.sleep(0.5)

def conservative_strategy(api_key, bot_name):
    """Execute conservative value investing pattern."""
    print(f"\n{bot_name} - Conservative Value Strategy")
    trades = [
        ("AAPL", "buy", 30, "Blue chip with strong fundamentals"),
        ("MSFT", "buy", 25, "Enterprise cloud leader"),
        ("GOOGL", "buy", 15, "Search dominance + cloud growth"),
        ("AAPL", "buy", 20, "Accumulating on dip"),
    ]

    for symbol, side, qty, reason in trades:
        execute_trade(api_key, symbol, side, qty, reason)
        time.sleep(0.5)

def tech_focused_strategy(api_key, bot_name):
    """Execute tech-focused trading pattern."""
    print(f"\n{bot_name} - Tech Focused Strategy")
    trades = [
        ("GOOGL", "buy", 40, "Cloud + AI leadership"),
        ("MSFT", "buy", 35, "Azure growth trajectory"),
        ("NVDA", "buy", 25, "GPU market dominance"),
        ("META", "buy", 30, "Ad revenue recovery"),
        ("GOOGL", "sell", 20, "Profit taking"),
        ("AMZN", "buy", 20, "AWS margin expansion"),
    ]

    for symbol, side, qty, reason in trades:
        execute_trade(api_key, symbol, side, qty, reason)
        time.sleep(0.5)

def buy_dips_strategy(api_key, bot_name):
    """Execute dip buying pattern."""
    print(f"\n{bot_name} - Buy the Dip Strategy")
    trades = [
        ("TSLA", "buy", 20, "Buying weakness in EV sector"),
        ("NFLX", "buy", 15, "Content library undervalued"),
        ("META", "buy", 25, "Privacy concerns overblown"),
        ("TSLA", "buy", 20, "Averaging down on dip"),
        ("NFLX", "sell", 10, "Partial exit on recovery"),
    ]

    for symbol, side, qty, reason in trades:
        execute_trade(api_key, symbol, side, qty, reason)
        time.sleep(0.5)

def random_strategy(api_key, bot_name):
    """Execute random trades (control group)."""
    print(f"\n{bot_name} - Random Walk Strategy")

    for _ in range(8):
        symbol = random.choice(SYMBOLS)
        side = random.choice(["buy", "sell"])
        qty = random.randint(5, 30)
        reason = random.choice([
            "Algorithmic signal detected",
            "Statistical arbitrage opportunity",
            "Mean reversion play",
            "Momentum breakout",
            "Technical indicator alignment"
        ])

        execute_trade(api_key, symbol, side, qty, reason)
        time.sleep(0.5)

def main():
    """Generate test data for all bots."""
    print("=" * 60)
    print("BotTrade Test Data Generator")
    print("=" * 60)

    bots_data = []

    # Register all bots
    print("\n📝 REGISTERING BOTS")
    print("-" * 60)
    for bot_config in TEST_BOTS:
        api_key, bot_id, claim_url = register_bot(bot_config)
        if api_key:
            bots_data.append({
                "config": bot_config,
                "api_key": api_key,
                "bot_id": bot_id,
                "claim_url": claim_url
            })
        time.sleep(0.2)

    # Claim all bots
    print("\n✋ CLAIMING BOTS")
    print("-" * 60)
    for bot in bots_data:
        claim_bot(bot["bot_id"])
        time.sleep(0.2)

    # Execute trades based on strategy
    print("\n📈 EXECUTING TRADES")
    print("-" * 60)

    strategies = {
        "aggressive_growth": aggressive_growth_strategy,
        "conservative": conservative_strategy,
        "tech_focused": tech_focused_strategy,
        "buy_dips": buy_dips_strategy,
        "random": random_strategy
    }

    for bot in bots_data:
        strategy = bot["config"]["strategy"]
        if strategy in strategies:
            strategies[strategy](bot["api_key"], bot["config"]["name"])
        time.sleep(0.5)

    # Display final results
    print("\n" + "=" * 60)
    print("📊 FINAL PORTFOLIO SUMMARY")
    print("=" * 60)

    results = []
    for bot in bots_data:
        portfolio = get_portfolio(bot["api_key"])
        if portfolio:
            results.append({
                "name": bot["config"]["name"],
                "value": portfolio["total_value"],
                "pnl": portfolio["total_pnl"],
                "pnl_percent": portfolio["total_pnl_percent"]
            })

    # Sort by performance
    results.sort(key=lambda x: x["value"], reverse=True)

    print(f"\n{'Rank':<6}{'Bot Name':<20}{'Portfolio Value':<20}{'P&L':<15}{'Return':<10}")
    print("-" * 70)

    for i, result in enumerate(results, 1):
        pnl_sign = "+" if result["pnl"] >= 0 else ""
        value_str = f"${result['value']:,.2f}"
        pnl_str = f"{pnl_sign}${result['pnl']:,.2f}"
        return_str = f"{pnl_sign}{result['pnl_percent']:.2f}%"

        print(f"{i:<6}{result['name']:<20}{value_str:<20}{pnl_str:<15}{return_str:<10}")

    print("\n" + "=" * 60)
    print("✅ Test data generation complete!")
    print("=" * 60)
    print("\nVisit http://localhost:3000 to see the live dashboard")
    print("Visit http://localhost:3000/leaderboard.html to see rankings")
    print("\nBot Profile URLs:")
    for bot in bots_data:
        print(f"  {bot['config']['name']}: http://localhost:3000/bot.html?id={bot['bot_id']}")

if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("\n\n⚠️  Interrupted by user")
    except Exception as e:
        print(f"\n\n❌ Error: {e}")
        import traceback
        traceback.print_exc()
