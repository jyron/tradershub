#!/usr/bin/env python3
"""
Create OpenAI/xAI/Google BotTrade keys and run each provider once on every
currently-live scenario.

The script:
- loads .env.local and .env from the repo root
- creates one BotTrade API key per provider bot
- lists scenarios from the Benchmark API
- launches handlers/apiv1/multi_ai_bot.py for every provider/scenario pair
- publishes each completed run
- retries failed jobs up to 3 times without stopping other jobs

Usage:
    source venv/bin/activate
    python scripts/seed_multi_ai_runs.py

Common options:
    python scripts/seed_multi_ai_runs.py --api-base http://localhost:3000
    python scripts/seed_multi_ai_runs.py --max-workers 3 --retries 3
    python scripts/seed_multi_ai_runs.py --scenario tech-2024-q2 --scenario trump-trade-q4-2024
"""
from __future__ import annotations

import argparse
import concurrent.futures
import json
import os
import subprocess
import sys
import threading
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import requests


REPO_ROOT = Path(__file__).resolve().parents[1]
BOT_SCRIPT = REPO_ROOT / "handlers" / "apiv1" / "multi_ai_bot.py"
KEY_DIR = Path("/tmp/bottrade-keys")
LOG_DIR = Path("/tmp/bottrade-run-logs")

PROVIDERS = {
    "openai": {"name": "GPT-4o Mini", "model": "gpt-4o-mini", "key_env": "OPENAI_API_KEY"},
    "xai": {"name": "Grok 3 Mini", "model": "grok-3-mini", "key_env": "XAI_API_KEY"},
    "google": {"name": "Gemini 2.5 Flash", "model": "gemini-2.5-flash", "key_env": "GOOGLE_API_KEY"},
}

PROVIDER_LOCKS = {provider: threading.Lock() for provider in PROVIDERS}


@dataclass(frozen=True)
class Job:
    provider: str
    model: str
    bot_name: str
    bot_api_key: str
    scenario: str


def load_env() -> None:
    for path in (REPO_ROOT / ".env.local", REPO_ROOT / ".env"):
        if not path.exists():
            continue
        for raw in path.read_text(encoding="utf-8").splitlines():
            line = raw.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            key, value = line.split("=", 1)
            os.environ.setdefault(key.strip(), value.strip().strip('"').strip("'"))


def request_json(method: str, url: str, *, headers: dict[str, str] | None = None, body: dict[str, Any] | None = None) -> Any:
    r = requests.request(method, url, headers=headers, json=body, timeout=45)
    if r.ok:
        return r.json() if r.content else None
    raise RuntimeError(f"{method} {url} failed: HTTP {r.status_code}: {r.text}")


def list_scenarios(api_base: str) -> list[str]:
    data = request_json("GET", f"{api_base}/api/v1/scenarios")
    scenarios = data.get("scenarios", data) if isinstance(data, dict) else data
    slugs = [s["slug"] for s in scenarios if s.get("slug")]
    if not slugs:
        raise RuntimeError("No scenarios returned from API")
    return sorted(slugs)


def create_key(api_base: str, name: str) -> str:
    payload = {"name": name}
    data = request_json(
        "POST",
        f"{api_base}/api/v1/keys",
        headers={"content-type": "application/json"},
        body=payload,
    )
    key = data.get("api_key")
    if not key:
        raise RuntimeError(f"Key response did not include api_key: {data}")
    KEY_DIR.mkdir(parents=True, exist_ok=True)
    (KEY_DIR / name).write_text(key + "\n", encoding="utf-8")
    return key


def run_job(job: Job, api_base: str, retries: int, decide_every: int, lookback: int) -> tuple[Job, bool, str]:
    # Keep one active run per provider/API key. The script still runs providers
    # in parallel, but avoids hammering a single model API/key with several
    # long scenarios at once.
    with PROVIDER_LOCKS[job.provider]:
        return run_job_locked(job, api_base, retries, decide_every, lookback)


