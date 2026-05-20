#!/usr/bin/env python3
"""
Gemini (Google) official benchmark bot.

Usage:
  python bots/gemini_bot.py --replay 90
  python bots/gemini_bot.py --live
  python bots/gemini_bot.py --once
"""
from __future__ import annotations
import os
from bots import common

try:
    from google import genai
    from google.genai import types
except ImportError:
    common.die("google-genai SDK not installed. pip install google-genai")

MODEL = os.getenv("GEMINI_MODEL", "gemini-2.5-pro")
# 2.5-pro's thinking budget is a separate token bucket in google-genai; the
# documented range is 128–32768. 1024 keeps replays cheap without starving it.
THINKING_BUDGET = int(os.getenv("GEMINI_THINKING_BUDGET", "1024"))
MAX_OUTPUT_TOKENS = int(os.getenv("GEMINI_MAX_OUTPUT_TOKENS", "1024"))


def decide_llm(system_prompt: str, user_prompt: str) -> str:
    client = genai.Client(api_key=common.llm_key("gemini"))
    resp = client.models.generate_content(
        model=MODEL,
        contents=user_prompt,
        config=types.GenerateContentConfig(
            system_instruction=system_prompt,
            response_mime_type="application/json",
            max_output_tokens=MAX_OUTPUT_TOKENS,
            thinking_config=types.ThinkingConfig(thinking_budget=THINKING_BUDGET),
        ),
    )
    return resp.text or ""


if __name__ == "__main__":
    common.run_cli("gemini", decide_llm)
