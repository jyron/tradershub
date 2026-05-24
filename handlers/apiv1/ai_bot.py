#!/usr/bin/env python3
"""
ai_bot.py — Claude-powered reference agent for the BotTrade Benchmark API.

Plug in your Anthropic API key, point it at a scenario, and Claude trades
the simulation for you. Every N bars the bot snapshots the market plus
your portfolio, asks Claude what to do, queues the trades, and steps the
sim forward. The bot's only role is plumbing — every actual decision is
the model's.

Requirements:
    pip install requests anthropic

Usage:
    # 1. Get a BotTrade API key (no signup):
    #      curl -X POST https://bot-trade.org/api/v1/keys
    export BOT_API_KEY=<the api_key from above>

    # 2. Get an Anthropic key from https://console.anthropic.com
    export ANTHROPIC_API_KEY=sk-ant-...

    # 3. Run it
    python ai_bot.py
    python ai_bot.py --scenario tech-2024-q2 --decide-every 6
    python ai_bot.py --model claude-sonnet-4-6 --lookback 50

Defaults to claude-haiku-4-5 for cost. Switch with --model when you want
smarter decisions.
"""
from __future__ import annotations

import argparse
import os
import sys
import time
import uuid
from dataclasses import dataclass, field
from typing import Any

import anthropic
import requests
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry


# =============================================================================
# BotTrade API client (minimal — see /api/agent-skills.md for the full surface).
# =============================================================================

class APIError(RuntimeError):
    def __init__(self, status: int, detail: str):
        super().__init__(f"HTTP {status}: {detail}")
        self.status = status
        self.detail = detail


class BotTradeClient:
    def __init__(self, api_key: str, base: str = "https://bot-trade.org"):
        self.base = base.rstrip("/")
        self.s = requests.Session()
        self.s.headers["X-API-Key"] = api_key
        # Retry transient edge errors (Cloudflare 502/503/504). Safe on POSTs
        # because every mutating call carries a unique idempotency_key.
        retry = Retry(total=5, backoff_factor=1.0,
                      status_forcelist=[502, 503, 504],
                      allowed_methods=["GET", "POST"], raise_on_status=False)
        self.s.mount("https://", HTTPAdapter(max_retries=retry))

    def _req(self, method: str, path: str, **kw) -> Any:
        r = self.s.request(method, self.base + path, timeout=30, **kw)
        if r.ok:
            return r.json() if r.content else None
        try:
            j = r.json()
            detail = j.get("detail") or j.get("title") or r.text
        except Exception:
            detail = r.text
        raise APIError(r.status_code, detail)

    def get_scenario(self, slug: str) -> dict:
        return self._req("GET", f"/api/v1/scenarios/{slug}")["scenario"]

    def start_run(self, slug: str) -> dict:
        return self._req("POST", "/api/v1/runs", json={"scenario_slug": slug})["run"]

    def get_run(self, run_id: str) -> dict:
        return self._req("GET", f"/api/v1/runs/{run_id}")

    def get_market(self, run_id: str, symbols: list[str], lookback: int) -> dict:
        return self._req("GET", f"/api/v1/runs/{run_id}/market", params={
            "symbols": ",".join(symbols),
            "lookback": lookback,
        })

    def queue_trade(self, run_id: str, symbol: str, side: str, qty: int, reasoning: str) -> dict:
        return self._req("POST", f"/api/v1/runs/{run_id}/trades", json={
            "symbol": symbol, "side": side, "quantity": qty, "reasoning": reasoning,
            "idempotency_key": str(uuid.uuid4()),
        })["order"]

    def step(self, run_id: str, count: int = 1) -> dict:
        return self._req("POST", f"/api/v1/runs/{run_id}/step", json={
            "count": count, "idempotency_key": str(uuid.uuid4()),
        })

    def results(self, run_id: str) -> dict:
        return self._req("GET", f"/api/v1/runs/{run_id}/results")["results"]

    def publish(self, run_id: str) -> dict:
        return self._req("POST", f"/api/v1/runs/{run_id}/publish")


