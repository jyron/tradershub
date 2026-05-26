package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type BotTradeClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewBotTradeClient(baseURL, apiKey string) *BotTradeClient {
	return &BotTradeClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type Scenario struct {
	ID              string         `json:"id"`
	Slug            string         `json:"slug"`
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	BarResolution   string         `json:"bar_resolution"`
	StartTs         time.Time      `json:"start_ts"`
	EndTs           time.Time      `json:"end_ts"`
	StartingCash    float64        `json:"starting_cash"`
	LeverageCap     float64        `json:"leverage_cap"`
	ShortEnabled    bool           `json:"short_enabled"`
	Universe        []string       `json:"universe"`
	SlippageBps     map[string]int `json:"slippage_bps"`
	BenchmarkSymbol string         `json:"benchmark_symbol"`
	Status          string         `json:"status"`
	CurrentVersion  int            `json:"current_version"`
	CreatedAt       time.Time      `json:"created_at"`
}

type Run struct {
	ID              string     `json:"id"`
	APIKeyID        string     `json:"api_key_id,omitempty"`
	BotName         string     `json:"bot_name,omitempty"`
	ScenarioID      string     `json:"scenario_id"`
	ScenarioVersion int        `json:"scenario_version"`
	Status          string     `json:"status"`
	SimTime         time.Time  `json:"sim_time"`
	Cash            float64    `json:"cash"`
	StartingCash    float64    `json:"starting_cash"`
	LastActivityAt  time.Time  `json:"last_activity_at"`
	CreatedAt       time.Time  `json:"created_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	Published       bool       `json:"published"`
}

type Position struct {
	RunID     string    `json:"run_id"`
	Symbol    string    `json:"symbol"`
	Quantity  int       `json:"quantity"`
	AvgCost   float64   `json:"avg_cost"`
	UpdatedAt time.Time `json:"updated_at"`
}

type QueuedOrder struct {
	ID              string    `json:"id"`
	RunID           string    `json:"run_id"`
	Symbol          string    `json:"symbol"`
	Side            string    `json:"side"`
	Quantity        int       `json:"quantity"`
	Reasoning       string    `json:"reasoning,omitempty"`
	QueuedAt        time.Time `json:"queued_at"`
	QueuedAtSimTime time.Time `json:"queued_at_sim_time"`
}

type Trade struct {
	ID            string    `json:"id"`
	RunID         string    `json:"run_id"`
	Symbol        string    `json:"symbol"`
	Side          string    `json:"side"`
	Quantity      int       `json:"quantity"`
	FillPrice     float64   `json:"fill_price"`
	SlippageBps   int       `json:"slippage_bps"`
	SimTimeFilled time.Time `json:"sim_time_filled"`
	TotalValue    float64   `json:"total_value"`
	RealizedPnL   float64   `json:"realized_pnl"`
	Reasoning     string    `json:"reasoning,omitempty"`
}

type Equity struct {
	RunID          string    `json:"run_id"`
	SimTime        time.Time `json:"sim_time"`
	Cash           float64   `json:"cash"`
	PositionsValue float64   `json:"positions_value"`
	Equity         float64   `json:"equity"`
}

type RunSnapshot struct {
	Run          Run           `json:"run"`
	Positions    []Position    `json:"positions"`
	QueuedOrders []QueuedOrder `json:"queued_orders"`
	LastEquity   *Equity       `json:"last_equity,omitempty"`
}

type MarketBar struct {
	Ts     string  `json:"ts"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume int64   `json:"volume"`
}

type MarketResponse struct {
	SimTime string                 `json:"sim_time"`
	Bars    map[string][]MarketBar `json:"bars"`
}

type StepResult struct {
	BarsAdvanced    int        `json:"bars_advanced"`
	NewSimTime      time.Time  `json:"new_sim_time"`
	Fills           []Trade    `json:"fills"`
	Liquidated      bool       `json:"liquidated"`
	LiquidationAtTs *time.Time `json:"liquidation_at_ts,omitempty"`
	NewEquity       float64    `json:"equity"`
	NewCash         float64    `json:"cash"`
	PositionsValue  float64    `json:"positions_value"`
	Done            bool       `json:"done"`
}

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

type TradeOrder struct {
	Symbol    string `json:"symbol"`
	Side      string `json:"side"`
	Quantity  int    `json:"quantity"`
	Reasoning string `json:"reasoning,omitempty"`
}

type QueuedTradeResult struct {
	Order QueuedOrder `json:"order"`
}

type SubmitTurnResult struct {
	QueuedOrders []QueuedOrder `json:"queued_orders"`
	Step         StepResult    `json:"step"`
	Snapshot     *RunSnapshot  `json:"snapshot,omitempty"`
}

func (c *BotTradeClient) ListScenarios(ctx context.Context) ([]Scenario, error) {
	var out struct {
		Scenarios []Scenario `json:"scenarios"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/scenarios", nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Scenarios, nil
}

func (c *BotTradeClient) GetScenario(ctx context.Context, idOrSlug string) (*Scenario, error) {
	var out struct {
		Scenario Scenario `json:"scenario"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/scenarios/"+url.PathEscape(idOrSlug), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out.Scenario, nil
}

func (c *BotTradeClient) StartRun(ctx context.Context, scenarioSlug, botName string) (*Run, error) {
	var out struct {
		Run Run `json:"run"`
	}
	body := map[string]string{"scenario_slug": scenarioSlug}
	if botName != "" {
		body["bot_name"] = botName
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/runs", nil, body, &out); err != nil {
		return nil, err
	}
	return &out.Run, nil
}

func (c *BotTradeClient) GetRun(ctx context.Context, runID string) (*RunSnapshot, error) {
	var out RunSnapshot
	if err := c.do(ctx, http.MethodGet, "/api/v1/runs/"+url.PathEscape(runID), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *BotTradeClient) GetMarket(ctx context.Context, runID string, symbols []string, lookback int) (*MarketResponse, error) {
	query := url.Values{}
	if len(symbols) > 0 {
		query.Set("symbols", strings.Join(symbols, ","))
	}
	if lookback > 0 {
		query.Set("lookback", fmt.Sprintf("%d", lookback))
	}
	var out MarketResponse
	if err := c.do(ctx, http.MethodGet, "/api/v1/runs/"+url.PathEscape(runID)+"/market", query, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *BotTradeClient) QueueTrade(ctx context.Context, runID string, order TradeOrder) (*QueuedOrder, error) {
	body := map[string]any{
		"symbol":          strings.ToUpper(strings.TrimSpace(order.Symbol)),
		"side":            strings.ToLower(strings.TrimSpace(order.Side)),
		"quantity":        order.Quantity,
		"idempotency_key": newID("trade"),
	}
	if order.Reasoning != "" {
		body["reasoning"] = order.Reasoning
	}
	var out QueuedTradeResult
	if err := c.do(ctx, http.MethodPost, "/api/v1/runs/"+url.PathEscape(runID)+"/trades", nil, body, &out); err != nil {
		return nil, err
	}
	return &out.Order, nil
}

func (c *BotTradeClient) StepRun(ctx context.Context, runID string, count int) (*StepResult, error) {
	if count < 1 {
		count = 1
	}
	body := map[string]any{
		"count":           count,
		"idempotency_key": newID("step"),
	}
	var out StepResult
	if err := c.do(ctx, http.MethodPost, "/api/v1/runs/"+url.PathEscape(runID)+"/step", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *BotTradeClient) SubmitTurn(ctx context.Context, runID string, orders []TradeOrder, stepCount int) (*SubmitTurnResult, error) {
	queued := make([]QueuedOrder, 0, len(orders))
	for _, order := range orders {
		o, err := c.QueueTrade(ctx, runID, order)
		if err != nil {
			return nil, err
		}
		queued = append(queued, *o)
	}
	step, err := c.StepRun(ctx, runID, stepCount)
	if err != nil {
		return nil, err
	}
	snapshot, _ := c.GetRun(ctx, runID)
	return &SubmitTurnResult{
		QueuedOrders: queued,
		Step:         *step,
		Snapshot:     snapshot,
	}, nil
}

func (c *BotTradeClient) GetResults(ctx context.Context, runID string) (*RunResults, error) {
	var out struct {
		Results RunResults `json:"results"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/runs/"+url.PathEscape(runID)+"/results", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out.Results, nil
}

func (c *BotTradeClient) PublishRun(ctx context.Context, runID string) (*RunResults, error) {
	var out struct {
		Published bool       `json:"published"`
		Results   RunResults `json:"results"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/runs/"+url.PathEscape(runID)+"/publish", nil, map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out.Results, nil
}

func (c *BotTradeClient) do(ctx context.Context, method, relPath string, query url.Values, body any, out any) error {
	if c.apiKey == "" && relPath != "/api/v1/scenarios" && !strings.HasPrefix(relPath, "/api/v1/scenarios/") {
		return fmt.Errorf("auth required")
	}

	u, err := url.Parse(c.baseURL)
	if err != nil {
		return err
	}
	u.Path = path.Join(u.Path, relPath)
	u.RawQuery = query.Encode()

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return apiError(resp.StatusCode, respBody)
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode BotTrade response: %w", err)
	}
	return nil
}

func apiError(status int, body []byte) error {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err == nil {
		for _, key := range []string{"detail", "error", "title"} {
			if v, ok := payload[key].(string); ok && v != "" {
				return fmt.Errorf("BotTrade API %d: %s", status, v)
			}
		}
	}
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = http.StatusText(status)
	}
	return fmt.Errorf("BotTrade API %d: %s", status, msg)
}

func newID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}
