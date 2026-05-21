#!/usr/bin/env python3
"""
Runner for synthetic baseline bots.

Unlike the LLM bots, baselines:
  - don't go through common.run_cli / replay_loop, because equal_weight needs
    to place up to 15 actions on day 0 and the LLM loop slices to 3.
  - reuse common's data-fetch + DB helpers, just with their own loop.

Usage:
  python -m bots.baselines.runner --strategy spy_buyandhold --replay 30
  python -m bots.baselines.runner --strategy equal_weight    --live
  python -m bots.baselines.runner --strategy random_walker   --once

The strategy name doubles as the provider key used by load_bot_key, which
looks up bots/.keys/<strategy>.json (written by scripts/seed_baselines.py).
"""
from __future__ import annotations

import argparse
import os
import sys
from datetime import datetime, timezone

import requests

from bots import common
from bots.baselines.strategies import STRATEGIES


def replay_loop(strategy_name: str, bot_id: str, days: int, limit: int | None = None) -> None:
    fn = STRATEGIES[strategy_name]
    print(f"[{strategy_name}] REPLAY: fetching {days} days of Alpaca bars…")
    client = common.alpaca_client()
    bars = common.fetch_daily_bars(client, days)
    dates = common.trading_days(bars)
    if limit:
        dates = dates[-limit:]
    print(f"[{strategy_name}] {len(dates)} trading days: {dates[0]} → {dates[-1]}")

    con = common._open_db()
    # Safeguard: the SQLite path is whatever BOTTRADE_DB (or the repo default)
    # resolves to, but the bot might only exist in a different DB (Turso /
    # smoke DB / production). Refuse to write orphan trades into a file
    # that doesn't know about this bot. The fix is to set BOTTRADE_DB to the
    # same file the Go server opened.
    row = con.execute("SELECT 1 FROM bots WHERE id = ?", (bot_id,)).fetchone()
    if row is None:
        common.die(
            f"bot_id {bot_id} does not exist in {common.DEFAULT_DB_PATH!r}.\n"
            f"set BOTTRADE_DB to the SQLite file the server is using "
            f"(or run the server with TURSO_DATABASE_URL=file:./bottrade.db)."
        )
    print(f"[{strategy_name}] wiping prior trades/positions/snapshots in {common.DEFAULT_DB_PATH}…")
    con.execute("DELETE FROM trades WHERE bot_id = ? AND season_id IS NULL", (bot_id,))
    con.execute("DELETE FROM positions WHERE bot_id = ? AND season_id IS NULL", (bot_id,))
    con.execute("DELETE FROM portfolio_snapshots WHERE bot_id = ? AND season_id IS NULL", (bot_id,))
    con.execute("UPDATE bots SET cash_balance = ? WHERE id = ?", (common.STARTING_BALANCE, bot_id))

    for day_idx, date_str in enumerate(dates):
        as_of = {sym: bars[sym][date_str] for sym in common.UNIVERSE if date_str in bars[sym]}
        if len(as_of) < 5:
            continue

        portfolio = common.replay_portfolio(con, bot_id, as_of)
        snap_dt = datetime.strptime(date_str, "%Y-%m-%d").replace(hour=20, minute=0)
        decisions = fn(day_idx, as_of, portfolio)

        symbols_today: set[str] = set()
        for t_idx, decision in enumerate(decisions):
            if decision.action == "hold":
                # log holds for the first day only — otherwise the output is noisy
                if day_idx == 0:
                    print(f"  [{date_str}] HOLD  | {decision.reasoning[:80]}")
                continue
            if decision.symbol in symbols_today:
                continue

            # Same safety clamps replay_loop applies, sans the 3-action limit.
            if decision.action == "buy" and decision.symbol and decision.quantity:
                price = as_of.get(decision.symbol, 0)
                if price > 0 and decision.quantity * price > portfolio.cash:
                    decision.quantity = max(0, int(portfolio.cash / price))
                if decision.quantity <= 0:
                    continue
            elif decision.action == "sell" and decision.symbol:
                pos = portfolio.position(decision.symbol)
                if pos is None or pos["quantity"] <= 0:
                    continue
                if decision.quantity and decision.quantity > pos["quantity"]:
                    decision.quantity = pos["quantity"]

            price = as_of.get(decision.symbol, 0)
            if price <= 0 or not decision.quantity:
                continue
            exec_time = datetime.strptime(date_str, "%Y-%m-%d").replace(
                hour=15, minute=(30 + t_idx) % 60
            )
            common.record_trade_db(con, bot_id, decision, price, exec_time)
            symbols_today.add(decision.symbol)
            portfolio = common.replay_portfolio(con, bot_id, as_of)
            if day_idx % 5 == 0 or day_idx < 2:
                print(f"  [{date_str}] {decision.action.upper()} {decision.quantity} "
                      f"{decision.symbol} @ ${price:.2f}  | port=${portfolio.total_value:,.0f}")

        portfolio_after = common.replay_portfolio(con, bot_id, as_of)
        common.record_snapshot_db(con, bot_id, portfolio_after.total_value, snap_dt)

    final = common.replay_portfolio(con, bot_id, as_of)
    print(f"\n[{strategy_name}] REPLAY DONE.")
    print(f"  final value: ${final.total_value:,.2f}  "
          f"({(final.total_value / common.STARTING_BALANCE - 1) * 100:+.2f}%)")
    print(f"  positions: {len(final.positions)}  cash: ${final.cash:,.2f}")


