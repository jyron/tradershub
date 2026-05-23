#!/usr/bin/env python3
"""
test_bot.py — reference test bot for the BotTrade Benchmark API.

A complete, self-contained agent that exercises every endpoint correctly.
Read this top to bottom — it IS the canonical example of how the API is
meant to be used.

Requirements:
    pip install requests

Usage:
    # Get a key (no signup, no body required):
    #   curl -X POST https://bot-trade.org/api/v1/keys
    export BOT_API_KEY=...
    python test_bot.py              # uses default scenario + strategy
    python test_bot.py --scenario tech-2024-q2 --strategy equal_weight
    python test_bot.py --strategy buy_hold --symbol AAPL
    python test_bot.py --list-scenarios

Strategies:
    buy_hold      - Buy one symbol on bar 1, hold to end.
    equal_weight  - Open equal-weight positions in all universe symbols,
                    rebalance every 50 bars.
    momentum      - Each step, hold the symbol with the strongest
                    20-bar return; flip if leadership changes.
    random        - Random buy/sell on a random symbol each step (for
                    smoke-testing only; do not expect a positive return).
"""

from __future__ import annotations

import argparse
import os
import random
import sys
import time
import uuid
from typing import Any

try:
    import requests
except ImportError:
    sys.exit("error: requests not installed. run: pip install requests")


API_BASE = os.environ.get("BOTTRADE_API", "https://bot-trade.org")
DEFAULT_SCENARIO = "tech-2024-q2"


# -----------------------------------------------------------------------------
# Thin API client. Every method maps 1:1 to an HTTP endpoint.
# -----------------------------------------------------------------------------

class APIError(RuntimeError):
    def __init__(self, status: int, detail: str, body: Any):
        super().__init__(f"HTTP {status}: {detail}")
        self.status = status
        self.detail = detail
        self.body = body


class BotTradeClient:
    def __init__(self, api_key: str, base: str = API_BASE):
        self.base = base.rstrip("/")
        self.s = requests.Session()
        self.s.headers["X-API-Key"] = api_key
        self.s.headers["User-Agent"] = "bottrade-test-bot/1.0"

    def _request(self, method: str, path: str, **kw) -> Any:
        r = self.s.request(method, f"{self.base}{path}", timeout=30, **kw)
        if r.status_code >= 400:
            try:
                body = r.json()
                detail = body.get("detail") or body.get("title") or r.text
            except ValueError:
                body, detail = r.text, r.text
            raise APIError(r.status_code, detail, body)
        if r.status_code == 204 or not r.content:
            return None
        return r.json()

    # --- endpoints -----------------------------------------------------------

    def list_scenarios(self) -> list[dict]:
        return self._request("GET", "/api/v1/scenarios")["scenarios"]

    def get_scenario(self, id_or_slug: str) -> dict:
        return self._request("GET", f"/api/v1/scenarios/{id_or_slug}")["scenario"]

    def start_run(self, scenario_slug: str) -> dict:
        return self._request("POST", "/api/v1/runs",
                             json={"scenario_slug": scenario_slug})["run"]

    def get_run(self, run_id: str) -> dict:
        return self._request("GET", f"/api/v1/runs/{run_id}")

    def get_market(self, run_id: str, symbols: list[str], lookback: int = 50) -> dict:
        return self._request(
            "GET", f"/api/v1/runs/{run_id}/market",
            params={"symbols": ",".join(symbols), "lookback": lookback},
        )

    def queue_trade(self, run_id: str, symbol: str, side: str, quantity: int,
                    reasoning: str = "") -> dict:
        return self._request(
            "POST", f"/api/v1/runs/{run_id}/trades",
            json={
                "symbol": symbol, "side": side, "quantity": quantity,
                "reasoning": reasoning,
                "idempotency_key": str(uuid.uuid4()),
            },
        )["order"]

    def step(self, run_id: str, count: int = 1) -> dict:
        return self._request(
            "POST", f"/api/v1/runs/{run_id}/step",
            json={"count": count, "idempotency_key": str(uuid.uuid4())},
        )

    def get_results(self, run_id: str) -> dict:
        return self._request("GET", f"/api/v1/runs/{run_id}/results")["results"]

    def publish(self, run_id: str) -> dict:
        return self._request("POST", f"/api/v1/runs/{run_id}/publish")


