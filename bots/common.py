"""
common.py — shared infrastructure for the four official bot scripts.

What each bot script (claude_bot.py / gpt_bot.py / gemini_bot.py / grok_bot.py)
needs to do:

  1. import this module
  2. implement   decide(snapshot, portfolio, system_prompt) -> Decision
     which calls its LLM and returns a parsed Decision
  3. call       run_cli(provider, decide)

This module handles:
  - loading the bot's BotTrade API key from bots/.keys/<provider>.json
  - fetching historical Alpaca bars for replay
  - building the market snapshot prompt
  - parsing the LLM's JSON response into a Decision
  - REPLAY MODE: walking N trading days back in time, prompting the LLM
    for each day, and writing trades to SQLite directly (with the day's
    historical timestamp) plus a daily portfolio snapshot
  - LIVE MODE: hitting POST /api/trade/stock via HTTP with the bot's key
  - CLI: --replay N | --live | --once

Two modes, one decision function. The LLM never knows whether it's replay
or live — the snapshot looks identical either way.
"""

from __future__ import annotations

import argparse
import json
import os
import sqlite3
import sys
import time
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Callable, Optional
from uuid import uuid4

import requests

try:
    from alpaca.data.historical.stock import StockHistoricalDataClient
    from alpaca.data.requests import StockBarsRequest, StockLatestQuoteRequest
    from alpaca.data.timeframe import TimeFrame
except ImportError:  # alpaca-py not installed → fail loudly when first needed
    StockHistoricalDataClient = None

try:
    from dotenv import load_dotenv
    load_dotenv()
    load_dotenv(".env.local")
except ImportError:
    pass

# -----------------------------------------------------------------------------
# Config
# -----------------------------------------------------------------------------

REPO_ROOT = Path(__file__).resolve().parent.parent
KEYS_DIR = REPO_ROOT / "bots" / ".keys"
DEFAULT_DB_PATH = os.getenv("BOTTRADE_DB", str(REPO_ROOT / "bottrade.db"))
BASE_URL = os.getenv("BASE_URL", "http://localhost:3000")
STARTING_BALANCE = 100_000.0

# The universe of tradeable symbols. Kept small and large-cap so the LLM
# can reason about them sensibly without needing a research pass.
UNIVERSE = [
    "AAPL", "MSFT", "GOOGL", "AMZN", "NVDA", "META", "TSLA",
    "AMD", "NFLX", "SPY", "QQQ", "JPM", "XOM", "DIS", "PLTR",
]

# Each bot persona — keep these short; they're appended to the bot's own
# system prompt to give it a personality but stay consistent across providers.
SYSTEM_PROMPT = """\
You are a portfolio manager running a public paper-trading account on \
BotTrade. You start with $100,000 in cash. You can BUY or SELL US equities \
or HOLD on any given trading day. Long-only, no margin, no shorts. \
Each trade must include a 1-2 sentence reasoning that explains the why.

You will receive a market snapshot (yesterday's close, today's open, your \
current portfolio). Decide ONE action and respond ONLY with strict JSON \
matching this schema (no markdown, no prose):

  {"action": "buy" | "sell" | "hold",
   "symbol": "<TICKER>" | null,
   "quantity": <integer> | null,
   "reasoning": "<1-2 sentences>"}

Rules:
  - "hold" → symbol and quantity must be null.
  - Position size: never put more than 25% of total portfolio value into a single new buy.
  - Don't sell what you don't hold.
  - Reasoning must reference something concrete (price action, position, market context).
"""


# -----------------------------------------------------------------------------
# Data types
# -----------------------------------------------------------------------------

@dataclass
class Decision:
    action: str   # "buy" | "sell" | "hold"
    symbol: Optional[str]
    quantity: Optional[int]
    reasoning: str


@dataclass
class MarketSnapshot:
    # date is the "as-of" trading day. Prices are that day's close.
    date: datetime
    prices: dict[str, float]  # symbol -> last close


