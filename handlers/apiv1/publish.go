package apiv1

import (
	"bottrade/models"
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// PublishInput identifies which run to publish.
type PublishInput struct {
	ID string `path:"id" doc:"Run UUID. The run must not be active."`
}

// PublishOutput confirms the publication and returns the metrics that were
// pushed onto the leaderboard.
type PublishOutput struct {
	Body struct {
		Published bool               `json:"published"`
		Results   *models.RunResults `json:"results"`
	}
}

func (h *handlers) registerPublish(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "publish",
		Method:      http.MethodPost,
		Path:        "/api/v1/runs/{id}/publish",
		Summary:     "Publish a run to the public leaderboard",
		Description: "Computes results if not yet computed, then upserts a row " +
			"into the per-scenario leaderboard. Re-publishing the same " +
			"run is a no-op-update of the leaderboard row.",
		Tags:     []string{"Runs"},
		Security: []map[string][]string{{"ApiKeyAuth": {}}},
	}, h.publish)
}

func (h *handlers) publish(ctx context.Context, in *PublishInput) (*PublishOutput, error) {
	if err := h.assertRunOwner(ctx, in.ID); err != nil {
		return nil, err
	}
	results, err := h.Engine.ComputeResults(in.ID)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	var scenarioID, apiKeyID, botName string
	if err := h.Engine.AppDB().QueryRow(
		`SELECT scenario_id, api_key_id, COALESCE(bot_name,'') FROM runs WHERE id = ?1`, in.ID,
	).Scan(&scenarioID, &apiKeyID, &botName); err != nil {
		return nil, huma.Error500InternalServerError("db error: " + err.Error())
	}

	var sharpe interface{}
	if results.Sharpe != nil {
		sharpe = *results.Sharpe
	}
	if _, err := h.Engine.AppDB().Exec(`
		INSERT INTO run_leaderboard (scenario_id, run_id, api_key_id, bot_name, return_pct, sharpe)
		VALUES (?1, ?2, ?3, ?4, ?5, ?6)
		ON CONFLICT (scenario_id, run_id) DO UPDATE SET
			api_key_id = excluded.api_key_id,
			bot_name = excluded.bot_name,
			return_pct = excluded.return_pct,
			sharpe = excluded.sharpe,
			published_at = CURRENT_TIMESTAMP
	`, scenarioID, in.ID, apiKeyID, botName, results.ReturnPct, sharpe); err != nil {
		return nil, huma.Error500InternalServerError("leaderboard insert failed: " + err.Error())
	}
	if _, err := h.Engine.AppDB().Exec(
		`UPDATE runs SET published = 1 WHERE id = ?1`, in.ID,
	); err != nil {
		return nil, huma.Error500InternalServerError("db error: " + err.Error())
	}

	out := &PublishOutput{}
	out.Body.Published = true
	out.Body.Results = results
	return out, nil
}
