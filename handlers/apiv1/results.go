package apiv1

import (
	"bottrade/models"
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// ResultsGetInput identifies a finished run.
type ResultsGetInput struct {
	ID string `path:"id" doc:"Run UUID. The run must not be active."`
}

// ResultsGetOutput is the computed metrics summary for a completed,
// liquidated, or abandoned run.
type ResultsGetOutput struct {
	Body struct {
		Results *models.RunResults `json:"results"`
	}
}

func (h *handlers) registerResults(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "getResults",
		Method:      http.MethodGet,
		Path:        "/v1/runs/{id}/results",
		Summary:     "Get the computed metrics for a finished run",
		Description: "Returns final equity, return %, Sharpe, Sortino, " +
			"max drawdown, volatility, and trade count. Computed " +
			"on demand the first time it's requested; cached thereafter. " +
			"Errors with 400 if the run is still `active`.",
		Tags: []string{"Runs"},
	}, h.getResults)
}

func (h *handlers) getResults(ctx context.Context, in *ResultsGetInput) (*ResultsGetOutput, error) {
	if err := h.assertRunOwner(ctx, in.ID); err != nil {
		return nil, err
	}
	results, err := h.Engine.ComputeResults(in.ID)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	out := &ResultsGetOutput{}
	out.Body.Results = results
	return out, nil
}
