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
    import google.generativeai as genai
except ImportError:
    common.die("google-generativeai SDK not installed. pip install google-generativeai")

MODEL = os.getenv("GEMINI_MODEL", "gemini-2.5-pro")


def decide_llm(system_prompt: str, user_prompt: str) -> str:
    genai.configure(api_key=common.llm_key("gemini"))
    model = genai.GenerativeModel(
        model_name=MODEL,
        system_instruction=system_prompt,
        generation_config={
            "response_mime_type": "application/json",
            "max_output_tokens": 400,
        },
    )
    resp = model.generate_content(user_prompt)
    return getattr(resp, "text", "") or ""


if __name__ == "__main__":
    common.run_cli("gemini", decide_llm)