@dataclass
class Portfolio:
    cash: float
    positions: dict[str, dict]  # symbol -> {"quantity": int, "avg_cost": float}
    total_value: float

    def position(self, symbol: str) -> Optional[dict]:
        return self.positions.get(symbol)


# -----------------------------------------------------------------------------
# Key loading
# -----------------------------------------------------------------------------

def load_bot_key(provider: str) -> dict:
    """Returns the {bot_id, api_key, provider} dict written by seed_official_bots.py."""
    path = KEYS_DIR / f"{provider}.json"
    if not path.exists():
        die(f"no key file at {path}.\n"
            f"run: python scripts/seed_official_bots.py")
    with open(path) as f:
        return json.load(f)


def llm_key(provider: str) -> str:
    """Reads the provider's LLM API key from the environment."""
    env_var = {
        "claude":  "ANTHROPIC_API_KEY",
        "gpt":     "OPENAI_API_KEY",
        "gemini":  "GOOGLE_API_KEY",
        "grok":    "XAI_API_KEY",
    }[provider]
    val = os.getenv(env_var, "").strip()
    if not val:
        die(f"{env_var} is not set. add it to .env or .env.local:\n"
            f"    {env_var}=sk-...")
    return val


# -----------------------------------------------------------------------------
# Alpaca historical prices
# -----------------------------------------------------------------------------

def alpaca_client() -> "StockHistoricalDataClient":
    if StockHistoricalDataClient is None:
        die("alpaca-py not installed. pip install alpaca-py")
    key = os.getenv("ALPACA_API_KEY", "").strip()
    sec = os.getenv("ALPACA_SECRET_KEY", "").strip()
    if not key or not sec:
        die("ALPACA_API_KEY and ALPACA_SECRET_KEY must be set in .env")
    return StockHistoricalDataClient(key, sec)


def fetch_daily_bars(client, days_back: int) -> dict[str, dict[str, float]]:
    """
    Returns bars[symbol][YYYY-MM-DD] = close_price for every UNIVERSE symbol
    over the past `days_back` calendar days. One Alpaca call total — much
    faster than per-day fetches.
    """
    end = datetime.now(timezone.utc)
    start = end - timedelta(days=days_back + 5)  # pad for weekends/holidays
    req = StockBarsRequest(
        symbol_or_symbols=UNIVERSE,
        timeframe=TimeFrame.Day,
        start=start, end=end,
    )
    bars = client.get_stock_bars(req)
    out: dict[str, dict[str, float]] = {sym: {} for sym in UNIVERSE}
    for bar in bars.data.get("bars", []) if hasattr(bars, "data") else []:
        # fall through to df-style fallback below
        pass
    # alpaca-py returns a BarSet — iterate via .df
    df = bars.df
    if df is None or df.empty:
        die("alpaca returned no bars; check your API key + plan")
    # df is multi-indexed (symbol, timestamp)
    for (symbol, ts), row in df.iterrows():
        d = ts.strftime("%Y-%m-%d")
        out[symbol][d] = float(row["close"])
    return out


def fetch_latest_quotes(client) -> dict[str, float]:
    """Latest quote for each universe symbol — used in --live mode."""
    req = StockLatestQuoteRequest(symbol_or_symbols=UNIVERSE)
    quotes = client.get_stock_latest_quote(req)
    out: dict[str, float] = {}
    for sym, q in quotes.items():
        # midpoint of bid/ask, fall back to ask
        if q.bid_price and q.ask_price:
            out[sym] = (float(q.bid_price) + float(q.ask_price)) / 2.0
        elif q.ask_price:
            out[sym] = float(q.ask_price)
    return out


def trading_days(bars: dict[str, dict[str, float]]) -> list[str]:
    """All dates for which we have bars for at least one symbol. Sorted ascending."""
    dates: set[str] = set()
    for sym, by_date in bars.items():
        dates.update(by_date.keys())
    return sorted(dates)


# -----------------------------------------------------------------------------
# Portfolio reconstruction (replay) — mirrors services/portfolio.go semantics
# -----------------------------------------------------------------------------

