package services

import (
	"bottrade/models"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ScenarioEngine is the deterministic simulator: it owns run lifecycle,
// fills queued orders against frozen bars at next-bar open + slippage,
// mark-to-markets equity, and force-liquidates on margin violation.
//
// Concurrency: per-run sync.Mutex via the `locks` sync.Map. All writes for
// a given run serialize through that lock. This is enough for a single
// API process. Cross-process safety would need row-version checks; out of
// scope for MVP.
type ScenarioEngine struct {
	appDB   *sql.DB
	bars    *ScenarioBarCache
	locksMu sync.Mutex // guards locks; plain map because Go 1.25.0's sync.Map (HashTrieMap) panicked in prod ("ran out of hash bits")
	locks   map[string]*sync.Mutex
	writes  chan struct{}
}

func NewScenarioEngine(appDB, marketDB *sql.DB) *ScenarioEngine {
	return &ScenarioEngine{
		appDB:  appDB,
		bars:   NewScenarioBarCache(marketDB),
		locks:  make(map[string]*sync.Mutex),
		writes: make(chan struct{}, 8),
	}
}

// AppDB exposes the engine's app-side DB pool for handlers that need
// to do their own queries (ownership checks, leaderboard inserts).
func (e *ScenarioEngine) AppDB() *sql.DB { return e.appDB }

// Bars exposes the bar cache so handlers (e.g. GET /api/v1/runs/:id/market)
// can serve lookback queries without re-loading.
func (e *ScenarioEngine) Bars() *ScenarioBarCache { return e.bars }

// Locks acquires the per-run mutex. Returned func releases it.
func (e *ScenarioEngine) lockRun(runID string) func() {
	e.locksMu.Lock()
	mu, ok := e.locks[runID]
	if !ok {
		mu = &sync.Mutex{}
		e.locks[runID] = mu
	}
	e.locksMu.Unlock()
	mu.Lock()
	return func() { mu.Unlock() }
}

func (e *ScenarioEngine) withWriteRetry(fn func() error) error {
	var last error
	for attempt := 0; attempt < 6; attempt++ {
		e.writes <- struct{}{}
		err := fn()
		<-e.writes
		if err == nil {
			return nil
		}
		last = err
		if !isTransientDBErr(err) {
			return err
		}
		time.Sleep(time.Duration(75*(attempt+1)*(attempt+1)) * time.Millisecond)
	}
	return last
}

func isTransientDBErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "SQLITE_BUSY") ||
		strings.Contains(msg, "SQLITE_LOCKED")
}

// ----------------------------------------------------------------------------
// StartRun
// ----------------------------------------------------------------------------

// StartRun creates a new run pinned to the scenario's current_version.
// sim_time is set to the FIRST timeline timestamp — the agent sees that
// initial bar's data on its first /market call and may immediately queue
// orders that fill on the second bar.
func (e *ScenarioEngine) StartRun(apiKeyID, scenarioID, botName string) (*models.Run, error) {
	// Load scenario
	scen, err := e.loadScenario(scenarioID)
	if err != nil {
		return nil, fmt.Errorf("load scenario: %w", err)
	}
	if scen.Status != "ready" {
		return nil, fmt.Errorf("scenario %s not ready (status=%s)", scenarioID, scen.Status)
	}

	if err := e.bars.Load(scen.ID, scen.CurrentVersion); err != nil {
		return nil, fmt.Errorf("load bars: %w", err)
	}
	timeline := e.bars.Timeline(scen.ID, scen.CurrentVersion)
	if len(timeline) == 0 {
		return nil, fmt.Errorf("scenario %s has no bars", scenarioID)
	}

	runID := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	startTime := timeline[0]
	if err := e.withWriteRetry(func() error {
		tx, err := e.appDB.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`
			INSERT INTO runs (
				id, api_key_id, bot_name, scenario_id, scenario_version, status,
				sim_time, cash, starting_cash, last_activity_at, created_at
			) VALUES (?1, ?2, ?3, ?4, ?5, 'active', ?6, ?7, ?8, ?9, ?10)`,
			runID, apiKeyID, botName, scen.ID, scen.CurrentVersion,
			startTime.UTC().Format(time.RFC3339),
			scen.StartingCash, scen.StartingCash, now, now,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(`
			INSERT INTO run_equity (run_id, sim_time, cash, positions_value, equity)
			VALUES (?1, ?2, ?3, 0, ?3)
		`, runID, startTime.UTC().Format(time.RFC3339), scen.StartingCash); err != nil {
			return err
		}
		return tx.Commit()
	}); err != nil {
		return nil, fmt.Errorf("insert run: %w", err)
	}

	return e.LoadRun(runID)
}

// ----------------------------------------------------------------------------
// GetRunState
// ----------------------------------------------------------------------------

