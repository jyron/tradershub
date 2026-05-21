#!/usr/bin/env python3
"""
Hosted-bot adapter for Google Gemini models. Generalized from
bots/gemini_bot.py — same google-genai SDK calls, env-driven config.

NOTE: the canonical Google AI SDK is `google-genai` (`from google import genai`).
The older `google-generativeai` package is deprecated; do not import it.

Env contract:
  BOT_ID            uuid of the bot in the bots table
  BOT_LLM_API_KEY   submitter-supplied Google AI Studio key
  BOT_MODEL_ID      e.g. "gemini-2.5-pro", "gemini-2.5-flash"
  BACKFILL_JOB_ID   set when invoked from the backfill runner

Usage:
  python -m bots.google_compat_bot --replay 30
  python -m bots.google_compat_bot --live
  python -m bots.google_compat_bot --once
"""
from __future__ import annotations
import os
from bots import common

try:
    from google import genai
    from google.genai import types
except ImportError:
    common.die("google-genai SDK not installed. pip install google-genai")

MODEL = os.getenv("BOT_MODEL_ID", "").strip()
API_KEY = os.getenv("BOT_LLM_API_KEY", "").strip()
# Match the official Gemini bot's thinking budget defaults; submitters can
# override via env if they want.
THINKING_BUDGET = int(os.getenv("BOT_GEMINI_THINKING_BUDGET", "1024"))
MAX_OUTPUT_TOKENS = int(os.getenv("BOT_GEMINI_MAX_OUTPUT_TOKENS", "1024"))

if not MODEL:
    common.die("BOT_MODEL_ID env var is required")
if not API_KEY:
    common.die("BOT_LLM_API_KEY env var is required")


def decide_llm(system_prompt: str, user_prompt: str) -> str:
    client = genai.Client(api_key=API_KEY)
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
    common.run_cli("google", decide_llm)
