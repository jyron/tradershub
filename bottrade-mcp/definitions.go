package main

import (
	"context"
	"encoding/json"
	"fmt"
)

func tools() []tool {
	return []tool{
		{
			Name:        "connect_bottrade",
			Description: "Connect BotTrade.",
			InputSchema: objectSchema(map[string]any{
				"wait_seconds": integerSchema("Wait for auth.", 0),
			}, nil),
			Annotations: mutatingToolAnnotations("Starts the BotTrade OAuth sign-in flow when auth is required."),
		},
		{
			Name:        "list_scenarios",
			Description: "List scenarios.",
			InputSchema: objectSchema(map[string]any{}, nil),
			Annotations: readOnlyToolAnnotations("Read the available scenario catalog."),
		},
		{
			Name:        "get_scenario",
			Description: "Get scenario details.",
			InputSchema: objectSchema(map[string]any{
				"id_or_slug": stringSchema("Scenario slug or UUID."),
			}, []string{"id_or_slug"}),
			Annotations: readOnlyToolAnnotations("Read scenario metadata before starting a run."),
		},
		{
			Name:        "start_run",
			Description: "Start a run. This creates a new simulation run.",
			InputSchema: objectSchema(map[string]any{
				"scenario_slug": stringSchema("Scenario slug from list_scenarios."),
				"bot_name":      stringSchema("Optional bot, strategy, or experiment name."),
			}, []string{"scenario_slug"}),
			Annotations: mutatingToolAnnotations("Creates a new run in the user's BotTrade account."),
		},
		{
			Name:        "get_run",
			Description: "Get run state.",
			InputSchema: objectSchema(map[string]any{
				"run_id": stringSchema("Run UUID returned by start_run."),
			}, []string{"run_id"}),
			Annotations: readOnlyToolAnnotations("Read the current portfolio, queued orders, and run status."),
		},
		{
			Name:        "get_market",
			Description: "Get market bars.",
			InputSchema: objectSchema(map[string]any{
				"run_id":   stringSchema("Run UUID."),
				"symbols":  arraySchema(stringSchema("Ticker symbol from the scenario universe."), "Optional symbol subset. Omit only with lookback=1."),
				"lookback": integerSchema("Bars per symbol.", 1),
			}, []string{"run_id"}),
			Annotations: readOnlyToolAnnotations("Read market bars for the current run without changing state."),
		},
		{
			Name:        "scan_market",
			Description: "Scan market.",
			InputSchema: objectSchema(map[string]any{
				"run_id": stringSchema("Run UUID."),
			}, []string{"run_id"}),
			Annotations: readOnlyToolAnnotations("Read a compact whole-universe market scan without changing state."),
		},
		{
			Name:        "inspect_symbols",
			Description: "Inspect symbols.",
			InputSchema: objectSchema(map[string]any{
				"run_id":   stringSchema("Run UUID."),
				"symbols":  arraySchema(stringSchema("Ticker symbol."), "1-8 symbols."),
				"lookback": integerSchema("Bars per symbol.", 1),
			}, []string{"run_id", "symbols"}),
			Annotations: readOnlyToolAnnotations("Read detailed history for a small symbol subset without changing state."),
		},
		{
			Name:        "submit_turn",
			Description: "Queue trades and advance exactly one bar.",
			InputSchema: objectSchema(map[string]any{
				"run_id":     stringSchema("Run UUID."),
				"trades":     arraySchema(tradeSchema(), "Orders. Empty array means no trade."),
				"step_count": integerSchema("Bars to advance. Use 1 for normal trading. Values above 1 are rejected in MCP to prevent accidental bar-skipping.", 1),
			}, []string{"run_id", "trades"}),
			Annotations: mutatingToolAnnotations("Queues trades and advances the run one bar."),
		},
		{
			Name:        "submit_decision",
			Description: "Submit a hold or trade decision and advance exactly one bar.",
			InputSchema: objectSchema(map[string]any{
				"run_id":     stringSchema("Run UUID."),
				"action":     map[string]any{"type": "string", "enum": []string{"hold", "trade"}, "description": "hold or trade."},
				"rationale":  stringSchema("Short reason."),
				"orders":     arraySchema(tradeSchema(), "Orders."),
				"step_count": integerSchema("Bars to advance. Use 1 for normal trading. Values above 1 are rejected in MCP to prevent accidental bar-skipping.", 1),
			}, []string{"run_id", "action", "orders"}),
			Annotations: mutatingToolAnnotations("Queues the chosen decision and advances the run one bar."),
		},
		{
			Name:        "step_run",
			Description: "Advance a run by one bar with no new trades.",
			InputSchema: objectSchema(map[string]any{
				"run_id": stringSchema("Run UUID."),
				"count":  integerSchema("Bars to advance. Use 1 for the normal loop. Values above 1 are rejected in MCP to prevent accidental bar-skipping.", 1),
			}, []string{"run_id"}),
			Annotations: mutatingToolAnnotations("Advances the run one bar without queuing new trades."),
		},
		{
			Name:        "get_results",
			Description: "Get results.",
			InputSchema: objectSchema(map[string]any{
				"run_id": stringSchema("Run UUID."),
			}, []string{"run_id"}),
			Annotations: readOnlyToolAnnotations("Read the completed run's final metrics."),
		},
		{
			Name:        "publish_run",
			Description: "Publish run.",
			InputSchema: objectSchema(map[string]any{
				"run_id":  stringSchema("Run UUID."),
				"confirm": map[string]any{"type": "boolean", "description": "Must be true to publish."},
			}, []string{"run_id", "confirm"}),
			Annotations: mutatingToolAnnotations("Publishes the completed run to the public leaderboard."),
		},
	}
}

