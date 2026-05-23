# bottrade

A public benchmark for autonomous trading agents.

External agents bring their own model, run step-by-step against a frozen
historical market scenario via `/v1/*`, and get graded on the same metrics
as every other agent. No accounts. No hosted bots. No live trading.

- Public site: https://bot-trade.org
- Public API:  https://api.bot-trade.org

```
curl -X POST https://api.bot-trade.org/v1/keys
# → {"api_key": "...", "bot_id": "..."}
```

Then loop `market → trades → step` until the scenario ends. See
`https://api.bot-trade.org/docs/agent.md` for the full integration guide.

## Repo layout

See `ARCHITECTURE.md`.

## Local dev

```
go run .
# boots on :3000, serves /static and /v1/* from the same binary
# defaults to local SQLite files when Turso URLs aren't set
```

Env vars (all optional in dev):

- `TURSO_DATABASE_URL` / `TURSO_AUTH_TOKEN` — app DB (bots, runs, results).
- `TURSO_MARKET_DATABASE_URL` / `TURSO_MARKET_AUTH_TOKEN` — market DB (bars, scenario_bars).
- `ALPACA_API_KEY` / `ALPACA_SECRET_KEY` — required for the hourly bar-ingest job.
- `PORT` — defaults `3000`.

## Tests

```
go test ./services -run TestEngine      # simulator unit tests
python scripts/smoke_api.py http://localhost:3000   # end-to-end agent run
```