def _open_db() -> sqlite3.Connection:
    if not Path(DEFAULT_DB_PATH).exists():
        die(f"database not found at {DEFAULT_DB_PATH}.\n"
            f"start the server once (go run main.go) so migrations create it, then re-run.")
    con = sqlite3.connect(DEFAULT_DB_PATH, isolation_level=None)
    con.row_factory = sqlite3.Row
    return con


def replay_portfolio(con: sqlite3.Connection, bot_id: str, as_of_close: dict[str, float]) -> Portfolio:
    """Reconstructs the bot's portfolio from all main-account trades. Marks
    positions to the prices in `as_of_close`. Mirrors services/portfolio.go."""
    cur = con.execute(
        """SELECT symbol, side, quantity, price
           FROM trades
           WHERE bot_id = ? AND season_id IS NULL
           ORDER BY executed_at ASC""",
        (bot_id,),
    )
    cash = STARTING_BALANCE
    positions: dict[str, dict] = {}
    for r in cur:
        sym, side, qty, px = r["symbol"], r["side"].lower(), int(r["quantity"]), float(r["price"])
        if side == "buy":
            cash -= qty * px
            pos = positions.get(sym)
            if pos is None:
                positions[sym] = {"quantity": qty, "avg_cost": px}
            else:
                # weighted avg cost
                total_cost = pos["avg_cost"] * pos["quantity"] + px * qty
                pos["quantity"] += qty
                pos["avg_cost"] = total_cost / pos["quantity"]
        elif side == "sell":
            cash += qty * px
            pos = positions.get(sym)
            if pos is not None:
                pos["quantity"] -= qty
                if pos["quantity"] <= 0:
                    positions.pop(sym, None)

    # mark to market
    mv = 0.0
    for sym, p in positions.items():
        mv += p["quantity"] * as_of_close.get(sym, p["avg_cost"])
    return Portfolio(cash=cash, positions=positions, total_value=cash + mv)


# -----------------------------------------------------------------------------
# Trade recording
# -----------------------------------------------------------------------------

def record_trade_db(
    con: sqlite3.Connection, bot_id: str, decision: Decision,
    price: float, executed_at: datetime,
) -> None:
    """Writes a trade row + updates the bot's cash + position state, mirroring
    the Go trading engine but offline. Used by replay mode only."""
    if decision.action == "hold" or not decision.symbol or not decision.quantity:
        return
    qty = int(decision.quantity)
    if qty <= 0:
        return
    total = qty * price
    trade_id = str(uuid4())
    con.execute(
        """INSERT INTO trades
           (id, bot_id, symbol, trade_type, side, quantity, price, total_value, reasoning, executed_at)
           VALUES (?, ?, ?, 'stock', ?, ?, ?, ?, ?, ?)""",
        (trade_id, bot_id, decision.symbol, decision.action, qty, price, total,
         decision.reasoning, executed_at.strftime("%Y-%m-%d %H:%M:%S")),
    )
    # Update the position table to match what the live trading engine would do
    # so the live server's GetPortfolio returns sensible numbers post-replay.
    row = con.execute(
        """SELECT id, quantity, avg_cost FROM positions
           WHERE bot_id = ? AND symbol = ? AND season_id IS NULL""",
        (bot_id, decision.symbol),
    ).fetchone()
    if decision.action == "buy":
        if row is None:
            con.execute(
                """INSERT INTO positions (id, bot_id, symbol, position_type, quantity, avg_cost, season_id, created_at, updated_at)
                   VALUES (?, ?, ?, 'stock', ?, ?, NULL, ?, ?)""",
                (str(uuid4()), bot_id, decision.symbol, qty, price,
                 executed_at.strftime("%Y-%m-%d %H:%M:%S"),
                 executed_at.strftime("%Y-%m-%d %H:%M:%S")),
            )
        else:
            new_qty = row["quantity"] + qty
            new_avg = (row["avg_cost"] * row["quantity"] + price * qty) / new_qty
            con.execute(
                "UPDATE positions SET quantity = ?, avg_cost = ?, updated_at = ? WHERE id = ?",
                (new_qty, new_avg, executed_at.strftime("%Y-%m-%d %H:%M:%S"), row["id"]),
            )
    else:  # sell
        if row is not None:
            new_qty = row["quantity"] - qty
            if new_qty <= 0:
                con.execute("DELETE FROM positions WHERE id = ?", (row["id"],))
            else:
                con.execute(
                    "UPDATE positions SET quantity = ?, updated_at = ? WHERE id = ?",
                    (new_qty, executed_at.strftime("%Y-%m-%d %H:%M:%S"), row["id"]),
                )

    # Adjust cash balance on the bot row
    cash_delta = -total if decision.action == "buy" else total
    con.execute(
        "UPDATE bots SET cash_balance = cash_balance + ? WHERE id = ?",
        (cash_delta, bot_id),
    )


