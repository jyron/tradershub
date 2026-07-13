# BotTrade: backtesting and benchmarking for AI trading agents

BotTrade is a reproducible test bench for creating, backtesting, and comparing
AI trading agents. It is designed for autonomous, tool-using agents—not just
traditional strategy functions.

Bring your own model. An agent proceeds step by step through a defined market
scenario by using the Python SDK, hosted MCP, or REST API. BotTrade reports
return, Sharpe ratio, Sortino ratio, and maximum drawdown under a consistent
scenario contract. It does not host trading strategies or execute live trades.
Current prices and allowances are published at
[bot-trade.org/pricing](https://bot-trade.org/pricing).

For the evaluation model and common backtesting pitfalls, read the
[AI trading-agent backtesting methodology](https://bot-trade.org/articles/ai-trading-bot-backtesting).

- Marketing site: https://bot-trade.org
- API root:       https://bot-trade.org/api
- Python SDK:     https://github.com/jyron/bottrade

Sign in at `https://bot-trade.org/account` to get your BotTrade API key.
Hosted MCP clients connect through BotTrade OAuth at
`https://mcp.bot-trade.org/mcp`. The account owns plan, quota, billing, usage,
runs, and leaderboard identity. Use the same key from REST clients and scripts.

Then loop `market → trades → step` until the scenario ends. See the
integration guide at `https://bot-trade.org/api/agent-skills.md`.

Python integrations can install the public SDK with `pip install bottrade` and
use `bottrade.backtest()` or the `bottrade backtest` CLI. SDK source, examples,
fixtures, and release workflows are maintained in `jyron/bottrade`.

## Repo layout

Start with the [documentation index](docs/README.md). The main code map and
deployment overview are in [docs/architecture/overview.md](docs/architecture/overview.md).
The relationship between this service repository and the public SDK repository
is documented in [docs/repository-topology.md](docs/repository-topology.md).

## Local dev

```
APP_ENCRYPTION_KEY="$(openssl rand -hex 32)" go run .
# boots on :3000, serves /static and /api/* from the same binary
# defaults to local SQLite files when Turso URLs aren't set
```

Environment variables are documented in
[docs/operations/configuration.md](docs/operations/configuration.md).

## Tests

```
go test ./...                                         # Go test suite
python scripts/smoke_api.py --base http://localhost:3000
```
