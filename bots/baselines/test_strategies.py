"""
Unit tests for the baseline strategies.

Run from repo root inside the project venv:
    source venv/bin/activate
    python -m unittest bots.baselines.test_strategies -v
"""
from __future__ import annotations

import unittest

from bots.common import Decision, Portfolio
from bots.baselines.strategies import (
    REBALANCE_EVERY,
    equal_weight,
    random_walker,
    spy_buyandhold,
)

UNIVERSE_PRICES = {
    "AAPL": 200.0, "MSFT": 400.0, "GOOGL": 150.0, "AMZN": 175.0, "NVDA": 100.0,
    "META": 500.0, "TSLA": 250.0, "AMD":  150.0, "NFLX": 600.0, "SPY":  500.0,
    "QQQ":  400.0, "JPM":  200.0, "XOM":  100.0, "DIS":   90.0, "PLTR": 25.0,
}


def empty_portfolio(cash: float = 100_000.0) -> Portfolio:
    return Portfolio(cash=cash, positions={}, total_value=cash)


class TestSpyBuyAndHold(unittest.TestCase):
    def test_day_zero_buys_spy(self):
        decisions = spy_buyandhold(0, UNIVERSE_PRICES, empty_portfolio())
        self.assertEqual(len(decisions), 1)
        d = decisions[0]
        self.assertEqual(d.action, "buy")
        self.assertEqual(d.symbol, "SPY")
        # 100k cash / $500 SPY = 200 shares.
        self.assertEqual(d.quantity, 200)

    def test_subsequent_days_hold(self):
        held = Portfolio(
            cash=0.0,
            positions={"SPY": {"quantity": 200, "avg_cost": 500.0}},
            total_value=100_000.0,
        )
        decisions = spy_buyandhold(5, UNIVERSE_PRICES, held)
        self.assertEqual(len(decisions), 1)
        self.assertEqual(decisions[0].action, "hold")

    def test_no_spy_price_holds(self):
        prices = dict(UNIVERSE_PRICES)
        del prices["SPY"]
        decisions = spy_buyandhold(0, prices, empty_portfolio())
        self.assertEqual(decisions[0].action, "hold")

    def test_zero_cash_holds(self):
        decisions = spy_buyandhold(0, UNIVERSE_PRICES, empty_portfolio(cash=0.0))
        self.assertEqual(decisions[0].action, "hold")


class TestEqualWeight(unittest.TestCase):
    def test_day_zero_buys_all_15_names(self):
        decisions = equal_weight(0, UNIVERSE_PRICES, empty_portfolio())
        buys = [d for d in decisions if d.action == "buy"]
        self.assertEqual(len(buys), 15)
        symbols = {d.symbol for d in buys}
        self.assertEqual(symbols, set(UNIVERSE_PRICES.keys()))

    def test_off_rebalance_day_holds(self):
        held = Portfolio(
            cash=0.0,
            positions={"SPY": {"quantity": 13, "avg_cost": 500.0}},  # arbitrary
            total_value=100_000.0,
        )
        # Day 5 is not a multiple of REBALANCE_EVERY.
        self.assertNotEqual(5 % REBALANCE_EVERY, 0)
        decisions = equal_weight(5, UNIVERSE_PRICES, held)
        self.assertEqual(len(decisions), 1)
        self.assertEqual(decisions[0].action, "hold")

    def test_rebalance_day_only_acts_above_drift_threshold(self):
        # Build a portfolio that is at target on most names but heavily over on one.
        target_each_value = 100_000.0 / 15  # ≈ $6666 per name
        positions = {}
        for sym, px in UNIVERSE_PRICES.items():
            target_qty = int(target_each_value // px)
            positions[sym] = {"quantity": target_qty, "avg_cost": px}
        # NVDA is way over (10x target).
        positions["NVDA"]["quantity"] *= 10

        held = Portfolio(cash=0.0, positions=positions, total_value=100_000.0)
        decisions = equal_weight(REBALANCE_EVERY, UNIVERSE_PRICES, held)
        # Only NVDA should rebalance; others are at target so drift < threshold.
        non_hold = [d for d in decisions if d.action != "hold"]
        self.assertEqual(len(non_hold), 1)
        self.assertEqual(non_hold[0].action, "sell")
        self.assertEqual(non_hold[0].symbol, "NVDA")


class TestRandomWalker(unittest.TestCase):
    def test_deterministic_for_same_day_idx(self):
        # Two independent invocations with the same day_idx must produce
        # identical action/symbol/qty — that's what makes the baseline reproducible.
        d1 = random_walker(42, UNIVERSE_PRICES, empty_portfolio())[0]
        d2 = random_walker(42, UNIVERSE_PRICES, empty_portfolio())[0]
        self.assertEqual(d1.action, d2.action)
        self.assertEqual(d1.symbol, d2.symbol)
        self.assertEqual(d1.quantity, d2.quantity)

    def test_different_day_idx_can_diverge(self):
        # Iterate a window; we expect at least 2 distinct (action, symbol)
        # pairs across 20 days. If RNG seeding is broken we'd see one fixed
        # decision repeat.
        seen = set()
        for day in range(20):
            d = random_walker(day, UNIVERSE_PRICES, empty_portfolio())[0]
            seen.add((d.action, d.symbol))
        self.assertGreater(len(seen), 1)

    def test_sell_without_position_holds(self):
        # Force the rng-chosen action to be "sell" on a day with no positions.
        # The deterministic seed means we can scan day_idx values until one
        # picks "sell", then assert behavior.
        for day in range(50):
            empty = empty_portfolio()
            d = random_walker(day, UNIVERSE_PRICES, empty)[0]
            # If the rng picked "sell" and we have no positions, fallback hold.
            if d.action == "sell":
                self.fail(f"day {day}: expected hold fallback when selling with empty portfolio, got {d}")
            if d.action == "hold" and "no" in d.reasoning and "position to sell" in d.reasoning:
                return  # found the fallback path
        # If no day in the window happened to pick "sell", that's fine too —
        # the test passes by not failing.


if __name__ == "__main__":
    unittest.main()