// GetRunState returns the run + open positions + queued orders + last equity sample.
func (e *ScenarioEngine) GetRunState(runID string) (*models.RunSnapshot, error) {
	run, err := e.LoadRun(runID)
	if err != nil {
		return nil, err
	}
	positions, err := e.loadPositions(runID)
	if err != nil {
		return nil, err
	}
	orders, err := e.loadOrders(runID)
	if err != nil {
		return nil, err
	}
	lastEq, err := e.loadLastEquity(runID)
	if err != nil {
		return nil, err
	}
	return &models.RunSnapshot{
		Run:          *run,
		Positions:    positions,
		QueuedOrders: orders,
		LastEquity:   lastEq,
	}, nil
}

// ----------------------------------------------------------------------------
// QueueTrade
// ----------------------------------------------------------------------------

// QueueTradeRequest is what comes in from POST /api/v1/runs/:id/trades.
type QueueTradeRequest struct {
	Symbol    string
	Side      string // buy | sell | short | cover
	Quantity  float64
	Reasoning string
}

// qtyEpsilon is the dust threshold below which a position is treated as flat.
// Fractional (crypto) quantities accumulate floating-point rounding, so closing
// 0.3 against a 0.3 long can leave a phantom ~1e-17 residue. 1e-9 units is far
// below any real position (a satoshi is 1e-8 BTC), so it snaps clean without
// ever swallowing a legitimate holding. Also gives the sell/cover "exceeds"
// checks a tolerance so closing your exact position isn't rejected by rounding.
const qtyEpsilon = 1e-9

// QueueTrade validates the request against scenario rules + projected
// margin and inserts a row into run_orders. The order fills on the
// next /step.
func (e *ScenarioEngine) QueueTrade(runID string, req QueueTradeRequest) (*models.RunOrder, error) {
	release := e.lockRun(runID)
	defer release()

	run, scen, err := e.loadRunAndScenario(runID)
	if err != nil {
		return nil, err
	}
	if run.Status != "active" {
		return nil, fmt.Errorf("run is %s, not active", run.Status)
	}
	if req.Quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}
	side := req.Side
	if side != "buy" && side != "sell" && side != "short" && side != "cover" {
		return nil, fmt.Errorf("side must be buy|sell|short|cover (got %q)", side)
	}
	if (side == "short" || side == "cover") && !scen.ShortEnabled {
		return nil, fmt.Errorf("shorting is disabled on this scenario")
	}

	// Symbol must be in scenario universe.
	if !inSet(scen.Universe, req.Symbol) {
		return nil, fmt.Errorf("symbol %q not in scenario universe", req.Symbol)
	}

	// Sell/cover validation: must reduce an existing position the right way.
	if side == "sell" || side == "cover" {
		positions, err := e.loadPositions(runID)
		if err != nil {
			return nil, err
		}
		pos := findPosition(positions, req.Symbol)
		if side == "sell" {
			if pos == nil || pos.Quantity <= 0 {
				return nil, fmt.Errorf("no long position in %s to sell", req.Symbol)
			}
			if req.Quantity > pos.Quantity+qtyEpsilon {
				return nil, fmt.Errorf("sell quantity %g exceeds long %g", req.Quantity, pos.Quantity)
			}
		}
		if side == "cover" {
			if pos == nil || pos.Quantity >= 0 {
				return nil, fmt.Errorf("no short position in %s to cover", req.Symbol)
			}
			if req.Quantity > -pos.Quantity+qtyEpsilon {
				return nil, fmt.Errorf("cover quantity %g exceeds short %g", req.Quantity, -pos.Quantity)
			}
		}
	}

	// Buy/short: rough leverage pre-check using the last known close.
	if side == "buy" || side == "short" {
		bar, ok := e.bars.LastBarAtOrBefore(scen.ID, scen.CurrentVersion, req.Symbol, run.SimTime)
		if !ok {
			return nil, fmt.Errorf("no bar yet for %s at or before sim_time", req.Symbol)
		}
		estPrice := bar.Close
		projected, err := e.projectedNotional(runID, scen, run.SimTime, req.Quantity, estPrice)
		if err != nil {
			return nil, err
		}
		bp := run.Cash
		required := projected / scen.LeverageCap
		if required > bp {
			return nil, fmt.Errorf("insufficient buying power: need $%.2f required margin, have $%.2f cash", required, bp)
		}
	}

	orderID := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := e.withWriteRetry(func() error {
		tx, err := e.appDB.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`
			INSERT INTO run_orders
				(id, run_id, symbol, side, quantity, reasoning, queued_at, queued_at_sim_time)
			VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)
		`, orderID, runID, req.Symbol, side, req.Quantity, req.Reasoning, now,
			run.SimTime.UTC().Format(time.RFC3339)); err != nil {
			return fmt.Errorf("insert order: %w", err)
		}
		if _, err := tx.Exec(`UPDATE runs SET last_activity_at = ?1 WHERE id = ?2`, now, runID); err != nil {
			return fmt.Errorf("touch run: %w", err)
		}
		return tx.Commit()
	}); err != nil {
		return nil, err
	}

	return &models.RunOrder{
		ID:              orderID,
		RunID:           runID,
		Symbol:          req.Symbol,
		Side:            side,
		Quantity:        req.Quantity,
		Reasoning:       req.Reasoning,
		QueuedAt:        time.Now().UTC(),
		QueuedAtSimTime: run.SimTime,
	}, nil
}

