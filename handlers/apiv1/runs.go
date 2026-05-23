package apiv1

import (
	"bottrade/models"
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// RunCreateInput accepts EITHER a scenario_id (UUID) or a scenario_slug.
// Exactly one must be provided; if both are set, scenario_id wins.
type RunCreateInput struct {
	Body struct {
		ScenarioID   string `json:"scenario_id,omitempty" doc:"Scenario UUID. Provide this OR scenario_slug."`
		ScenarioSlug string `json:"scenario_slug,omitempty" doc:"Scenario slug. Used if scenario_id is omitted."`
	}
}

// RunCreateOutput is returned on successful POST /v1/runs.
type RunCreateOutput struct {
	Status int        `header:"-"`
	Body   struct {
		Run *models.Run `json:"run"`
	}
}

// RunGetInput identifies a run by id (UUID).
type RunGetInput struct {
	ID string `path:"id" doc:"Run UUID."`
}

// RunGetOutput is the full snapshot of an in-flight or finished run.
type RunGetOutput struct {
	Body models.RunSnapshot `json:"-"`
}

func (h *handlers) registerRuns(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "createRun",
		Method:      http.MethodPost,
		Path:        "/api/v1/runs",
		Summary:     "Start a new run on a scenario",
		Description: "Creates a run pinned to the scenario's current_version. " +
			"sim_time starts at the first bar in the scenario timeline. " +
			"Provide either scenario_id or scenario_slug.",
		Tags:          []string{"Runs"},
		DefaultStatus: http.StatusCreated,
	}, h.createRun)

	huma.Register(api, huma.Operation{
		OperationID: "getRun",
		Method:      http.MethodGet,
		Path:        "/api/v1/runs/{id}",
		Summary:     "Get the current state of a run",
		Description: "Returns the run, all open positions, all queued (unfilled) " +
			"orders, and the most recent equity sample. Only the run's own " +
			"bot can access this endpoint.",
		Tags: []string{"Runs"},
	}, h.getRun)
}

func (h *handlers) createRun(ctx context.Context, in *RunCreateInput) (*RunCreateOutput, error) {
	bot := botFrom(ctx)
	scenarioID := in.Body.ScenarioID
	if scenarioID == "" && in.Body.ScenarioSlug != "" {
		// Resolve slug → id.
		if err := h.Engine.AppDB().QueryRow(
			`SELECT id FROM scenarios WHERE slug = ?1`, in.Body.ScenarioSlug,
		).Scan(&scenarioID); err != nil {
			return nil, huma.Error404NotFound("no scenario with slug " + in.Body.ScenarioSlug)
		}
	}
	if scenarioID == "" {
		return nil, huma.Error400BadRequest("scenario_id or scenario_slug required")
	}
	run, err := h.Engine.StartRun(bot.ID.String(), scenarioID)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	out := &RunCreateOutput{Status: http.StatusCreated}
	out.Body.Run = run
	return out, nil
}

func (h *handlers) getRun(ctx context.Context, in *RunGetInput) (*RunGetOutput, error) {
	if err := h.assertRunOwner(ctx, in.ID); err != nil {
		return nil, err
	}
	snap, err := h.Engine.GetRunState(in.ID)
	if err != nil {
		return nil, huma.Error404NotFound("no such run")
	}
	return &RunGetOutput{Body: *snap}, nil
}

// assertRunOwner returns nil iff the authenticated bot owns the run.
// Returned errors are already huma errors with the correct status — callers
// should propagate as-is.
func (h *handlers) assertRunOwner(ctx context.Context, runID string) error {
	bot := botFrom(ctx)
	var ownerID string
	err := h.Engine.AppDB().QueryRow(
		`SELECT bot_id FROM runs WHERE id = ?1`, runID,
	).Scan(&ownerID)
	if err != nil {
		return huma.Error404NotFound("no such run")
	}
	if ownerID != bot.ID.String() {
		return huma.Error403Forbidden("you do not own this run")
	}
	return nil
}
