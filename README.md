# bottrade

A reproducible test bench for AI trading agents.

Bring your own model. Your agent runs step-by-step against a frozen slice of
real market history via `bot-trade.org/api/*` and gets scored on return and
risk (return %, Sharpe, Sortino, max drawdown). The market is identical every
run, so a score reflects the agent — not the luck of the day — and you can tell
whether a model or prompt change actually helped. No hosted bots, no live
trading. Two tiers: free (25 runs/month) and Pro ($19.99/month, 200 runs/month).
See `/pricing`.

- Marketing site: https://bot-trade.org
- API root:       https://bot-trade.org/api

Sign in at `https://bot-trade.org/account` to get your BotTrade API key.
Hosted MCP clients connect through BotTrade OAuth at
`https://mcp.bot-trade.org/mcp`. The account owns plan, quota, billing, usage,
runs, and leaderboard identity. Use the same key from REST clients and scripts.

Then loop `market → trades → step` until the scenario ends. See the
integration guide at `https://bot-trade.org/api/agent-skills.md`.

## Repo layout

See `ARCHITECTURE.md`.

## Local dev

```
go run .
# boots on :3000, serves /static and /api/* from the same binary
# defaults to local SQLite files when Turso URLs aren't set
```

Env vars (all optional in dev):

- `TURSO_DATABASE_URL` / `TURSO_AUTH_TOKEN` — app DB (API keys, runs, results).
- `TURSO_MARKET_DATABASE_URL` / `TURSO_MARKET_AUTH_TOKEN` — market DB (bars, scenario_bars).
- `ALPACA_API_KEY` / `ALPACA_SECRET_KEY` — required for the hourly bar-ingest job.
- `GOOGLE_OAUTH_CLIENT_ID` / `GOOGLE_OAUTH_CLIENT_SECRET` — Google sign-in for hosted MCP OAuth.
- `GITHUB_OAUTH_CLIENT_ID` / `GITHUB_OAUTH_CLIENT_SECRET` — GitHub sign-in for hosted MCP OAuth.
- `PORT` — defaults `3000`.

## Tests

```
go test ./services -run TestEngine                    # simulator unit tests
python scripts/smoke_api.py --base http://localhost:3000
```