def record_snapshot_db(
    con: sqlite3.Connection, bot_id: str, total_value: float, at: datetime,
) -> None:
    con.execute(
        """INSERT INTO portfolio_snapshots (id, bot_id, season_id, total_value, cash_balance, positions_value, snapshot_at)
           VALUES (?, ?, NULL, ?, ?, ?, ?)""",
        (str(uuid4()), bot_id, total_value, 0.0, 0.0,
         at.strftime("%Y-%m-%d %H:%M:%S")),
    )


def post_live_trade(api_key: str, decision: Decision) -> dict:
    """LIVE mode: hit the public /api/trade/stock endpoint."""
    if decision.action == "hold":
        return {"status": "held"}
    payload = {
        "symbol": decision.symbol,
        "side": decision.action,
        "quantity": decision.quantity,
        "reasoning": decision.reasoning,
    }
    r = requests.post(
        f"{BASE_URL}/api/trade/stock",
        json=payload,
        headers={"X-API-Key": api_key},
        timeout=15,
    )
    if r.status_code >= 400:
        raise RuntimeError(f"live trade failed ({r.status_code}): {r.text[:200]}")
    return r.json()


# -----------------------------------------------------------------------------
# Prompt building + response parsing
# -----------------------------------------------------------------------------

def build_user_prompt(snapshot: MarketSnapshot, portfolio: Portfolio) -> str:
    pos_lines = []
    for sym, p in portfolio.positions.items():
        last = snapshot.prices.get(sym, p["avg_cost"])
        mv = p["quantity"] * last
        pnl_pct = ((last - p["avg_cost"]) / p["avg_cost"]) * 100 if p["avg_cost"] > 0 else 0.0
        pos_lines.append(f"  - {sym}: {p['quantity']} sh @ avg ${p['avg_cost']:.2f} "
                         f"now ${last:.2f} ({pnl_pct:+.1f}%) = ${mv:,.0f}")
    pos_block = "\n".join(pos_lines) if pos_lines else "  (no positions)"

    price_block = "\n".join(
        f"  - {sym}: ${px:.2f}"
        for sym, px in sorted(snapshot.prices.items())
    )

    return f"""\
Market snapshot for {snapshot.date.strftime('%Y-%m-%d')}:

Today's closing prices:
{price_block}

Your portfolio:
  cash: ${portfolio.cash:,.2f}
  total value: ${portfolio.total_value:,.2f}
  positions:
{pos_block}

What's your move for tomorrow? Reply with JSON only."""


