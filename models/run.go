package models

import "time"

// AgentInfo identifies the implementation that produced a run.
type AgentInfo struct {
	Name           string                 `json:"name"`
	Framework      string                 `json:"framework,omitempty"`
	Model          string                 `json:"model,omitempty"`
	Version        string                 `json:"version,omitempty"`
	SourceURL      string                 `json:"source_url,omitempty"`
	SourceRevision string                 `json:"source_revision,omitempty"`
	Config         map[string]interface{} `json:"config,omitempty"`
}

// Run is one traversal of one (scenario, scenario_version), billed and quotaed
// against the API key that created it.
type Run struct {
	ID              string     `json:"id"`
	APIKeyID        string     `json:"api_key_id,omitempty"`
	BotName         string     `json:"bot_name,omitempty"`
	ScenarioID      string     `json:"scenario_id"`
	ScenarioVersion int        `json:"scenario_version"`
	Status          string     `json:"status"` // active|completed|liquidated|abandoned
	SimTime         time.Time  `json:"sim_time"`
	Cash            float64    `json:"cash"`
	StartingCash    float64    `json:"starting_cash"`
	LastActivityAt  time.Time  `json:"last_activity_at"`
	CreatedAt       time.Time  `json:"created_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	Published       bool       `json:"published"`
	AgentInfo       *AgentInfo `json:"agent_info,omitempty"`
}

// RunPosition is a single symbol holding within a run. quantity is signed:
// positive = long, negative = short. Fractional to support assets that trade
// in fractions of a unit (e.g. 0.25 BTC); stocks still use whole numbers.
type RunPosition struct {
	RunID     string    `json:"run_id"`
	Symbol    string    `json:"symbol"`
	Quantity  float64   `json:"quantity"`
	AvgCost   float64   `json:"avg_cost"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RunOrder is a queued, unfilled order awaiting the next /step.
type RunOrder struct {
	ID              string    `json:"id"`
	RunID           string    `json:"run_id"`
	Symbol          string    `json:"symbol"`
	Side            string    `json:"side"` // buy|sell|short|cover
	Quantity        float64   `json:"quantity"`
	Reasoning       string    `json:"reasoning,omitempty"`
	QueuedAt        time.Time `json:"queued_at"`
	QueuedAtSimTime time.Time `json:"queued_at_sim_time"`
}

// RunTrade is an immutable record of a filled order.
type RunTrade struct {
	ID            string    `json:"id"`
	RunID         string    `json:"run_id"`
	Symbol        string    `json:"symbol"`
	Side          string    `json:"side"`
	Quantity      float64   `json:"quantity"`
	FillPrice     float64   `json:"fill_price"`
	SlippageBps   int       `json:"slippage_bps"`
	SimTimeFilled time.Time `json:"sim_time_filled"`
	TotalValue    float64   `json:"total_value"`
	RealizedPnL   float64   `json:"realized_pnl"`
	Reasoning     string    `json:"reasoning,omitempty"`
}

// RunEquity is a portfolio-value sample taken at the end of each /step.
type RunEquity struct {
	RunID          string    `json:"run_id"`
	SimTime        time.Time `json:"sim_time"`
	Cash           float64   `json:"cash"`
	PositionsValue float64   `json:"positions_value"`
	Equity         float64   `json:"equity"`
}

// RunResults is the computed-once-on-finalize summary.
type RunResults struct {
	RunID       string    `json:"run_id"`
	FinalEquity float64   `json:"final_equity"`
	ReturnPct   float64   `json:"return_pct"`
	Sharpe      *float64  `json:"sharpe,omitempty"`
	Sortino     *float64  `json:"sortino,omitempty"`
	MaxDrawdown *float64  `json:"max_drawdown,omitempty"`
	Volatility  *float64  `json:"volatility,omitempty"`
	TradeCount  int       `json:"trade_count"`
	Liquidated  bool      `json:"liquidated"`
	ComputedAt  time.Time `json:"computed_at"`
}

// RunSnapshot is the GET /api/v1/runs/:id response payload.
type RunSnapshot struct {
	Run          Run           `json:"run"`
	Positions    []RunPosition `json:"positions"`
	QueuedOrders []RunOrder    `json:"queued_orders"`
	LastEquity   *RunEquity    `json:"last_equity,omitempty"`
}
