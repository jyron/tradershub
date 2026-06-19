package apiv1

import (
	"bottrade/analytics"
	"bottrade/models"
	"bottrade/services"
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// TradeQueueInput queues a new market order on a run. The order fills on
// the next /step at the next bar's open + per-symbol slippage.
type TradeQueueInput struct {
	ID   string `path:"id" doc:"Run UUID."`
	Body struct {
		Symbol         string  `json:"symbol"   example:"AAPL" doc:"Symbol from the scenario universe."`
		Side           string  `json:"side"     enum:"buy,sell,short,cover" doc:"Trade direction. buy/sell are for long positions, short opens a short, cover reduces a short."`
		Quantity       float64 `json:"quantity" exclusiveMinimum:"0" doc:"Order quantity (positive). Fractional is allowed, e.g. 0.25 for crypto pairs like BTC/USD; equities are typically whole numbers."`
		Reasoning      string  `json:"reasoning,omitempty" doc:"Optional free-text note recorded with the fill."`
		IdempotencyKey string  `json:"idempotency_key,omitempty" doc:"If set, retries of this same key + body return the cached response; same key + different body returns 409."`
	}
}

// TradeQueueOutput is returned after a successful queue. The order is NOT
// filled yet; it fills on the next /step.
type TradeQueueOutput struct {
	Body struct {
		Order *models.RunOrder `json:"order"`
	}
}

// TradesListInput identifies a run whose filled trades should be listed.
type TradesListInput struct {
	ID string `path:"id" doc:"Run UUID."`
}

// TradesListOutput returns filled trades for the authenticated run owner.
type TradesListOutput struct {
	Body struct {
		Trades []models.RunTrade `json:"trades"`
	}
}

func (h *handlers) registerTrades(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "queueTrade",
		Method:      http.MethodPost,
		Path:        "/api/v1/runs/{id}/trades",
		Summary:     "Queue a market order for the next /step",
		Description: "Inserts an order into the run's queue. The order is " +
			"validated against the scenario universe, leverage cap, and " +
			"current position. It is NOT filled here; it fills at the next " +
			"bar's open + slippage when you call POST /api/v1/runs/{id}/step. " +
			"\n\nProvide `idempotency_key` to safely retry on network blips.",
		Tags:          []string{"Runs"},
		DefaultStatus: http.StatusCreated,
		Security:      []map[string][]string{{"ApiKeyAuth": {}}},
	}, h.queueTrade)

	huma.Register(api, huma.Operation{
		OperationID: "listTrades",
		Method:      http.MethodGet,
		Path:        "/api/v1/runs/{id}/trades",
		Summary:     "List filled trades for a run",
		Description: "Returns the immutable filled-trade records for a run owned by the authenticated account. " +
			"Useful for post-run attribution without publishing the run first.",
		Tags:     []string{"Runs"},
		Security: []map[string][]string{{"ApiKeyAuth": {}}},
	}, h.listTrades)
}

func (h *handlers) queueTrade(ctx context.Context, in *TradeQueueInput) (*TradeQueueOutput, error) {
	if err := h.assertRunOwner(ctx, in.ID); err != nil {
		return nil, err
	}
	key := apiKeyFrom(ctx)
	return idempotent(in.ID, in.Body.IdempotencyKey, in.Body, func() (*TradeQueueOutput, error) {
		order, err := h.Engine.QueueTrade(in.ID, services.QueueTradeRequest{
			Symbol:    in.Body.Symbol,
			Side:      in.Body.Side,
			Quantity:  in.Body.Quantity,
			Reasoning: in.Body.Reasoning,
		})
		if err != nil {
			if transient := transientDBError(err); transient != nil {
				return nil, transient
			}
			return nil, huma.Error400BadRequest(err.Error())
		}

		h.Analytics.Capture(key.AccountID.String(), "trade_queued", analytics.Props().
			Set("run_id", in.ID).
			Set("symbol", in.Body.Symbol).
			Set("side", in.Body.Side).
			Set("quantity", in.Body.Quantity))

		out := &TradeQueueOutput{}
		out.Body.Order = order
		return out, nil
	})
}

func (h *handlers) listTrades(ctx context.Context, in *TradesListInput) (*TradesListOutput, error) {
	if err := h.assertRunOwner(ctx, in.ID); err != nil {
		return nil, err
	}
	trades, err := loadRunTrades(in.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("db error: " + err.Error())
	}
	out := &TradesListOutput{}
	out.Body.Trades = trades
	return out, nil
}
