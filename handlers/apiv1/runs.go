package apiv1

import (
	"bottrade/analytics"
	"bottrade/database"
	"bottrade/models"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// quotaUpgradeError is returned (as HTTP 402 Payment Required) when a free or
// pro account has exhausted its monthly allowance and a bigger plan exists.
// Its JSON shape:
//
//	{ "error": "...", "runs_used": N, "runs_limit": N, "resets_at": "...",
//	  "checkout_url": "...", "upgrade_hint": "..." }
type quotaUpgradeError struct {
	Err         string `json:"error"`
	RunsUsed    int    `json:"runs_used"`
	RunsLimit   int    `json:"runs_limit"`
	ResetsAt    string `json:"resets_at"`
	CheckoutURL string `json:"checkout_url"`
	UpgradeHint string `json:"upgrade_hint"`
}

func (e *quotaUpgradeError) Error() string  { return e.Err }
func (e *quotaUpgradeError) GetStatus() int { return http.StatusPaymentRequired }

// quotaTopLimitError is returned (as HTTP 429) when a max-tier account has
// exhausted its 1000-run monthly allowance — there is no bigger plan.
//
//	{ "error": "...", "runs_used": N, "runs_limit": 1000, "resets_at": "..." }
type quotaTopLimitError struct {
	Err       string `json:"error"`
	RunsUsed  int    `json:"runs_used"`
	RunsLimit int    `json:"runs_limit"`
	ResetsAt  string `json:"resets_at"`
}

func (e *quotaTopLimitError) Error() string  { return e.Err }
func (e *quotaTopLimitError) GetStatus() int { return http.StatusTooManyRequests }

// planRunLimit is the monthly run allowance per plan.
func planRunLimit(plan string) int {
	switch plan {
	case "max":
		return 1000
	case "pro":
		return 200
	default:
		return 25
	}
}

// RunCreateInput accepts EITHER a scenario_id (UUID) or a scenario_slug.
// Exactly one must be provided; if both are set, scenario_id wins.
type RunCreateInput struct {
	Body struct {
		ScenarioID   string            `json:"scenario_id,omitempty" doc:"Scenario UUID. Provide this OR scenario_slug."`
		ScenarioSlug string            `json:"scenario_slug,omitempty" doc:"Scenario slug. Used if scenario_id is omitted."`
		BotName      string            `json:"bot_name,omitempty" doc:"Optional display name for the bot, strategy, or experiment creating this run."`
		AgentInfo    *models.AgentInfo `json:"agent_info,omitempty" doc:"Structured agent name, framework, model, version, source, and configuration."`
	}
}

// RunCreateOutput is returned on successful POST /api/v1/runs.
type RunCreateOutput struct {
	Status int `header:"-"`
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
			"orders, and the most recent equity sample. Only the account " +
			"that created the run can access this endpoint.",
		Tags:     []string{"Runs"},
		Security: []map[string][]string{{"ApiKeyAuth": {}}},
	}, h.getRun)
}

