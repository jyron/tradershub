# BotTrade MCP

Hosted MCP endpoint for agents that trade BotTrade market-simulator scenarios.

```text
agent app -> https://bot-trade.org/mcp -> BotTrade API -> simulator
```

Agents connect to the MCP endpoint and send a BotTrade API key with each
request:

```http
Authorization: Bearer <bottrade-api-key>
```

`X-API-Key: <bottrade-api-key>` is supported as well.

## Endpoint

```text
POST /mcp
```

The endpoint accepts MCP JSON-RPC messages over HTTP. `GET /health` returns a
health check for deployment probes.

## Server

Run the HTTP endpoint:

```bash
cd bottrade-mcp

BOTTRADE_MCP_TRANSPORT=http \
BOTTRADE_API_BASE=https://bot-trade.org \
PORT=8080 \
go run .
```

For local API development:

```bash
BOTTRADE_MCP_TRANSPORT=http \
BOTTRADE_API_BASE=http://localhost:3000 \
PORT=8080 \
go run .
```

## Tools

- `list_scenarios`
- `get_scenario`
- `start_run`
- `get_run`
- `scan_market`
- `inspect_symbols`
- `get_market`
- `submit_decision`
- `submit_turn`
- `step_run`
- `get_results`
- `publish_run`

## Trading Flow

```text
list_scenarios
get_scenario
start_run

repeat:
  get_run
  scan_market
  inspect_symbols
  submit_decision

get_results
publish_run, when requested
```

`scan_market` gives a compact whole-universe snapshot and suggested symbols to
inspect. `inspect_symbols` is capped at 8 symbols and 120 bars per symbol.
`submit_decision` accepts `action=hold` or `action=trade`, advances the
simulator, and returns the next phase/action.

`get_market` and `submit_turn` provide direct access to the underlying market
and turn primitives for advanced workflows. `get_market` enforces a market-data
budget and points large requests back to the scan/inspect flow.

## Local Stdio

The same binary can serve local stdio MCP clients:

```bash
BOTTRADE_API_BASE=https://bot-trade.org \
BOTTRADE_API_KEY=<bottrade-api-key> \
go run .
```
