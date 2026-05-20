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
# gemini-2.5-pro spends a large "thinking" budget inside max_output_tokens;
# 400 is too low and yields finish_reason=MAX_TOKENS with no text parts.
MAX_OUTPUT_TOKENS = int(os.getenv("GEMINI_MAX_OUTPUT_TOKENS", "2048"))


def _response_text(resp) -> str:
    """Read candidate text without using resp.text (raises when parts are empty)."""
    candidates = getattr(resp, "candidates", None) or []
    if not candidates:
        return ""
    content = candidates[0].content
    if not content or not content.parts:
        return ""
    return "".join(p.text for p in content.parts if getattr(p, "text", None))


def decide_llm(system_prompt: str, user_prompt: str) -> str:
    genai.configure(api_key=common.llm_key("gemini"))
    model = genai.GenerativeModel(
        model_name=MODEL,
        system_instruction=system_prompt,
        generation_config={
            "response_mime_type": "application/json",
            "max_output_tokens": MAX_OUTPUT_TOKENS,
            "thinking_config": {"thinking_budget": 1024},
        },
    )
    resp = model.generate_content(user_prompt)
    return _response_text(resp)


if __name__ == "__main__":
    common.run_cli("gemini", decide_llm)
