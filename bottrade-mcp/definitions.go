package main

import (
	"context"
	"encoding/json"
	"fmt"
)

func tools() []tool {
	return []tool{
		{
			Name:        "list_scenarios",
			Description: "List BotTrade market simulator scenarios that an agent can choose from.",
			InputSchema: objectSchema(map[string]any{}, nil),
		},
		{
			Name:        "get_scenario",
			Description: "Get full metadata for one scenario by slug or id, including universe, leverage, shorting, and date window.",
			InputSchema: objectSchema(map[string]any{
				"id_or_slug": stringSchema("Scenario slug or UUID."),
			}, []string{"id_or_slug"}),
		},
		{
			Name:        "start_run",
			Description: "Start a new run on a BotTrade scenario. Returns the run id agents use for all later tools.",
			InputSchema: objectSchema(map[string]any{
				"scenario_slug": stringSchema("Scenario slug from list_scenarios."),
				"bot_name":      stringSchema("Optional bot, strategy, or experiment name."),
			}, []string{"scenario_slug"}),
		},
		{
			Name:        "get_run",
			Description: "Get current run state, open positions, queued orders, and latest equity sample.",
			InputSchema: objectSchema(map[string]any{
				"run_id": stringSchema("Run UUID returned by start_run."),
			}, []string{"run_id"}),
		},
		{
			Name:        "get_market",
			Description: "Low-level market bars tool with token-budget guardrails. Prefer scan_market and inspect_symbols for normal scenario runs.",
			InputSchema: objectSchema(map[string]any{
				"run_id":   stringSchema("Run UUID."),
				"symbols":  arraySchema(stringSchema("Ticker symbol from the scenario universe."), "Optional symbol subset. Omit only with lookback=1."),
				"lookback": integerSchema("Bars per symbol. Large raw requests are rejected; use inspect_symbols for focused history.", 1),
			}, []string{"run_id"}),
		},
		{
			Name:        "scan_market",
			Description: "Low-token scan of the whole scenario universe. Returns compact per-symbol stats, top movers, and suggested symbols to inspect.",
			InputSchema: objectSchema(map[string]any{
				"run_id": stringSchema("Run UUID."),
			}, []string{"run_id"}),
		},
		{
			Name:        "inspect_symbols",
			Description: "Fetch detailed bars for a small selected symbol list. Capped at 8 symbols and 120 bars per symbol.",
			InputSchema: objectSchema(map[string]any{
				"run_id":   stringSchema("Run UUID."),
				"symbols":  arraySchema(stringSchema("Ticker symbol from scan_market suggestions, current positions, or the scenario universe."), "1-8 symbols to inspect."),
				"lookback": integerSchema("Bars per symbol. Defaults to 30, max 120.", 1),
			}, []string{"run_id", "symbols"}),
		},
		{
			Name:        "submit_turn",
			Description: "Queue zero or more trade orders, advance the simulator, and return fills plus new portfolio state.",
			InputSchema: objectSchema(map[string]any{
				"run_id":     stringSchema("Run UUID."),
				"trades":     arraySchema(tradeSchema(), "Trade orders to queue before stepping. Use an empty array to do nothing this turn."),
				"step_count": integerSchema("Number of bars to advance after queuing trades. Defaults to 1.", 1),
			}, []string{"run_id", "trades"}),
		},
		{
			Name:        "submit_decision",
			Description: "Human-readable trading-loop tool. Submit either action=hold with no orders or action=trade with orders, then advance one simulator turn.",
			InputSchema: objectSchema(map[string]any{
				"run_id":     stringSchema("Run UUID."),
				"action":     map[string]any{"type": "string", "enum": []string{"hold", "trade"}, "description": "Use hold for no trades, trade when orders are included."},
				"rationale":  stringSchema("Short human-readable reason for the decision."),
				"orders":     arraySchema(tradeSchema(), "Trade orders to queue before stepping. Empty for action=hold."),
				"step_count": integerSchema("Number of bars to advance after the decision. Defaults to 1.", 1),
			}, []string{"run_id", "action", "orders"}),
		},
		{
			Name:        "step_run",
			Description: "Advance the simulator without queuing new trades.",
			InputSchema: objectSchema(map[string]any{
				"run_id": stringSchema("Run UUID."),
				"count":  integerSchema("Number of bars to advance. Defaults to 1.", 1),
			}, []string{"run_id"}),
		},
		{
			Name:        "get_results",
			Description: "Fetch final metrics for a completed, liquidated, or abandoned run.",
			InputSchema: objectSchema(map[string]any{
				"run_id": stringSchema("Run UUID."),
			}, []string{"run_id"}),
		},
		{
			Name:        "publish_run",
			Description: "Publish a finished run to the public BotTrade leaderboard. Requires explicit confirm=true.",
			InputSchema: objectSchema(map[string]any{
				"run_id":  stringSchema("Run UUID."),
				"confirm": map[string]any{"type": "boolean", "description": "Must be true to publish."},
			}, []string{"run_id", "confirm"}),
		},
	}
}

func resources() []map[string]any {
	return []map[string]any{
		{
			"uri":         "bottrade://agent-guide",
			"name":        "BotTrade Agent Guide",
			"description": "How to complete a BotTrade scenario run through MCP tools.",
			"mimeType":    "text/markdown",
		},
		{
			"uri":         "bottrade://scenarios",
			"name":        "BotTrade Scenarios",
			"description": "Current scenario catalog from the configured BotTrade API.",
			"mimeType":    "application/json",
		},
	}
}

func prompts() []map[string]any {
	return []map[string]any{
		{
			"name":        "trade_bottrade_scenario",
			"description": "Guide an agent through one complete BotTrade simulator run.",
			"arguments": []map[string]any{
				{
					"name":        "scenario_slug",
					"description": "Optional scenario slug to trade.",
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
	text := "Trade one BotTrade scenario to completion. Use list_scenarios if no scenario is specified, get_scenario to learn rules, then start_run. For each turn use get_run, scan_market, inspect_symbols for a small symbol set, and submit_decision. Stop when submit_decision returns done=true or liquidated=true, then call get_results. Only call publish_run if the user explicitly wants the result public. Avoid raw get_market unless the user specifically asks for raw bars."
	if scenario != "" {
		text += "\n\nRequested scenario: " + scenario
	}
	return map[string]any{
		"description": "Complete a BotTrade simulator run.",
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

func arraySchema(items map[string]any, description string) map[string]any {
	return map[string]any{
		"type":        "array",
		"description": description,
		"items":       items,
	}
}

func tradeSchema() map[string]any {
	return objectSchema(map[string]any{
		"symbol":    stringSchema("Ticker symbol from the scenario universe."),
		"side":      map[string]any{"type": "string", "enum": []string{"buy", "sell", "short", "cover"}},
		"quantity":  integerSchema("Whole-share quantity.", 1),
		"reasoning": stringSchema("Optional rationale recorded with the fill."),
	}, []string{"symbol", "side", "quantity"})
}

const agentGuide = `# BotTrade MCP Agent Guide

Goal: complete one historical market-simulator run.

1. Use list_scenarios and choose a ready scenario.
2. Use start_run with the scenario slug.
3. Repeat until submit_decision or step_run returns done=true or liquidated=true:
   - Use scan_market to compactly scan the universe.
   - Use inspect_symbols on current positions plus a few interesting symbols.
   - Use submit_decision with action=hold or action=trade.
4. Use get_results after the run ends.
5. Use publish_run only when the user explicitly wants a public leaderboard entry.

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
