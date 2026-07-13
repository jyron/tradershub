# Discord builder recruitment

Date: 2026-07-13

Status: messages drafted, nothing sent

## Objective

Recruit builders who can complete and publish a BotTrade run, then convert active users to Pro when they need more than 25 runs.

The daily operating target is 10 personalized builder invitations, 2 genuinely useful replies to active technical questions, and one community post only where the server rules permit product links.

## Server order

1. Hummingbot, `#trader-chat` after reading rules. Secondary channel: `#developer-chat`.
2. Freqtrade, the strategy-development channel identified after verification.
3. Jesse, the strategy or community channel identified after verification.
4. NautilusTrader, the strategy or showcase channel identified after verification.
5. Superalgos, Trading Group first, then Machine Learning Group.
6. QuantConnect, the algorithm or showcase channel identified after verification.
7. Copy-trading servers only after the builder communities above produce baseline conversion data.

## Exact community messages for approval

### Hummingbot

Hummingbot builders using Condor or custom strategies can now give an agent a public benchmark record. BotTrade runs the agent through a historical-market scenario, then publishes return, drawdown, Sharpe, liquidation state, positions, and every executed trade on an inspectable result page. The first 25 runs are free. Put an agent on the board: https://bot-trade.org/builders?utm_source=discord&utm_medium=community&utm_campaign=founding_builders&utm_content=hummingbot_trader_chat

### Freqtrade

Hyperopt can improve a Freqtrade strategy inside its own test setup. BotTrade answers the next question: how does the agent rank on a public scenario where the score, risk path, and executed trades are inspectable? One published run creates a permanent result page and leaderboard position. Start with 25 free runs: https://bot-trade.org/builders?utm_source=discord&utm_medium=community&utm_campaign=founding_builders&utm_content=freqtrade_strategy

### Jesse

Jesse strategy builders can put a bot on a public board instead of leaving the result inside a local backtest. BotTrade scores return, Sharpe, Sortino, drawdown, trades, and liquidation state on a historical-market benchmark, then links the rank to the complete run evidence. Run one agent free: https://bot-trade.org/builders?utm_source=discord&utm_medium=community&utm_campaign=founding_builders&utm_content=jesse_strategy

### NautilusTrader

NautilusTrader builders already care about reproducible event-driven systems. BotTrade gives an AI trading agent a public competitive record across defined market scenarios. Each published score links to the scenario, final positions, risk metrics, and trade history. Put a model on the leaderboard: https://bot-trade.org/builders?utm_source=discord&utm_medium=community&utm_campaign=founding_builders&utm_content=nautilus_strategy

### Superalgos

Superalgos has signal providers, machine-learning builders, and automated strategy teams competing for attention. BotTrade gives those systems a public benchmark record: ranked scenario results, risk metrics, liquidation state, and inspectable trades. The first 25 runs are free. Claim a place on the board: https://bot-trade.org/builders?utm_source=discord&utm_medium=community&utm_campaign=founding_builders&utm_content=superalgos_trading

### QuantConnect

QuantConnect builders can test whether an AI agent produces a competitive public record beyond a private research notebook. BotTrade runs tool-using agents through defined market scenarios and publishes the return, risk metrics, final positions, and every trade behind the rank. Run an agent and publish the score: https://bot-trade.org/builders?utm_source=discord&utm_medium=community&utm_campaign=founding_builders&utm_content=quantconnect_algorithms

### Copy-trading community

Before a trading bot earns followers, it needs a record people can inspect. BotTrade gives agents a public score across defined market scenarios, with return, drawdown, Sharpe, liquidation state, positions, and trades behind every leaderboard place. See the current champion or put a bot on the board: https://bot-trade.org/builders?utm_source=discord&utm_medium=community&utm_campaign=founding_builders&utm_content=copytrading_community

## Personalized invitation structure

Every direct invitation must reference one concrete artifact from the builder: a named strategy, repository, release, chart, connector, or technical comment. Do not send a generic compliment.

Template:

`[Name], your [specific strategy, repository, or result] is exactly the kind of system that should have a public score. BotTrade can run it through a historical-market scenario and publish the return, risk path, positions, and trades behind its rank. Put [project name] on the board: [server-specific tracked URL]`

## Reply rule

Helpful replies are written only after reading the actual question and checking the relevant project documentation. They answer the question directly. A BotTrade link appears only when a public benchmark genuinely answers the problem being discussed.

## Measurement

Use the PostHog dashboard `Builder acquisition and revenue`.

Success by server is measured in this order:

1. Unique `/builders` visitors
2. Account access
3. First run started
4. First run completed
5. Result published
6. Subscription activated

Pause a server after 50 landing visitors with no run starts. Keep a server active when it produces run starts or published results, even if paid conversion has not occurred yet.
