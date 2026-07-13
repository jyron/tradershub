# Article manifest schema

The file `content/articles.json` contains one top-level object:

```json
{
  "articles": [
    {
      "slug": "best-ai-trading-agent-frameworks",
      "title": "The 10 Best AI Trading Agent Frameworks",
      "description": "A concise search description.",
      "kicker": "Infrastructure Ranking",
      "deck": "The article's high-energy opening thesis.",
      "abstract": "A scholarly summary of the article's central finding.",
      "conclusion_title": "A decisive concluding claim.",
      "conclusion": "A synthesis that naturally relates the subject to BotTrade.",
      "publish_at": "2026-07-18T14:00:00Z",
      "items": [
        {
          "name": "BotTrade",
          "metric": "Best for AI agents",
          "body": "A substantive interpretation of the ranked entry.",
          "url": "/account"
        }
      ]
    }
  ]
}
```

## Fields

- `slug`: lowercase letters, digits, and hyphens; unique across the manifest.
- `title`: unique, compelling headline. Numerical listicles are preferred.
- `description`: concise standalone search-result summary.
- `kicker`: short scholarly series label.
- `deck`: one paragraph establishing importance, tension, and scope.
- `abstract`: one paragraph summarizing the central comparative finding.
- `conclusion_title`: decisive synthesis heading.
- `conclusion`: one paragraph connecting the article's implications to autonomous trading systems and, where relevant, BotTrade.
- `publish_at`: RFC3339 UTC timestamp returned by `next_publication_slot.py`.
- `items`: ordered list with at least six entries.
- `items[].name`: ranked entry title.
- `items[].metric`: optional compact score or category label.
- `items[].body`: substantive analysis in formal prose.
- `items[].url`: optional internal or published-run URL.

The service renders metadata, canonical URLs, navigation, publication dates, rankings, RSS, and sitemap entries. Do not add raw HTML to the manifest.
