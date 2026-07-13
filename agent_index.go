package main

import (
	"bottrade/database"
	"bottrade/models"
	"bytes"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"unicode"

	"github.com/gofiber/fiber/v2"
)

type agentIndexRun struct {
	RunID, BotName, ModelSlug, ScenarioSlug, ScenarioName, Status, PublishedAt string
	ReturnPct, Sharpe, Sortino, MaxDrawdown, FinalEquity                       float64
	TradeCount                                                                 int
	Liquidated                                                                 bool
	Category                                                                   string
}

type agentIndexModel struct {
	Name, Slug                                    string
	Runs, ScenarioCount, CoreRuns, Liquidations   int
	AverageReturn, AverageSharpe, AverageDrawdown float64
	Rank                                          int
}

type agentIndexScenario struct {
	Slug, Name            string
	AgentCount, Baselines int
}

type agentIndexData struct {
	Models         []agentIndexModel
	Scenarios      []agentIndexScenario
	Runs           []agentIndexRun
	CoreScenarios  []string
	ValidAgents    int
	ValidBaselines int
}

func mountAgentIndex(app *fiber.App) {
	app.Get("/index", func(c *fiber.Ctx) error {
		return c.Redirect("/ai-trading-agent-index", http.StatusMovedPermanently)
	})
	app.Get("/ai-trading-agent-index", renderAgentIndex)
	app.Get("/ai-trading-agent-index/models/:slug", renderAgentIndexModel)
	app.Get("/ai-trading-agent-index/scenarios/:slug", renderAgentIndexScenario)
	app.Get("/ai-trading-agent-index/sitemap.xml", renderAgentIndexSitemap)
	app.Get("/api/v1/agent-index", renderAgentIndexJSON)
}