# =============================================================================
# Claude agent
# =============================================================================

# USD per 1M tokens. Refreshed manually when models change.
PRICING = {
    "claude-haiku-4-5":  {"input": 1.00, "output":  5.00},
    "claude-sonnet-4-6": {"input": 3.00, "output": 15.00},
    "claude-opus-4-7":   {"input": 5.00, "output": 25.00},
    "claude-opus-4-6":   {"input": 5.00, "output": 25.00},
}


PLACE_TRADES_TOOL = {
    "name": "place_trades",
    "description": (
        "Submit zero or more trade orders for this turn. Orders queue now and "
        "fill at the NEXT bar's open price plus per-symbol slippage. Pass an "
        "empty trades array to skip trading this turn — often the right call."
    ),
    "input_schema": {
        "type": "object",
        "properties": {
            "rationale": {
                "type": "string",
                "description": "One short sentence on why you are taking this action (or none).",
            },
            "trades": {
                "type": "array",
                "items": {
                    "type": "object",
                    "properties": {
                        "symbol": {
                            "type": "string",
                            "description": "Ticker symbol from the scenario universe.",
                        },
                        "side": {
                            "type": "string",
                            "enum": ["buy", "sell", "short", "cover"],
                            "description": "buy/sell for longs; short/cover only if short_enabled.",
                        },
                        "quantity": {
                            "type": "integer",
                            "description": "Whole shares, positive integer.",
                        },
                    },
                    "required": ["symbol", "side", "quantity"],
                    "additionalProperties": False,
                },
            },
        },
        "required": ["rationale", "trades"],
        "additionalProperties": False,
    },
}


def build_system_prompt(scen: dict) -> str:
    """Per-run static prompt. Stays byte-stable for every turn in the run so
    the prompt cache hits on call #2 onward."""
    short_yes = "ENABLED" if scen.get("short_enabled") else "DISABLED"
    lev = scen.get("leverage_cap", 1)
    universe = scen["universe"]
    slip = scen.get("slippage_bps") or {}
    slip_lines = "\n".join(f"  {s}: {slip.get(s, '?')} bps" for s in universe)

    return f"""You are a quantitative trading agent operating a portfolio over a frozen historical-bar scenario on the BotTrade Benchmark API. Goal: maximize risk-adjusted return (return %, Sharpe, low drawdown) over the timeline.

MARKET MODEL
- One bar at a time. You see OHLCV bars up to and including the current sim_time. No lookahead.
- Orders you queue this turn fill at the NEXT bar's open price plus per-symbol slippage. No same-bar fills.
- You'll receive feedback on the previous turn's fills and any rejected orders at the start of each turn.

DECISION TOOL
You have one tool, `place_trades`. Each call submits zero or more orders.
- side: buy/sell for long positions; short/cover for shorts (only if shorting is enabled).
- quantity: positive integer (whole shares).
- Empty trades array = "do nothing this turn". A valid and often correct choice.

SCENARIO
- Universe ({len(universe)} symbols): {", ".join(universe)}
- Bar resolution: {scen.get("bar_resolution", "1Hour")}
- Window: {scen.get("start_ts")} → {scen.get("end_ts")}
- Starting cash: ${scen.get("starting_cash", 0):,.2f}
- Leverage cap: {lev}x  (long+short notional ≤ {lev} × equity)
- Short selling: {short_yes}
- Slippage per fill:
{slip_lines}

RISK
- Equity below maintenance margin (notional / (2 × {lev})) → all positions force-close at the next bar's open. Run over.
- You cannot sell shares you don't own. You cannot cover a short you don't hold.
- Insufficient buying power → order is rejected outright; you'll see the rejection next turn.

STYLE
- Be decisive but patient. Every fill pays slippage. Over-trading erodes returns.
- Empty turns are common in good strategies. Don't trade just because you can.
- The `rationale` you write is the only memory you carry forward — say WHY in one sentence (signal, regime, event) so future-you can reason about it next turn."""


