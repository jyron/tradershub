package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

//go:embed content/articles.json
var articleManifestJSON []byte

type articleItem struct {
	Name   string `json:"name"`
	Metric string `json:"metric,omitempty"`
	Body   string `json:"body"`
	URL    string `json:"url,omitempty"`
}

type scheduledArticle struct {
	Slug            string        `json:"slug"`
	Title           string        `json:"title"`
	Description     string        `json:"description"`
	Kicker          string        `json:"kicker"`
	Deck            string        `json:"deck"`
	Abstract        string        `json:"abstract"`
	ConclusionTitle string        `json:"conclusion_title"`
	Conclusion      string        `json:"conclusion"`
	PublishAt       time.Time     `json:"publish_at"`
	Items           []articleItem `json:"items"`
}

type articleLibrary struct {
	Articles []scheduledArticle `json:"articles"`
}

func loadArticleLibrary() (articleLibrary, error) {
	var library articleLibrary
	if err := json.Unmarshal(articleManifestJSON, &library); err != nil {
		return articleLibrary{}, fmt.Errorf("parse article manifest: %w", err)
	}
	sort.Slice(library.Articles, func(i, j int) bool {
		return library.Articles[i].PublishAt.Before(library.Articles[j].PublishAt)
	})
	seen := make(map[string]struct{}, len(library.Articles))
	for _, article := range library.Articles {
		if article.Slug == "" || article.Title == "" || article.PublishAt.IsZero() || len(article.Items) == 0 {
			return articleLibrary{}, fmt.Errorf("article manifest contains an incomplete entry")
		}
		if _, ok := seen[article.Slug]; ok {
			return articleLibrary{}, fmt.Errorf("duplicate article slug %q", article.Slug)
		}
		seen[article.Slug] = struct{}{}
	}
	return library, nil
}

func mountArticlePublishing(app *fiber.App, now func() time.Time) error {
	library, err := loadArticleLibrary()
	if err != nil {
		return err
	}

	published := func() []scheduledArticle {
		cutoff := now().UTC()
		out := make([]scheduledArticle, 0, len(library.Articles))
		for _, article := range library.Articles {
			if !article.PublishAt.After(cutoff) {
				out = append(out, article)
			}
		}
		sort.Slice(out, func(i, j int) bool {
			return out[i].PublishAt.After(out[j].PublishAt)
		})
		return out
	}

	app.Get("/articles", func(c *fiber.Ctx) error {
		return renderArticleIndex(c, published(), "/articles", false)
	})
	app.Get("/articles/feed.xml", func(c *fiber.Ctx) error {
		return renderArticleFeed(c, published())
	})
	app.Get("/articles/sitemap.xml", func(c *fiber.Ctx) error {
		return renderArticleSitemap(c, published())
	})
	app.Get("/articles/preview", func(c *fiber.Ctx) error {
		return renderArticleIndex(c, library.Articles, "/articles/preview", true)
	})
	app.Get("/articles/preview/:slug", func(c *fiber.Ctx) error {
		slug := c.Params("slug")
		for _, article := range library.Articles {
			if article.Slug == slug {
				return renderArticlePreview(c, article)
			}
		}
		return c.SendStatus(http.StatusNotFound)
	})
	app.Get("/articles/:slug", func(c *fiber.Ctx) error {
		slug := c.Params("slug")
		for _, article := range published() {
			if article.Slug == slug {
				return renderArticle(c, article)
			}
		}
		return c.SendStatus(http.StatusNotFound)
	})
	return nil
}

var articleTemplate = template.Must(template.New("article").Funcs(template.FuncMap{
	"inc": func(i int) int { return i + 1 },
	"date": func(t time.Time) string {
		return t.In(time.FixedZone("America/Bogota", -5*60*60)).Format("January 2, 2006")
	},
}).Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}} | BotTrade Research</title>
  <meta name="description" content="{{.Description}}">
  <link rel="canonical" href="https://bot-trade.org/articles/{{.Slug}}">
  <meta property="og:type" content="article"><meta property="og:site_name" content="BotTrade"><meta property="og:title" content="{{.Title}}"><meta property="og:description" content="{{.Description}}">
  <meta name="twitter:card" content="summary_large_image"><meta name="twitter:title" content="{{.Title}}"><meta name="twitter:description" content="{{.Description}}">
  <script type="application/ld+json">{"@context":"https://schema.org","@type":"Article","headline":{{printf "%q" .Title}},"description":{{printf "%q" .Description}},"datePublished":"{{.PublishAt.Format "2006-01-02"}}","mainEntityOfPage":"https://bot-trade.org/articles/{{.Slug}}","author":{"@type":"Organization","name":"BotTrade Research"},"publisher":{"@type":"Organization","name":"BotTrade","url":"https://bot-trade.org/"}}</script>
  <link rel="alternate" type="application/rss+xml" title="BotTrade Research" href="https://bot-trade.org/articles/feed.xml">
  <link rel="preconnect" href="https://fonts.googleapis.com"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Sans:wght@400;500;600;700;800&family=IBM+Plex+Mono:wght@400;500;600&display=swap" rel="stylesheet">
  <link rel="icon" type="image/svg+xml" href="/favicon.svg"><link rel="stylesheet" href="/vs-page.css"><link rel="stylesheet" href="/listicle-page.css"><script src="/posthog-init.js?v=2"></script>
