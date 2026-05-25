package services

import (
	"database/sql"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ScenarioBar is one frozen bar lookup result. It carries the slippage_bps
// the scenario was provisioned with so fills bake in slippage without a
// second lookup.
type ScenarioBar struct {
	Open        float64
	High        float64
	Low         float64
	Close       float64
	Volume      int64
	SlippageBps int
	Ts          time.Time
}

// ScenarioBarCache is an in-process cache of frozen scenario bars. Bars are
// immutable per (scenario_id, version), so we never need to invalidate; the
// first agent request that touches a scenario loads its bars and they stay
// resident for the life of the process.
//
// For the MVP universe (~50 symbols × ~1638 hrs/yr × 8 bytes-ish per number
// × 5 numbers per bar) one year of one scenario fits in well under 5 MB
// per scenario. A handful of active scenarios is negligible memory.
type ScenarioBarCache struct {
	db        *sql.DB
	mu        sync.RWMutex
	scenarios map[string]*loadedScenario
}

type loadedScenario struct {
	Timeline []time.Time                       // sorted, distinct ts of benchmark_symbol
	BySymTs  map[string]map[string]ScenarioBar // bars[symbol][ts.Format(RFC3339)] = bar
}

func keyOf(scenarioID string, version int) string {
	return fmt.Sprintf("%s/v%d", scenarioID, version)
}

func NewScenarioBarCache(db *sql.DB) *ScenarioBarCache {
	return &ScenarioBarCache{
		db:        db,
		scenarios: map[string]*loadedScenario{},
	}
}

// Load reads every scenario_bar for (scenarioID, version) into memory.
// Subsequent calls for the same key are no-ops.
func (c *ScenarioBarCache) Load(scenarioID string, version int) error {
	key := keyOf(scenarioID, version)
	c.mu.RLock()
	if _, ok := c.scenarios[key]; ok {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	rows, err := c.db.Query(`
		SELECT symbol, ts, open, high, low, close, volume, slippage_bps
		  FROM scenario_bars
		 WHERE scenario_id = ?1 AND scenario_version = ?2
		 ORDER BY ts ASC, symbol ASC
	`, scenarioID, version)
	if err != nil {
		return fmt.Errorf("query scenario_bars: %w", err)
	}
	defer rows.Close()

	loaded := &loadedScenario{
		BySymTs: map[string]map[string]ScenarioBar{},
	}
	tsSet := map[string]struct{}{}

	for rows.Next() {
		var symbol, ts string
		var open, high, low, close float64
		var volume int64
		var slippage int
		if err := rows.Scan(&symbol, &ts, &open, &high, &low, &close, &volume, &slippage); err != nil {
			return fmt.Errorf("scan bar: %w", err)
		}
		parsed, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			return fmt.Errorf("parse ts %q: %w", ts, err)
		}
		bar := ScenarioBar{
			Open: open, High: high, Low: low, Close: close,
			Volume: volume, SlippageBps: slippage, Ts: parsed,
		}
		if _, ok := loaded.BySymTs[symbol]; !ok {
			loaded.BySymTs[symbol] = map[string]ScenarioBar{}
		}
		loaded.BySymTs[symbol][ts] = bar
		tsSet[ts] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows: %w", err)
	}

	// Sort timeline.
	tsList := make([]string, 0, len(tsSet))
	for ts := range tsSet {
		tsList = append(tsList, ts)
	}
	sort.Strings(tsList)
	timeline := make([]time.Time, 0, len(tsList))
	for _, s := range tsList {
		t, _ := time.Parse(time.RFC3339, s)
		timeline = append(timeline, t)
	}
	loaded.Timeline = timeline

	c.mu.Lock()
	c.scenarios[key] = loaded
	c.mu.Unlock()
	return nil
}

// Timeline returns the sorted, distinct bar timestamps for this scenario
// version. Returns nil if Load was not called.
func (c *ScenarioBarCache) Timeline(scenarioID string, version int) []time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.scenarios[keyOf(scenarioID, version)]
	if !ok {
		return nil
	}
	return s.Timeline
}

// BarAt returns the bar for (symbol, ts) if it exists exactly. ok=false
// means there is no bar at this timestamp for this symbol — caller may
// fall back to LastBarAtOrBefore.
func (c *ScenarioBarCache) BarAt(scenarioID string, version int, symbol string, ts time.Time) (ScenarioBar, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.scenarios[keyOf(scenarioID, version)]
	if !ok {
		return ScenarioBar{}, false
	}
	bySym, ok := s.BySymTs[symbol]
	if !ok {
		return ScenarioBar{}, false
	}
	bar, ok := bySym[ts.UTC().Format(time.RFC3339)]
	return bar, ok
}

// LastBarAtOrBefore returns the most recent bar for symbol with ts ≤ asOf.
// Used by mark-to-market when the exact timestamp has no bar (gap in data).
func (c *ScenarioBarCache) LastBarAtOrBefore(scenarioID string, version int, symbol string, asOf time.Time) (ScenarioBar, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.scenarios[keyOf(scenarioID, version)]
	if !ok {
		return ScenarioBar{}, false
	}
	bySym, ok := s.BySymTs[symbol]
	if !ok {
		return ScenarioBar{}, false
	}
	// Walk the (small) timeline backwards from asOf. For MVP universe
	// sizes the timeline is hundreds to low thousands of ticks — linear
	// scan is fine. If it ever isn't, swap to a per-symbol sorted slice.
	cutoff := asOf.UTC().Format(time.RFC3339)
	var best ScenarioBar
	found := false
	for tsStr, bar := range bySym {
		if tsStr <= cutoff {
			if !found || tsStr > best.Ts.UTC().Format(time.RFC3339) {
				best = bar
				found = true
			}
		}
	}
	return best, found
}

// Lookback returns the last n bars for symbol with ts ≤ asOf, sorted
// ascending by ts. Used by GET /api/v1/runs/:id/market.
func (c *ScenarioBarCache) Lookback(scenarioID string, version int, symbol string, asOf time.Time, n int) []ScenarioBar {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.scenarios[keyOf(scenarioID, version)]
	if !ok {
		return nil
	}
	bySym, ok := s.BySymTs[symbol]
	if !ok {
		return nil
	}

	cutoff := asOf.UTC().Format(time.RFC3339)
	keys := make([]string, 0, len(bySym))
	for k := range bySym {
		if k <= cutoff {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if n > 0 && len(keys) > n {
		keys = keys[len(keys)-n:]
	}
	out := make([]ScenarioBar, 0, len(keys))
	for _, k := range keys {
		out = append(out, bySym[k])
	}
	return out
}