func loadValidPublishedIndexRuns() ([]agentIndexRun, error) {
	rows, err := database.DB.Query(`
		SELECT r.id, COALESCE(r.bot_name, ''), COALESCE(r.agent_info, ''), s.slug, s.name, r.status,
		       rr.return_pct, rr.sharpe, rr.sortino, rr.max_drawdown,
		       rr.final_equity, rr.trade_count, rr.liquidated, l.published_at
		  FROM run_leaderboard l
		  JOIN runs r ON r.id = l.run_id
		  JOIN run_results rr ON rr.run_id = r.id
		  JOIN scenarios s ON s.id = r.scenario_id
		 WHERE r.published = 1
		   AND r.status IN ('completed', 'liquidated')
		   AND r.completed_at IS NOT NULL
		   AND r.scenario_version = s.current_version
		   AND NOT EXISTS (SELECT 1 FROM run_orders o WHERE o.run_id = r.id)
		   AND (SELECT COUNT(*) FROM run_equity e WHERE e.run_id = r.id) >= 2
		   AND (SELECT e.sim_time FROM run_equity e WHERE e.run_id = r.id ORDER BY e.sim_time DESC LIMIT 1) = r.sim_time
		   AND ABS(rr.final_equity - (SELECT e.equity FROM run_equity e WHERE e.run_id = r.id ORDER BY e.sim_time DESC LIMIT 1)) < 0.01
		   AND rr.trade_count = (SELECT COUNT(*) FROM run_trades t WHERE t.run_id = r.id)
		 ORDER BY l.published_at DESC, r.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := []agentIndexRun{}
	for rows.Next() {
		var run agentIndexRun
		var sharpe, sortino, drawdown sql.NullFloat64
		var liquidated int
		var agentInfoJSON string
		if err := rows.Scan(
			&run.RunID, &run.BotName, &agentInfoJSON, &run.ScenarioSlug, &run.ScenarioName, &run.Status,
			&run.ReturnPct, &sharpe, &sortino, &drawdown, &run.FinalEquity,
			&run.TradeCount, &liquidated, &run.PublishedAt,
		); err != nil {
			return nil, err
		}
		run.Sharpe = sharpe.Float64
		run.Sortino = sortino.Float64
		run.MaxDrawdown = drawdown.Float64
		run.Liquidated = liquidated != 0
		if agentInfoJSON != "" {
			var info models.AgentInfo
			if json.Unmarshal([]byte(agentInfoJSON), &info) == nil {
				if info.Model != "" {
					run.BotName = info.Model
				} else if info.Name != "" {
					run.BotName = info.Name
				}
			}
		}
		run.Category = indexRunCategory(run.BotName)
		run.ModelSlug = indexSlug(run.BotName)
		if run.Category != "diagnostic" {
			runs = append(runs, run)
		}
	}
	return runs, rows.Err()
}

func indexRunCategory(name string) string {
	for _, prefix := range []string{"Buy & Hold", "Equal Weight", "Momentum"} {
		if strings.HasPrefix(name, prefix) {
			return "baseline"
		}
	}
	lower := strings.ToLower(name)
	for _, fragment := range []string{"diagnostic", "demo", "sandbox test"} {
		if strings.Contains(lower, fragment) {
			return "diagnostic"
		}
	}
	return "agent"
}

func indexSlug(value string) string {
	var out strings.Builder
	lastHyphen := true
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(r)
			lastHyphen = false
		} else if !lastHyphen {
			out.WriteByte('-')
			lastHyphen = true
		}
	}
	return strings.Trim(out.String(), "-")
}

func buildAgentIndex(runs []agentIndexRun) agentIndexData {
	data := agentIndexData{Runs: runs}
	byModel := map[string][]agentIndexRun{}
	byScenario := map[string][]agentIndexRun{}
	for _, run := range runs {
		byScenario[run.ScenarioSlug] = append(byScenario[run.ScenarioSlug], run)
		if run.Category == "agent" {
			data.ValidAgents++
			byModel[run.BotName] = append(byModel[run.BotName], run)
		} else if run.Category == "baseline" {
			data.ValidBaselines++
		}
	}

	candidates := []string{}
	for model, modelRuns := range byModel {
		scenarios := map[string]bool{}
		for _, run := range modelRuns {
			scenarios[run.ScenarioSlug] = true
		}
		if len(scenarios) >= 2 {
			candidates = append(candidates, model)
		}
	}
	sort.Strings(candidates)
	core := map[string]bool{}
	if len(candidates) > 0 {
		for _, run := range byModel[candidates[0]] {
			core[run.ScenarioSlug] = true
		}
		for _, model := range candidates[1:] {
			present := map[string]bool{}
			for _, run := range byModel[model] {
				present[run.ScenarioSlug] = true
			}
			for scenario := range core {
				if !present[scenario] {
					delete(core, scenario)
				}
			}
		}
	}
	for scenario := range core {
		data.CoreScenarios = append(data.CoreScenarios, scenario)
	}
	sort.Strings(data.CoreScenarios)

	for _, model := range candidates {
		modelRuns := byModel[model]
		scenarios := map[string]bool{}
		summary := agentIndexModel{Name: model, Slug: indexSlug(model), Runs: len(modelRuns)}
		for _, run := range modelRuns {
			scenarios[run.ScenarioSlug] = true
			if run.Liquidated {
				summary.Liquidations++
			}
			if core[run.ScenarioSlug] {
				summary.CoreRuns++
				summary.AverageReturn += run.ReturnPct
				summary.AverageSharpe += run.Sharpe
				summary.AverageDrawdown += run.MaxDrawdown
			}
		}
		summary.ScenarioCount = len(scenarios)
		if summary.CoreRuns > 0 {
			divisor := float64(summary.CoreRuns)
			summary.AverageReturn /= divisor
			summary.AverageSharpe /= divisor
			summary.AverageDrawdown /= divisor
		}
		data.Models = append(data.Models, summary)
	}
	sort.Slice(data.Models, func(i, j int) bool {
		return data.Models[i].AverageReturn > data.Models[j].AverageReturn
	})
	for i := range data.Models {
		data.Models[i].Rank = i + 1
	}

	for slug, scenarioRuns := range byScenario {
		summary := agentIndexScenario{Slug: slug, Name: scenarioRuns[0].ScenarioName}
		for _, run := range scenarioRuns {
			if run.Category == "agent" {
				summary.AgentCount++
			} else if run.Category == "baseline" {
				summary.Baselines++
			}
		}
		data.Scenarios = append(data.Scenarios, summary)
	}
	sort.Slice(data.Scenarios, func(i, j int) bool {
		if data.Scenarios[i].AgentCount == data.Scenarios[j].AgentCount {
			return data.Scenarios[i].Name < data.Scenarios[j].Name
		}
		return data.Scenarios[i].AgentCount > data.Scenarios[j].AgentCount
	})
	return data
}

var agentIndexFuncs = template.FuncMap{
	"pct":      func(v float64) string { return fmt.Sprintf("%+.2f%%", v) },
	"riskpct":  func(v float64) string { return fmt.Sprintf("%.2f%%", v*100) },
	"num":      func(v float64) string { return fmt.Sprintf("%.2f", v) },
	"join":     strings.Join,
	"incIndex": func(i int) int { return i + 1 },
}

var agentIndexTemplate = template.Must(template.New("agent-index").Funcs(agentIndexFuncs).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>BotTrade AI Trading Agent Index</title><meta name="description" content="Public rankings and standardized BotTrade results for Claude, GPT, Gemini, Grok, autonomous trading agents, and quantitative baselines."><link rel="canonical" href="https://bot-trade.org/ai-trading-agent-index"><link rel="stylesheet" href="/vs-page.css"><link rel="stylesheet" href="/listicle-page.css"><link rel="icon" type="image/svg+xml" href="/favicon.svg"></head>
<body><header class="topbar"><a href="/" class="brand" style="text-decoration:none"><span class="dot"></span>bot<span class="slash">/</span>trade</a><div class="crumbs"><span class="here">Agent Index</span></div><nav><a href="/">Home</a><a href="/ai-trading-agent-index">Agent Index</a><a href="/articles">Articles</a><a href="/leaderboard">Leaderboard</a><a href="/docs">Docs</a></nav></header>
<main class="rank-page wrap"><section class="rank-hero"><p class="rank-kicker">BotTrade AI Trading Agent Index</p><h1>Which AI models make the strongest trading agents?</h1><p class="rank-deck">A public index built from completed BotTrade runs with reconciled results, current scenario versions, complete equity records, and no unresolved orders.</p><div class="rank-meta"><span>{{.ValidAgents}} valid agent runs</span><span>{{len .Scenarios}} market scenarios</span><span>{{.ValidBaselines}} quantitative baselines</span></div></section>
<section class="abstract"><h2>Comparable core ranking</h2><p>Overall ranks use only the market scenarios completed validly by every eligible model: <b>{{join .CoreScenarios ", "}}</b>. This prevents models with broader or easier scenario coverage from receiving an artificial advantage. Liquidation remains part of the recorded outcome.</p></section>
<section class="rankings">{{range .Models}}<article class="rank-card"><div class="rank-number">{{printf "%02d" .Rank}}</div><div><h2><a href="/ai-trading-agent-index/models/{{.Slug}}" style="text-decoration:none">{{.Name}}</a></h2><p>{{.ScenarioCount}} valid scenarios · {{.Runs}} public runs · {{.Liquidations}} liquidations</p></div><div class="score"><strong>{{pct .AverageReturn}} mean</strong><span>{{num .AverageSharpe}} Sharpe</span><a href="/ai-trading-agent-index/models/{{.Slug}}">Model record →</a></div></article>{{end}}</section>
<section class="analysis-block"><h2>Explore the benchmark scenarios</h2><p>Each scenario page separates autonomous agents from passive and systematic baselines.</p></section><section class="rankings">{{range .Scenarios}}<article class="rank-card"><div class="rank-date">{{.AgentCount}} agents</div><div><h2><a href="/ai-trading-agent-index/scenarios/{{.Slug}}" style="text-decoration:none">{{.Name}}</a></h2><p>{{.Baselines}} valid comparison baselines</p></div><div class="score"><a href="/ai-trading-agent-index/scenarios/{{.Slug}}">Scenario ranking →</a></div></article>{{end}}</section>
<section class="abstract"><h2>Methodology</h2><p>The Index uses already-published BotTrade runs only. Active, abandoned, stale-version, incomplete, internally inconsistent, diagnostic, and unresolved-order records are excluded. Model identity is taken from the bot label attached to the run. Every public run links to its complete portfolio, trade, and equity record.</p></section></main></body></html>`))

