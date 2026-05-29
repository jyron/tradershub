package apiv1

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// MarketGetInput is the query for GET /api/v1/runs/{id}/market.
type MarketGetInput struct {
	ID       string `path:"id" doc:"Run UUID."`
	Symbols  string `query:"symbols" doc:"Comma-separated list of symbols, e.g. AAPL,MSFT,SPY. Omit to return all symbols in the scenario universe."`
	Lookback int    `query:"lookback" default:"50" minimum:"1" maximum:"1000" doc:"Number of most-recent bars per symbol up to (and including) sim_time."`
}

// MarketBar is one OHLCV bar in the response.
type MarketBar struct {
	Ts     string  `json:"ts"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

// MarketGetOutput is the payload of GET /api/v1/runs/{id}/market.
type MarketGetOutput struct {
	Body struct {
		SimTime string                 `json:"sim_time" doc:"Current run sim_time (ISO 8601 UTC)."`
		Bars    map[string][]MarketBar `json:"bars"   doc:"Map of symbol → ordered ascending bars."`
	}
}

func (h *handlers) registerMarket(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "getRunMarket",
		Method:      http.MethodGet,
		Path:        "/api/v1/runs/{id}/market",
		Summary:     "Get market data visible to this run",
		Description: "Returns the most recent N bars per requested symbol, " +
			"only including bars with timestamp ≤ run.sim_time. " +
			"This is how the agent observes price history without ever " +
			"seeing the future.",
		Tags:     []string{"Market"},
		Security: []map[string][]string{{"ApiKeyAuth": {}}},
	}, h.getMarket)
}

func (h *handlers) getMarket(ctx context.Context, in *MarketGetInput) (*MarketGetOutput, error) {
	if err := h.assertRunOwner(ctx, in.ID); err != nil {
		return nil, err
	}
	run, err := h.Engine.LoadRun(in.ID)
	if err != nil {
		return nil, huma.Error404NotFound("no such run")
	}

	scen, err := h.Engine.LoadScenario(run.ScenarioID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to load scenario")
	}
	bars := h.Engine.Bars()
	if err := bars.Load(run.ScenarioID, run.ScenarioVersion); err != nil {
		return nil, huma.Error500InternalServerError("failed to load market data")
	}

	var symbols []string
	if in.Symbols == "" {
		symbols = scen.Universe
	} else {
		for _, symbol := range strings.Split(in.Symbols, ",") {
			if symbol = strings.TrimSpace(symbol); symbol != "" {
				symbols = append(symbols, symbol)
			}
		}
	}

	out := &MarketGetOutput{}
	out.Body.SimTime = run.SimTime.UTC().Format("2006-01-02T15:04:05Z")
	out.Body.Bars = map[string][]MarketBar{}
	for _, sym := range symbols {
		series := bars.Lookback(run.ScenarioID, run.ScenarioVersion, sym, run.SimTime, in.Lookback)
		mb := make([]MarketBar, 0, len(series))
		for _, b := range series {
			mb = append(mb, MarketBar{
				Ts:     b.Ts.UTC().Format("2006-01-02T15:04:05Z"),
				Open:   b.Open,
				High:   b.High,
				Low:    b.Low,
				Close:  b.Close,
				Volume: b.Volume,
			})
		}
		out.Body.Bars[sym] = mb
	}
	return out, nil
}