</head>
<body>
  <header class="topbar"><a href="/" class="brand" style="text-decoration:none"><span class="dot"></span>bot<span class="slash">/</span>trade</a><div class="crumbs"><span class="here">Articles</span></div><nav aria-label="Primary"><a href="/articles" class="on">Articles</a><details class="nav-explore"><summary>Explore</summary><div class="nav-menu"><div class="nav-menu-featured"><a href="/ai-trading-agent-index"><strong>AI Trading Agent Index</strong><span>Compare models across eligible public runs.</span></a><a href="/leaderboard"><strong>Leaderboard</strong><span>Inspect published returns and risk metrics.</span></a></div><div class="nav-menu-links"><a href="/challenge">Challenge</a><a href="/scenarios">Scenarios</a><a href="/demo">Live demo</a><a href="/articles/ai-trading-bot-backtesting">Backtesting guide</a><a href="/pricing">Pricing</a><a href="/contact">Contact</a></div></div></details><a href="/docs">Docs</a><a href="/account">Account</a></nav></header>
  <main class="rank-page wrap">
    <section class="rank-hero"><p class="rank-kicker">{{.Kicker}}</p><h1>{{.Title}}</h1><p class="rank-deck">{{.Deck}}</p><div class="rank-meta"><span>BotTrade Research</span><span>Published {{date .PublishAt}}</span><span>{{len .Items}} ranked entries</span></div></section>
    <section class="abstract"><h2>Abstract</h2><p>{{.Abstract}}</p></section>
    <section class="rankings">{{range $i, $item := .Items}}
      <article class="rank-card"><div class="rank-number">{{printf "%02d" (inc $i)}}</div><div><h2>{{$item.Name}}</h2><p>{{$item.Body}}</p></div><div class="score">{{if $item.Metric}}<strong>{{$item.Metric}}</strong>{{end}}{{if $item.URL}}<a href="{{$item.URL}}">Inspect source →</a>{{end}}</div></article>{{end}}
    </section>
    <section class="analysis-block"><h2>{{.ConclusionTitle}}</h2><p>{{.Conclusion}}</p></section>
    <nav class="related-ranks"><a href="/articles"><b>More BotTrade research</b><span>Browse every published ranking and field guide.</span></a><a href="/leaderboard"><b>Live agent leaderboard</b><span>Explore published historical-market benchmark results.</span></a><a href="/account"><b>Benchmark an agent</b><span>Connect through hosted MCP or REST.</span></a></nav>
  </main>
</body>
</html>`))

type articleIndexView struct {
	Articles []scheduledArticle
	BaseURL  string
	Preview  bool
}

var articleIndexTemplate = template.Must(template.New("index").Funcs(template.FuncMap{
	"date": func(t time.Time) string {
		return t.In(time.FixedZone("America/Bogota", -5*60*60)).Format("Jan 2, 2006")
	},
}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>AI Trading Agent Articles and Rankings | BotTrade</title><meta name="description" content="BotTrade Research publishes rankings, comparative studies, and field guides about AI trading agents, autonomous finance, MCP tools, and backtesting."><link rel="canonical" href="https://bot-trade.org/articles"><link rel="alternate" type="application/rss+xml" title="BotTrade Research" href="https://bot-trade.org/articles/feed.xml"><link rel="preconnect" href="https://fonts.googleapis.com"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin><link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Sans:wght@400;500;600;700;800&family=IBM+Plex+Mono:wght@400;500;600&display=swap" rel="stylesheet"><link rel="icon" type="image/svg+xml" href="/favicon.svg"><link rel="stylesheet" href="/vs-page.css"><link rel="stylesheet" href="/listicle-page.css"><script src="/posthog-init.js?v=2"></script></head>
<body><header class="topbar"><a href="/" class="brand" style="text-decoration:none"><span class="dot"></span>bot<span class="slash">/</span>trade</a><div class="crumbs"><span class="here">Articles</span></div><nav aria-label="Primary"><a href="/articles" class="on">Articles</a><details class="nav-explore"><summary>Explore</summary><div class="nav-menu"><div class="nav-menu-featured"><a href="/ai-trading-agent-index"><strong>AI Trading Agent Index</strong><span>Compare models across eligible public runs.</span></a><a href="/leaderboard"><strong>Leaderboard</strong><span>Inspect published returns and risk metrics.</span></a></div><div class="nav-menu-links"><a href="/challenge">Challenge</a><a href="/scenarios">Scenarios</a><a href="/demo">Live demo</a><a href="/articles/ai-trading-bot-backtesting">Backtesting guide</a><a href="/pricing">Pricing</a><a href="/contact">Contact</a></div></div></details><a href="/docs">Docs</a><a href="/account">Account</a></nav></header>
<main class="rank-page wrap"><section class="rank-hero"><p class="rank-kicker">BotTrade Research{{if .Preview}} · Editorial Preview{{end}}</p><h1>{{if .Preview}}Scheduled article preview.{{else}}Research on autonomous trading systems.{{end}}</h1><p class="rank-deck">{{if .Preview}}All scheduled articles appear here before public release.{{else}}Comparative studies, system architecture analyses, and historical-market benchmark research for AI trading-agent development.{{end}}</p></section>
{{if not .Preview}}<section class="abstract"><h2>Research guides</h2><p><a href="/articles/ai-trading-bot-backtesting">AI trading-agent backtesting methodology</a> · <a href="/articles/backtest-ai-trading-agents">BotTrade agent-evaluation architecture</a> · <a href="/articles/ai-trading-agent-evaluation">Controlled evaluation protocol</a> · <a href="/articles/mcp-for-trading-agents">MCP evaluation infrastructure</a></p></section>{{end}}
<section class="rankings">{{if .Articles}}{{range .Articles}}<article class="rank-card"><div class="rank-date">{{date .PublishAt}}</div><div><h2><a href="{{$.BaseURL}}/{{.Slug}}" style="text-decoration:none">{{.Title}}</a></h2><p>{{.Description}}</p></div><div class="score"><a href="{{$.BaseURL}}/{{.Slug}}">Read article →</a></div></article>{{end}}{{else}}<section class="abstract"><h2>Publication schedule active</h2><p>The first BotTrade Research articles will appear here automatically.</p></section>{{end}}</section></main></body></html>`))