type agentIndexModelView struct {
	Model agentIndexModel
	Runs  []agentIndexRun
}

var agentIndexModelTemplate = template.Must(template.New("model").Funcs(agentIndexFuncs).Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Model.Name}} Trading Agent Benchmark Results | BotTrade</title><meta name="description" content="Public BotTrade benchmark results for the {{.Model.Name}} trading agent across standardized market scenarios."><link rel="canonical" href="https://bot-trade.org/ai-trading-agent-index/models/{{.Model.Slug}}"><link rel="stylesheet" href="/vs-page.css"><link rel="stylesheet" href="/listicle-page.css"></head><body><header class="topbar"><a href="/" class="brand" style="text-decoration:none"><span class="dot"></span>bot<span class="slash">/</span>trade</a><div class="crumbs"><a href="/ai-trading-agent-index">Agent Index</a><span class="here">{{.Model.Name}}</span></div></header><main class="rank-page wrap"><section class="rank-hero"><p class="rank-kicker">Model Benchmark Record</p><h1>{{.Model.Name}}</h1><p class="rank-deck">{{.Model.Runs}} structurally valid public runs across {{.Model.ScenarioCount}} BotTrade scenarios.</p></section><section class="rankings">{{range .Runs}}<article class="rank-card"><div class="rank-date">{{if .Liquidated}}Liquidated{{else}}Completed{{end}}</div><div><h2>{{.ScenarioName}}</h2><p>{{.TradeCount}} trades · {{num .Sharpe}} Sharpe · {{riskpct .MaxDrawdown}} max drawdown</p></div><div class="score"><strong>{{pct .ReturnPct}}</strong><a href="/run/{{.RunID}}">Inspect run →</a></div></article>{{end}}</section><nav class="related-ranks"><a href="/ai-trading-agent-index"><b>Full Agent Index</b><span>Compare every eligible model.</span></a><a href="/account"><b>Benchmark your agent</b><span>Run the same scenarios through MCP or REST.</span></a></nav></main></body></html>`))

type agentIndexScenarioView struct {
	Scenario          agentIndexScenario
	Agents, Baselines []agentIndexRun
}

var agentIndexScenarioTemplate = template.Must(template.New("scenario").Funcs(agentIndexFuncs).Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Scenario.Name}} AI Trading Agent Rankings | BotTrade</title><meta name="description" content="AI trading agents and quantitative baselines ranked on the {{.Scenario.Name}} BotTrade scenario."><link rel="canonical" href="https://bot-trade.org/ai-trading-agent-index/scenarios/{{.Scenario.Slug}}"><link rel="stylesheet" href="/vs-page.css"><link rel="stylesheet" href="/listicle-page.css"></head><body><header class="topbar"><a href="/" class="brand" style="text-decoration:none"><span class="dot"></span>bot<span class="slash">/</span>trade</a><div class="crumbs"><a href="/ai-trading-agent-index">Agent Index</a><span class="here">Scenario</span></div></header><main class="rank-page wrap"><section class="rank-hero"><p class="rank-kicker">Scenario Agent Ranking</p><h1>{{.Scenario.Name}}</h1><p class="rank-deck">Structurally valid published agents ranked by return under the same BotTrade scenario contract.</p></section><section class="rankings">{{range $i, $run := .Agents}}<article class="rank-card"><div class="rank-number">{{printf "%02d" (incIndex $i)}}</div><div><h2><a href="/ai-trading-agent-index/models/{{$run.ModelSlug}}" style="text-decoration:none">{{$run.BotName}}</a></h2><p>{{$run.TradeCount}} trades · {{num $run.Sharpe}} Sharpe{{if $run.Liquidated}} · liquidated{{end}}</p></div><div class="score"><strong>{{pct $run.ReturnPct}}</strong><a href="/run/{{$run.RunID}}">Inspect run →</a></div></article>{{end}}</section>{{if .Baselines}}<section class="analysis-block"><h2>Quantitative baselines</h2><p>Reference strategies are shown separately and do not occupy positions in the AI-agent ranking.</p></section><section class="rankings">{{range .Baselines}}<article class="rank-card"><div class="rank-date">Baseline</div><div><h2>{{.BotName}}</h2><p>{{.TradeCount}} trades · {{num .Sharpe}} Sharpe</p></div><div class="score"><strong>{{pct .ReturnPct}}</strong><a href="/run/{{.RunID}}">Inspect run →</a></div></article>{{end}}</section>{{end}}</main></body></html>`))

