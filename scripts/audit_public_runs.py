#!/usr/bin/env python3
"""Audit published BotTrade runs for use in the public agent index."""

from __future__ import annotations

import collections
import json
import math
import sys
import urllib.request


BASE = "https://bot-trade.org"
BASELINES = ("Buy & Hold", "Equal Weight", "Momentum")
NON_INDEX_BOTS = ("diagnostic", "demo", "sandbox test")


def get_json(path: str) -> dict:
    request = urllib.request.Request(
        BASE + path,
        headers={"Cache-Control": "no-cache", "User-Agent": "BotTrade-index-audit/1.0"},
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response)


def close(a: float, b: float) -> bool:
    return math.isclose(a, b, rel_tol=1e-9, abs_tol=1e-6)


def audit_run(expected_id: str, scenario: dict, entry: dict) -> tuple[list[str], list[str], dict]:
    payload = get_json(f"/api/v1/runs/{expected_id}/public?index_audit=1")
    run = payload.get("run") or {}
    results = payload.get("results") or {}
    curve = payload.get("equity_curve") or []
    trades = payload.get("trades") or []
    errors: list[str] = []
    warnings: list[str] = []

    if run.get("id") != expected_id:
        errors.append("public endpoint returned a different run")
    if not run.get("published"):
        errors.append("run is not marked published")
    if run.get("scenario_id") != scenario["id"]:
        errors.append("scenario does not match leaderboard")
    if run.get("scenario_version") != scenario["current_version"]:
        errors.append("run is not pinned to the current scenario version")
    if run.get("status") not in {"completed", "liquidated"}:
        errors.append(f"terminal status is {run.get('status')!r}")
    if run.get("status") == "liquidated" or results.get("liquidated"):
        warnings.append("liquidated run; valid outcome but rank separately")
    if not run.get("completed_at"):
        errors.append("completed_at is missing")
    if not results:
        errors.append("results are missing")
    if len(curve) < 2:
        errors.append("equity curve has fewer than two samples")
    if payload.get("queued_orders"):
        errors.append("terminal run still has queued orders")

    numeric = ("final_equity", "return_pct", "sharpe", "sortino", "max_drawdown", "volatility")
    for field in numeric:
        value = results.get(field)
        if value is not None and (not isinstance(value, (int, float)) or not math.isfinite(value)):
            errors.append(f"result {field} is not finite")
    if isinstance(results.get("max_drawdown"), (int, float)) and results["max_drawdown"] < 0:
        errors.append("maximum drawdown is negative")
    if isinstance(results.get("volatility"), (int, float)) and results["volatility"] < 0:
        errors.append("volatility is negative")
    if results.get("trade_count") != len(trades):
        errors.append("trade_count does not match public trade records")
    if any(trade.get("run_id") != expected_id for trade in trades):
        errors.append("trade record belongs to a different run")
    if curve:
        if curve[-1].get("sim_time") != run.get("sim_time"):
            errors.append("last equity timestamp does not match terminal simulation time")
        if isinstance(results.get("final_equity"), (int, float)) and not close(
            results["final_equity"], curve[-1].get("equity", float("nan"))
        ):
            errors.append("final equity does not match the equity curve")
    expected_return = None
    if isinstance(results.get("final_equity"), (int, float)) and run.get("starting_cash"):
        expected_return = (results["final_equity"] / run["starting_cash"] - 1) * 100
        if not close(expected_return, results.get("return_pct", float("nan"))):
            errors.append("return_pct does not reconcile with starting and final equity")
    if entry.get("bot_name") and run.get("bot_name") and not entry["bot_name"].endswith(run["bot_name"]):
        errors.append("leaderboard and run bot names disagree")

    bot_name = run.get("bot_name", "")
    category = "agent"
    if bot_name.startswith(BASELINES):
        category = "baseline"
    elif any(term in bot_name.lower() for term in NON_INDEX_BOTS):
        category = "diagnostic"
    return errors, warnings, {
        "run_id": expected_id,
        "scenario": scenario["slug"],
        "bot_name": bot_name,
        "category": category,
        "status": run.get("status"),
        "return_pct": results.get("return_pct"),
    }


def main() -> int:
    catalog = get_json("/api/v1/scenarios")["scenarios"]
    scenarios = {scenario["slug"]: scenario for scenario in catalog}
    records: list[dict] = []
    failures: list[tuple[dict, list[str]]] = []
    warnings: list[tuple[dict, list[str]]] = []

    for slug, scenario in scenarios.items():
        leaderboard = get_json(f"/api/v1/leaderboard?scenario={slug}&limit=500")
        for entry in leaderboard.get("entries", []):
            errors, run_warnings, record = audit_run(entry["run_id"], scenario, entry)
            records.append(record)
            if errors:
                failures.append((record, errors))
            if run_warnings:
                warnings.append((record, run_warnings))

    valid = [record for record in records if all(record["run_id"] != failed[0]["run_id"] for failed in failures)]
    usable_agents = [record for record in valid if record["category"] == "agent"]
    print(f"published audited: {len(records)}")
    print(f"structurally valid: {len(valid)}")
    print(f"index-usable agent runs: {len(usable_agents)}")
    print(f"valid baselines: {sum(record['category'] == 'baseline' for record in valid)}")
    print(f"excluded diagnostics: {sum(record['category'] == 'diagnostic' for record in valid)}")
    print(f"invalid: {len(failures)}")
    print(f"warnings: {len(warnings)}")

    coverage: dict[str, set[str]] = collections.defaultdict(set)
    counts: collections.Counter[str] = collections.Counter()
    for record in usable_agents:
        coverage[record["bot_name"]].add(record["scenario"])
        counts[record["bot_name"]] += 1
    print("\nagent coverage:")
    for bot_name in sorted(coverage, key=lambda name: (-len(coverage[name]), name)):
        print(f"  {bot_name}: {counts[bot_name]} runs across {len(coverage[bot_name])} scenarios")

    if failures:
        print("\ninvalid runs:")
        for record, errors in failures:
            print(f"  {record['run_id']} {record['scenario']} {record['bot_name']}: {'; '.join(errors)}")
    if warnings:
        print("\nwarnings:")
        for record, run_warnings in warnings:
            print(f"  {record['run_id']} {record['scenario']} {record['bot_name']}: {'; '.join(run_warnings)}")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
