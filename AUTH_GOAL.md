# BotTrade Auth Goal

BotTrade should have one account-centered auth model.

A BotTrade account owns plan, quota, billing, runs, published results, usage history, and public leaderboard identity. A BotTrade API key is a reusable credential for that account. The same key should work from REST clients, scripts, custom agents, and MCP clients that accept bearer tokens.

Claude and ChatGPT should connect through BotTrade OAuth. OAuth is the connector authorization flow for those products; it grants access to the same BotTrade account and usage bucket.

BotTrade exposes OAuth authorization-server metadata, dynamic client registration, PKCE authorization-code exchange, refresh tokens, and Google/GitHub sign-in. MCP exposes protected-resource metadata that points connector clients to the BotTrade authorization server.

The user-facing story should stay simple:

1. Sign in to BotTrade with Google or GitHub.
2. Use your BotTrade API key anywhere you interact with BotTrade.
3. Connect Claude or ChatGPT and approve access to the same BotTrade account.

Usage tracking should attach every request to an account, credential, surface, and action. Store metadata needed for quotas, billing, support, rate limiting, and audits. Avoid storing raw prompts or full market payloads by default.