def build_user_message(
    market: dict,
    run_snap: dict,
    last_step_fills: list[dict],
    last_rejections: list[str],
) -> str:
    """Per-turn state — bars, positions, cash, prior-turn outcomes.

    Everything that changes turn-to-turn lives here so the cached system
    prefix stays valid."""
    pos = {p["symbol"]: p["quantity"] for p in (run_snap.get("positions") or [])}
    eq = run_snap.get("last_equity") or {}
    run = run_snap.get("run", {})
    cash = run.get("cash", 0)
    equity = eq.get("equity", cash)
    starting = run.get("starting_cash", 100000) or 100000
    pnl_pct = (equity / starting - 1) * 100 if starting else 0.0

    bars_lines: list[str] = []
    for sym, bars in market["bars"].items():
        bars_lines.append(f"  {sym}:")
        for b in bars:
            bars_lines.append(
                f"    {b['ts']} O={b['open']:.2f} H={b['high']:.2f} "
                f"L={b['low']:.2f} C={b['close']:.2f} V={int(b['volume'])}"
            )

    pos_str = ", ".join(f"{s}:{q}" for s, q in pos.items()) if pos else "(none)"

    sections = [
        f"sim_time: {market['sim_time']}",
        f"cash:     ${cash:,.2f}",
        f"equity:   ${equity:,.2f}  ({pnl_pct:+.2f}% vs start)",
        f"positions: {pos_str}",
    ]
    if last_step_fills:
        sections.append("LAST TURN'S FILLS:")
        for f in last_step_fills:
            sections.append(
                f"  filled {f['side']} {f['quantity']} {f['symbol']} @ "
                f"{f['fill_price']:.2f}  (slippage {f.get('slippage_bps', 0)} bps, "
                f"realized PnL ${f.get('realized_pnl', 0):+.2f})"
            )
    if last_rejections:
        sections.append("LAST TURN'S REJECTIONS:")
        for r in last_rejections:
            sections.append(f"  {r}")
    sections.append("RECENT BARS:")
    sections.extend(bars_lines)
    sections.append("\nCall `place_trades` with your decision for this turn.")
    return "\n".join(sections)


@dataclass
class Decision:
    rationale: str
    trades: list[dict]
    raw_usage: dict = field(default_factory=dict)


class ClaudeAgent:
    def __init__(self, api_key: str, model: str):
        # SDK auto-retries 408/409/429/5xx with exponential backoff.
        self.client = anthropic.Anthropic(api_key=api_key)
        self.model = model
        self.system_prompt: str | None = None

        self.total_input = 0
        self.total_cache_write = 0
        self.total_cache_read = 0
        self.total_output = 0
        self.call_count = 0

    def set_scenario(self, scen: dict) -> None:
        self.system_prompt = build_system_prompt(scen)

    def decide(
        self,
        market: dict,
        run_snap: dict,
        last_step_fills: list[dict],
        last_rejections: list[str],
    ) -> Decision:
        assert self.system_prompt is not None, "call set_scenario() first"

        user_msg = build_user_message(market, run_snap, last_step_fills, last_rejections)

        # cache_control on the last system block caches both the tool schemas
        # (rendered first) and the system prompt. The user message — which
        # varies every turn — sits after the cache and pays full price.
        # Note: Haiku 4.5's min cacheable prefix is 4096 tokens, so on small
        # universes the cache may silently not activate. That's fine; it'll
        # work for Sonnet/Opus and for larger universes.
        resp = self.client.messages.create(
            model=self.model,
            max_tokens=2048,
            system=[{
                "type": "text",
                "text": self.system_prompt,
                "cache_control": {"type": "ephemeral"},
            }],
            tools=[PLACE_TRADES_TOOL],
            tool_choice={"type": "tool", "name": "place_trades"},
            messages=[{"role": "user", "content": user_msg}],
        )

        u = resp.usage
        self.total_input += u.input_tokens
        self.total_cache_write += u.cache_creation_input_tokens or 0
        self.total_cache_read += u.cache_read_input_tokens or 0
        self.total_output += u.output_tokens
        self.call_count += 1

        # tool_choice forces exactly one tool_use block.
        tool_use = next((b for b in resp.content if b.type == "tool_use"), None)
        if tool_use is None:
            return Decision(rationale="(model returned no tool_use)", trades=[])

        args = tool_use.input or {}
        return Decision(
            rationale=str(args.get("rationale", ""))[:200],
            trades=list(args.get("trades", [])),
            raw_usage=dict(u.model_dump()) if hasattr(u, "model_dump") else {},
        )

    def cost_summary(self) -> dict:
        p = PRICING.get(self.model)
        if p is None:
            # Unknown model — leave the cost blank rather than guess.
            return {"model": self.model, "calls": self.call_count, "estimated_usd": None}
        cost = (
            self.total_input * p["input"] / 1_000_000
            + self.total_cache_write * p["input"] * 1.25 / 1_000_000
            + self.total_cache_read * p["input"] * 0.10 / 1_000_000
            + self.total_output * p["output"] / 1_000_000
        )
        return {
            "model": self.model,
            "calls": self.call_count,
            "input_tokens": self.total_input,
            "cache_write_tokens": self.total_cache_write,
            "cache_read_tokens": self.total_cache_read,
            "output_tokens": self.total_output,
            "estimated_usd": round(cost, 4),
        }


