package services

import (
	"bottrade/models"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ScenarioProvisioner creates and freezes scenarios. "Freezing" copies the
// relevant slice of `market.bars` into `market.scenario_bars` with the
// per-symbol slippage_bps baked in. Once frozen, runs against this
// (scenario_id, version) read only from scenario_bars and are reproducible
// forever — the source `bars` table can be wiped without affecting them.
type ScenarioProvisioner struct {
	appDB    *sql.DB
	marketDB *sql.DB
}

func NewScenarioProvisioner(appDB, marketDB *sql.DB) *ScenarioProvisioner {
	return &ScenarioProvisioner{appDB: appDB, marketDB: marketDB}
}

// CreateScenarioInput captures the fields a caller (CLI / admin handler)
// supplies. Status starts as 'draft'; FreezeScenario flips it to 'ready'.
type CreateScenarioInput struct {
	Slug            string
	Name            string
	Description     string
	BarResolution   string // default "1Hour"
	StartTs         time.Time
	EndTs           time.Time
	StartingCash    float64 // default 100000
	LeverageCap     float64 // 1 | 2 | 4 | 10
	ShortEnabled    bool
	Universe        []string
	SlippageBps     map[string]int // optional overrides; absent → DefaultSlippageBps
	BenchmarkSymbol string         // default "SPY"
}

// CreateScenario inserts a draft scenario row. Bars are not yet copied.
// Call FreezeScenario(id) afterward to produce scenario_bars and mark ready.
func (p *ScenarioProvisioner) CreateScenario(in CreateScenarioInput) (*models.Scenario, error) {
	if in.Slug == "" || in.Name == "" {
		return nil, fmt.Errorf("slug and name are required")
	}
	if in.EndTs.Before(in.StartTs) || in.EndTs.Equal(in.StartTs) {
		return nil, fmt.Errorf("end_ts must be after start_ts")
	}
	if len(in.Universe) == 0 {
		return nil, fmt.Errorf("universe must contain at least one symbol")
	}
	switch in.LeverageCap {
	case 1, 2, 4, 10:
	default:
		return nil, fmt.Errorf("leverage_cap must be one of 1, 2, 4, 10 (got %v)", in.LeverageCap)
	}
	if in.BarResolution == "" {
		in.BarResolution = "1Hour"
	}
	if in.StartingCash == 0 {
		in.StartingCash = 100000
	}
	if in.BenchmarkSymbol == "" {
		in.BenchmarkSymbol = "SPY"
	}

	// Fill in default slippage for symbols not overridden.
	slippage := map[string]int{}
	for _, sym := range in.Universe {
		if v, ok := in.SlippageBps[sym]; ok {
			slippage[sym] = v
		} else {
			slippage[sym] = DefaultSlippageBps(sym)
		}
	}

	universeJSON, _ := json.Marshal(in.Universe)
	slippageJSON, _ := json.Marshal(slippage)

	id := uuid.NewString()
	_, err := p.appDB.Exec(`
		INSERT INTO scenarios (
			id, slug, name, description, bar_resolution,
			start_ts, end_ts, starting_cash, leverage_cap, short_enabled,
			universe_json, slippage_json, benchmark_symbol, status, current_version
		) VALUES (
			?1, ?2, ?3, ?4, ?5,
			?6, ?7, ?8, ?9, ?10,
			?11, ?12, ?13, 'draft', 1
		)
	`,
		id, in.Slug, in.Name, in.Description, in.BarResolution,
		in.StartTs.UTC().Format(time.RFC3339), in.EndTs.UTC().Format(time.RFC3339),
		in.StartingCash, in.LeverageCap, boolToInt(in.ShortEnabled),
		string(universeJSON), string(slippageJSON), in.BenchmarkSymbol,
	)
	if err != nil {
		return nil, fmt.Errorf("insert scenario: %w", err)
	}

	return p.LoadScenario(id)
}

// LoadScenario reads a scenario row + its parsed universe/slippage.
func (p *ScenarioProvisioner) LoadScenario(id string) (*models.Scenario, error) {
	row := p.appDB.QueryRow(`
		SELECT id, slug, name, COALESCE(description,''), bar_resolution,
		       start_ts, end_ts, starting_cash, leverage_cap, short_enabled,
		       universe_json, slippage_json, benchmark_symbol, status, current_version, created_at
		  FROM scenarios WHERE id = ?1
	`, id)

	var s models.Scenario
	var startStr, endStr, createdStr, universeStr, slippageStr string
	var shortEnabled int
	if err := row.Scan(
		&s.ID, &s.Slug, &s.Name, &s.Description, &s.BarResolution,
		&startStr, &endStr, &s.StartingCash, &s.LeverageCap, &shortEnabled,
		&universeStr, &slippageStr, &s.BenchmarkSymbol, &s.Status, &s.CurrentVersion, &createdStr,
	); err != nil {
		return nil, err
	}
	s.StartTs, _ = time.Parse(time.RFC3339, startStr)
	s.EndTs, _ = time.Parse(time.RFC3339, endStr)
	s.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
	s.ShortEnabled = shortEnabled != 0
	s.Universe, _ = models.UnmarshalUniverse(universeStr)
	s.SlippageBps, _ = models.UnmarshalSlippage(slippageStr)
	return &s, nil
}

// FreezeScenario copies bars from market.bars into market.scenario_bars
// for (scenario_id, current_version), baking in the per-symbol slippage_bps.
// Idempotent against the (scenario_id, version) key — re-running for the
// same version is a no-op for existing rows.
//
// Bumps the scenario to status='ready' and records the freeze in
// scenario_versions.
func (p *ScenarioProvisioner) FreezeScenario(scenarioID string) (int, error) {
	s, err := p.LoadScenario(scenarioID)
	if err != nil {
		return 0, fmt.Errorf("load scenario: %w", err)
	}
	if s.Status == "ready" {
		log.Printf("scenario %s already ready (version %d) — re-freeze will upsert", s.ID, s.CurrentVersion)
	}

	// Mark provisioning so a concurrent freeze can be detected (cheap;
	// no proper locking — admin tool, one caller).
	if _, err := p.appDB.Exec(`UPDATE scenarios SET status='provisioning' WHERE id = ?1`, scenarioID); err != nil {
		return 0, fmt.Errorf("mark provisioning: %w", err)
	}

	tx, err := p.marketDB.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin market tx: %w", err)
	}
	defer tx.Rollback()

	startStr := s.StartTs.UTC().Format(time.RFC3339)
	endStr := s.EndTs.UTC().Format(time.RFC3339)
	totalBars := 0

	// freezeBatchSize keeps a single multi-VALUES INSERT well under SQLite's
	// SQLITE_MAX_VARIABLE_NUMBER (10 cols × 50 = 500, under the 999 default).
	// Without batching this loop was one network round-trip per row to remote
	// Turso — ~3s of throughput per minute. Batched it's ~50x faster.
	const freezeBatchSize = 50

	type barRow struct {
		ts         string
		o, h, l, c float64
		v          float64
	}

	for _, symbol := range s.Universe {
		slip := s.SlippageBps[symbol]
		rows, err := p.marketDB.Query(`
			SELECT ts, open, high, low, close, volume
			  FROM bars
			 WHERE symbol = ?1 AND ts >= ?2 AND ts < ?3
			 ORDER BY ts ASC
		`, symbol, startStr, endStr)
		if err != nil {
			return 0, fmt.Errorf("query bars %s: %w", symbol, err)
		}

		batch := make([]barRow, 0, freezeBatchSize)
		flush := func() error {
			if len(batch) == 0 {
				return nil
			}
			placeholders := make([]string, len(batch))
			args := make([]interface{}, 0, len(batch)*10)
			for i, b := range batch {
				placeholders[i] = "(?,?,?,?,?,?,?,?,?,?)"
				args = append(args,
					scenarioID, s.CurrentVersion, symbol, b.ts,
					b.o, b.h, b.l, b.c, b.v, slip,
				)
			}
			query := `INSERT INTO scenario_bars
				(scenario_id, scenario_version, symbol, ts, open, high, low, close, volume, slippage_bps)
				VALUES ` + strings.Join(placeholders, ",") +
				` ON CONFLICT (scenario_id, scenario_version, symbol, ts) DO UPDATE SET
					open = excluded.open, high = excluded.high, low = excluded.low,
					close = excluded.close, volume = excluded.volume,
					slippage_bps = excluded.slippage_bps`
			if _, err := tx.Exec(query, args...); err != nil {
				return fmt.Errorf("batched insert %s: %w", symbol, err)
			}
			batch = batch[:0]
			return nil
		}

		count := 0
		for rows.Next() {
			var b barRow
			if err := rows.Scan(&b.ts, &b.o, &b.h, &b.l, &b.c, &b.v); err != nil {
				rows.Close()
				return 0, fmt.Errorf("scan bar %s: %w", symbol, err)
			}
			batch = append(batch, b)
			count++
			if len(batch) >= freezeBatchSize {
				if err := flush(); err != nil {
					rows.Close()
					return 0, err
				}
			}
		}
		rows.Close()
		if err := flush(); err != nil {
			return 0, err
		}

		if count == 0 {
			log.Printf("  ⚠️  %s: no bars in [%s, %s) — symbol will be unavailable in this scenario",
				symbol, startStr, endStr)
		} else {
			log.Printf("  %-6s: %d bars frozen", symbol, count)
		}
		totalBars += count
	}

	if totalBars == 0 {
		return 0, fmt.Errorf("no bars found for any universe symbol in [%s, %s); did you backfill yet?",
			strings.TrimSuffix(startStr, "Z"), strings.TrimSuffix(endStr, "Z"))
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit market tx: %w", err)
	}

	// Record the freeze on the app side. Two writes; we accept a brief
	// inconsistency window (scenario_bars exists but app rows haven't
	// updated yet). Idempotent: ON CONFLICT updates the existing row.
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := p.appDB.Exec(`
		INSERT INTO scenario_versions (scenario_id, version, bars_frozen_at, bar_count)
		VALUES (?1, ?2, ?3, ?4)
		ON CONFLICT (scenario_id, version) DO UPDATE SET
			bars_frozen_at = excluded.bars_frozen_at,
			bar_count      = excluded.bar_count
	`, scenarioID, s.CurrentVersion, now, totalBars); err != nil {
		return 0, fmt.Errorf("insert scenario_versions: %w", err)
	}
	if _, err := p.appDB.Exec(`UPDATE scenarios SET status='ready' WHERE id = ?1`, scenarioID); err != nil {
		return 0, fmt.Errorf("mark ready: %w", err)
	}

	return totalBars, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
