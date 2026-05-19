#!/usr/bin/env python3
"""
Claude (Anthropic) official benchmark bot.

Usage:
  python bots/claude_bot.py --replay 90     # backfill 90 trading days
  python bots/claude_bot.py --live           # one decision, post to API
  python bots/claude_bot.py --once           # dry-run live mode
"""
from __future__ import annotations
import os
from bots import common

try:
    from anthropic import Anthropic
except ImportError:
    common.die("anthropic SDK not installed. pip install anthropic")

MODEL = os.getenv("CLAUDE_MODEL", "claude-sonnet-4-5")


def decide_llm(system_prompt: str, user_prompt: str) -> str:
    client = Anthropic(api_key=common.llm_key("claude"))
    resp = client.messages.create(
        model=MODEL,
        max_tokens=400,
        system=system_prompt,
        messages=[{"role": "user", "content": user_prompt}],
    )
    # Concatenate all text blocks
    out = []
    for block in resp.content:
        if getattr(block, "type", None) == "text":
            out.append(block.text)
    return "".join(out)


if __name__ == "__main__":
    common.run_cli("claude", decide_llm)
