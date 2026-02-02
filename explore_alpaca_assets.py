#!/usr/bin/env python3
"""
Explore Alpaca assets (US equities) and option contracts.
Answers:
  1. How many stocks are in the assets list?
  2. How much DB space would it take to store this?
  3. Do you have to store historical data?

Requirements: pip install alpaca-py python-dotenv

Usage: python3 explore_alpaca_assets.py
"""

import os
import sys

from dotenv import load_dotenv

load_dotenv()

ALPACA_API_KEY = os.getenv("ALPACA_API_KEY")
ALPACA_SECRET_KEY = os.getenv("ALPACA_SECRET_KEY")

if not ALPACA_API_KEY or not ALPACA_SECRET_KEY:
    print("Error: ALPACA_API_KEY and ALPACA_SECRET_KEY must be set in .env")
    sys.exit(1)


def main():
    print("=" * 60)
    print("Alpaca Assets & Contracts Explorer")
    print("=" * 60)

    try:
        from alpaca.trading.client import TradingClient
        from alpaca.trading.requests import GetAssetsRequest, GetOptionContractsRequest
        from alpaca.trading.enums import AssetClass
    except ImportError as e:
        print("Error: alpaca-py required. pip install alpaca-py")
        print(e)
        sys.exit(1)

    client = TradingClient(ALPACA_API_KEY, ALPACA_SECRET_KEY)

    # --- 1. US Equity Assets (stocks) ---
    print("\n1. US EQUITY ASSETS (stocks)")
    print("-" * 40)
    try:
        req = GetAssetsRequest(asset_class=AssetClass.US_EQUITY)
        assets = client.get_all_assets(req)
        count_stocks = len(assets)
        print(f"   Total stocks (US equity): {count_stocks:,}")

        if assets:
            a = assets[0]
            print(f"   Sample: {getattr(a, 'symbol', '?')} | {getattr(a, 'name', '?')[:50]}")
            tradeable = sum(1 for a in assets if getattr(a, "tradable", True))
            print(f"   Tradable: {tradeable:,} | Not tradable: {count_stocks - tradeable:,}")
    except Exception as e:
        print(f"   Error: {e}")
        count_stocks = 0

    # --- 2. Option contracts (sample: AAPL) ---
    print("\n2. OPTION CONTRACTS (sample AAPL)")
    print("-" * 40)
    count_contracts = 0
    try:
        opt_req = GetOptionContractsRequest(underlying_symbols=["AAPL"], limit=500)
        response = client.get_option_contracts(opt_req)
        count_contracts = len(response.option_contracts or [])
        print(f"   AAPL contracts returned: {count_contracts:,}")
        print("   (Total options across all underlyings is typically 100k–500k+)")
    except Exception as e:
        print(f"   Error: {e} (options may need options-enabled account)")

    # --- 3. DB size estimates ---
    print("\n3. DATABASE SIZE ESTIMATES")
    print("-" * 40)
    bytes_per_asset = 350  # symbol, name, exchange, etc.
    bytes_quote = 60  # symbol, price, bid, ask, updated_at
    bytes_hist_row = 72  # symbol, date, o,h,l,c, volume

    size_assets_mb = (count_stocks * bytes_per_asset) / (1024 * 1024)
    size_quotes_mb = (count_stocks * bytes_quote) / (1024 * 1024)
    print(f"   Assets table (metadata only):     {count_stocks:,} rows  ~{size_assets_mb:.2f} MB")
    print(f"   Current quotes (one row/symbol):  {count_stocks:,} rows  ~{size_quotes_mb:.2f} MB")
    print(f"   Total (assets + current only):    ~{size_assets_mb + size_quotes_mb:.2f} MB")

    rows_1y = count_stocks * 252
    size_hist_mb = (rows_1y * bytes_hist_row) / (1024 * 1024)
    print(f"   + 1 year daily history:           {rows_1y:,} rows  ~{size_hist_mb:.2f} MB")

    if count_contracts > 0:
        bytes_contract = 140
        print(f"   Option contracts (AAPL sample):   {count_contracts:,} rows  ~{count_contracts * bytes_contract / 1024:.2f} KB")

    # --- 4. Do you have to store historical data? ---
    print("\n4. DO YOU HAVE TO STORE HISTORICAL DATA?")
    print("-" * 40)
    print("   No.")
    print("   - Current-only: one row per symbol, updated by a background job.")
    print("   - GET /market/quote reads from DB. Minimal space (few MB).")
    print("   - Historical (daily bars) only if you need charts/backtesting.")
    print("   - You can add history later; start with current-only.")
    print("=" * 60)


if __name__ == "__main__":
    main()
