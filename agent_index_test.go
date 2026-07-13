package main

import (
	"bottrade/database"
	"os"
	"testing"
)

func TestAgentIndexUsesOnlySharedScenariosForOverallRanking(t *testing.T) {
	runs := []agentIndexRun{
		{BotName: "Model A", ScenarioSlug: "one", ReturnPct: 10, Category: "agent"},
		{BotName: "Model A", ScenarioSlug: "two", ReturnPct: 20, Category: "agent"},
		{BotName: "Model A", ScenarioSlug: "extra", ReturnPct: 1000, Category: "agent"},
		{BotName: "Model B", ScenarioSlug: "one", ReturnPct: 5, Category: "agent"},
		{BotName: "Model B", ScenarioSlug: "two", ReturnPct: 15, Category: "agent"},
		{BotName: "One-Off Agent", ScenarioSlug: "one", ReturnPct: 5000, Category: "agent"},
		{BotName: "Buy & Hold (SPY)", ScenarioSlug: "one", ReturnPct: 2, Category: "baseline"},
	}
	data := buildAgentIndex(runs)
	if len(data.Models) != 2 {
		t.Fatalf("ranked models = %d, want 2", len(data.Models))
	}
	if len(data.CoreScenarios) != 2 || data.CoreScenarios[0] != "one" || data.CoreScenarios[1] != "two" {
		t.Fatalf("core scenarios = %#v, want [one two]", data.CoreScenarios)
	}
	if data.Models[0].Name != "Model A" || data.Models[0].AverageReturn != 15 {
		t.Fatalf("top model = %#v, want Model A with 15%% mean", data.Models[0])
	}
	if data.ValidAgents != 6 || data.ValidBaselines != 1 {
		t.Fatalf("run counts agents=%d baselines=%d", data.ValidAgents, data.ValidBaselines)
	}
}

func TestAgentIndexClassificationAndSlug(t *testing.T) {
	if got := indexRunCategory("Momentum (20-bar)"); got != "baseline" {
		t.Fatalf("Momentum category = %q", got)
	}
	if got := indexRunCategory("Codex MCP sandbox test"); got != "diagnostic" {
		t.Fatalf("sandbox category = %q", got)
	}
	if got := indexSlug("Claude Opus 4.8"); got != "claude-opus-4-8" {
		t.Fatalf("slug = %q", got)
	}
}

func TestAgentIndexProductionSmoke(t *testing.T) {
	if os.Getenv("BOTTRADE_PRODUCTION_AUDIT") != "1" {
		t.Skip("set BOTTRADE_PRODUCTION_AUDIT=1 for a read-only production-data smoke test")
	}
	if err := database.Connect(os.Getenv("TURSO_DATABASE_URL"), os.Getenv("TURSO_AUTH_TOKEN")); err != nil {
		t.Fatalf("connect production database: %v", err)
	}
	defer database.Close()
	data, err := loadAgentIndexData()
	if err != nil {
		t.Fatalf("load agent index: %v", err)
	}
	if data.ValidAgents == 0 || data.ValidBaselines == 0 || len(data.Models) < 2 || len(data.CoreScenarios) == 0 {
		t.Fatalf("incomplete production index: agents=%d baselines=%d models=%d core=%v", data.ValidAgents, data.ValidBaselines, len(data.Models), data.CoreScenarios)
	}
}
