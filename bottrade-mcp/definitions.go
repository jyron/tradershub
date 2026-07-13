package main

import (
	"context"
	"encoding/json"
	"fmt"
)

func tools() []tool {
	return []tool{
		{
			Name:        "auth_status",
			Title:       "Check BotTrade authentication",
			Description: "Return the current MCP session's BotTrade authentication state and required next action. This is a read-only status check; OAuth starts through connect_bottrade.",
			InputSchema: objectSchema(map[string]any{}, nil),
			Annotations: readOnlyToolAnnotations("Check whether the MCP session is authenticated."),
		},
		{
			Name:        "connect_bottrade",
			Title:       "Connect a BotTrade account",
			Description: "Start or resume BotTrade OAuth for the current MCP session and return a login URL when interaction is required. wait_seconds optionally polls that sign-in flow for completion; the tool creates no benchmark runs or orders.",
			InputSchema: objectSchema(map[string]any{
				"wait_seconds": integerSchema("Optional seconds to poll for OAuth completion before returning; values above 120 are capped at 120. Use 0 to return the current status immediately.", 0),
			}, nil),
			Annotations: mutatingToolAnnotations("Starts the BotTrade OAuth sign-in flow when auth is required."),
		},
		{
			Name:        "list_scenarios",
			Title:       "List benchmark scenarios",
			Description: "List the available BotTrade benchmark scenarios and their identifiers. This public, read-only catalog supplies the slugs accepted by get_scenario and start_run.",
			InputSchema: objectSchema(map[string]any{}, nil),
			Annotations: readOnlyToolAnnotations("Read the available scenario catalog."),
		},
		{
			Name:        "get_scenario",
			Title:       "Get benchmark scenario details",
			Description: "Return configuration and market-universe metadata for one scenario slug or UUID. This public, read-only lookup expands an entry from list_scenarios before start_run.",
			InputSchema: objectSchema(map[string]any{
				"id_or_slug": stringSchema("Exact scenario slug or UUID returned by list_scenarios."),
			}, []string{"id_or_slug"}),
			Annotations: readOnlyToolAnnotations("Read scenario metadata before starting a run."),
		},
		{
			Name:        "start_run",
			Title:       "Start a benchmark run",
			Description: "Create a new private run for one scenario and optionally record agent provenance. Every successful call creates a distinct authenticated run at the scenario's initial market time; publication remains a separate action.",
			InputSchema: objectSchema(map[string]any{
				"scenario_slug": stringSchema("Exact scenario slug returned by list_scenarios."),
				"bot_name":      stringSchema("Optional display name for the bot, strategy, or experiment associated with this run."),
				"agent_info": describedObjectSchema("Optional structured provenance for the agent executing the run.", map[string]any{
					"name":            stringSchema("Agent name recorded with the run."),
					"framework":       stringSchema("Agent framework or orchestration system."),
					"model":           stringSchema("Model identifier used for the run."),
					"version":         stringSchema("Agent or strategy version."),
					"source_url":      stringSchema("Public or private source repository URL."),
					"source_revision": stringSchema("Commit hash or other immutable source revision."),
				}, []string{"name"}),
			}, []string{"scenario_slug"}),
			Annotations: mutatingToolAnnotations("Creates a new run in the user's BotTrade account."),
		},
		{
			Name:        "get_run",
			Title:       "Get current run state",
			Description: "Return an authenticated run's current status, simulator time, portfolio, positions, and queued orders without advancing it. This is the read-only state snapshot for resuming or monitoring an in-progress run.",
			InputSchema: objectSchema(map[string]any{
				"run_id": stringSchema("Run UUID returned by start_run."),
			}, []string{"run_id"}),
			Annotations: readOnlyToolAnnotations("Read the current portfolio, queued orders, and run status."),
		},
		{
			Name:        "get_market",
			Title:       "Get raw market bars",
			Description: "Return raw bars at the current simulator time for an authenticated run, optionally limited to selected symbols. This read-only advanced-data path enforces a 500-row budget; the compact workflow is scan_market followed by inspect_symbols.",
			InputSchema: objectSchema(map[string]any{
				"run_id":   stringSchema("Run UUID returned by start_run."),
				"symbols":  arraySchema(stringSchema("Exact ticker symbol from the scenario universe."), "Optional symbol subset. Omit only when lookback is 1; larger whole-universe requests are rejected."),
				"lookback": integerSchema("Number of bars to return per symbol; the total request must remain within the server's 500-row budget.", 1),
			}, []string{"run_id"}),
			Annotations: readOnlyToolAnnotations("Read market bars for the current run without changing state."),
		},
		{
			Name:        "scan_market",
			Title:       "Scan the full market compactly",
			Description: "Return a token-bounded snapshot of every symbol at the current simulator time, including recent movement, position exposure, top movers, and suggested symbols. This authenticated, read-only scan is the first market read in each trading step.",
			InputSchema: objectSchema(map[string]any{
				"run_id": stringSchema("Run UUID returned by start_run."),
			}, []string{"run_id"}),
			Annotations: readOnlyToolAnnotations("Read a token-bounded whole-universe market scan without changing state."),
		},
		{
			Name:        "inspect_symbols",
			Title:       "Inspect selected symbols",
			Description: "Return detailed recent bars for 1–8 symbols at the current simulator time. This authenticated, read-only inspection follows scan_market and supplies focused data for submit_decision.",
			InputSchema: objectSchema(map[string]any{
				"run_id":   stringSchema("Run UUID returned by start_run."),
				"symbols":  arraySchema(stringSchema("Exact ticker symbol from the scenario universe."), "Between 1 and 8 symbols, normally selected from scan_market.suggested_inspection."),
				"lookback": integerSchema("Bars per symbol; defaults to 30 when omitted and is capped at 120.", 1),
			}, []string{"run_id", "symbols"}),
			Annotations: readOnlyToolAnnotations("Read detailed history for a small symbol subset without changing state."),
		},
		{
			Name:        "submit_turn",
			Title:       "Submit a low-level trading turn",
			Description: "Queue zero or more raw orders for an authenticated run and advance exactly one bar. This is the low-level turn primitive; submit_decision adds an explicit action, rationale, validation, and workflow guidance.",
			InputSchema: objectSchema(map[string]any{
				"run_id":     stringSchema("Run UUID returned by start_run."),
				"trades":     arraySchema(tradeSchema(), "Orders to queue before the next bar; an empty array means advance without placing an order."),
				"step_count": integerSchema("Bars to advance. Omit or use 1; values above 1 are rejected to prevent accidental bar skipping.", 1),
			}, []string{"run_id", "trades"}),
			Annotations: mutatingToolAnnotations("Queues trades and advances the run one bar."),
		},
		{
			Name:        "submit_decision",
			Title:       "Submit a trading decision",
			Description: "Record an explicit hold or trade decision, queue any orders, and advance an authenticated run exactly one bar. This is the normal action after scan_market and inspect_symbols; queued orders fill on the next bar.",
			InputSchema: objectSchema(map[string]any{
				"run_id":     stringSchema("Run UUID returned by start_run."),
				"action":     map[string]any{"type": "string", "enum": []string{"hold", "trade"}, "description": "Decision type: hold requires no orders; trade requires at least one order."},
				"rationale":  stringSchema("Optional short reason recorded with the decision."),
				"orders":     arraySchema(tradeSchema(), "Orders to queue when action is trade; use an empty array when action is hold."),
				"step_count": integerSchema("Bars to advance. Omit or use 1; values above 1 are rejected to prevent accidental bar skipping.", 1),
			}, []string{"run_id", "action", "orders"}),
			Annotations: mutatingToolAnnotations("Queues the chosen decision and advances the run one bar."),
		},
		{
			Name:        "step_run",
			Title:       "Advance one bar without orders",
			Description: "Advance an authenticated run exactly one bar without queuing orders or recording a decision rationale. This is the single-bar no-order primitive used beneath the bounded waiting tools.",
			InputSchema: objectSchema(map[string]any{
				"run_id": stringSchema("Run UUID returned by start_run."),
				"count":  integerSchema("Bars to advance. Omit or use 1; values above 1 are rejected to prevent accidental bar skipping.", 1),
			}, []string{"run_id"}),
			Annotations: mutatingToolAnnotations("Advances the run one bar without queuing new trades."),
		},
		{
			Name:        "advance_until_next_session",
			Title:       "Advance to the next market session",
			Description: "Repeatedly advance an authenticated run without new orders until the trading date changes, the run ends, or max_bars is reached. This bounded helper compresses session-boundary waiting while preserving one-bar simulation steps.",
			InputSchema: objectSchema(map[string]any{
				"run_id":   stringSchema("Run UUID returned by start_run."),
				"max_bars": integerSchema("Maximum one-bar advances before stopping; defaults to 32 and acts as a safety cap.", 1),
			}, []string{"run_id"}),
			Annotations: mutatingToolAnnotations("Safely compresses repeated hold steps across an overnight/session boundary."),
		},
		{
			Name:        "hold_until_end",
			Title:       "Hold without orders until completion",
			Description: "Repeatedly advance an authenticated run without adding orders until it completes, liquidates, or reaches max_bars. This bounded helper handles terminal waiting; require_flat can enforce cash-only execution.",
			InputSchema: objectSchema(map[string]any{
				"run_id":       stringSchema("Run UUID returned by start_run."),
				"max_bars":     integerSchema("Maximum one-bar advances before stopping; defaults to 256 and acts as a safety cap.", 1),
				"require_flat": map[string]any{"type": "boolean", "description": "When true, reject the call unless the run has no open positions; use this guard for cash-only waiting."},
			}, []string{"run_id"}),
			Annotations: mutatingToolAnnotations("Safely compresses repeated hold steps without adding strategy advice or trades."),
		},
		{
			Name:        "liquidate_and_finish",
			Title:       "Liquidate positions and finish",
			Description: "Create sell/cover orders that flatten every current position, advance to fill them, then hold without new orders until completion or max_bars. The tool executes an existing exit decision and does not select a strategy.",
			InputSchema: objectSchema(map[string]any{
				"run_id":    stringSchema("Run UUID returned by start_run."),
				"rationale": stringSchema("Optional short reason copied onto the generated exit orders."),
				"max_bars":  integerSchema("Maximum post-liquidation one-bar advances before stopping; defaults to 256.", 1),
			}, []string{"run_id"}),
			Annotations: mutatingToolAnnotations("Queues only sell/cover orders needed to flatten existing positions, then holds."),
		},
		{
			Name:        "run_sandbox_smoke_test",
			Title:       "Verify the sandbox workflow",
			Description: "Create an authenticated sandbox run, scan its market once, submit one hold decision, and return a compact end-to-end verification summary. Each call creates a new private, unpublished run for integration testing.",
			InputSchema: objectSchema(map[string]any{
				"scenario_slug": stringSchema("Sandbox scenario slug; defaults to sandbox-nov-2024 when omitted."),
				"bot_name":      stringSchema("Optional display name recorded on the sandbox run."),
			}, nil),
			Annotations: mutatingToolAnnotations("Verifies auth, run creation, scan, and hold decision without publishing or giving strategy advice."),
		},
		{
			Name:        "get_results",
			Title:       "Get final run results",
			Description: "Return final performance metrics, benchmark comparison, ending portfolio, and compact trade attribution for an authenticated completed run. This read-only result summary keeps publication separate; get_trades supplies the full execution ledger.",
			InputSchema: objectSchema(map[string]any{
				"run_id": stringSchema("Completed run UUID returned by start_run."),
			}, []string{"run_id"}),
			Annotations: readOnlyToolAnnotations("Read final metrics plus compact trade attribution without publishing."),
		},
		{
			Name:        "get_trades",
			Title:       "List filled run trades",
			Description: "Return every immutable filled-trade record for an authenticated run. This read-only execution ledger excludes unfilled queued orders; get_results supplies aggregate performance and compact attribution.",
			InputSchema: objectSchema(map[string]any{
				"run_id": stringSchema("Run UUID returned by start_run."),
			}, []string{"run_id"}),
			Annotations: readOnlyToolAnnotations("Read immutable filled-trade records without changing state."),
		},
		{
			Name:        "publish_run",
			Title:       "Publish a run to the leaderboard",
			Description: "Make an authenticated completed run publicly accessible and submit its metrics to the BotTrade leaderboard. This changes the run's visibility and requires confirm=true; private run completion remains independent of publication.",
			InputSchema: objectSchema(map[string]any{
				"run_id":  stringSchema("Completed run UUID returned by start_run."),
				"confirm": map[string]any{"type": "boolean", "description": "Explicit publication confirmation; the server rejects the call unless this is true."},
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
	text := "Run one BotTrade scenario. Use auth_status, list_scenarios, get_scenario, start_run, then loop scan_market, inspect_symbols, submit_decision until done or liquidated. Continue autonomously without asking the user to confirm each normal loop iteration. Use scan_market for compact whole-universe observation and inspect_symbols only for a capped symbol subset. The MCP server does not provide strategy advice; you must choose any trades yourself. Advance one bar at a time during trading decisions. For strategy-neutral waiting, you may use advance_until_next_session or hold_until_end. Then get_results. Do not publish unless asked. If auth is required, use connect_bottrade."
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

func describedObjectSchema(description string, properties map[string]any, required []string) map[string]any {
	schema := objectSchema(properties, required)
	schema["description"] = description
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

func numberSchema(description string, exclusiveMinimum float64) map[string]any {
	return map[string]any{
		"type":             "number",
		"description":      description,
		"exclusiveMinimum": exclusiveMinimum,
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
		"symbol":    stringSchema("Exact ticker symbol from the scenario universe."),
		"side":      map[string]any{"type": "string", "enum": []string{"buy", "sell", "short", "cover"}, "description": "Order direction: buy opens/increases a long, sell reduces a long, short opens/increases a short, and cover reduces a short."},
		"quantity":  numberSchema("Order size, positive. Fractional allowed for crypto pairs (e.g. 0.25 for BTC/USD); equities are typically whole.", 0),
		"reasoning": stringSchema("Optional short reason recorded with this order."),
	}, []string{"symbol", "side", "quantity"})
}

const agentGuide = `# BotTrade MCP Agent Guide

Goal: complete one historical market-simulator run.

1. Use auth_status to check whether the current session is connected.
2. Use list_scenarios and choose a ready scenario.
3. If a protected action requires auth, use connect_bottrade and complete BotTrade sign-in.
4. Use start_run with the scenario slug.
5. Repeat until submit_decision or step_run returns done=true or liquidated=true:
   - Use scan_market to compactly scan the universe.
   - Use inspect_symbols on current positions plus a few interesting symbols.
   - Use submit_decision with action=hold or action=trade.
6. Use get_results after the run ends.
7. Use publish_run only when the user explicitly wants a public leaderboard entry.

Boundary:
- The MCP server is workflow infrastructure, not a strategy engine.
- scan_market may identify high-movement symbols and current-position symbols for inspection only.
- The server does not recommend trades, portfolio allocations, entries, exits, or directional views.

Autonomy rules:
- Continue the loop autonomously. Do not ask the user for confirmation between normal scan, inspect, decide, trade, and step calls.
- Only stop to ask the user for help if authentication is required, the user explicitly wants to intervene, or the API returns an unrecoverable error.
- Advance one bar at a time during the normal trading loop. Do not batch-step or skip many bars unless the user explicitly asks for that behavior.
- For strategy-neutral waiting, use advance_until_next_session or hold_until_end instead of repeatedly asking the user to confirm hold steps.
- Use liquidate_and_finish only when the agent or user has already decided to flatten; it creates sell/cover orders from existing positions and does not choose a strategy.

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
