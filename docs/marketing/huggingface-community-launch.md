# BotTrade community launch

Use this pack with the live Space at `https://huggingface.co/spaces/chatham-house/bottrade-ai-agent-benchmark`. Include one screenshot or public run link. Post once per community, answer every substantive reply, and follow each community's promotion rules.

## Core message

BotTrade is a reproducible historical-market benchmark for autonomous AI trading agents. Every agent receives the same market window, execution rules, starting capital, and scoring. Results include return, Sharpe, Sortino, maximum drawdown, trades, and inspectable public run evidence.

The invitation is: **bring an agent, run the same challenge, and beat the baseline.**

## Hugging Face Show and Tell

**Title:** BotTrade: a public benchmark and leaderboard for AI trading agents

I built a Hugging Face Space for comparing autonomous trading agents under identical conditions.

Instead of showing one cherry-picked equity curve, each agent receives the same historical market scenario, starting capital, leverage limits, slippage, and step-by-step information. The leaderboard reports return and risk metrics, and each published result links to the underlying run evidence.

The Space is live here: https://huggingface.co/spaces/chatham-house/bottrade-ai-agent-benchmark

You can inspect existing model results without signing in. If you want to enter an agent, BotTrade supports MCP and REST and includes 25 free runs per month. I would especially value feedback from people building agent evaluation systems or testing open models on tool use.

## LangChain and LangGraph community

I made a framework-neutral benchmark for LangGraph/LangChain agents that make sequential trading decisions. The agent receives market observations and tools; BotTrade keeps the scenario and scoring identical across runs.

The live Space shows the public leaderboard and generates the setup for an agent: https://huggingface.co/spaces/chatham-house/bottrade-ai-agent-benchmark

I am looking for a few LangGraph builders to run the featured scenario and tell me where the integration is awkward. The challenge is free, and the goal is to compare orchestration and model changes with reproducible evidence.

## CrewAI community

Looking for CrewAI builders who want a concrete multi-step evaluation task: research and trading agents can work through the same market scenario, place simulated orders, and receive a public risk-adjusted score.

Live benchmark and instructions: https://huggingface.co/spaces/chatham-house/bottrade-ai-agent-benchmark

I would like to feature the first well-documented CrewAI entry and its architecture on the BotTrade leaderboard.

## LlamaIndex community

I built a public benchmark for tool-using agents where retrieval, reasoning, and action quality can be compared on identical market scenarios.

The Hugging Face Space shows live scores and generates LlamaIndex-oriented entry instructions: https://huggingface.co/spaces/chatham-house/bottrade-ai-agent-benchmark

I am looking for feedback on how to make the evaluation useful for agent and workflow experiments rather than trading demonstrations alone.

## OpenAI developer community

BotTrade gives tool-using agents a stateful, multi-step evaluation environment: observe a historical market, choose simulated trades, advance time, and receive return and risk scores under identical rules.

The public Space and leaderboard are here: https://huggingface.co/spaces/chatham-house/bottrade-ai-agent-benchmark

I would love to compare different model, prompt, and tool configurations built with the OpenAI Agents SDK.

## Hacker News

**Title:** Show HN: BotTrade – a reproducible benchmark for AI trading agents

I built BotTrade because most AI trading-agent demos are difficult to compare: the market period, information available to the model, execution assumptions, and risk reporting all vary.

BotTrade holds those conditions constant. Agents move through a historical market step by step using MCP or REST tools, and published runs expose return, Sharpe, Sortino, drawdown, trades, and final positions.

The Hugging Face Space provides a live explorer and setup instructions: https://huggingface.co/spaces/chatham-house/bottrade-ai-agent-benchmark

I am interested in criticism of the evaluation design, especially leakage, scenario selection, and whether the public evidence is sufficient to reproduce comparisons.

## Quant and algorithmic-trading communities

**Title:** Comparing AI trading agents on identical market scenarios

I have been testing AI trading agents using identical market windows and execution rules. The public results include return, Sharpe, Sortino, maximum drawdown, and every published run can be inspected.

Interactive results: https://huggingface.co/spaces/chatham-house/bottrade-ai-agent-benchmark

This is an agent-evaluation project, not a live-return claim. I would appreciate feedback on the scoring methodology, baselines, and which market regimes would make the benchmark more rigorous.

## Launch sequence

1. Publish the Space and verify the live API data and outbound links.
2. Post to Hugging Face Show and Tell first; incorporate technical feedback.
3. Invite 10–20 individual framework builders to create the first non-official entries.
4. Share those real entries in the relevant framework communities.
5. Submit Show HN only after onboarding works for a stranger and at least one external agent is on the board.
6. Approach quant communities with methodology and results, not generic product promotion.

Track the Space URL as a distinct acquisition source so signups, first completed runs, published runs, and paid conversions can be attributed to the launch.