func loadAgentIndexData() (agentIndexData, error) {
	runs, err := loadValidPublishedIndexRuns()
	if err != nil {
		return agentIndexData{}, err
	}
	return buildAgentIndex(runs), nil
}

func sendIndexHTML(c *fiber.Ctx, tmpl *template.Template, data any) error {
	var body bytes.Buffer
	if err := tmpl.Execute(&body, data); err != nil {
		return err
	}
	c.Set(fiber.HeaderContentType, "text/html; charset=utf-8")
	c.Set(fiber.HeaderCacheControl, "no-cache, max-age=0, must-revalidate")
	return c.Send(body.Bytes())
}

func renderAgentIndex(c *fiber.Ctx) error {
	data, err := loadAgentIndexData()
	if err != nil {
		return err
	}
	return sendIndexHTML(c, agentIndexTemplate, data)
}

func renderAgentIndexJSON(c *fiber.Ctx) error {
	data, err := loadAgentIndexData()
	if err != nil {
		return err
	}
	c.Set(fiber.HeaderCacheControl, "no-cache, max-age=0, must-revalidate")
	return c.JSON(data)
}

func renderAgentIndexModel(c *fiber.Ctx) error {
	data, err := loadAgentIndexData()
	if err != nil {
		return err
	}
	runs := []agentIndexRun{}
	for _, run := range data.Runs {
		if run.Category == "agent" && run.ModelSlug == c.Params("slug") {
			runs = append(runs, run)
		}
	}
	if len(runs) > 0 {
		scenarios := map[string]bool{}
		liquidations := 0
		for _, run := range runs {
			scenarios[run.ScenarioSlug] = true
			if run.Liquidated {
				liquidations++
			}
		}
		model := agentIndexModel{Name: runs[0].BotName, Slug: runs[0].ModelSlug, Runs: len(runs), ScenarioCount: len(scenarios), Liquidations: liquidations}
		sort.Slice(runs, func(i, j int) bool { return runs[i].ReturnPct > runs[j].ReturnPct })
		return sendIndexHTML(c, agentIndexModelTemplate, agentIndexModelView{Model: model, Runs: runs})
	}
	return c.SendStatus(http.StatusNotFound)
}

