#!/usr/bin/env python3
"""
Idempotent seed for the 4 canonical "official" benchmark bots — one per
provider. Run this once after migrations and the showdown page will have
something to render.

What it does:
  - Registers a bot for each of: Claude, GPT, Gemini, Grok (via POST
    /api/bots/register) IF that provider doesn't already have an official bot.
  - Marks each with is_official=true so the cleanup script
    (cleanup_test_bots.sh) never touches them.
  - Auto-claims them so they can trade immediately (replay or live).
  - Writes the issued API keys to ./bots/.keys/<provider>.key — these are
    consumed by the corresponding bots/<provider>_bot.py script.

Re-running is safe: if a bot already exists for a provider it's left alone.

Requires the server to be running at BASE_URL.
"""

from __future__ import annotations

import json
import os
import sys
from pathlib import Path

import requests

BASE_URL = os.getenv("BASE_URL", "http://localhost:3000")
KEYS_DIR = Path(__file__).resolve().parent.parent / "bots" / ".keys"


# (provider, display name, description, system prompt persona)
# Keep names short — they show up on every leaderboard row.
OFFICIAL_BOTS = [
    {
        "provider": "claude",
        "name": "Claude Sonnet 4.5",
        "description": (
            "Anthropic's official benchmark bot. Reasoning-first momentum trader."
        ),
    },
    {
        "provider": "gpt",
        "name": "GPT-5 Trader",
        "description": (
            "OpenAI's official benchmark bot. Sentiment + technicals."
        ),
    },
    {
        "provider": "gemini",
        "name": "Gemini 2.5 Pro",
        "description": (
            "Google's official benchmark bot. Multimodal market reads."
        ),
    },
    {
        "provider": "grok",
        "name": "Grok 4",
        "description": (
            "xAI's official benchmark bot. Contrarian, X-pilled."
        ),
    },
]


def find_official_bot(provider: str) -> dict | None:
    """Returns the leaderboard entry for an official bot of this provider, or None."""
    try:
        r = requests.get(f"{BASE_URL}/api/leaderboard?limit=200", timeout=10)
        r.raise_for_status()
    except Exception as e:
        die(f"could not reach BotTrade at {BASE_URL}: {e}\n"
            f"is the server running? try: go run main.go")
    data = r.json()
    for row in data.get("rankings", []):
        if row.get("is_official") and row.get("model_provider") == provider:
            return row
    return None


def register(provider: str, name: str, description: str) -> dict:
    payload = {
        "name": name,
        "description": description,
        "creator_email": "official@bottrade.local",
        "model_provider": provider,
        "is_official": True,
    }
    r = requests.post(f"{BASE_URL}/api/bots/register", json=payload, timeout=10)
    if r.status_code == 429:
        die(f"rate limited registering {provider} — wait an hour or restart the server")
    r.raise_for_status()
    return r.json()


def claim(bot_id: str) -> None:
    r = requests.post(f"{BASE_URL}/api/claim/{bot_id}", timeout=10)
    if r.status_code not in (200, 400):  # 400 if already claimed (idempotent)
        r.raise_for_status()


def save_key(provider: str, bot_id: str, api_key: str) -> Path:
    KEYS_DIR.mkdir(parents=True, exist_ok=True)
    key_path = KEYS_DIR / f"{provider}.json"
    with open(key_path, "w") as f:
        json.dump({"bot_id": bot_id, "api_key": api_key, "provider": provider}, f, indent=2)
    # 0600 — nobody else should read these
    os.chmod(key_path, 0o600)
    return key_path


def die(msg: str) -> None:
    print(f"error: {msg}", file=sys.stderr)
    sys.exit(1)


def main() -> None:
    print(f"seeding official bots against {BASE_URL}")
    print(f"keys will be saved to {KEYS_DIR}")
    print()

    for spec in OFFICIAL_BOTS:
        provider = spec["provider"]
        existing = find_official_bot(provider)
        if existing:
            print(f"  [skip] {provider:7s} → already exists: "
                  f"{existing['bot_name']} ({existing['bot_id'][:8]}…)")
            # If we don't have a key on disk for this bot, we can't write one
            # back — the API key is only revealed at registration time. Note
            # this and continue.
            key_path = KEYS_DIR / f"{provider}.json"
            if not key_path.exists():
                print(f"    ⚠  no key file at {key_path}.")
                print(f"       you'll need to delete this bot from the DB and re-run if")
                print(f"       you want to drive it from the bot script.")
            continue

        print(f"  [new]  {provider:7s} → registering '{spec['name']}'…")
        result = register(provider, spec["name"], spec["description"])
        bot_id = result["bot_id"]
        api_key = result["api_key"]
        claim(bot_id)
        key_path = save_key(provider, bot_id, api_key)
        print(f"         id:  {bot_id}")
        print(f"         key: saved to {key_path.relative_to(Path.cwd()) if key_path.is_relative_to(Path.cwd()) else key_path}")

    print()
    print("done. next: run a replay to backfill 90 days of decisions:")
    print("    make replay-bots")
    print("or one at a time:")
    print("    python bots/claude_bot.py --replay 90")


if __name__ == "__main__":
    main()
