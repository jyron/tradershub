package apiv1

import (
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
		Symbol         string `json:"symbol"   example:"AAPL" doc:"Symbol from the scenario universe."`
		Side           string `json:"side"     enum:"buy,sell,short,cover" doc:"Trade direction. buy/sell are for long positions, short opens a short, cover reduces a short."`
		Quantity       int    `json:"quantity" minimum:"1" doc:"Whole-share quantity (positive)."`
		Reasoning      string `json:"reasoning,omitempty" doc:"Optional free-text note recorded with the fill."`
		IdempotencyKey string `json:"idempotency_key,omitempty" doc:"If set, retries of this same key + body return the cached response; same key + different body returns 409."`
	}
}

// TradeQueueOutput is returned after a successful queue. The order is NOT
// filled yet; it fills on the next /step.
type TradeQueueOutput struct {
	Body struct {
		Order *models.RunOrder `json:"order"`
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
			"bar's open + slippage when you call POST /v1/runs/{id}/step. " +
			"\n\nProvide `idempotency_key` to safely retry on network blips.",
		Tags:          []string{"Runs"},
		DefaultStatus: http.StatusCreated,
	}, h.queueTrade)
}

func (h *handlers) queueTrade(ctx context.Context, in *TradeQueueInput) (*TradeQueueOutput, error) {
	if err := h.assertRunOwner(ctx, in.ID); err != nil {
		return nil, err
	}
	return idempotent(in.ID, in.Body.IdempotencyKey, in.Body, func() (*TradeQueueOutput, error) {
		order, err := h.Engine.QueueTrade(in.ID, services.QueueTradeRequest{
			Symbol:    in.Body.Symbol,
			Side:      in.Body.Side,
			Quantity:  in.Body.Quantity,
			Reasoning: in.Body.Reasoning,
		})
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		out := &TradeQueueOutput{}
		out.Body.Order = order
		return out, nil
	})
}