# -----------------------------------------------------------------------------
# Strategies. Each returns a list of orders given the current market + state.
# Order shape: {"symbol": str, "side": "buy"|"sell"|"short"|"cover",
#               "quantity": int, "reasoning": str}
# -----------------------------------------------------------------------------

def strat_buy_hold(symbol: str):
    """Buy as much as cash allows on the first decision, then hold forever."""
    fired = False
    def decide(bars: dict, cash: float, positions: dict, step_idx: int):
        nonlocal fired
        if fired or symbol not in bars or not bars[symbol]:
            return []
        fired = True
        last_close = bars[symbol][-1]["close"]
        qty = int(cash * 0.95 / last_close)
        if qty <= 0:
            return []
        return [{"symbol": symbol, "side": "buy", "quantity": qty,
                 "reasoning": f"buy-hold {symbol}"}]
    return decide


def strat_equal_weight(universe: list[str], rebalance_every: int = 50):
    """Open equal-weight positions across the universe; rebalance periodically."""
    def decide(bars: dict, cash: float, positions: dict, step_idx: int):
        # Open positions on the first decision and every rebalance_every steps.
        if positions and step_idx % rebalance_every != 0:
            return []
        actions = []
        symbols_with_bars = [s for s in universe if bars.get(s)]
        if not symbols_with_bars:
            return []
        # Use total portfolio value (cash + mark-to-market positions).
        portfolio_value = cash
        for sym, qty in positions.items():
            if bars.get(sym):
                portfolio_value += qty * bars[sym][-1]["close"]
        target_per_symbol = portfolio_value / len(symbols_with_bars)
        for sym in symbols_with_bars:
            last_close = bars[sym][-1]["close"]
            target_qty = int(target_per_symbol / last_close)
            current_qty = positions.get(sym, 0)
            delta = target_qty - current_qty
            if delta > 0:
                actions.append({"symbol": sym, "side": "buy", "quantity": delta,
                                "reasoning": "equal-weight rebalance"})
            elif delta < 0:
                actions.append({"symbol": sym, "side": "sell", "quantity": -delta,
                                "reasoning": "equal-weight rebalance"})
        return actions
    return decide


def strat_momentum(universe: list[str], lookback: int = 20):
    """Hold the single symbol with the strongest N-bar return. Flip on change."""
    def decide(bars: dict, cash: float, positions: dict, step_idx: int):
        scored = []
        for sym in universe:
            series = bars.get(sym, [])
            if len(series) < lookback + 1:
                continue
            start, end = series[-lookback - 1]["close"], series[-1]["close"]
            if start > 0:
                scored.append((sym, (end - start) / start))
        if not scored:
            return []
        scored.sort(key=lambda kv: kv[1], reverse=True)
        winner, winner_ret = scored[0]
        actions = []
        # Sell everything that isn't the winner.
        for sym, qty in list(positions.items()):
            if sym != winner and qty > 0:
                actions.append({"symbol": sym, "side": "sell", "quantity": qty,
                                "reasoning": f"rotate out (best: {winner} {winner_ret:+.1%})"})
        # If we don't hold the winner, open it.
        if positions.get(winner, 0) == 0:
            last_close = bars[winner][-1]["close"]
            qty = int(cash * 0.95 / last_close)
            if qty > 0:
                actions.append({"symbol": winner, "side": "buy", "quantity": qty,
                                "reasoning": f"momentum leader {winner_ret:+.1%}"})
        return actions
    return decide


