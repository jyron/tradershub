# BotTrade repository topology

BotTrade is maintained across two GitHub repositories. They share the product
name and public API contract but have different release and deployment paths.

## Repository responsibilities

| Repository | Local checkout | Responsibility | Release or deployment |
|---|---|---|---|
| `jyron/tradershub` | Workspace root | Go service, website, REST API, authentication, billing, historical-market benchmark engine, article publishing, agent index, database migrations, and hosted MCP source | Railway services `tradershub` and `bottrade-mcp` |
| `jyron/bottrade` | `.bottrade-public-work/` | Public Python SDK and CLI, integration examples, fixtures, evidence badges, contributor documentation, and package-release automation | PyPI package `bottrade` and GitHub releases |

The nested `.bottrade-public-work/` directory has its own `.git` directory and
remote. Git operations must be run from the repository that owns the files.
A root-repository commit must not include public-SDK changes, and a public-SDK
commit must not include service/runtime code.

## Shared product contract

Both repositories depend on the public interfaces operated by the service:

- Product and account site: `https://bot-trade.org`
- REST API: `https://bot-trade.org/api/v1/*`
- OpenAPI document: `https://bot-trade.org/api/openapi.json`
- Hosted MCP endpoint: `https://mcp.bot-trade.org/mcp`
- Published skill: `https://bot-trade.org/skills/bottrade-benchmark/SKILL.md`
- Public run evidence: `https://bot-trade.org/run/<run-id>`

The service repository defines the API and MCP behavior. The public repository
must remain compatible with those interfaces and should use offline contract
tests wherever possible.

## Source ownership

Use these ownership rules when a change spans repositories:

| Change | Authoritative repository |
|---|---|
| REST endpoint, response type, authentication, quota, simulator behavior | `jyron/tradershub` |
| Hosted MCP tool implementation or OAuth bridge | `jyron/tradershub` (`bottrade-mcp/`) |
| Website, articles, sitemap, agent index, public run page | `jyron/tradershub` |
| Python client, `backtest()` runner, CLI, Python model types | `jyron/bottrade` |
| Framework examples and public result fixtures | `jyron/bottrade` |
| Canonical hosted benchmark skill | `jyron/tradershub` (`skills/bottrade-benchmark/`) |
| Public copy of the benchmark skill | `jyron/bottrade` (`docs/BOTTRADE_SKILL.md`) |

When a shared contract changes, update the service implementation first, then
update the public SDK and examples. Validate each repository with its own test
suite and commit the changes separately.

## Local verification

Service repository:

```bash
GOCACHE=/tmp/bottrade-go-build /usr/local/go/bin/go test ./...
```

Public SDK repository:

```bash
cd .bottrade-public-work
.venv/bin/ruff check .
.venv/bin/mypy
.venv/bin/pytest
```