def run_job_locked(job: Job, api_base: str, retries: int, decide_every: int, lookback: int) -> tuple[Job, bool, str]:
    LOG_DIR.mkdir(parents=True, exist_ok=True)
    last_error = ""

    for attempt in range(1, retries + 1):
        log_path = LOG_DIR / f"{job.provider}__{job.scenario}__attempt-{attempt}.log"
        cmd = [
            sys.executable,
            str(BOT_SCRIPT),
            "--provider", job.provider,
            "--model", job.model,
            "--bot-api-key", job.bot_api_key,
            "--api-base", api_base,
            "--scenario", job.scenario,
            "--decide-every", str(decide_every),
            "--lookback", str(lookback),
            "--publish",
        ]

        started = time.time()
        with log_path.open("w", encoding="utf-8") as log:
            log.write("$ " + " ".join(cmd) + "\n\n")
            log.flush()
            proc = subprocess.run(
                cmd,
                cwd=REPO_ROOT,
                text=True,
                stdout=log,
                stderr=subprocess.STDOUT,
                env=os.environ.copy(),
            )

        elapsed = time.time() - started
        if proc.returncode == 0:
            return job, True, f"ok in {elapsed:.1f}s; log={log_path}"

        last_error = f"exit={proc.returncode}; log={log_path}"
        if attempt < retries:
            time.sleep(min(30, 5 * attempt))

    return job, False, last_error


def main() -> int:
    load_env()

    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--api-base", default=os.environ.get("BOTTRADE_API", "https://bot-trade.org").rstrip("/"))
    p.add_argument("--scenario", action="append", dest="scenarios",
                   help="Scenario slug to run. Can be repeated. Defaults to every live scenario.")
    p.add_argument("--max-workers", type=int, default=3,
                   help="Parallel run workers. Default 3, usually one active job per provider.")
    p.add_argument("--retries", type=int, default=3)
    p.add_argument("--decide-every", type=int, default=8)
    p.add_argument("--lookback", type=int, default=24)
    args = p.parse_args()

    if not BOT_SCRIPT.exists():
        raise SystemExit(f"missing bot script: {BOT_SCRIPT}")

    for provider, cfg in PROVIDERS.items():
        if not os.environ.get(cfg["key_env"]):
            raise SystemExit(f"{cfg['key_env']} is required in .env.local/.env for provider={provider}")

    scenarios = args.scenarios or list_scenarios(args.api_base)
    print(f"API: {args.api_base}")
    print(f"Scenarios ({len(scenarios)}): {', '.join(scenarios)}")
    print("Creating one BotTrade API key per provider bot...")

    keys: dict[str, str] = {}
    for provider, cfg in PROVIDERS.items():
        key = create_key(args.api_base, cfg["name"])
        keys[provider] = key
        print(f"  {cfg['name']} -> saved to {KEY_DIR / cfg['name']}")

    jobs = [
        Job(
            provider=provider,
            model=cfg["model"],
            bot_name=cfg["name"],
            bot_api_key=keys[provider],
            scenario=scenario,
        )
        for scenario in scenarios
        for provider, cfg in PROVIDERS.items()
    ]

    print(f"Launching {len(jobs)} published runs with max_workers={args.max_workers}, retries={args.retries}.")
    print(f"Logs: {LOG_DIR}")

    failures: list[tuple[Job, str]] = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.max_workers) as pool:
        futures = [
            pool.submit(run_job, job, args.api_base, args.retries, args.decide_every, args.lookback)
            for job in jobs
        ]
        for future in concurrent.futures.as_completed(futures):
            job, ok, message = future.result()
            label = f"{job.provider}/{job.model} on {job.scenario}"
            if ok:
                print(f"✓ {label}: {message}")
            else:
                print(f"✗ {label}: {message}")
                failures.append((job, message))

    print("\nSummary")
    print(f"  successful: {len(jobs) - len(failures)}")
    print(f"  failed:     {len(failures)}")
    if failures:
        print("\nFailed jobs:")
        for job, message in failures:
            print(f"  - {job.provider}/{job.model} on {job.scenario}: {message}")
        return 1

    print("\nAll runs completed and published.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