# =============================================================================
# Main loop
# =============================================================================

def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    p.add_argument("--bot-api-key", default=os.environ.get("BOT_API_KEY"),
                   help="BotTrade API key. Defaults to $BOT_API_KEY.")
    p.add_argument("--anthropic-api-key", default=os.environ.get("ANTHROPIC_API_KEY"),
                   help="Anthropic API key. Defaults to $ANTHROPIC_API_KEY.")
    p.add_argument("--api-base", default="https://bot-trade.org",
                   help="Override for local testing.")
    p.add_argument("--scenario", default="tech-2024-q2",
                   help="Scenario slug to trade.")
    p.add_argument("--model", default="claude-haiku-4-5",
                   help="Anthropic model id (default claude-haiku-4-5 for cost).")
    p.add_argument("--decide-every", type=int, default=6,
                   help="Bars between LLM calls (default 6 ≈ one trading day on 1H bars).")
    p.add_argument("--lookback", type=int, default=30,
                   help="Bars of history shown to the model per call (default 30).")
    p.add_argument("--max-decisions", type=int, default=10_000,
                   help="Safety cap on LLM calls.")
    p.add_argument("--max-bars", type=int, default=100_000,
                   help="Safety cap on simulator steps.")
    p.add_argument("--publish", action="store_true",
                   help="Publish final results to the public leaderboard.")
    args = p.parse_args(argv)

    if not args.bot_api_key:
        p.error("--bot-api-key or BOT_API_KEY env var required. "
                "Get one: curl -X POST https://bot-trade.org/api/v1/keys")
    if not args.anthropic_api_key:
        p.error("--anthropic-api-key or ANTHROPIC_API_KEY env var required. "
                "Get one at https://console.anthropic.com")

    api = BotTradeClient(args.bot_api_key, args.api_base)
    claude = ClaudeAgent(args.anthropic_api_key, args.model)

    scen = api.get_scenario(args.scenario)
    print(f"→ Scenario: {scen['slug']} ({scen.get('name', '')})")
    print(f"  universe ({len(scen['universe'])}): {', '.join(scen['universe'])}")
    print(f"  starting cash: ${scen.get('starting_cash', 0):,.2f}  "
          f"leverage: {scen.get('leverage_cap', 1)}x")
    print(f"  bar resolution: {scen.get('bar_resolution')}")
    print(f"  llm: {args.model}  decide-every={args.decide_every}  lookback={args.lookback}")
    print()

    run = api.start_run(scen["slug"])
    print(f"→ Run: {run['id']}  sim_time={run['sim_time']}")
    print()

    claude.set_scenario(scen)

    step_count = 0
    decisions_made = 0
    last_step_fills: list[dict] = []
    last_rejections: list[str] = []

    t0 = time.time()
    while step_count < args.max_bars:
        if step_count % args.decide_every == 0 and decisions_made < args.max_decisions:
            run_snap = api.get_run(run["id"])
            market = api.get_market(run["id"], scen["universe"], args.lookback)

            try:
                decision = claude.decide(market, run_snap, last_step_fills, last_rejections)
            except anthropic.APIError as e:
                print(f"  [llm error: {e}] — skipping decision this turn")
                decision = Decision(rationale=f"llm error: {e}", trades=[])

            decisions_made += 1
            last_rejections = []
            sim_now = run_snap.get("run", {}).get("sim_time", "?")
            print(f"[decision {decisions_made} @ {sim_now}]")
            print(f"  rationale: {decision.rationale}")

            for t in decision.trades:
                try:
                    symbol = str(t["symbol"]).upper()
                    side = str(t["side"]).lower()
                    qty = int(t["quantity"])
                except (KeyError, TypeError, ValueError) as e:
                    msg = f"malformed order {t!r}: {e}"
                    last_rejections.append(msg)
                    print(f"  ! {msg}")
                    continue

                try:
                    api.queue_trade(run["id"], symbol, side, qty, decision.rationale)
                    print(f"  → queued {side} {qty} {symbol}")
                except APIError as e:
                    msg = f"{side} {qty} {symbol}: {e.detail}"
                    last_rejections.append(msg)
                    print(f"  ! rejected: {msg}")

        step_result = api.step(run["id"], count=1)
        last_step_fills = step_result.get("fills") or []
        step_count += 1

        if step_count % 50 == 0:
            print(f"  [bar {step_count}] sim_time={step_result.get('new_sim_time')}  "
                  f"equity=${step_result['equity']:,.2f}  cash=${step_result['cash']:,.2f}")

        if step_result.get("done"):
            print(f"\n✓ Scenario complete after {step_count} bars "
                  f"({time.time() - t0:.1f}s wall).")
            break
        if step_result.get("liquidated"):
            print(f"\n✗ Liquidated after {step_count} bars.")
            break

    # Results.
    results = api.results(run["id"])
    print("\n=== RESULTS ===")
    print(f"  final equity: ${results['final_equity']:,.2f}")
    print(f"  return:       {results['return_pct']:+.2f}%")
    print(f"  sharpe:       {results.get('sharpe')}")
    print(f"  sortino:      {results.get('sortino')}")
    print(f"  max drawdown: {results.get('max_drawdown')}")
    print(f"  volatility:   {results.get('volatility')}")
    print(f"  trades:       {results['trade_count']}")
    print(f"  liquidated:   {results.get('liquidated')}")

    cost = claude.cost_summary()
    print("\n=== LLM COST ===")
    print(f"  model:         {cost['model']}")
    print(f"  calls:         {cost['calls']}")
    if cost.get("estimated_usd") is not None:
        print(f"  input tokens:  {cost['input_tokens']:>10,}  (fresh, not cached)")
        print(f"  cache writes:  {cost['cache_write_tokens']:>10,}  (1.25× input rate)")
        print(f"  cache reads:   {cost['cache_read_tokens']:>10,}  (~0.10× input rate)")
        print(f"  output tokens: {cost['output_tokens']:>10,}")
        print(f"  est. cost:     ${cost['estimated_usd']:.4f}")
    else:
        print(f"  (unknown model pricing — token totals below)")
        print(f"  input/cache_w/cache_r/output: "
              f"{claude.total_input}/{claude.total_cache_write}/"
              f"{claude.total_cache_read}/{claude.total_output}")

    if args.publish:
        api.publish(run["id"])
        print("\n✓ Published to leaderboard.")

    return 0


if __name__ == "__main__":
    sys.exit(main())
