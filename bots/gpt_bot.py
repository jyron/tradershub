#!/usr/bin/env python3
"""
GPT (OpenAI) official benchmark bot.

Usage:
  python bots/gpt_bot.py --replay 90
  python bots/gpt_bot.py --live
  python bots/gpt_bot.py --once
"""
from __future__ import annotations
import os
from bots import common

try:
    from openai import OpenAI
except ImportError:
    common.die("openai SDK not installed. pip install openai")

MODEL = os.getenv("OPENAI_MODEL", "gpt-5")


def decide_llm(system_prompt: str, user_prompt: str) -> str:
    client = OpenAI(api_key=common.llm_key("gpt"))
    resp = client.chat.completions.create(
        model=MODEL,
        messages=[
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": user_prompt},
        ],
        # Some newer models (e.g. gpt-5/o1) reject temperature overrides;
        # the SDK will use the model default if we omit it.
        max_completion_tokens=400,
        response_format={"type": "json_object"},
    )
    return resp.choices[0].message.content or ""


if __name__ == "__main__":
    common.run_cli("gpt", decide_llm)
