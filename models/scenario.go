package models

import (
	"encoding/json"
	"time"
)

// Scenario is a frozen historical market window an external agent trades
// against. Universe / slippage are stored as JSON blobs so they survive
// without a join; the values aren't large.
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

// ScenarioVersion records each time a scenario's bars are frozen. Runs
// pin to a version so future re-freezes don't change historical results.
type ScenarioVersion struct {
	ScenarioID   string    `json:"scenario_id"`
	Version      int       `json:"version"`
	BarsFrozenAt time.Time `json:"bars_frozen_at"`
	BarCount     int       `json:"bar_count"`
}

// MarshalUniverse returns the JSON array form stored in scenarios.universe_json.
func MarshalUniverse(symbols []string) (string, error) {
	b, err := json.Marshal(symbols)
	return string(b), err
}

// MarshalSlippage returns the JSON object form stored in scenarios.slippage_json.
func MarshalSlippage(slippage map[string]int) (string, error) {
	b, err := json.Marshal(slippage)
	return string(b), err
}

// UnmarshalUniverse parses universe_json.
func UnmarshalUniverse(s string) ([]string, error) {
	var out []string
	err := json.Unmarshal([]byte(s), &out)
	return out, err
}

// UnmarshalSlippage parses slippage_json.
func UnmarshalSlippage(s string) (map[string]int, error) {
	out := map[string]int{}
	err := json.Unmarshal([]byte(s), &out)
	return out, err
}
