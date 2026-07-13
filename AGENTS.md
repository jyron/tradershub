## Non-negotiable terminology

Never use the phrases "frozen history" or "frozen historical data" for BotTrade.
They are inaccurate. Use "historical-market benchmark" instead.
This applies to code, documentation, marketing, research, and conversation.

Before every final response, scan the drafted response for banned terminology.
If found, rewrite before sending.

## Persistent project context

BotTrade uses two GitHub repositories with different responsibilities:

- `jyron/tradershub` is the service and runtime repository represented by this
  workspace root. It contains the Go application, REST API, website, article
  publisher, agent index, billing and authentication, database migrations,
  Railway deployment code, and the hosted MCP implementation in
  `bottrade-mcp/`.
- `jyron/bottrade` is the public developer repository. Its local checkout is
  `.bottrade-public-work/`, which is a separate Git repository. It contains the
  `bottrade` Python SDK, CLI, framework examples, public fixtures, badges,
  contributor documentation, and release workflows.

Do not treat `.bottrade-public-work/` as part of the root Git repository. Run
Git commands from the appropriate repository and keep commits separate.

Production topology:

- `bot-trade.org` and the primary API are served by the Railway service
  `tradershub` from `jyron/tradershub`.
- `mcp.bot-trade.org` is served by the separately deployed `bottrade-mcp/`
  module, whose source remains in `jyron/tradershub`.
- The public Python package is released from `jyron/bottrade` and published as
  `bottrade` on PyPI.

Authoritative project orientation is in `docs/README.md`, with repository
boundaries in `docs/repository-topology.md` and runtime architecture in
`docs/architecture/overview.md`.