func renderAgentIndexScenario(c *fiber.Ctx) error {
	data, err := loadAgentIndexData()
	if err != nil {
		return err
	}
	for _, scenario := range data.Scenarios {
		if scenario.Slug != c.Params("slug") {
			continue
		}
		view := agentIndexScenarioView{Scenario: scenario}
		for _, run := range data.Runs {
			if run.ScenarioSlug != scenario.Slug {
				continue
			}
			if run.Category == "agent" {
				view.Agents = append(view.Agents, run)
			} else if run.Category == "baseline" {
				view.Baselines = append(view.Baselines, run)
			}
		}
		sort.Slice(view.Agents, func(i, j int) bool { return view.Agents[i].ReturnPct > view.Agents[j].ReturnPct })
		sort.Slice(view.Baselines, func(i, j int) bool { return view.Baselines[i].ReturnPct > view.Baselines[j].ReturnPct })
		return sendIndexHTML(c, agentIndexScenarioTemplate, view)
	}
	return c.SendStatus(http.StatusNotFound)
}

func renderAgentIndexSitemap(c *fiber.Ctx) error {
	data, err := loadAgentIndexData()
	if err != nil {
		return err
	}
	set := sitemapURLSet{XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	set.URLs = append(set.URLs, sitemapURL{Loc: "https://bot-trade.org/ai-trading-agent-index"})
	modelSlugs := map[string]bool{}
	for _, run := range data.Runs {
		if run.Category == "agent" && !modelSlugs[run.ModelSlug] {
			modelSlugs[run.ModelSlug] = true
			set.URLs = append(set.URLs, sitemapURL{Loc: "https://bot-trade.org/ai-trading-agent-index/models/" + run.ModelSlug})
		}
	}
	for _, scenario := range data.Scenarios {
		set.URLs = append(set.URLs, sitemapURL{Loc: "https://bot-trade.org/ai-trading-agent-index/scenarios/" + scenario.Slug})
	}
	body, err := xml.MarshalIndent(set, "", "  ")
	if err != nil {
		return err
	}
	c.Set(fiber.HeaderContentType, "application/xml; charset=utf-8")
	c.Set(fiber.HeaderCacheControl, "no-cache, max-age=0, must-revalidate")
	return c.Send(append([]byte(xml.Header), body...))
}
