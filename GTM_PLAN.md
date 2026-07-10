# BotTrade GTM Plan

**Goal:** Build a profitable developer product and reach 500 paying customers.

## Customer

**Primary persona:** An individual developer or small technical team that already has an autonomous trading agent.

They change models, prompts, strategies, tools, and risk controls. They need fast evidence that a new version performs better than the previous version.

**Job to be done:**

> Show me whether this version improved before I spend weeks paper trading or use real money.

## Evidence

PostHog data as of July 9, 2026:

- 2 identified Pro customers and 1 Free account.
- One customer used all 25 Free runs and subscribed 81 minutes later.
- That customer completed 17 additional Pro runs.
- Another customer reached the 200-run Pro limit and generated 209 blocked retries in 19 minutes.
- One customer used BotTrade across 6 separate days.

**Conclusion:** The Free quota creates a purchase moment, repeat evaluation creates recurring value, and heavy users need more capacity.

## Positioning

**Category:** The repeatable test suite for autonomous trading agents.

**Core message:**

> Stop guessing whether your trading agent improved.

**Homepage copy:**

> Run your agent against repeatable historic market scenarios through MCP or REST. Compare model and prompt changes, then publish a credible score.

**CTAs:** Run a free benchmark · Inspect a real result

## Offer

| Tier | Offer |
|---|---|
| Free | 25 runs per month and public results |
| Pro | $19.99 per month and 200 runs |
| Power | $79 per month, 2,000 runs, private comparisons, and exports |

Keep the current Pro offer through the first 20 paying customers. Offer Power directly to the customer who reached the Pro limit.

## Acquisition

### 1. Targeted GitHub outreach

Find active repositories involving AI trading agents, MCP, Alpaca, Interactive Brokers, exchange APIs, paper trading, or multiple model versions.

Send a personalized invitation:

> Hey {name}—I found {repo} while looking at autonomous trading agents. How are you comparing versions when you change the model or prompt? BotTrade lets your own agent rerun the same historic market scenarios through MCP or REST. I can give you a benchmark path tailored to {project detail}. Interested?

**Target per 50 qualified contacts:** 10 replies, 5 first runs, 3 multi-run evaluations, and 1 paid customer.

### 2. Agent ecosystem distribution

- Publish BotTrade in the official MCP Registry and relevant aggregators.
- Maintain the BotTrade Agent Skill as a primary installation path.
- Add working examples for frameworks used by target builders.
- Use the same positioning across the website, GitHub, MCP listing, skill, and docs.

### 3. Technical benchmark content

Publish reproducible comparisons:

- same agent, different model;
- same model, different prompt;
- strategy change before and after;
- agent versus a simple baseline;
- agent performance during a historic crisis.

Each post links directly to the configuration, result, and scenario.

### 4. Public result referrals

Add a clear scorecard and **Run your agent on this scenario** CTA to every public result. Track result viewer → run started → paid customer.

### 5. BotTrade Agent Stress Test

Recruit 10 builders directly from GitHub. Evaluate several scenarios, including a hidden holdout. Publish replays, rankings, and a benchmark report. Award recognition, case studies, and BotTrade credits.

## Immediate actions

1. Apply the positioning and homepage copy above.
2. Add the $79 Power offer.
3. Present Power to the capacity-constrained customer.
4. Build a list of 50 qualified GitHub projects.
5. Send personalized benchmark invitations.
6. Publish one model-or-prompt comparison as the flagship example.
7. Add the scenario CTA to public results.
8. Build a version-versus-version comparison report.
9. Recruit 10 builders for the first stress test.
10. Connect website, account, MCP, run, and billing identities in PostHog.

## Scorecard

**North-star metric:** External accounts testing at least two agent versions or scenarios within 30 days.

| Metric | Target |
|---|---:|
| Qualified visitor → first run | 10%+ |
| First run → completed run | 70%+ |
| First completion → second comparison | 50%+ |
| Activated account → paid | 10%+ |
| Paid customer returning next billing period | 80%+ |
| Public result viewer → run started | 5%+ |

## Growth math

- 500 Pro customers produce **$9,995 MRR**.
- At 10% activated-to-paid conversion, 500 customers require about 5,000 activated builders.
- At 10% visitor-to-activation conversion, that requires about 50,000 qualified visitors or equivalent partner distribution.

Founder outreach proves the customer and message. MCP distribution, integrations, benchmark content, public results, and competitions provide scale.

## Review with the advisor

Ask for decisions on four points:

1. Is the customer specific enough?
2. Does the positioning describe a repeatable paid problem?
3. Which acquisition motion has the strongest path to scale?
4. Does the Free, Pro, and Power structure match the value?
