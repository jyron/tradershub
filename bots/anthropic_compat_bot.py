#!/usr/bin/env python3
"""
Hosted-bot adapter for Anthropic-native models. Generalized from
bots/claude_bot.py — same SDK call, but reads model + key + (optional)
base_url from the env vars the Go dynamic_bot_runner sets.

Env contract:
  BOT_ID            uuid of the bot in the bots table
  BOT_LLM_API_KEY   submitter-supplied Anthropic key
  BOT_MODEL_ID      e.g. "claude-sonnet-5", "claude-opus-4-7"
  BOT_BASE_URL      optional; only set for proxy / Bedrock-style setups
  BACKFILL_JOB_ID   set when invoked from the backfill runner

Usage:
  python -m bots.anthropic_compat_bot --replay 30
  python -m bots.anthropic_compat_bot --live
  python -m bots.anthropic_compat_bot --once
"""
from __future__ import annotations
import os
from bots import common

try:
    from anthropic import Anthropic
except ImportError:
    common.die("anthropic SDK not installed. pip install anthropic")

MODEL = os.getenv("BOT_MODEL_ID", "").strip()
API_KEY = os.getenv("BOT_LLM_API_KEY", "").strip()
BASE_URL = os.getenv("BOT_BASE_URL", "").strip() or None

if not MODEL:
    common.die("BOT_MODEL_ID env var is required")
if not API_KEY:
    common.die("BOT_LLM_API_KEY env var is required")


def decide_llm(system_prompt: str, user_prompt: str) -> str:
    client = (
        Anthropic(api_key=API_KEY, base_url=BASE_URL) if BASE_URL
        else Anthropic(api_key=API_KEY)
    )
    resp = client.messages.create(
        model=MODEL,
        max_tokens=1024,
        system=system_prompt,
        messages=[{"role": "user", "content": user_prompt}],
    )
    out = []
    for block in resp.content:
        if getattr(block, "type", None) == "text":
            out.append(block.text)
    return "".join(out)


if __name__ == "__main__":
    common.run_cli("anthropic", decide_llm)