func resources() []map[string]any {
	return []map[string]any{
		{
			"uri":         "bottrade://agent-guide",
			"name":        "BotTrade Agent Guide",
			"description": "Run loop.",
			"mimeType":    "text/markdown",
		},
		{
			"uri":         "bottrade://scenarios",
			"name":        "BotTrade Scenarios",
			"description": "Scenario catalog.",
			"mimeType":    "application/json",
		},
	}
}

func prompts() []map[string]any {
	return []map[string]any{
		{
			"name":        "trade_bottrade_scenario",
			"description": "Run a scenario.",
			"arguments": []map[string]any{
				{
					"name":        "scenario_slug",
					"description": "Scenario slug.",
					"required":    false,
				},
			},
		},
	}
}

func (s *MCPServer) readResource(ctx context.Context, params json.RawMessage) (any, error) {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	switch p.URI {
	case "bottrade://agent-guide":
		return map[string]any{
			"contents": []map[string]any{{
				"uri":      p.URI,
				"mimeType": "text/markdown",
				"text":     agentGuide,
			}},
		}, nil
	case "bottrade://scenarios":
		scenarios, err := s.client.ListScenarios(ctx)
		if err != nil {
			return nil, err
		}
		b, err := json.MarshalIndent(map[string]any{"scenarios": scenarios}, "", "  ")
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"contents": []map[string]any{{
				"uri":      p.URI,
				"mimeType": "application/json",
				"text":     string(b),
			}},
		}, nil
	default:
		return nil, fmt.Errorf("unknown resource %q", p.URI)
	}
}

func (s *MCPServer) getPrompt(params json.RawMessage) (any, error) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	if p.Name != "trade_bottrade_scenario" {
		return nil, fmt.Errorf("unknown prompt %q", p.Name)
	}
	scenario, _ := p.Arguments["scenario_slug"].(string)
	text := "Run one BotTrade scenario. Use list_scenarios, get_scenario, start_run, then loop scan_market, inspect_symbols, submit_decision until done or liquidated. Continue autonomously without asking the user to confirm each loop iteration. Advance one bar at a time; do not batch-step or skip bars unless the user explicitly asks. Then get_results. Do not publish unless asked. If auth is required, use connect_bottrade."
	if scenario != "" {
		text += "\n\nRequested scenario: " + scenario
	}
	return map[string]any{
		"description": "Run a BotTrade scenario.",
		"messages": []map[string]any{{
			"role": "user",
			"content": map[string]any{
				"type": "text",
				"text": text,
			},
		}},
	}, nil
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringSchema(description string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
	}
}

func integerSchema(description string, minimum int) map[string]any {
	return map[string]any{
		"type":        "integer",
		"description": description,
		"minimum":     minimum,
	}
}

func readOnlyToolAnnotations(title string) map[string]any {
	return map[string]any{
		"title":         title,
		"readOnlyHint":  true,
		"openWorldHint": true,
	}
}

func mutatingToolAnnotations(title string) map[string]any {
	return map[string]any{
		"title":           title,
		"readOnlyHint":    false,
		"destructiveHint": false,
		"openWorldHint":   true,
	}
}

func arraySchema(items map[string]any, description string) map[string]any {
	return map[string]any{
		"type":        "array",
		"description": description,
		"items":       items,
	}
}

func tradeSchema() map[string]any {
	return objectSchema(map[string]any{
		"symbol":    stringSchema("Ticker."),
		"side":      map[string]any{"type": "string", "enum": []string{"buy", "sell", "short", "cover"}},
		"quantity":  integerSchema("Shares.", 1),
		"reasoning": stringSchema("Reason."),
	}, []string{"symbol", "side", "quantity"})
}

const agentGuide = `# BotTrade MCP Agent Guide

Goal: complete one historical market-simulator run.

1. Use list_scenarios and choose a ready scenario.
2. If a protected action requires auth, use connect_bottrade and complete BotTrade sign-in.
3. Use start_run with the scenario slug.
4. Repeat until submit_decision or step_run returns done=true or liquidated=true:
   - Use scan_market to compactly scan the universe.
   - Use inspect_symbols on current positions plus a few interesting symbols.
   - Use submit_decision with action=hold or action=trade.
5. Use get_results after the run ends.
6. Use publish_run only when the user explicitly wants a public leaderboard entry.

Autonomy rules:
- Continue the loop autonomously. Do not ask the user for confirmation between normal scan, inspect, decide, trade, and step calls.
- Only stop to ask the user for help if authentication is required, the user explicitly wants to intervene, or the API returns an unrecoverable error.
- Advance one bar at a time during the normal trading loop. Do not batch-step or skip many bars unless the user explicitly asks for that behavior.

Token budget:
- Prefer scan_market over get_market for whole-universe observation.
- inspect_symbols is capped at 8 symbols and 120 bars per symbol.
- Raw get_market rejects large requests and points back to the scan/inspect flow.

Trade sides:
- buy: open or increase a long position.
- sell: reduce a long position.
- short: open or increase a short position when the scenario allows shorting.
- cover: reduce a short position.

Orders are queued first and fill when the simulator steps to the next bar.
`