def parse_decision(raw: str) -> Decision:
    """Tolerant parser: pulls the first {...} block out and validates fields."""
    raw = raw.strip()
    # Strip code fences if present
    if raw.startswith("```"):
        raw = raw.split("```", 2)[-1].strip()
        if raw.startswith("json"):
            raw = raw[4:].strip()
        if raw.endswith("```"):
            raw = raw[:-3].strip()
    # Find the first { and matching close
    start = raw.find("{")
    if start < 0:
        return Decision("hold", None, None, "(unparseable response)")
    depth = 0
    end = -1
    for i in range(start, len(raw)):
        if raw[i] == "{":
            depth += 1
        elif raw[i] == "}":
            depth -= 1
            if depth == 0:
                end = i + 1
                break
    if end < 0:
        return Decision("hold", None, None, "(unparseable response)")
    try:
        obj = json.loads(raw[start:end])
    except json.JSONDecodeError:
        return Decision("hold", None, None, "(invalid JSON)")
    action = str(obj.get("action", "hold")).lower()
    if action not in ("buy", "sell", "hold"):
        action = "hold"
    sym = obj.get("symbol")
    if sym is not None:
        sym = str(sym).upper().strip()
        if sym not in UNIVERSE:
            return Decision("hold", None, None,
                            f"(unsupported symbol: {sym})")
    qty = obj.get("quantity")
    if qty is not None:
        try:
            qty = max(0, int(qty))
        except (TypeError, ValueError):
            qty = None
    reasoning = str(obj.get("reasoning", "")).strip() or "(no reasoning)"
    return Decision(action, sym, qty, reasoning)


# -----------------------------------------------------------------------------
# CLI entry point
# -----------------------------------------------------------------------------

DecideFn = Callable[[str, str], str]  # (system_prompt, user_prompt) -> raw LLM response


def run_cli(provider: str, decide_llm: DecideFn) -> None:
    parser = argparse.ArgumentParser(description=f"{provider} BotTrade bot")
    parser.add_argument("--replay", type=int, metavar="N",
                        help="Replay last N trading days, calling the LLM once per day. Writes trades and daily snapshots to SQLite directly.")
    parser.add_argument("--live", action="store_true",
                        help="Run once against live market data via the public API. Designed to be called daily by cron.")
    parser.add_argument("--once", action="store_true",
                        help="Same as --live but dry-runs (no trade actually posted).")
    parser.add_argument("--limit-days", type=int, default=None,
                        help="(replay only) cap the loop at this many days for testing")
    args = parser.parse_args()

    if not args.replay and not args.live and not args.once:
        parser.print_help()
        sys.exit(1)

    keyrec = load_bot_key(provider)
    print(f"[{provider}] bot_id={keyrec['bot_id']}")

    if args.replay:
        replay_loop(provider, keyrec["bot_id"], decide_llm, args.replay,
                    limit=args.limit_days)
    else:
        live_once(provider, keyrec["api_key"], decide_llm, dry_run=args.once)


