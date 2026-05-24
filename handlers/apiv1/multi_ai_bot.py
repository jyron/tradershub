#!/usr/bin/env python3
"""
multi_ai_bot.py — minimal OpenAI/xAI/Google reference agent for BotTrade.

The script loads .env.local/.env, creates one benchmark run, asks an LLM for
JSON trade decisions every N bars, submits the trades, steps to the end, and
optionally publishes the result.

Requirements:
    pip install requests

Examples:
    python handlers/apiv1/multi_ai_bot.py --provider openai --scenario tech-2024-q2 --publish
    python handlers/apiv1/multi_ai_bot.py --provider xai --model grok-3-mini --scenario fed-pivot-sep-oct-2024 --publish
    python handlers/apiv1/multi_ai_bot.py --provider google --model gemini-2.5-flash --scenario trump-trade-q4-2024 --publish
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys
import time
import uuid
from typing import Any

import requests


DEFAULT_MODELS = {
    "openai": "gpt-4o-mini",
    "xai": "grok-3-mini",
    "google": "gemini-2.5-flash",
}


def load_env() -> None:
    for path in (".env.local", ".env"):
        if not os.path.exists(path):
            continue
        with open(path, "r", encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#") or "=" not in line:
                    continue
                key, value = line.split("=", 1)
                os.environ.setdefault(key.strip(), value.strip().strip('"').strip("'"))


class BotTrade:
    def __init__(self, api_key: str, base: str):
        self.base = base.rstrip("/")
        self.s = requests.Session()
        self.s.headers["X-API-Key"] = api_key

    def req(self, method: str, path: str, **kwargs) -> Any:
        r = self.s.request(method, self.base + path, timeout=45, **kwargs)
        if r.ok:
            return r.json() if r.content else None
        raise RuntimeError(f"{method} {path} failed: HTTP {r.status_code}: {r.text}")

    def retry_req(self, method: str, path: str, attempts: int = 6, **kwargs) -> Any:
        last = None
        for i in range(attempts):
            try:
                return self.req(method, path, **kwargs)
            except RuntimeError as e:
                last = e
                msg = str(e)
                retryable = (
                    "HTTP 502" in msg
                    or "HTTP 503" in msg
                    or "database is locked" in msg
                    or "SQLITE_BUSY" in msg
                    or "SQLITE_LOCKED" in msg
                )
                if not retryable or i == attempts - 1:
                    raise
                time.sleep(min(20, 1.5 * (i + 1)))
        raise last  # type: ignore[misc]

    def scenario(self, slug: str) -> dict:
        return self.req("GET", f"/api/v1/scenarios/{slug}")["scenario"]

    def start(self, slug: str) -> dict:
        return self.req("POST", "/api/v1/runs", json={"scenario_slug": slug})["run"]

    def run(self, run_id: str) -> dict:
        return self.req("GET", f"/api/v1/runs/{run_id}")

    def market(self, run_id: str, symbols: list[str] | None = None, lookback: int = 1) -> dict:
        params = {"lookback": lookback}
        if symbols:
            params["symbols"] = ",".join(symbols)
        return self.req("GET", f"/api/v1/runs/{run_id}/market", params=params)

    def trade(self, run_id: str, symbol: str, side: str, quantity: int, reasoning: str) -> None:
        self.retry_req("POST", f"/api/v1/runs/{run_id}/trades", json={
            "symbol": symbol,
            "side": side,
            "quantity": quantity,
            "reasoning": reasoning[:500],
            "idempotency_key": str(uuid.uuid4()),
        })

    def step(self, run_id: str) -> dict:
        return self.retry_req("POST", f"/api/v1/runs/{run_id}/step", json={
            "count": 1,
            "idempotency_key": str(uuid.uuid4()),
        })

    def results(self, run_id: str) -> dict:
        return self.req("GET", f"/api/v1/runs/{run_id}/results")["results"]

    def publish(self, run_id: str) -> None:
        self.req("POST", f"/api/v1/runs/{run_id}/publish")


def current_positions(run_snap: dict) -> dict[str, int]:
    return {p["symbol"]: int(p["quantity"]) for p in (run_snap.get("positions") or [])}


def pick_focus(scan: dict, positions: dict[str, int], n: int = 8) -> list[str]:
    focus = set(positions.keys())
    movers = []
    for symbol, bars in scan.get("bars", {}).items():
        if not bars:
            continue
        b = bars[-1]
        pct = abs((b["close"] - b["open"]) / b["open"]) if b.get("open") else 0
        movers.append((pct, symbol))
    for _, symbol in sorted(movers, reverse=True)[:n]:
        focus.add(symbol)
    return sorted(focus)


def build_prompt(scenario: dict, run_snap: dict, scan: dict, detail: dict, fills: list[dict]) -> str:
    run = run_snap.get("run", {})
    equity = (run_snap.get("last_equity") or {}).get("equity", run.get("cash", 0))
    positions = current_positions(run_snap)

    lines = [
        "You are trading a deterministic BotTrade benchmark scenario.",
        "Return JSON only. Do not wrap it in markdown.",
        "",
        "Schema:",
        '{"rationale":"short reason","trades":[{"symbol":"AAPL","side":"buy","quantity":10}]}',
        "",
        "Rules:",
        "- trades can be empty.",
        "- side must be one of buy, sell, short, cover.",
        "- quantity must be a positive whole number.",
        "- orders fill on the next bar open with slippage.",
        "- avoid overtrading.",
        "",
        f"Scenario: {scenario['slug']} — {scenario.get('name', '')}",
        f"Universe: {', '.join(scenario['universe'])}",
        f"Short enabled: {scenario.get('short_enabled')}",
        f"Leverage cap: {scenario.get('leverage_cap')}x",
        f"sim_time: {scan.get('sim_time')}",
        f"cash: {run.get('cash')}",
        f"equity: {equity}",
        f"positions: {positions or {}}",
    ]

    if fills:
        lines.append(f"last_fills: {fills}")

    lines.append("\nCurrent bar for every symbol:")
    for symbol, bars in scan.get("bars", {}).items():
        if not bars:
            continue
        b = bars[-1]
        lines.append(f"{symbol}: O={b['open']} H={b['high']} L={b['low']} C={b['close']} V={int(b['volume'])}")

    lines.append("\nRecent bars for held symbols and biggest movers:")
    for symbol, bars in detail.get("bars", {}).items():
        compact = [
            [b["ts"], round(b["open"], 2), round(b["high"], 2), round(b["low"], 2), round(b["close"], 2), int(b["volume"])]
            for b in bars
        ]
        lines.append(f"{symbol}: {compact}")

    return "\n".join(lines)


def extract_json(text: str) -> dict:
    text = text.strip()
    if text.startswith("```"):
        text = re.sub(r"^```(?:json)?", "", text).strip()
        text = re.sub(r"```$", "", text).strip()
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        match = re.search(r"\{.*\}", text, re.S)
        if not match:
            raise
        return json.loads(match.group(0))


class LLM:
    def __init__(self, provider: str, model: str, api_key: str):
        self.provider = provider
        self.model = model
        self.api_key = api_key

    def decide(self, prompt: str) -> dict:
        if self.provider in {"openai", "xai"}:
            base = "https://api.openai.com/v1" if self.provider == "openai" else "https://api.x.ai/v1"
            r = requests.post(
                base + "/chat/completions",
                headers={"Authorization": f"Bearer {self.api_key}", "Content-Type": "application/json"},
                json={
                    "model": self.model,
                    "messages": [
                        {"role": "system", "content": "Return only valid JSON matching the requested schema."},
                        {"role": "user", "content": prompt},
                    ],
                    "temperature": 0.2,
                    "response_format": {"type": "json_object"},
                },
                timeout=90,
            )
            if not r.ok:
                raise RuntimeError(f"{self.provider} HTTP {r.status_code}: {r.text}")
            return extract_json(r.json()["choices"][0]["message"]["content"])

        if self.provider == "google":
            url = f"https://generativelanguage.googleapis.com/v1beta/models/{self.model}:generateContent"
            r = requests.post(
                url,
                params={"key": self.api_key},
                json={
                    "contents": [{"role": "user", "parts": [{"text": prompt}]}],
                    "generationConfig": {
                        "temperature": 0.2,
                        "responseMimeType": "application/json",
                    },
                },
                timeout=90,
            )
            if not r.ok:
                raise RuntimeError(f"google HTTP {r.status_code}: {r.text}")
            parts = r.json()["candidates"][0]["content"]["parts"]
            return extract_json("".join(p.get("text", "") for p in parts))

        raise ValueError(f"unknown provider: {self.provider}")


def main() -> int:
    load_env()

    p = argparse.ArgumentParser(description="Minimal OpenAI/xAI/Google BotTrade runner.")
    p.add_argument("--provider", choices=["openai", "xai", "google"], required=True)
    p.add_argument("--model", help="Provider model ID. Defaults to a cheap/fast model for each provider.")
    p.add_argument("--bot-api-key", default=os.environ.get("BOT_API_KEY"))
    p.add_argument("--api-base", default=os.environ.get("BOTTRADE_API", "https://bot-trade.org"))
    p.add_argument("--scenario", default="sandbox-nov-2024")
    p.add_argument("--decide-every", type=int, default=8)
    p.add_argument("--lookback", type=int, default=24)
    p.add_argument("--max-bars", type=int, default=100_000)
    p.add_argument("--publish", action="store_true")
    args = p.parse_args()

    model = args.model or DEFAULT_MODELS[args.provider]
    key_env = {"openai": "OPENAI_API_KEY", "xai": "XAI_API_KEY", "google": "GOOGLE_API_KEY"}[args.provider]
    provider_key = os.environ.get(key_env)

    if not args.bot_api_key:
        p.error("--bot-api-key or BOT_API_KEY is required")
    if not provider_key:
        p.error(f"{key_env} is required in the environment or .env.local")

    api = BotTrade(args.bot_api_key, args.api_base)
    llm = LLM(args.provider, model, provider_key)

    scenario = api.scenario(args.scenario)
    run = api.start(args.scenario)
    print(f"Scenario: {scenario['slug']} ({scenario.get('name', '')})")
    print(f"Provider: {args.provider}  model={model}")
    print(f"Run: {run['id']}")

    last_fills: list[dict] = []
    decisions = 0
    started = time.time()

    for bar in range(args.max_bars):
        if bar % args.decide_every == 0:
            run_snap = api.run(run["id"])
            scan = api.market(run["id"], lookback=1)
            focus = pick_focus(scan, current_positions(run_snap))
            detail = api.market(run["id"], symbols=focus, lookback=args.lookback)
            prompt = build_prompt(scenario, run_snap, scan, detail, last_fills)

            try:
                decision = llm.decide(prompt)
            except Exception as e:
                print(f"[bar {bar}] LLM error: {e}. Skipping this decision.")
                decision = {"rationale": f"LLM error: {e}", "trades": []}

            decisions += 1
            rationale = str(decision.get("rationale", ""))[:300]
            trades = decision.get("trades") or []
            print(f"[decision {decisions} bar={bar}] {rationale} ({len(trades)} trades)")

            for t in trades:
                try:
                    symbol = str(t["symbol"]).upper()
                    side = str(t["side"]).lower()
                    quantity = int(t["quantity"])
                    if side not in {"buy", "sell", "short", "cover"} or quantity <= 0:
                        raise ValueError("bad side or quantity")
                    api.trade(run["id"], symbol, side, quantity, rationale)
                    print(f"  queued {side} {quantity} {symbol}")
                except Exception as e:
                    print(f"  skipped malformed/rejected trade {t}: {e}")

        step = api.step(run["id"])
        last_fills = step.get("fills") or []

        if (bar + 1) % 50 == 0:
            print(f"[bar {bar + 1}] equity=${step['equity']:,.2f} sim_time={step.get('new_sim_time')}")

        if step.get("done") or step.get("liquidated"):
            break

    results = api.results(run["id"])
    print("\nRESULTS")
    print(f"final_equity: ${results['final_equity']:,.2f}")
    print(f"return_pct:   {results['return_pct']:+.2f}%")
    print(f"sharpe:       {results.get('sharpe')}")
    print(f"sortino:      {results.get('sortino')}")
    print(f"max_drawdown: {results.get('max_drawdown')}")
    print(f"trades:       {results.get('trade_count')}")
    print(f"elapsed:      {time.time() - started:.1f}s")

    if args.publish:
        api.publish(run["id"])
        print("Published to leaderboard.")

    return 0


if __name__ == "__main__":
    sys.exit(main())