def live_once(strategy_name: str, api_key: str, dry_run: bool = False) -> None:
    """One pass of the strategy against today's prices via the HTTP API."""
    fn = STRATEGIES[strategy_name]
    print(f"[{strategy_name}] LIVE: fetching today's snapshot…")
    client = common.alpaca_client()
    prices = common.fetch_latest_quotes(client)
    if len(prices) < 5:
        common.die("alpaca returned too few quotes; aborting")

    r = requests.get(f"{common.BASE_URL}/api/portfolio",
                     headers={"X-API-Key": api_key}, timeout=10)
    if r.status_code != 200:
        common.die(f"could not fetch portfolio: HTTP {r.status_code} {r.text[:200]}")
    p = r.json()
    positions = {
        pos["symbol"]: {"quantity": int(pos["quantity"]), "avg_cost": float(pos["avg_cost"])}
        for pos in p.get("positions", [])
    }
    portfolio = common.Portfolio(
        cash=float(p["cash_balance"]),
        positions=positions,
        total_value=float(p["total_value"]),
    )
    decisions = fn(0, prices, portfolio)
    print(f"[{strategy_name}] {len(decisions)} decision(s):")
    for d in decisions:
        print(f"  {d}")
    if dry_run:
        print(f"[{strategy_name}] --once: not posting trade(s)")
        return
    for decision in decisions:
        if decision.action == "hold":
            continue
        result = common.post_live_trade(api_key, decision)
        print(f"[{strategy_name}] result: {result}")


def main() -> None:
    parser = argparse.ArgumentParser(description="bottrade baseline runner")
    parser.add_argument("--strategy", required=True, choices=list(STRATEGIES.keys()))
    parser.add_argument("--replay", type=int, metavar="N",
                        help="Replay last N trading days; writes trades + snapshots directly.")
    parser.add_argument("--live", action="store_true", help="One pass against live market via HTTP API.")
    parser.add_argument("--once", action="store_true", help="Dry-run live (no trades posted).")
    parser.add_argument("--limit-days", type=int, default=None,
                        help="(replay only) cap to last N days of the fetched window")
    args = parser.parse_args()

    if not args.replay and not args.live and not args.once:
        parser.print_help()
        sys.exit(1)

    keyrec = common.load_bot_key(args.strategy)
    print(f"[{args.strategy}] bot_id={keyrec['bot_id']}")

    if args.replay:
        replay_loop(args.strategy, keyrec["bot_id"], args.replay, limit=args.limit_days)
    else:
        live_once(args.strategy, keyrec["api_key"], dry_run=args.once)


if __name__ == "__main__":
    main()