def replay_loop(provider: str, bot_id: str, decide_llm: DecideFn,
                days: int, limit: Optional[int] = None) -> None:
    print(f"[{provider}] REPLAY: fetching {days} days of Alpaca bars…")
    client = alpaca_client()
    bars = fetch_daily_bars(client, days)
    dates = trading_days(bars)
    if limit:
        dates = dates[-limit:]
    print(f"[{provider}] {len(dates)} trading days: {dates[0]} → {dates[-1]}")

    con = _open_db()
    # Reset the bot to a clean state so re-runs are idempotent
    print(f"[{provider}] wiping prior trades/positions/snapshots for this bot…")
    con.execute("DELETE FROM trades WHERE bot_id = ? AND season_id IS NULL", (bot_id,))
    con.execute("DELETE FROM positions WHERE bot_id = ? AND season_id IS NULL", (bot_id,))
    con.execute("DELETE FROM portfolio_snapshots WHERE bot_id = ? AND season_id IS NULL", (bot_id,))
    con.execute("UPDATE bots SET cash_balance = ? WHERE id = ?", (STARTING_BALANCE, bot_id))

    for i, date_str in enumerate(dates):
        # Today's prices = bars[symbol][date_str] (close)
        as_of = {sym: bars[sym][date_str] for sym in UNIVERSE if date_str in bars[sym]}
        if len(as_of) < 5:
            continue  # not enough data this day
        portfolio = replay_portfolio(con, bot_id, as_of)
        snap_dt = datetime.strptime(date_str, "%Y-%m-%d").replace(hour=20, minute=0)
        snapshot = MarketSnapshot(date=snap_dt, prices=as_of)

        prompt = build_user_prompt(snapshot, portfolio)
        try:
            raw = decide_llm(SYSTEM_PROMPT, prompt)
        except Exception as e:
            print(f"  [{date_str}] LLM error: {e!r} — skipping day")
            record_snapshot_db(con, bot_id, portfolio.total_value, snap_dt)
            continue
        decision = parse_decision(raw)

        # Sanity: clip quantity for buys so it can't exceed 25% of portfolio
        if decision.action == "buy" and decision.symbol and decision.quantity:
            max_dollars = portfolio.total_value * 0.25
            price = as_of.get(decision.symbol, 0)
            if price > 0:
                max_qty = int(max_dollars / price)
                if decision.quantity > max_qty:
                    decision.quantity = max_qty
            # Don't let cash go negative
            if price > 0 and decision.quantity * price > portfolio.cash:
                decision.quantity = max(0, int(portfolio.cash / price))
            if decision.quantity <= 0:
                decision = Decision("hold", None, None, "(insufficient cash)")
        if decision.action == "sell" and decision.symbol:
            pos = portfolio.position(decision.symbol)
            if pos is None or pos["quantity"] <= 0:
                decision = Decision("hold", None, None, "(no position to sell)")
            elif decision.quantity and decision.quantity > pos["quantity"]:
                decision.quantity = pos["quantity"]

        exec_time = datetime.strptime(date_str, "%Y-%m-%d").replace(hour=15, minute=30)
        if decision.action != "hold":
            price = as_of.get(decision.symbol, 0)
            record_trade_db(con, bot_id, decision, price, exec_time)
            print(f"  [{date_str}] {decision.action.upper()} {decision.quantity} {decision.symbol} @ ${price:.2f}"
                  f"  | port=${portfolio.total_value:,.0f}")
        else:
            print(f"  [{date_str}] HOLD  | port=${portfolio.total_value:,.0f}")

        # End-of-day snapshot (recomputed since we may have traded)
        portfolio_after = replay_portfolio(con, bot_id, as_of)
        record_snapshot_db(con, bot_id, portfolio_after.total_value, snap_dt)

    final = replay_portfolio(con, bot_id, as_of)
    print(f"\n[{provider}] REPLAY DONE.")
    print(f"  final value: ${final.total_value:,.2f}  "
          f"({(final.total_value / STARTING_BALANCE - 1) * 100:+.2f}%)")
    print(f"  positions: {len(final.positions)}  cash: ${final.cash:,.2f}")


def live_once(provider: str, api_key: str, decide_llm: DecideFn,
              dry_run: bool = False) -> None:
    """One LLM decision against today's market. Designed for cron."""
    print(f"[{provider}] LIVE: fetching today's snapshot…")
    client = alpaca_client()
    prices = fetch_latest_quotes(client)
    if len(prices) < 5:
        die("alpaca returned too few quotes; aborting")
    # Pull current portfolio from the API (so we don't rely on DB access)
    r = requests.get(f"{BASE_URL}/api/portfolio", headers={"X-API-Key": api_key}, timeout=10)
    if r.status_code != 200:
        die(f"could not fetch portfolio: HTTP {r.status_code} {r.text[:200]}")
    p = r.json()
    positions = {
        pos["symbol"]: {"quantity": int(pos["quantity"]),
                        "avg_cost": float(pos["avg_cost"])}
        for pos in p.get("positions", [])
    }
    portfolio = Portfolio(
        cash=float(p["cash_balance"]),
        positions=positions,
        total_value=float(p["total_value"]),
    )
    snapshot = MarketSnapshot(date=datetime.now(timezone.utc), prices=prices)
    prompt = build_user_prompt(snapshot, portfolio)
    raw = decide_llm(SYSTEM_PROMPT, prompt)
    decision = parse_decision(raw)
    print(f"[{provider}] decision: {decision}")
    if dry_run:
        print(f"[{provider}] --once: not posting trade")
        return
    result = post_live_trade(api_key, decision)
    print(f"[{provider}] result: {result}")


# -----------------------------------------------------------------------------
# helpers
# -----------------------------------------------------------------------------

def die(msg: str) -> None:
    print(f"error: {msg}", file=sys.stderr)
    sys.exit(1)
