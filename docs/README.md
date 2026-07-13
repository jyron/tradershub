# BotTrade documentation

Repository documentation lives here. Product pages and live API documentation
remain beside the code that serves them, so they are tested with the product.

## Start here

- [Architecture overview](architecture/overview.md) — services, routes,
  databases, code map, and deployment.
- [Repository topology](repository-topology.md) — ownership boundaries between
  the service/runtime repository and Python SDK/developer repository.
- [Configuration](operations/configuration.md) — environment variables and
  which source is authoritative for changeable settings.
- [Authentication model](design/authentication.md) — account, API key, and
  OAuth relationships.
- [API functional test plan](testing/api-functional-plan.md) — behavioral test
  coverage.

## Operations

- [Stripe billing](operations/stripe-billing.md) — configuration, webhook
  testing, and safe price changes.
- [Run and scenario cookbook](operations/run-and-scenario-cookbook.md) — agent
  runs, scenario provisioning, and operational examples.

## Documentation shipped by the application

- `handlers/apiv1/agent-skills.md` is served at `/api/agent-skills.md`.
- `handlers/apiv1/llms.txt` is served at `/api/llms.txt`.
- Huma generates `/api/openapi.json` and `/api/docs` from the Go API types.
- `static/docs.html` and `static/methodology.html` are public site pages.
- Component-specific instructions stay with their component in
  `bottrade-mcp/`, `skills/bottrade-benchmark/`, and `stripee2e/`.

## Related repository

The public `jyron/bottrade` repository contains the Python SDK, CLI,
integration examples, fixtures, and package-release workflows. A separate
local checkout is available at `.bottrade-public-work/`; it has its own Git
history and must be committed independently from this service repository. See
[Repository topology](repository-topology.md) for ownership rules.

## Changeable product values

Prices, plan allowances, available scenarios, model names, tool counts,
campaign targets, and deployment identifiers can change without changing the
product's architecture. Documentation should link to the source that owns each
value instead of presenting a copied value as policy:

| Value | Authoritative source |
|---|---|
| Public prices and plan presentation | `/pricing` and active Stripe Price IDs in environment configuration |
| Plan allowances and limit behavior | application quota configuration and its tests |
| Available scenarios | `GET /api/v1/scenarios` |
| REST contract | `GET /api/openapi.json` |
| MCP tools | MCP tool discovery from `https://mcp.bot-trade.org/mcp` |
| Deployment state | Railway service configuration and deployment history |

Historical implementation records belong in `docs/archive/` and must say that
their values are a dated snapshot, not current policy.

## Local outreach material

Campaign drafts, partner research, contact lists, creator operations, and other
outreach material belong in `/outreach`. The directory is intentionally ignored
by Git and is not part of repository documentation.