func (h *handlers) createRun(ctx context.Context, in *RunCreateInput) (*RunCreateOutput, error) {
	key := apiKeyFrom(ctx)

	// Enforce monthly run quotas before creating the run.
	if err := h.enforceRunQuota(key); err != nil {
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
	if in.Body.AgentInfo != nil && in.Body.AgentInfo.Name == "" {
		return nil, huma.Error400BadRequest("agent_info.name is required")
	}
	if in.Body.AgentInfo != nil {
		encoded, err := json.Marshal(in.Body.AgentInfo)
		if err != nil || len(encoded) > 8192 {
			return nil, huma.Error400BadRequest("agent_info must be valid JSON up to 8 KB")
		}
		if len(in.Body.AgentInfo.Name) > 120 || len(in.Body.AgentInfo.Framework) > 120 ||
			len(in.Body.AgentInfo.Model) > 200 || len(in.Body.AgentInfo.Version) > 120 ||
			len(in.Body.AgentInfo.SourceURL) > 500 || len(in.Body.AgentInfo.SourceRevision) > 200 {
			return nil, huma.Error400BadRequest("agent_info contains a field that is too long")
		}
	}
	agentFramework := ""
	if in.Body.AgentInfo != nil {
		agentFramework = in.Body.AgentInfo.Framework
	}
	run, err := h.Engine.StartRunWithAgentInfo(
		key.ID.String(), scenarioID, in.Body.BotName, in.Body.AgentInfo,
	)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	h.Analytics.Capture(key.AccountID.String(), "run_started", analytics.Props().
		Set("scenario_id", scenarioID).
		Set("scenario_slug", in.Body.ScenarioSlug).
		Set("bot_name", in.Body.BotName).
		Set("agent_framework", agentFramework).
		Set("plan", key.Plan))

	out := &RunCreateOutput{Status: http.StatusCreated}
	out.Body.Run = run
	return out, nil
}

// enforceRunQuota checks the account's monthly run count against its plan limit.
// Returns a huma error if the quota is exceeded, nil otherwise.
func (h *handlers) enforceRunQuota(key models.APIKey) error {
	// Count runs this UTC calendar month.
	var runsUsed int
	err := database.DB.QueryRow(
		`SELECT COUNT(*)
		   FROM runs r
		   JOIN api_keys k ON k.id = r.api_key_id
		  WHERE COALESCE(k.account_id, k.id) = ?1
		    AND r.created_at >= datetime('now', 'start of month')`,
		key.AccountID.String(),
	).Scan(&runsUsed)
	if err != nil && err != sql.ErrNoRows {
		// Non-fatal: let the run proceed if we can't count.
		return nil
	}

	plan := key.Plan
	if plan != "pro" && plan != "max" {
		plan = "free"
	}
	limit := planRunLimit(plan)
	if runsUsed < limit {
		return nil
	}

	now := time.Now().UTC()
	nextMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	h.Analytics.Capture(key.AccountID.String(), "run_quota_exceeded", analytics.Props().
		Set("plan", plan).
		Set("runs_used", runsUsed).
		Set("runs_limit", limit))

	if plan == "max" {
		return &quotaTopLimitError{
			Err:       "Monthly run limit reached",
			RunsUsed:  runsUsed,
			RunsLimit: limit,
			ResetsAt:  nextMonth.Format(time.RFC3339),
		}
	}

	// A bigger plan exists: 402 Payment Required with the upgrade path.
	next, nextRuns, nextPrice := "pro", 200, "$19.99/mo"
	if plan == "pro" {
		next, nextRuns, nextPrice = "max", 1000, "$79.99/mo"
	}
	h.sendQuotaUpgradeEmail(key, runsUsed, limit, nextMonth)
	return &quotaUpgradeError{
		Err:         "Monthly run limit reached",
		RunsUsed:    runsUsed,
		RunsLimit:   limit,
		ResetsAt:    nextMonth.Format(time.RFC3339),
		CheckoutURL: h.AppBaseURL + "/pricing",
		UpgradeHint: fmt.Sprintf("POST /api/v1/billing/checkout?plan=%s — %s: %d runs/month for %s", next, next, nextRuns, nextPrice),
	}
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

// assertRunOwner returns nil iff the authenticated account owns the run.
// Returned errors are already huma errors with the correct status — callers
// should propagate as-is.
func (h *handlers) assertRunOwner(ctx context.Context, runID string) error {
	key := apiKeyFrom(ctx)
	var ownerAccountID string
	err := h.Engine.AppDB().QueryRow(
		`SELECT COALESCE(k.account_id, k.id)
		   FROM runs r
		   JOIN api_keys k ON k.id = r.api_key_id
		  WHERE r.id = ?1`,
		runID,
	).Scan(&ownerAccountID)
	if err != nil {
		return huma.Error404NotFound("no such run")
	}
	if ownerAccountID != key.AccountID.String() {
		return huma.Error403Forbidden("you do not own this run")
	}
	return nil
}
