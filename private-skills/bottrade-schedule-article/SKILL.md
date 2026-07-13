---
name: bottrade-schedule-article
description: Create and schedule scholarly BotTrade research articles in content/articles.json. Use when asked to write a new BotTrade article, listicle, ranking, comparison, field guide, research-style post, SEO article, or to add content to the automated three-per-day /articles publishing queue.
---

# BotTrade Article Scheduler

Create one distinct article and add it to BotTrade's timestamp-gated publication manifest. The running service publishes it automatically under `/articles/<slug>` when `publish_at` arrives.

## Workflow

1. Read `content/articles.json` and scan existing titles, slugs, topics, and scheduled dates.
2. Choose a distinct high-interest topic. Avoid duplicating an existing search intent.
3. Read [references/article-schema.md](references/article-schema.md).
4. Draft the article as one JSON object matching the manifest schema.
5. Calculate the next queue slot:

   ```bash
   python3 private-skills/bottrade-schedule-article/scripts/next_publication_slot.py content/articles.json
   ```

6. Set the returned RFC3339 value as `publish_at`.
7. Append the object to the `articles` array with `apply_patch`. Do not rewrite or reformat existing articles.
8. Validate the complete manifest:

   ```bash
   python3 private-skills/bottrade-schedule-article/scripts/validate_articles.py content/articles.json
   ```

9. Confirm the article URL is `/articles/<slug>`. Future articles must return 404 until their timestamp; the service adds published articles to `/articles`, `/articles/feed.xml`, and `/articles/sitemap.xml` automatically.
10. Deploy the existing Railway service `tradershub` from the
    `jyron/tradershub` repository only when the user asks to update the live
    queue. The public `jyron/bottrade` SDK repository is not the article
    deployment source. A separate service or cron job is unnecessary.

## Editorial standard

- Use formal, authoritative, research-oriented prose.
- Prefer numerical listicle titles with strong search intent.
- Include a descriptive deck, an abstract, ranked items, and a decisive synthesis.
- Give every item a substantive interpretation, not a keyword-only sentence.
- Integrate BotTrade naturally as the benchmark, comparison, or implementation layer.
- Use published BotTrade run URLs when an article presents leaderboard performance.
- Keep titles, slugs, descriptions, and conclusions unique across the library.
- Do not alter previously published articles while adding a new one.

## Scheduling contract

- Time zone: America/Bogota (UTC-5).
- Daily slots: 09:00, 13:00, and 17:00.
- Maximum: three articles per local calendar day.
- Queue policy: append after the latest scheduled article; never backfill an earlier slot unless explicitly requested.
- The timestamp controls visibility. No background scheduler is required.