// projectedNotional returns the absolute total notional the run would have
// AFTER adding the new order, using estPrice as the marker for the new order.
func (e *ScenarioEngine) projectedNotional(runID string, scen *models.Scenario, asOf time.Time, addQty float64, estPrice float64) (float64, error) {
	positions, err := e.loadPositions(runID)
	if err != nil {
		return 0, err
	}
	total := 0.0
	for _, p := range positions {
		bar, ok := e.bars.LastBarAtOrBefore(scen.ID, scen.CurrentVersion, p.Symbol, asOf)
		if !ok {
			continue
		}
		total += math.Abs(p.Quantity * bar.Close)
	}
	total += addQty * estPrice
	return total, nil
}

// ----------------------------------------------------------------------------
// AdvanceStep
// ----------------------------------------------------------------------------

// StepResult is what /step returns to the caller.
type StepResult struct {
	BarsAdvanced    int               `json:"bars_advanced"`
	NewSimTime      time.Time         `json:"new_sim_time"`
	Fills           []models.RunTrade `json:"fills"`
	Liquidated      bool              `json:"liquidated"`
	LiquidationAtTs *time.Time        `json:"liquidation_at_ts,omitempty"`
	NewEquity       float64           `json:"equity"`
	NewCash         float64           `json:"cash"`
	PositionsValue  float64           `json:"positions_value"`
	Done            bool              `json:"done"` // scenario reached end
}

