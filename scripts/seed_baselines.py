#!/usr/bin/env python3
"""
Idempotent seed for the 3 synthetic baseline bots: SPY buy-and-hold,
equal-weight, and a random walker. These run as tier='official' and are
flagged with is_baseline=true so the leaderboard can pin them to a
"reference line" row rather than treating them as competitors.

Requires the server to be running at BASE_URL.

After seeding, backfill them:
    python -m bots.baselines.runner --strategy spy_buyandhold --replay 30
    python -m bots.baselines.runner --strategy equal_weight   --replay 30
    python -m bots.baselines.runner --strategy random_walker  --replay 30
"""
from __future__ import annotations

import json
import os
import sys
from pathlib import Path

import requests

BASE_URL = os.getenv("BASE_URL", "http://localhost:3000")
KEYS_DIR = Path(__file__).resolve().parent.parent / "bots" / ".keys"


# Strategy keys match bots/baselines/strategies.py:STRATEGIES.
# "provider" gets stored as model_provider and used as the .keys/<name>.json
# filename; we use "baseline" prefix so they don't collide with LLM bots.
BASELINES = [
    {
        "strategy": "spy_buyandhold",
        "name": "SPY Buy & Hold",
        "description": "Baseline: spend ~all cash on SPY day 0, hold forever. The market itself.",
    },
    {
        "strategy": "equal_weight",
        "name": "Equal Weight (15)",
        "description": "Baseline: equal-dollar slice of every universe symbol, rebalanced monthly.",
    },
    {
        "strategy": "random_walker",
        "name": "Random Walker",
        "description": "Baseline: each day a random buy/sell/hold of a random symbol. The price of luck.",
    },
]


def find_baseline_bot(strategy: str) -> dict | None:
    try:
        r = requests.get(f"{BASE_URL}/api/leaderboard?tier=all&limit=200", timeout=10)
        r.raise_for_status()
    except Exception as e:
        die(f"could not reach BotTrade at {BASE_URL}: {e}\n"
            f"is the server running? try: go run main.go")
    data = r.json()
    for row in data.get("rankings", []):
        if row.get("is_baseline") and row.get("bot_name") in (b["name"] for b in BASELINES if b["strategy"] == strategy):
            return row
    return None


def register(spec: dict) -> dict:
    payload = {
        "name":          spec["name"],
        "description":   spec["description"],
        "creator_email": "baseline@bottrade.local",
        "is_official":   True,
        "is_baseline":   True,
    }
    r = requests.post(f"{BASE_URL}/api/bots/register", json=payload, timeout=10)
    if r.status_code == 429:
        die(f"rate limited registering baseline — wait an hour or restart the server")
    r.raise_for_status()
    return r.json()


def claim(bot_id: str) -> None:
    r = requests.post(f"{BASE_URL}/api/claim/{bot_id}", timeout=10)
    if r.status_code not in (200, 400):
        r.raise_for_status()


def save_key(strategy: str, bot_id: str, api_key: str) -> Path:
    KEYS_DIR.mkdir(parents=True, exist_ok=True)
    key_path = KEYS_DIR / f"{strategy}.json"
    with open(key_path, "w") as f:
        json.dump({"bot_id": bot_id, "api_key": api_key, "provider": strategy}, f, indent=2)
    os.chmod(key_path, 0o600)
    return key_path


def die(msg: str) -> None:
    print(f"error: {msg}", file=sys.stderr)
    sys.exit(1)


def main() -> None:
    print(f"seeding baselines against {BASE_URL}")
    print(f"keys will be saved to {KEYS_DIR}\n")

    for spec in BASELINES:
        existing = find_baseline_bot(spec["strategy"])
        if existing:
            print(f"  [skip] {spec['strategy']:18s} → already exists: {existing['bot_id'][:8]}…")
            continue

        print(f"  [new]  {spec['strategy']:18s} → registering '{spec['name']}'…")
        result = register(spec)
        bot_id = result["bot_id"]
        api_key = result["api_key"]
        claim(bot_id)
        key_path = save_key(spec["strategy"], bot_id, api_key)
        print(f"         id:  {bot_id}")
        print(f"         key: saved to {key_path}")

    print()
    print("done. next: backfill each strategy:")
    for spec in BASELINES:
        print(f"    python -m bots.baselines.runner --strategy {spec['strategy']} --replay 30")


if __name__ == "__main__":
    main()
