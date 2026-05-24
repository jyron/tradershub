package apiv1

import (
	"bottrade/database"
	"bottrade/models"
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// quotaFreeLimitError is returned (as HTTP 402) when a free-tier bot has
// exhausted its 25-run monthly allowance. Its JSON shape matches the spec:
//
//	{ "error": "...", "runs_used": N, "runs_limit": 25,
//	  "checkout_url": "...", "upgrade_hint": "..." }
type quotaFreeLimitError struct {
	Err         string `json:"error"`
	RunsUsed    int    `json:"runs_used"`
	RunsLimit   int    `json:"runs_limit"`
	CheckoutURL string `json:"checkout_url"`
	UpgradeHint string `json:"upgrade_hint"`
}

func (e *quotaFreeLimitError) Error() string    { return e.Err }
func (e *quotaFreeLimitError) GetStatus() int   { return http.StatusPaymentRequired }

// quotaProLimitError is returned (as HTTP 429) when a pro-tier bot has
// exhausted its 500-run monthly allowance. Its JSON shape matches the spec:
//
//	{ "error": "...", "runs_used": N, "runs_limit": 500, "resets_at": "..." }
type quotaProLimitError struct {
	Err       string `json:"error"`
	RunsUsed  int    `json:"runs_used"`
	RunsLimit int    `json:"runs_limit"`
	ResetsAt  string `json:"resets_at"`
}

func (e *quotaProLimitError) Error() string  { return e.Err }
func (e *quotaProLimitError) GetStatus() int { return http.StatusTooManyRequests }

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
		Security:      []map[string][]string{{"ApiKeyAuth": {}}},
	}, h.createRun)

	huma.Register(api, huma.Operation{
		OperationID: "getRun",
		Method:      http.MethodGet,
		Path:        "/api/v1/runs/{id}",
		Summary:     "Get the current state of a run",
		Description: "Returns the run, all open positions, all queued (unfilled) " +
			"orders, and the most recent equity sample. Only the run's own " +
			"bot can access this endpoint.",
		Tags:     []string{"Runs"},
		Security: []map[string][]string{{"ApiKeyAuth": {}}},
	}, h.getRun)
}

func (h *handlers) createRun(ctx context.Context, in *RunCreateInput) (*RunCreateOutput, error) {
	bot := botFrom(ctx)

	// Enforce monthly run quotas before creating the run.
	if err := h.enforceRunQuota(bot); err != nil {
		return nil, err
	}

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

// enforceRunQuota checks the bot's monthly run count against its tier limit.
// Returns a huma error if the quota is exceeded, nil otherwise.
func (h *handlers) enforceRunQuota(bot models.Bot) error {
	// Count runs this UTC calendar month.
	var runsUsed int
	err := database.DB.QueryRow(
		`SELECT COUNT(*) FROM runs
		  WHERE bot_id = ?1
		    AND created_at >= datetime('now', 'start of month')`,
		bot.ID.String(),
	).Scan(&runsUsed)
	if err != nil && err != sql.ErrNoRows {
		// Non-fatal: let the run proceed if we can't count.
		return nil
	}

	// Determine effective tier.
	isPro := false
	if bot.AccountID != "" {
		var subStatus string
		_ = database.DB.QueryRow(
			`SELECT COALESCE(subscription_status,'') FROM accounts WHERE id = ?1`,
			bot.AccountID,
		).Scan(&subStatus)
		isPro = subStatus == "active" || subStatus == "past_due"
	}

	if isPro {
		const limit = 500
		if runsUsed >= limit {
			now := time.Now().UTC()
			nextMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
			return &quotaProLimitError{
				Err:       "Monthly run limit reached",
				RunsUsed:  runsUsed,
				RunsLimit: limit,
				ResetsAt:  nextMonth.Format(time.RFC3339),
			}
		}
	} else {
		const limit = 25
		if runsUsed >= limit {
			return &quotaFreeLimitError{
				Err:         "Free tier limit reached",
				RunsUsed:    runsUsed,
				RunsLimit:   limit,
				CheckoutURL: "<call POST /api/v1/billing/checkout to get one>",
				UpgradeHint: "POST /api/v1/billing/checkout",
			}
		}
	}

	return nil
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