// AdvanceStep advances the run by `count` bars. For each bar:
//  1. Fill queued orders at the bar's open ± slippage
//  2. Upsert positions (signed quantity; FIFO on reduce)
//  3. Mark-to-market at the bar's close → compute equity
//  4. If equity < maintenance: queue close-all orders, set status=liquidated, stop
//  5. After all advancing, write ONE run_equity sample, advance sim_time
//
// Per-bar behavior is wrapped in a single DB transaction PER BAR. If the
// fill or position upsert fails midway the bar is rolled back; sim_time
// doesn't move and the agent can retry.
func (e *ScenarioEngine) AdvanceStep(runID string, count int) (*StepResult, error) {
	release := e.lockRun(runID)
	defer release()

	if count < 1 {
		count = 1
	}

	run, scen, err := e.loadRunAndScenario(runID)
	if err != nil {
		return nil, err
	}
	if run.Status != "active" {
		return nil, fmt.Errorf("run is %s, not active", run.Status)
	}

	timeline := e.bars.Timeline(scen.ID, scen.CurrentVersion)
	if len(timeline) == 0 {
		return nil, fmt.Errorf("scenario has no timeline")
	}

	// Find current index in timeline.
	idx := timelineIndex(timeline, run.SimTime)
	if idx < 0 {
		return nil, fmt.Errorf("sim_time %s not in timeline", run.SimTime)
	}

	result := &StepResult{
		Fills: []models.RunTrade{},
	}

	cur := run.Cash
	if err := e.withWriteRetry(func() error {
		tx, err := e.appDB.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		for step := 0; step < count; step++ {
			nextIdx := idx + 1
			if nextIdx >= len(timeline) {
				result.Done = true
				break
			}
			nextTs := timeline[nextIdx]

			// 1. Fill queued orders at nextTs's open.
			orders, err := e.loadOrdersTx(tx, runID)
			if err != nil {
				return err
			}
			fills, newCash, err := e.executeOrdersTx(tx, runID, scen, orders, nextTs, cur)
			if err != nil {
				return fmt.Errorf("execute orders at %s: %w", nextTs, err)
			}
			result.Fills = append(result.Fills, fills...)
			cur = newCash

			// 2. Mark-to-market at nextTs's close.
			positions, err := e.loadPositionsTx(tx, runID)
			if err != nil {
				return err
			}
			positionsValue := 0.0
			notional := 0.0
			for _, p := range positions {
				bar, ok := e.bars.BarAt(scen.ID, scen.CurrentVersion, p.Symbol, nextTs)
				if !ok {
					bar, ok = e.bars.LastBarAtOrBefore(scen.ID, scen.CurrentVersion, p.Symbol, nextTs)
				}
				if !ok {
					continue
				}
				positionsValue += p.Quantity * bar.Close
				notional += math.Abs(p.Quantity * bar.Close)
			}
			equity := cur + positionsValue

			// 3. Liquidation check (only meaningful when leverage > 1).
			if scen.LeverageCap > 1.0 && notional > 0 {
				initialMargin := notional / scen.LeverageCap
				maintenanceMargin := initialMargin / 2.0
				if equity < maintenanceMargin {
					// Force-close every position at nextTs's open + slippage.
					// We're already at nextTs after fills above; close-all
					// runs "at this same bar" effectively at the close price
					// for simplicity (in real life this would be the next
					// bar's open; for MVP we accept the simplification).
					closingFills, postLiqCash, err := e.liquidatePositionsTx(tx, runID, scen, positions, nextTs, cur)
					if err != nil {
						return fmt.Errorf("liquidate at %s: %w", nextTs, err)
					}
					result.Fills = append(result.Fills, closingFills...)
					cur = postLiqCash
					result.Liquidated = true
					liqTs := nextTs
					result.LiquidationAtTs = &liqTs
					idx = nextIdx
					break
				}
			}

			idx = nextIdx
		}

		newSimTime := timeline[idx]

		// Update run, write equity sample.
		now := time.Now().UTC().Format(time.RFC3339)
		newStatus := "active"
		completedAt := sql.NullString{}
		if result.Liquidated {
			newStatus = "liquidated"
			completedAt = sql.NullString{String: now, Valid: true}
		} else if result.Done {
			newStatus = "completed"
			completedAt = sql.NullString{String: now, Valid: true}
		}

		if _, err := tx.Exec(`
			UPDATE runs SET sim_time = ?1, cash = ?2, status = ?3, last_activity_at = ?4, completed_at = ?5
			 WHERE id = ?6
		`, newSimTime.UTC().Format(time.RFC3339), cur, newStatus, now, completedAt, runID); err != nil {
			return fmt.Errorf("update run: %w", err)
		}

		// Compute positionsValue at the final sim_time for the sample.
		positionsValue := 0.0
		positions, _ := e.loadPositionsTx(tx, runID)
		for _, p := range positions {
			bar, ok := e.bars.BarAt(scen.ID, scen.CurrentVersion, p.Symbol, newSimTime)
			if !ok {
				bar, ok = e.bars.LastBarAtOrBefore(scen.ID, scen.CurrentVersion, p.Symbol, newSimTime)
			}
			if ok {
				positionsValue += p.Quantity * bar.Close
			}
		}
		equity := cur + positionsValue
		if _, err := tx.Exec(`
			INSERT INTO run_equity (run_id, sim_time, cash, positions_value, equity)
			VALUES (?1, ?2, ?3, ?4, ?5)
			ON CONFLICT (run_id, sim_time) DO UPDATE SET
				cash = excluded.cash,
				positions_value = excluded.positions_value,
				equity = excluded.equity
		`, runID, newSimTime.UTC().Format(time.RFC3339), cur, positionsValue, equity); err != nil {
			return fmt.Errorf("insert equity sample: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		result.BarsAdvanced = idx - timelineIndex(timeline, run.SimTime)
		result.NewSimTime = newSimTime
		result.NewCash = cur
		result.PositionsValue = positionsValue
		result.NewEquity = equity
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

func (e *ScenarioEngine) executeOrdersTx(tx *sql.Tx, runID string, scen *models.Scenario, orders []models.RunOrder, at time.Time, cashIn float64) ([]models.RunTrade, float64, error) {
	cash := cashIn
	fills := []models.RunTrade{}
	for _, o := range orders {
		bar, ok := e.bars.BarAt(scen.ID, scen.CurrentVersion, o.Symbol, at)
		if !ok {
			// No bar at exactly this ts for this symbol. Defer: leave
			// the order in run_orders, it'll try again next step.
			continue
		}
		slippage := bar.SlippageBps
		var fillPrice float64
		switch o.Side {
		case "buy", "cover":
			fillPrice = bar.Open * (1.0 + float64(slippage)/10000.0)
		case "sell", "short":
			fillPrice = bar.Open * (1.0 - float64(slippage)/10000.0)
		}
		totalValue := fillPrice * o.Quantity

		// Apply position delta.
		pos, err := e.getPositionTx(tx, runID, o.Symbol)
		if err != nil {
			return nil, cashIn, err
		}
		realized := 0.0
		switch o.Side {
		case "buy":
			pos = applyAdd(pos, o.Quantity, fillPrice)
			cash -= totalValue
		case "short":
			pos = applyAdd(pos, -o.Quantity, fillPrice)
			cash += totalValue
		case "sell":
			realized = (fillPrice - pos.AvgCost) * o.Quantity
			pos = applyReduce(pos, o.Quantity)
			cash += totalValue
		case "cover":
			realized = (pos.AvgCost - fillPrice) * o.Quantity
			pos = applyReduce(pos, o.Quantity)
			cash -= totalValue
		}

		if err := upsertPositionTx(tx, runID, o.Symbol, pos); err != nil {
			return nil, cashIn, err
		}

		// Insert fill record + delete the order.
		tradeID := uuid.NewString()
		if _, err := tx.Exec(`
			INSERT INTO run_trades
				(id, run_id, symbol, side, quantity, fill_price, slippage_bps,
				 sim_time_filled, total_value, realized_pnl, reasoning)
			VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11)
		`, tradeID, runID, o.Symbol, o.Side, o.Quantity, fillPrice, slippage,
			at.UTC().Format(time.RFC3339), totalValue, realized, o.Reasoning); err != nil {
			return nil, cashIn, fmt.Errorf("insert trade: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM run_orders WHERE id = ?1`, o.ID); err != nil {
			return nil, cashIn, fmt.Errorf("delete order: %w", err)
		}
		fills = append(fills, models.RunTrade{
			ID:            tradeID,
			RunID:         runID,
			Symbol:        o.Symbol,
			Side:          o.Side,
			Quantity:      o.Quantity,
			FillPrice:     fillPrice,
			SlippageBps:   slippage,
			SimTimeFilled: at,
			TotalValue:    totalValue,
			RealizedPnL:   realized,
			Reasoning:     o.Reasoning,
		})
	}
	return fills, cash, nil
}

func (e *ScenarioEngine) liquidatePositionsTx(tx *sql.Tx, runID string, scen *models.Scenario, positions []models.RunPosition, at time.Time, cashIn float64) ([]models.RunTrade, float64, error) {
	if len(positions) == 0 {
		return nil, cashIn, nil
	}
	cash := cashIn
	fills := []models.RunTrade{}
	for _, p := range positions {
		bar, ok := e.bars.BarAt(scen.ID, scen.CurrentVersion, p.Symbol, at)
		if !ok {
			bar, ok = e.bars.LastBarAtOrBefore(scen.ID, scen.CurrentVersion, p.Symbol, at)
		}
		if !ok {
			continue
		}
		slippage := bar.SlippageBps
		// Use close as the liquidation reference; apply slippage in the
		// "bad" direction relative to position side.
		var fillPrice float64
		var side string
		var qty float64
		if p.Quantity > 0 {
			side = "sell"
			fillPrice = bar.Close * (1.0 - float64(slippage)/10000.0)
			qty = p.Quantity
		} else {
			side = "cover"
			fillPrice = bar.Close * (1.0 + float64(slippage)/10000.0)
			qty = -p.Quantity
		}

		totalValue := fillPrice * qty
		realized := 0.0
		if side == "sell" {
			realized = (fillPrice - p.AvgCost) * qty
			cash += totalValue
		} else {
			realized = (p.AvgCost - fillPrice) * qty
			cash -= totalValue
		}

		if _, err := tx.Exec(`DELETE FROM run_positions WHERE run_id = ?1 AND symbol = ?2`, runID, p.Symbol); err != nil {
			return nil, cashIn, fmt.Errorf("delete position: %w", err)
		}
		tradeID := uuid.NewString()
		if _, err := tx.Exec(`
			INSERT INTO run_trades
				(id, run_id, symbol, side, quantity, fill_price, slippage_bps,
				 sim_time_filled, total_value, realized_pnl, reasoning)
			VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, 'forced liquidation')
		`, tradeID, runID, p.Symbol, side, qty, fillPrice, slippage,
			at.UTC().Format(time.RFC3339), totalValue, realized); err != nil {
			return nil, cashIn, fmt.Errorf("insert liq trade: %w", err)
		}
		fills = append(fills, models.RunTrade{
			ID:            tradeID,
			RunID:         runID,
			Symbol:        p.Symbol,
			Side:          side,
			Quantity:      qty,
			FillPrice:     fillPrice,
			SlippageBps:   slippage,
			SimTimeFilled: at,
			TotalValue:    totalValue,
			RealizedPnL:   realized,
			Reasoning:     "forced liquidation",
		})
	}

	// Also drop any remaining queued orders — they're meaningless post-liquidation.
	if _, err := tx.Exec(`DELETE FROM run_orders WHERE run_id = ?1`, runID); err != nil {
		return nil, cashIn, fmt.Errorf("drop orders: %w", err)
	}
	return fills, cash, nil
}

// applyAdd adds delta to a position, returning new quantity + avg_cost.
// delta can be negative (opening or extending a short).
func applyAdd(pos models.RunPosition, delta float64, price float64) models.RunPosition {
	if pos.Quantity == 0 || sign(pos.Quantity) == sign(delta) {
		// Same-direction add or fresh: recompute weighted avg cost.
		newQ := pos.Quantity + delta
		var newAvg float64
		if pos.Quantity == 0 {
			newAvg = price
		} else {
			newAvg = (pos.AvgCost*pos.Quantity + price*delta) / newQ
		}
		// For shorts (negative qty), avg_cost stays positive (entry price).
		if newAvg < 0 {
			newAvg = math.Abs(newAvg)
		}
		pos.Quantity = newQ
		pos.AvgCost = newAvg
	}
	// Reverse-direction additions are handled by applyReduce (sell/cover).
	return pos
}

// applyReduce reduces the magnitude of a position by qty. avg_cost stays the same.
func applyReduce(pos models.RunPosition, qty float64) models.RunPosition {
	if pos.Quantity > 0 {
		pos.Quantity -= qty
	} else {
		pos.Quantity += qty // qty here is positive; we're adding to a negative
	}
	// Snap dust to flat: a fractional close can leave float residue that would
	// otherwise persist as a phantom position and keep a stale avg_cost.
	if math.Abs(pos.Quantity) < qtyEpsilon {
		pos.Quantity = 0
		pos.AvgCost = 0
	}
	return pos
}

func sign(n float64) int {
	if n > 0 {
		return 1
	}
	if n < 0 {
		return -1
	}
	return 0
}

func upsertPositionTx(tx *sql.Tx, runID, symbol string, pos models.RunPosition) error {
	if math.Abs(pos.Quantity) < qtyEpsilon {
		_, err := tx.Exec(`DELETE FROM run_positions WHERE run_id = ?1 AND symbol = ?2`, runID, symbol)
		return err
	}
	_, err := tx.Exec(`
		INSERT INTO run_positions (run_id, symbol, quantity, avg_cost, updated_at)
		VALUES (?1, ?2, ?3, ?4, CURRENT_TIMESTAMP)
		ON CONFLICT (run_id, symbol) DO UPDATE SET
			quantity = excluded.quantity,
			avg_cost = excluded.avg_cost,
			updated_at = excluded.updated_at
	`, runID, symbol, pos.Quantity, pos.AvgCost)
	return err
}

// getPositionTx loads (and locks under tx) the run_position row. Returns
// zero-value position if absent.
func (e *ScenarioEngine) getPositionTx(tx *sql.Tx, runID, symbol string) (models.RunPosition, error) {
	row := tx.QueryRow(`SELECT quantity, avg_cost FROM run_positions WHERE run_id = ?1 AND symbol = ?2`, runID, symbol)
	var p models.RunPosition
	p.RunID = runID
	p.Symbol = symbol
	if err := row.Scan(&p.Quantity, &p.AvgCost); err != nil {
		if err == sql.ErrNoRows {
			return p, nil
		}
		return p, err
	}
	return p, nil
}

// ----------------------------------------------------------------------------
// ComputeResults
// ----------------------------------------------------------------------------

// ComputeResults finalizes a run. Idempotent — re-running rewrites the row.
// Pulls the equity series and computes Sharpe/Sortino/MaxDD/volatility.
func (e *ScenarioEngine) ComputeResults(runID string) (*models.RunResults, error) {
	run, err := e.LoadRun(runID)
	if err != nil {
		return nil, err
	}
	if run.Status == "active" {
		return nil, fmt.Errorf("run is still active; cannot compute results yet")
	}

	rows, err := e.appDB.Query(`SELECT equity FROM run_equity WHERE run_id = ?1 ORDER BY sim_time ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var equities []float64
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		equities = append(equities, v)
	}
	if len(equities) == 0 {
		return nil, fmt.Errorf("no equity samples for run")
	}

	final := equities[len(equities)-1]
	returnPct := (final - run.StartingCash) / run.StartingCash * 100.0

	// Per-step returns for Sharpe/Sortino/vol.
	var sharpe, sortino, maxDD, vol *float64
	if len(equities) >= 2 {
		var rets []float64
		for i := 1; i < len(equities); i++ {
			if equities[i-1] == 0 {
				continue
			}
			rets = append(rets, (equities[i]-equities[i-1])/equities[i-1])
		}
		if len(rets) >= 2 {
			mean := avg(rets)
			std := stddev(rets, mean)
			if std > 0 {
				s := mean / std * math.Sqrt(252) // annualized (rough; bars aren't days)
				sharpe = &s
				vol = &std
			}
			downside := downsideStd(rets, 0)
			if downside > 0 {
				so := mean / downside * math.Sqrt(252)
				sortino = &so
			}
		}
		dd := scenarioMaxDrawdown(equities)
		if dd > 0 {
			maxDD = &dd
		}
	}

	var tradeCount int
	if err := e.appDB.QueryRow(`SELECT count(*) FROM run_trades WHERE run_id = ?1`, runID).Scan(&tradeCount); err != nil {
		return nil, fmt.Errorf("count trades: %w", err)
	}

	liq := 0
	if run.Status == "liquidated" {
		liq = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = e.appDB.Exec(`
		INSERT INTO run_results
			(run_id, final_equity, return_pct, sharpe, sortino, max_drawdown,
			 volatility, trade_count, liquidated, computed_at)
		VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10)
		ON CONFLICT (run_id) DO UPDATE SET
			final_equity = excluded.final_equity,
			return_pct = excluded.return_pct,
			sharpe = excluded.sharpe,
			sortino = excluded.sortino,
			max_drawdown = excluded.max_drawdown,
			volatility = excluded.volatility,
			trade_count = excluded.trade_count,
			liquidated = excluded.liquidated,
			computed_at = excluded.computed_at
	`, runID, final, returnPct,
		nullableFloat(sharpe), nullableFloat(sortino), nullableFloat(maxDD), nullableFloat(vol),
		tradeCount, liq, now)
	if err != nil {
		return nil, fmt.Errorf("insert run_results: %w", err)
	}

	r := &models.RunResults{
		RunID:       runID,
		FinalEquity: final,
		ReturnPct:   returnPct,
		Sharpe:      sharpe,
		Sortino:     sortino,
		MaxDrawdown: maxDD,
		Volatility:  vol,
		TradeCount:  tradeCount,
		Liquidated:  liq == 1,
		ComputedAt:  time.Now().UTC(),
	}
	return r, nil
}

func nullableFloat(p *float64) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

// ----------------------------------------------------------------------------
// Loaders
// ----------------------------------------------------------------------------

// LoadRun reads a run row.
func (e *ScenarioEngine) LoadRun(runID string) (*models.Run, error) {
	row := e.appDB.QueryRow(`
		SELECT id, api_key_id, COALESCE(bot_name,''), scenario_id, scenario_version, status,
		       sim_time, cash, starting_cash, last_activity_at, created_at,
		       completed_at, published
		  FROM runs WHERE id = ?1
	`, runID)
	var r models.Run
	var simStr, lastStr, createdStr string
	var completedStr sql.NullString
	var published int
	if err := row.Scan(
		&r.ID, &r.APIKeyID, &r.BotName, &r.ScenarioID, &r.ScenarioVersion, &r.Status,
		&simStr, &r.Cash, &r.StartingCash, &lastStr, &createdStr,
		&completedStr, &published,
	); err != nil {
		return nil, err
	}
	r.SimTime, _ = time.Parse(time.RFC3339, simStr)
	r.LastActivityAt, _ = time.Parse(time.RFC3339, lastStr)
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	if completedStr.Valid {
		t, _ := time.Parse(time.RFC3339, completedStr.String)
		r.CompletedAt = &t
	}
	r.Published = published != 0
	return &r, nil
}

func (e *ScenarioEngine) loadRunAndScenario(runID string) (*models.Run, *models.Scenario, error) {
	run, err := e.LoadRun(runID)
	if err != nil {
		return nil, nil, fmt.Errorf("load run: %w", err)
	}
	scen, err := e.loadScenario(run.ScenarioID)
	if err != nil {
		return nil, nil, fmt.Errorf("load scenario: %w", err)
	}
	// Make sure the scenario's bars are in cache; agents may interleave runs
	// across many scenarios.
	if err := e.bars.Load(scen.ID, run.ScenarioVersion); err != nil {
		return nil, nil, fmt.Errorf("load bars: %w", err)
	}
	return run, scen, nil
}

func (e *ScenarioEngine) LoadScenario(scenarioID string) (*models.Scenario, error) {
	return e.loadScenario(scenarioID)
}

func (e *ScenarioEngine) loadScenario(scenarioID string) (*models.Scenario, error) {
	row := e.appDB.QueryRow(`
		SELECT id, slug, name, COALESCE(description,''), bar_resolution,
		       start_ts, end_ts, starting_cash, leverage_cap, short_enabled,
		       universe_json, slippage_json, benchmark_symbol, status, current_version, created_at
		  FROM scenarios WHERE id = ?1
	`, scenarioID)
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

func (e *ScenarioEngine) loadPositions(runID string) ([]models.RunPosition, error) {
	rows, err := e.appDB.Query(`
		SELECT run_id, symbol, quantity, avg_cost, updated_at
		  FROM run_positions WHERE run_id = ?1
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.RunPosition
	for rows.Next() {
		var p models.RunPosition
		var updated string
		if err := rows.Scan(&p.RunID, &p.Symbol, &p.Quantity, &p.AvgCost, &updated); err != nil {
			return nil, err
		}
		p.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updated)
		out = append(out, p)
	}
	return out, nil
}

func (e *ScenarioEngine) loadPositionsTx(tx *sql.Tx, runID string) ([]models.RunPosition, error) {
	rows, err := tx.Query(`
		SELECT run_id, symbol, quantity, avg_cost, updated_at
		  FROM run_positions WHERE run_id = ?1
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.RunPosition
	for rows.Next() {
		var p models.RunPosition
		var updated string
		if err := rows.Scan(&p.RunID, &p.Symbol, &p.Quantity, &p.AvgCost, &updated); err != nil {
			return nil, err
		}
		p.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updated)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (e *ScenarioEngine) loadOrders(runID string) ([]models.RunOrder, error) {
	rows, err := e.appDB.Query(`
		SELECT id, run_id, symbol, side, quantity, COALESCE(reasoning,''),
		       queued_at, queued_at_sim_time
		  FROM run_orders WHERE run_id = ?1 ORDER BY queued_at ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.RunOrder
	for rows.Next() {
		var o models.RunOrder
		var queuedAt, queuedSim string
		if err := rows.Scan(&o.ID, &o.RunID, &o.Symbol, &o.Side, &o.Quantity, &o.Reasoning, &queuedAt, &queuedSim); err != nil {
			return nil, err
		}
		o.QueuedAt, _ = time.Parse(time.RFC3339, queuedAt)
		o.QueuedAtSimTime, _ = time.Parse(time.RFC3339, queuedSim)
		out = append(out, o)
	}
	return out, nil
}

func (e *ScenarioEngine) loadOrdersTx(tx *sql.Tx, runID string) ([]models.RunOrder, error) {
	rows, err := tx.Query(`
		SELECT id, run_id, symbol, side, quantity, COALESCE(reasoning,''),
		       queued_at, queued_at_sim_time
		  FROM run_orders WHERE run_id = ?1 ORDER BY queued_at ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.RunOrder
	for rows.Next() {
		var o models.RunOrder
		var queuedAt, queuedSim string
		if err := rows.Scan(&o.ID, &o.RunID, &o.Symbol, &o.Side, &o.Quantity, &o.Reasoning, &queuedAt, &queuedSim); err != nil {
			return nil, err
		}
		o.QueuedAt, _ = time.Parse(time.RFC3339, queuedAt)
		o.QueuedAtSimTime, _ = time.Parse(time.RFC3339, queuedSim)
		out = append(out, o)
	}
	return out, rows.Err()
}

func (e *ScenarioEngine) loadLastEquity(runID string) (*models.RunEquity, error) {
	row := e.appDB.QueryRow(`
		SELECT sim_time, cash, positions_value, equity
		  FROM run_equity WHERE run_id = ?1
		 ORDER BY sim_time DESC LIMIT 1
	`, runID)
	var eq models.RunEquity
	var simStr string
	if err := row.Scan(&simStr, &eq.Cash, &eq.PositionsValue, &eq.Equity); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	eq.RunID = runID
	eq.SimTime, _ = time.Parse(time.RFC3339, simStr)
	return &eq, nil
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

func inSet(set []string, s string) bool {
	for _, x := range set {
		if x == s {
			return true
		}
	}
	return false
}

func findPosition(positions []models.RunPosition, symbol string) *models.RunPosition {
	for i, p := range positions {
		if p.Symbol == symbol {
			return &positions[i]
		}
	}
	return nil
}

// timelineIndex returns the index of `t` in the sorted timeline, or -1.
// Tolerates RFC3339 round-trip drift via timestamp-string equality.
func timelineIndex(timeline []time.Time, t time.Time) int {
	target := t.UTC().Format(time.RFC3339)
	for i, ts := range timeline {
		if ts.UTC().Format(time.RFC3339) == target {
			return i
		}
	}
	// Fall back to nearest with ts <= t
	for i := len(timeline) - 1; i >= 0; i-- {
		if !timeline[i].After(t) {
			return i
		}
	}
	return -1
}

func avg(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func stddev(xs []float64, mean float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	ss := 0.0
	for _, x := range xs {
		ss += (x - mean) * (x - mean)
	}
	return math.Sqrt(ss / float64(len(xs)-1))
}

func downsideStd(xs []float64, target float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	ss := 0.0
	n := 0
	for _, x := range xs {
		if x < target {
			ss += (x - target) * (x - target)
			n++
		}
	}
	if n < 2 {
		return 0
	}
	return math.Sqrt(ss / float64(n-1))
}

func scenarioMaxDrawdown(equities []float64) float64 {
	if len(equities) < 2 {
		return 0
	}
	peak := equities[0]
	worst := 0.0
	for _, e := range equities {
		if e > peak {
			peak = e
		}
		dd := (peak - e) / peak
		if dd > worst {
			worst = dd
		}
	}
	return worst
}
