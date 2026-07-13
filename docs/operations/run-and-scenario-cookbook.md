# Run and scenario operations

Durable operational reference for exercising the API and provisioning benchmark
scenarios. Campaign-specific leaderboard seeding, model rosters, provider-cost
estimates, and publishing plans belong in the local `/outreach` workspace.

## Sources of current values

- Discover scenarios with `GET /api/v1/scenarios`; do not maintain a copied
  list here.
- Read current prices and plan allowances at `https://bot-trade.org/pricing`.
- Use each script's `--help` output for current flags and defaults.
- Use provider documentation for current model IDs and API costs.
- Query the market database before assuming a date range has coverage.

## Run the API locally

The application requires an encryption key even when using local SQLite:

```bash
APP_ENCRYPTION_KEY="$(openssl rand -hex 32)" go run .
```

List the scenarios exposed by that instance:

```bash
curl -sS http://localhost:3000/api/v1/scenarios | jq .
```

Create or retrieve an account API key through `/account`, then run the reference
client. The client itself is the authoritative source for strategies and flags:

```bash
python handlers/apiv1/test_bot.py --help
BOT_API_KEY="..." python handlers/apiv1/test_bot.py --list-scenarios
```

Publishing is opt-in. Do not pass `--publish`, or call the publish endpoint,
unless the result is intended for the public leaderboard.

## Run provider-backed reference agents

Provider-backed clients live in `handlers/apiv1/ai_bot.py` and
`handlers/apiv1/multi_ai_bot.py`. Inspect their help before use:

```bash
python handlers/apiv1/ai_bot.py --help
python handlers/apiv1/multi_ai_bot.py --help
python scripts/seed_multi_ai_runs.py --help
```

Model availability, names, pricing, and output behavior belong to the provider.
Choose them at run time and record the exact model identifier with benchmark
results. Keep credentials in `.env.local`, `.env`, or the process environment;
both environment files are ignored by Git.

## Provision a scenario

Scenario JSON files are committed under `scenarios/`. Use an existing file as
the schema example because `cmd/provision_scenario/main.go` and the service
validation are authoritative.

1. Confirm the source `bars` table contains the intended symbols and time
   window.
2. Backfill missing market data when necessary.
3. Add `scenarios/<slug>.json` with the universe, time window, cash, leverage,
   shorting, slippage, and benchmark settings for that scenario.
4. Provision it into the application and market databases.
5. Verify it through the live scenario API.

Example commands:

```bash
go run ./cmd/backfill_bars \
  --start YYYY-MM-DD \
  --end YYYY-MM-DD \
  --symbols AAPL,MSFT

go run ./cmd/provision_scenario \
  --config scenarios/<slug>.json

curl -sS "$BOTTRADE_API/api/v1/scenarios/<slug>" | jq .
```

`backfill_bars` requires Alpaca credentials. Both commands use the database
connections described in [configuration.md](configuration.md) and fall back to
local SQLite where their implementation permits it.

## Safety and verification

- Provisioning copies source bars into versioned `scenario_bars`; verify the
  resulting bar count and status before announcing a scenario.
- Confirm equity and crypto universes use the intended market calendar and
  benchmark symbol.
- Treat deletion or manual mutation of shared database rows as a production
  operation: inspect first, back up affected data, and scope the statement to a
  specific scenario ID or slug.
- Run `go test ./...` after simulator, provisioning, configuration, or API
  changes.
- Use `scripts/smoke_api.py` against the intended base URL for an end-to-end
  deployment check.
