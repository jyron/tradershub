#!/usr/bin/env python3
"""
End-to-end smoke test for the Benchmark API (/api/v1/*).

What it does:
  1. Hits a running API server (default http://127.0.0.1:3000)
  2. Mints an API key unless --key is supplied
  3. Lists scenarios; picks the requested slug
  4. Creates a run on it
  5. Buys 10 AAPL with an idempotency_key, then retries that POST
     with the same key — asserts both responses are byte-identical
  6. Steps to scenario end
  7. Fetches results; asserts shape + that return_pct is finite

Assumes the requested scenario has already been provisioned.

Usage:
  python3 scripts/smoke_api.py [--base http://127.0.0.1:3000] \\
                               [--key existing-api-key] \\
                               [--slug tech-2024-q2]
"""
from __future__ import annotations

import argparse
import os
import sys

import requests


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--base", default="http://127.0.0.1:3000")
    p.add_argument("--key", default=os.environ.get("BOTTRADE_API_KEY", ""))
    p.add_argument("--slug", default="tech-2024-q2")
    args = p.parse_args()
    api = args.base.rstrip("/") + "/api/v1"

    sess = requests.Session()
    key = args.key.strip()
    if not key:
        r = requests.post(f"{api}/keys", json={"name": "smoke"})
        r.raise_for_status()
        key = r.json()["api_key"]
    sess.headers["X-API-Key"] = key

    # 1. List scenarios
    r = sess.get(f"{api}/scenarios")
    r.raise_for_status()
    scenarios = r.json()["scenarios"]
    print(f"[OK] /api/v1/scenarios returned {len(scenarios)} scenarios")
    found = next((s for s in scenarios if s["slug"] == args.slug), None)
    if not found:
        print(f"[FAIL] no scenario with slug={args.slug}", file=sys.stderr)
        return 1
    print(f"  using scenario {found['name']!r}")

    # 2. Create run
    r = sess.post(f"{api}/runs", json={"scenario_slug": args.slug})
    r.raise_for_status()
    run = r.json()["run"]
    run_id = run["id"]
    print(f"[OK] POST /api/v1/runs -> run_id={run_id} cash={run['cash']}")

    # 3. Queue a buy with an idempotency key, then RETRY with the same key.
    trade_body = {
        "symbol": "AAPL",
        "side": "buy",
        "quantity": 10,
        "reasoning": "smoke",
        "idempotency_key": "smoke-trade-1",
    }
    r1 = sess.post(f"{api}/runs/{run_id}/trades", json=trade_body)
    r1.raise_for_status()
    r2 = sess.post(f"{api}/runs/{run_id}/trades", json=trade_body)
    r2.raise_for_status()
    if r1.text != r2.text:
        print("[FAIL] idempotent retry returned different body:")
        print(f"  first  : {r1.text[:200]}")
        print(f"  second : {r2.text[:200]}")
        return 1
    print(f"[OK] idempotent trade retry returned byte-identical response")

    # 3b. Same key, different body → expect 409
    bad = dict(trade_body, quantity=11)
    r3 = sess.post(f"{api}/runs/{run_id}/trades", json=bad)
    if r3.status_code != 409:
        print(f"[FAIL] expected 409 on key reuse with different body, got {r3.status_code}")
        return 1
    print(f"[OK] mismatched body on same idempotency_key correctly 409'd")

    # 4. Step all the way through. count=1000 is way more than the scenario has;
    #    the engine returns done=True when timeline is exhausted.
    step_body = {"count": 1000, "idempotency_key": "smoke-step-final"}
    r = sess.post(f"{api}/runs/{run_id}/step", json=step_body)
    r.raise_for_status()
    step = r.json()
    print(f"[OK] /step advanced {step['bars_advanced']} bars; done={step['done']} "
          f"liquidated={step['liquidated']} equity={step['equity']:.2f}")

    # 5. Results
    r = sess.get(f"{api}/runs/{run_id}/results")
    r.raise_for_status()
    results = r.json()["results"]
    print(f"[OK] /results return_pct={results['return_pct']:.4f}% "
          f"trade_count={results['trade_count']} "
          f"liquidated={results['liquidated']}")

    # Basic sanity: return_pct must be a finite float
    rp = results["return_pct"]
    if not isinstance(rp, (int, float)) or rp != rp:  # NaN check
        print(f"[FAIL] return_pct not finite: {rp!r}")
        return 1

    print()
    print("=" * 50)
    print("smoke_api.py: ALL CHECKS PASSED")
    print("=" * 50)
    return 0


if __name__ == "__main__":
    sys.exit(main())
