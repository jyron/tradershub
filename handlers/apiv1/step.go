package apiv1

import (
	"bottrade/analytics"
	"bottrade/services"
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// StepInput advances the run's sim_time by N bars. Each bar fills any
// queued orders, marks-to-market, and may trigger force-liquidation.
type StepInput struct {
	ID   string `path:"id" doc:"Run UUID."`
	Body struct {
		Count          int    `json:"count,omitempty" minimum:"1" default:"1" doc:"Number of bars to advance. If a liquidation occurs mid-step, the remaining bars are skipped."`
		IdempotencyKey string `json:"idempotency_key,omitempty" doc:"If set, retries return the cached response."`
	}
}

// StepOutput summarizes what happened during the step.
type StepOutput struct {
	Body services.StepResult `json:"-"`
}

func (h *handlers) registerStep(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "step",
		Method:      http.MethodPost,
		Path:        "/api/v1/runs/{id}/step",
		Summary:     "Advance the run by N bars",
		Description: "Iterates the simulator forward `count` bars. For each bar: " +
			"(1) any queued orders fill at that bar's open ± per-symbol " +
			"slippage; (2) positions are upserted (signed quantity); " +
			"(3) equity is mark-to-market at the bar's close; (4) if " +
			"equity falls below the maintenance margin, ALL positions are " +
			"force-closed and the run's status flips to `liquidated`. " +
			"Returns the fills that landed, the new equity, and whether " +
			"the run is now `done` (scenario exhausted) or `liquidated`.",
		Tags:     []string{"Runs"},
		Security: []map[string][]string{{"ApiKeyAuth": {}}},
	}, h.step)
}

func (h *handlers) step(ctx context.Context, in *StepInput) (*StepOutput, error) {
	if err := h.assertRunOwner(ctx, in.ID); err != nil {
		return nil, err
	}
	key := apiKeyFrom(ctx)
	count := in.Body.Count
	if count == 0 {
		count = 1
	}
	return idempotent(in.ID, in.Body.IdempotencyKey, in.Body, func() (*StepOutput, error) {
		result, err := h.Engine.AdvanceStep(in.ID, count)
		if err != nil {
			if transient := transientDBError(err); transient != nil {
				return nil, transient
			}
			return nil, huma.Error400BadRequest(err.Error())
		}

		h.Analytics.Capture(key.AccountID.String(), "run_stepped", analytics.Props().
			Set("run_id", in.ID).
			Set("bars_advanced", result.BarsAdvanced).
			Set("done", result.Done).
			Set("liquidated", result.Liquidated))
		if result.Done || result.Liquidated {
			h.Analytics.Capture(key.AccountID.String(), "run_completed", analytics.Props().
				Set("run_id", in.ID).
				Set("liquidated", result.Liquidated).
				Set("final_equity", result.NewEquity))
		}

		return &StepOutput{Body: *result}, nil
	})
}
