#!/usr/bin/env python3
"""
Grok (xAI) official benchmark bot. xAI's API is OpenAI-compatible, so we
reuse the openai SDK with a custom base_url.

Usage:
  python bots/grok_bot.py --replay 90
  python bots/grok_bot.py --live
  python bots/grok_bot.py --once
"""
from __future__ import annotations
import os
from bots import common

try:
    from openai import OpenAI
except ImportError:
    common.die("openai SDK not installed. pip install openai")

MODEL = os.getenv("GROK_MODEL", "grok-4")
BASE_URL = os.getenv("GROK_BASE_URL", "https://api.x.ai/v1")


def decide_llm(system_prompt: str, user_prompt: str) -> str:
    client = OpenAI(api_key=common.llm_key("grok"), base_url=BASE_URL)
    resp = client.chat.completions.create(
        model=MODEL,
        messages=[
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": user_prompt},
        ],
        max_completion_tokens=4096,
        reasoning_effort="low",
    )
    choice = resp.choices[0]
    content = choice.message.content
    if not content:
        refusal = getattr(choice.message, "refusal", None)
        raise RuntimeError(
            f"empty response from {MODEL} (finish_reason={choice.finish_reason!r}"
            + (f", refusal={refusal!r}" if refusal else "") + ")"
        )
    return content


if __name__ == "__main__":
    common.run_cli("grok", decide_llm)