def strat_random(universe: list[str], seed: int = 42):
    """Random buy/sell each step. Smoke-test only — expected return is roughly zero minus slippage."""
    rng = random.Random(seed)
    def decide(bars: dict, cash: float, positions: dict, step_idx: int):
        sym = rng.choice([s for s in universe if bars.get(s)] or [None])
        if sym is None:
            return []
        last_close = bars[sym][-1]["close"]
        # 50% try to buy, 50% try to sell.
        if rng.random() < 0.5:
            qty = max(1, int(cash * 0.05 / last_close))
            if qty * last_close > cash:
                return []
            return [{"symbol": sym, "side": "buy", "quantity": qty,
                     "reasoning": "random"}]
        else:
            held = positions.get(sym, 0)
            if held <= 0:
                return []
            qty = max(1, held // 2)
            return [{"symbol": sym, "side": "sell", "quantity": qty,
                     "reasoning": "random"}]
    return decide


STRATEGIES = {
    "buy_hold":     lambda args, universe: strat_buy_hold(args.symbol or universe[0]),
    "equal_weight": lambda args, universe: strat_equal_weight(universe),
    "momentum":     lambda args, universe: strat_momentum(universe),
    "random":       lambda args, universe: strat_random(universe),
}


# -----------------------------------------------------------------------------
# The loop.
# -----------------------------------------------------------------------------

def positions_to_dict(snapshot_positions: list[dict]) -> dict[str, int]:
    return {p["symbol"]: int(p["quantity"]) for p in snapshot_positions}


def run_agent(client: BotTradeClient, scenario_slug: str, decide_fn,
              max_steps: int = 100_000, log_every: int = 25) -> tuple[str, dict]:
    print(f"\n→ Starting run on scenario: {scenario_slug}")
    run = client.start_run(scenario_slug)
    run_id = run["id"]
    cash = float(run["cash"])
    positions: dict[str, int] = {}
    starting_cash = float(run["starting_cash"])

    scen = client.get_scenario(scenario_slug)
    universe = scen["universe"]
    print(f"  run_id={run_id}")
    print(f"  starting cash: ${starting_cash:,.2f}")
    print(f"  universe ({len(universe)}): {', '.join(universe)}")
    print(f"  window: {scen['start_ts']} → {scen['end_ts']}")
    print(f"  bar resolution: {scen['bar_resolution']}\n")

    step_idx = 0
    t0 = time.time()
    last_equity = starting_cash

    while step_idx < max_steps:
        # 1. Observe.
        market = client.get_market(run_id, universe, lookback=50)
        bars = market["bars"]

        # 2. Decide.
        actions = decide_fn(bars, cash, positions, step_idx)

        # 3. Queue trades.
        queued_count = 0
        for a in actions:
            try:
                client.queue_trade(run_id, a["symbol"], a["side"],
                                   a["quantity"], a.get("reasoning", ""))
                queued_count += 1
            except APIError as e:
                # Most 400s here are "insufficient buying power" or
                # "sell exceeds position" — drop the order and continue.
                if e.status == 400:
                    print(f"  ! rejected {a['side']} {a['quantity']} {a['symbol']}: {e.detail}")
                else:
                    raise

        # 4. Advance one bar.
        result = client.step(run_id, count=1)
        cash = float(result["cash"])
        last_equity = float(result["equity"])

        # Refresh positions from the snapshot when anything happened.
        if queued_count or result["fills"]:
            snap = client.get_run(run_id)
            positions = positions_to_dict(snap["positions"])

        step_idx += 1
        if step_idx % log_every == 0 or queued_count or result["fills"]:
            pos_str = " ".join(f"{s}:{q}" for s, q in sorted(positions.items())) or "—"
            ret = (last_equity / starting_cash - 1) * 100
            print(f"  [step {step_idx:5d}] {result['new_sim_time']}  "
                  f"equity=${last_equity:>10,.2f} ({ret:+6.2f}%)  "
                  f"cash=${cash:>10,.2f}  pos: {pos_str}")

        if result["done"] or result["liquidated"]:
            why = "scenario complete" if result["done"] else "LIQUIDATED"
            print(f"\n→ {why} after {step_idx} steps  ({time.time() - t0:.1f}s wall)")
            break
    else:
        print(f"\n→ Hit max_steps={max_steps} without finishing. Bug, probably.")

    # 5. Get graded.
    print(f"\n→ Fetching results…")
    results = client.get_results(run_id)
    print(f"  final_equity:  ${results['final_equity']:,.2f}")
    print(f"  return_pct:    {results['return_pct']:+.2f}%")
    print(f"  sharpe:        {results.get('sharpe')}")
    print(f"  sortino:       {results.get('sortino')}")
    print(f"  max_drawdown:  {results.get('max_drawdown')}")
    print(f"  volatility:    {results.get('volatility')}")
    print(f"  trade_count:   {results['trade_count']}")
    print(f"  liquidated:    {results['liquidated']}")
    return run_id, results


# -----------------------------------------------------------------------------
# CLI.
# -----------------------------------------------------------------------------

def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(
        description="BotTrade Benchmark API reference test bot.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )
    p.add_argument("--api-key", default=os.environ.get("BOT_API_KEY"),
                   help="API key (or set BOT_API_KEY).")
    p.add_argument("--api-base", default=API_BASE,
                   help=f"API base URL (default {API_BASE}).")
    p.add_argument("--scenario", default=DEFAULT_SCENARIO,
                   help=f"Scenario slug to run (default {DEFAULT_SCENARIO}).")
    p.add_argument("--strategy", default="equal_weight",
                   choices=sorted(STRATEGIES.keys()),
                   help="Trading strategy.")
    p.add_argument("--symbol", default=None,
                   help="(buy_hold only) Symbol to buy. Default: first in universe.")
    p.add_argument("--list-scenarios", action="store_true",
                   help="Print the scenario catalog and exit.")
    p.add_argument("--publish", action="store_true",
                   help="Publish the result to the public leaderboard at the end.")
    p.add_argument("--max-steps", type=int, default=100_000,
                   help="Safety cap on iterations (default 100000).")
    p.add_argument("--log-every", type=int, default=25,
                   help="Print a status line every N steps even on quiet days.")
    args = p.parse_args(argv)

    if not args.api_key:
        p.error("--api-key or BOT_API_KEY env var required. "
                "Get a key with: curl -X POST https://bot-trade.org/api/v1/keys")

    client = BotTradeClient(args.api_key, args.api_base)

    if args.list_scenarios:
        print(f"\nScenarios from {args.api_base}:\n")
        for sc in client.list_scenarios():
            print(f"  {sc['slug']:30s}  {sc['name']}")
            print(f"  {'':30s}  universe={','.join(sc['universe'])}")
            print(f"  {'':30s}  {sc['start_ts']} → {sc['end_ts']}  "
                  f"cash=${sc['starting_cash']:,}  "
                  f"lev={sc['leverage_cap']}x  "
                  f"short={sc['short_enabled']}\n")
        return 0

    # Resolve scenario + universe once for strategy construction.
    scen = client.get_scenario(args.scenario)
    universe = scen["universe"]

    decide_fn = STRATEGIES[args.strategy](args, universe)

    try:
        run_id, _results = run_agent(
            client, args.scenario, decide_fn,
            max_steps=args.max_steps, log_every=args.log_every,
        )
    except APIError as e:
        print(f"\nerror: {e}", file=sys.stderr)
        return 1

    if args.publish:
        print(f"\n→ Publishing run {run_id} to the leaderboard…")
        client.publish(run_id)
        print("  published.")

    return 0


if __name__ == "__main__":
    sys.exit(main())