func renderArticle(c *fiber.Ctx, article scheduledArticle) error {
	return renderArticleMode(c, article, false)
}

func renderArticlePreview(c *fiber.Ctx, article scheduledArticle) error {
	return renderArticleMode(c, article, true)
}

func renderArticleMode(c *fiber.Ctx, article scheduledArticle, preview bool) error {
	var body bytes.Buffer
	if err := articleTemplate.Execute(&body, article); err != nil {
		return err
	}
	c.Set(fiber.HeaderContentType, "text/html; charset=utf-8")
	if preview {
		c.Set("X-Robots-Tag", "noindex, nofollow, noarchive")
		c.Set(fiber.HeaderCacheControl, "no-store")
	} else {
		c.Set(fiber.HeaderCacheControl, "public, max-age=300")
	}
	return c.Send(body.Bytes())
}

func renderArticleIndex(c *fiber.Ctx, articles []scheduledArticle, baseURL string, preview bool) error {
	var body bytes.Buffer
	view := articleIndexView{Articles: articles, BaseURL: baseURL, Preview: preview}
	if err := articleIndexTemplate.Execute(&body, view); err != nil {
		return err
	}
	c.Set(fiber.HeaderContentType, "text/html; charset=utf-8")
	c.Set(fiber.HeaderCacheControl, "no-cache, max-age=0, must-revalidate")
	if preview {
		c.Set("X-Robots-Tag", "noindex, nofollow, noarchive")
		c.Set(fiber.HeaderCacheControl, "no-store")
	}
	return c.Send(body.Bytes())
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

type sitemapURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	XMLNS   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

func renderArticleSitemap(c *fiber.Ctx, articles []scheduledArticle) error {
	set := sitemapURLSet{XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	set.URLs = append(set.URLs, sitemapURL{Loc: "https://bot-trade.org/articles"})
	for _, article := range articles {
		set.URLs = append(set.URLs, sitemapURL{
			Loc:     "https://bot-trade.org/articles/" + article.Slug,
			LastMod: article.PublishAt.Format("2006-01-02"),
		})
	}
	body, err := xml.MarshalIndent(set, "", "  ")
	if err != nil {
		return err
	}
	c.Set(fiber.HeaderContentType, "application/xml; charset=utf-8")
	c.Set(fiber.HeaderCacheControl, "no-cache, max-age=0, must-revalidate")
	return c.Send(append([]byte(xml.Header), body...))
}

func renderArticleFeed(c *fiber.Ctx, articles []scheduledArticle) error {
	var body strings.Builder
	body.WriteString(xml.Header)
	body.WriteString(`<rss version="2.0"><channel><title>BotTrade Research</title><link>https://bot-trade.org/articles</link><description>AI trading agent rankings and research.</description>`)
	for _, article := range articles {
		body.WriteString("<item><title>")
		_ = xml.EscapeText(&body, []byte(article.Title))
		body.WriteString("</title><link>https://bot-trade.org/articles/")
		_ = xml.EscapeText(&body, []byte(article.Slug))
		body.WriteString("</link><guid>https://bot-trade.org/articles/")
		_ = xml.EscapeText(&body, []byte(article.Slug))
		body.WriteString("</guid><pubDate>")
		body.WriteString(article.PublishAt.Format(time.RFC1123Z))
		body.WriteString("</pubDate><description>")
		_ = xml.EscapeText(&body, []byte(article.Description))
		body.WriteString("</description></item>")
	}
	body.WriteString("</channel></rss>")
	c.Set(fiber.HeaderContentType, "application/rss+xml; charset=utf-8")
	c.Set(fiber.HeaderCacheControl, "no-cache, max-age=0, must-revalidate")
	return c.SendString(body.String())
}
