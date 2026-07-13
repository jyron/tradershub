package main

import (
	"bottrade/database"
	"database/sql"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

type runAudit struct {
	id, bot, scenario, status, simTime            string
	version, currentVersion, published, completed int
	startingCash                                  float64
	finalEquity, returnPct                        sql.NullFloat64
	resultTrades                                  sql.NullInt64
	equityCount, actualTrades, queuedOrders       int
	lastEquity                                    sql.NullFloat64
	lastEquityTime                                sql.NullString
}

func main() {
	if err := database.Connect(os.Getenv("TURSO_DATABASE_URL"), os.Getenv("TURSO_AUTH_TOKEN")); err != nil {
		panic(err)
	}
	defer database.Close()

	rows, err := database.DB.Query(`
		SELECT r.id, COALESCE(r.bot_name, ''), s.slug, r.status, r.sim_time,
		       r.scenario_version, s.current_version, r.published,
		       CASE WHEN r.completed_at IS NULL OR r.completed_at = '' THEN 0 ELSE 1 END,
		       r.starting_cash, rr.final_equity, rr.return_pct, rr.trade_count,
		       (SELECT COUNT(*) FROM run_equity e WHERE e.run_id = r.id),
		       (SELECT COUNT(*) FROM run_trades t WHERE t.run_id = r.id),
		       (SELECT COUNT(*) FROM run_orders o WHERE o.run_id = r.id),
		       (SELECT e.equity FROM run_equity e WHERE e.run_id = r.id ORDER BY e.sim_time DESC LIMIT 1),
		       (SELECT e.sim_time FROM run_equity e WHERE e.run_id = r.id ORDER BY e.sim_time DESC LIMIT 1)
		  FROM runs r
		  JOIN scenarios s ON s.id = r.scenario_id
		  LEFT JOIN run_results rr ON rr.run_id = r.id
		 ORDER BY r.created_at, r.id
	`)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	statusCounts := map[string]int{}
	validByBot := map[string]int{}
	validScenarios := map[string]map[string]bool{}
	invalidReasons := map[string]int{}
	valid, validPublished, validUnpublished := 0, 0, 0
	invalidTerminal := 0
	total := 0

	for rows.Next() {
		var r runAudit
		if err := rows.Scan(&r.id, &r.bot, &r.scenario, &r.status, &r.simTime,
			&r.version, &r.currentVersion, &r.published, &r.completed,
			&r.startingCash, &r.finalEquity, &r.returnPct, &r.resultTrades,
			&r.equityCount, &r.actualTrades, &r.queuedOrders,
			&r.lastEquity, &r.lastEquityTime); err != nil {
			panic(err)
		}
		total++
		statusCounts[r.status]++
		terminal := r.status == "completed" || r.status == "liquidated"
		if !terminal {
			continue
		}

		reasons := []string{}
		if r.completed == 0 {
			reasons = append(reasons, "missing completed_at")
		}
		if r.version != r.currentVersion {
			reasons = append(reasons, "stale scenario version")
		}
		if !r.finalEquity.Valid || !r.returnPct.Valid || !r.resultTrades.Valid {
			reasons = append(reasons, "missing results")
		}
		if r.equityCount < 2 {
			reasons = append(reasons, "short equity curve")
		}
		if r.queuedOrders != 0 {
			reasons = append(reasons, "queued terminal orders")
		}
		if r.resultTrades.Valid && int(r.resultTrades.Int64) != r.actualTrades {
			reasons = append(reasons, "trade count mismatch")
		}
		if r.lastEquityTime.Valid && r.lastEquityTime.String != r.simTime {
			reasons = append(reasons, "terminal time mismatch")
		}
		if r.finalEquity.Valid && r.lastEquity.Valid && !close(r.finalEquity.Float64, r.lastEquity.Float64) {
			reasons = append(reasons, "final equity mismatch")
		}
		if r.finalEquity.Valid && r.returnPct.Valid && r.startingCash != 0 {
			expected := (r.finalEquity.Float64/r.startingCash - 1) * 100
			if !close(expected, r.returnPct.Float64) {
				reasons = append(reasons, "return mismatch")
			}
		}
		if len(reasons) > 0 {
			invalidTerminal++
			for _, reason := range reasons {
				invalidReasons[reason]++
			}
			continue
		}

		valid++
		if r.published != 0 {
			validPublished++
		} else {
			validUnpublished++
		}
		validByBot[r.bot]++
		if validScenarios[r.bot] == nil {
			validScenarios[r.bot] = map[string]bool{}
		}
		validScenarios[r.bot][r.scenario] = true
	}
	if err := rows.Err(); err != nil {
		panic(err)
	}

	fmt.Printf("all runs: %d\n", total)
	keys := sortedKeys(statusCounts)
	for _, key := range keys {
		fmt.Printf("status %s: %d\n", key, statusCounts[key])
	}
	fmt.Printf("structurally valid terminal runs: %d\n", valid)
	fmt.Printf("  published: %d\n", validPublished)
	fmt.Printf("  unpublished: %d\n", validUnpublished)
	fmt.Printf("invalid terminal runs: %d\n", invalidTerminal)
	for _, reason := range sortedKeys(invalidReasons) {
		fmt.Printf("  %s: %d\n", reason, invalidReasons[reason])
	}

	type coverage struct {
		bot             string
		runs, scenarios int
	}
	coverageRows := []coverage{}
	for bot, count := range validByBot {
		coverageRows = append(coverageRows, coverage{bot, count, len(validScenarios[bot])})
	}
	sort.Slice(coverageRows, func(i, j int) bool {
		if coverageRows[i].runs == coverageRows[j].runs {
			return coverageRows[i].bot < coverageRows[j].bot
		}
		return coverageRows[i].runs > coverageRows[j].runs
	})
	fmt.Println("valid terminal coverage by bot label:")
	for _, row := range coverageRows {
		if strings.TrimSpace(row.bot) == "" {
			row.bot = "(unnamed)"
		}
		fmt.Printf("  %s: %d runs across %d scenarios\n", row.bot, row.runs, row.scenarios)
	}
}

func close(a, b float64) bool {
	return math.Abs(a-b) <= 1e-6 || math.Abs(a-b) <= math.Max(math.Abs(a), math.Abs(b))*1e-9
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
