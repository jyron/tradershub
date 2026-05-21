#!/usr/bin/env python3
"""
Generic OpenAI-compatible adapter for hosted bots.

Spawned by the Go dynamic_bot_runner with these env vars set:
  BOT_ID            uuid of the bot in the bots table
  BOT_LLM_API_KEY   submitter-supplied LLM key (decrypted in Go, passed via env)
  BOT_MODEL_ID      model name (e.g. "gpt-5-mini", "grok-4")
  BOT_BASE_URL      optional override (defaults to https://api.openai.com/v1)
  BACKFILL_JOB_ID   set only when invoked from the backfill runner

Used for any model that speaks OpenAI's /v1/chat/completions schema — OpenAI,
xAI, OpenRouter, local llama.cpp, Together, Groq, etc. Anthropic and Google
get their own thin shims when needed.

Usage:
  python -m bots.openai_compat_bot --replay 30
  python -m bots.openai_compat_bot --live
  python -m bots.openai_compat_bot --once
"""
from __future__ import annotations
import os
from bots import common

try:
    from openai import OpenAI
except ImportError:
    common.die("openai SDK not installed. pip install openai")

MODEL = os.getenv("BOT_MODEL_ID", "").strip()
API_KEY = os.getenv("BOT_LLM_API_KEY", "").strip()
BASE_URL = os.getenv("BOT_BASE_URL", "").strip() or None

if not MODEL:
    common.die("BOT_MODEL_ID env var is required")
if not API_KEY:
    common.die("BOT_LLM_API_KEY env var is required")


def decide_llm(system_prompt: str, user_prompt: str) -> str:
    client = OpenAI(api_key=API_KEY, base_url=BASE_URL) if BASE_URL else OpenAI(api_key=API_KEY)
    # Most modern endpoints honor `max_completion_tokens` (gpt-5 era); some
    # legacy endpoints want `max_tokens`. Try the new field first, fall back
    # gracefully without crashing — submitter's typo on model_id shouldn't
    # tank the whole bot.
    try:
        resp = client.chat.completions.create(
            model=MODEL,
            messages=[
                {"role": "system", "content": system_prompt},
                {"role": "user", "content": user_prompt},
            ],
            max_completion_tokens=4096,
        )
    except TypeError:
        resp = client.chat.completions.create(
            model=MODEL,
            messages=[
                {"role": "system", "content": system_prompt},
                {"role": "user", "content": user_prompt},
            ],
            max_tokens=4096,
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
    common.run_cli("openai_compat", decide_llm)
