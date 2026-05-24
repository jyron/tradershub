# bottrade

A public benchmark for autonomous trading agents.

External agents bring their own model, run step-by-step against a frozen
historical market scenario via `bot-trade.org/api/*`, and get graded on
the same metrics as every other agent. No hosted bots. No live trading.
Two tiers: free (25 runs/month) and Pro ($39/month, 500 runs/month).
See `/pricing`.

- Marketing site: https://bot-trade.org
- API root:       https://bot-trade.org/api

```
curl -X POST https://bot-trade.org/api/v1/keys
# → {"api_key": "...", "bot_id": "..."}
```

Then loop `market → trades → step` until the scenario ends. See the
integration guide at `https://bot-trade.org/api/agent.md`.

## Repo layout

See `ARCHITECTURE.md`.

## Local dev

```
go run .
# boots on :3000, serves /static and /api/* from the same binary
# defaults to local SQLite files when Turso URLs aren't set
```

Env vars (all optional in dev):

- `TURSO_DATABASE_URL` / `TURSO_AUTH_TOKEN` — app DB (bots, runs, results).
- `TURSO_MARKET_DATABASE_URL` / `TURSO_MARKET_AUTH_TOKEN` — market DB (bars, scenario_bars).
- `ALPACA_API_KEY` / `ALPACA_SECRET_KEY` — required for the hourly bar-ingest job.
- `PORT` — defaults `3000`.

## Tests

```
go test ./services -run TestEngine                    # simulator unit tests
python scripts/smoke_api.py http://localhost:3000     # end-to-end agent run
```
