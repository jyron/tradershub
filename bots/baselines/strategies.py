"""
Deterministic baseline strategies.

Each strategy is a pure function (state in, decisions out) so it can be
exercised identically in replay and live mode. No LLM call — these exist as
reference lines so the leaderboard reads against something other than each
other.

Strategy contract:
    fn(day_idx: int, prices: dict[str, float], portfolio: Portfolio) -> list[Decision]

Where day_idx is the 0-based index into the trading-day sequence (live mode
always passes 0 for the day_idx since "today" is the only day).
"""
from __future__ import annotations

import random
from typing import Callable

from bots.common import UNIVERSE, Decision, Portfolio

StrategyFn = Callable[[int, dict[str, float], Portfolio], list[Decision]]


# --------------------------------------------------------------------------
# 1. SPY buy-and-hold.
# Day 0: spend ~all cash on SPY. Forever after: hold.
# --------------------------------------------------------------------------
def spy_buyandhold(day_idx: int, prices: dict[str, float], portfolio: Portfolio) -> list[Decision]:
    pos = portfolio.position("SPY")
    if pos and pos["quantity"] > 0:
        return [Decision("hold", None, None, "buy-and-hold: SPY already held")]
    px = prices.get("SPY")
    if not px or px <= 0:
        return [Decision("hold", None, None, "no SPY price available")]
    qty = int(portfolio.cash // px)
    if qty <= 0:
        return [Decision("hold", None, None, "insufficient cash")]
    return [Decision("buy", "SPY", qty, "buy SPY with all available cash (passive baseline)")]


# --------------------------------------------------------------------------
# 2. Equal-weight portfolio.
# Day 0: buy ~equal-dollar slices of every universe symbol.
# Once a month (every 21 trading days): rebalance toward equal weight.
# --------------------------------------------------------------------------
REBALANCE_EVERY = 21  # trading days, ≈ one calendar month
EQUAL_WEIGHT_DRIFT_THRESHOLD = 0.02  # 2 percentage points — anything tighter just churns


def equal_weight(day_idx: int, prices: dict[str, float], portfolio: Portfolio) -> list[Decision]:
    symbols = [s for s in UNIVERSE if s in prices and prices[s] > 0]
    if not symbols:
        return [Decision("hold", None, None, "no prices yet")]

    target_each = portfolio.total_value / len(symbols)

    # Day 0 initial deploy.
    if not portfolio.positions:
        out: list[Decision] = []
        for sym in symbols:
            qty = int(target_each // prices[sym])
            if qty > 0:
                out.append(Decision("buy", sym, qty,
                    f"equal-weight initial deploy: target ${target_each:,.0f} per of {len(symbols)} names"))
        return out or [Decision("hold", None, None, "could not deploy any positions")]

    # Off-rebalance days: just hold.
    if day_idx % REBALANCE_EVERY != 0:
        return [Decision("hold", None, None, "equal-weight: between rebalances")]

    # Rebalance: any position drifted more than the threshold from target gets nudged.
    out = []
    for sym in symbols:
        px = prices[sym]
        held_qty = portfolio.positions.get(sym, {"quantity": 0})["quantity"]
        target_qty = int(target_each // px)
        drift_dollars = abs(held_qty - target_qty) * px
        if drift_dollars / portfolio.total_value < EQUAL_WEIGHT_DRIFT_THRESHOLD:
            continue
        if target_qty > held_qty:
            out.append(Decision("buy", sym, target_qty - held_qty,
                f"equal-weight rebalance: add {target_qty - held_qty} to reach target"))
        elif held_qty > target_qty:
            out.append(Decision("sell", sym, held_qty - target_qty,
                f"equal-weight rebalance: trim {held_qty - target_qty} toward target"))
    return out or [Decision("hold", None, None, "equal-weight: within threshold, no rebalance needed")]


# --------------------------------------------------------------------------
# 3. Random walker.
# Each day, pick a random universe symbol and act on it. Sizes are small so
# the random bot doesn't blow up in either direction inside a few days.
# Seeded by (bot_id, date) so replays are reproducible.
# --------------------------------------------------------------------------
def random_walker(day_idx: int, prices: dict[str, float], portfolio: Portfolio) -> list[Decision]:
    symbols = [s for s in UNIVERSE if s in prices and prices[s] > 0]
    if not symbols:
        return [Decision("hold", None, None, "no prices")]

    rng = random.Random(day_idx)  # day_idx-seeded → deterministic per replay
    action = rng.choice(["buy", "sell", "hold"])
    sym = rng.choice(symbols)
    px = prices[sym]

    if action == "buy":
        # Spend ~5% of total value, capped by cash.
        budget = min(portfolio.total_value * 0.05, portfolio.cash)
        qty = int(budget // px)
        if qty <= 0:
            return [Decision("hold", None, None, "random: insufficient cash for buy")]
        return [Decision("buy", sym, qty, f"random walker: buy {sym} (~5% of NAV)")]
    if action == "sell":
        pos = portfolio.positions.get(sym)
        if not pos or pos["quantity"] <= 0:
            return [Decision("hold", None, None, f"random: no {sym} position to sell")]
        # Sell ~half the position.
        qty = max(1, pos["quantity"] // 2)
        return [Decision("sell", sym, qty, f"random walker: trim half of {sym}")]
    return [Decision("hold", None, None, "random: hold day")]


STRATEGIES: dict[str, StrategyFn] = {
    "spy_buyandhold": spy_buyandhold,
    "equal_weight":   equal_weight,
    "random_walker":  random_walker,
}
