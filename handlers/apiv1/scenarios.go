package apiv1

import (
	"bottrade/database"
	"bottrade/models"
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// ScenarioListOutput is the payload of GET /api/v1/scenarios.
type ScenarioListOutput struct {
	Body struct {
		Scenarios []models.Scenario `json:"scenarios"`
	}
}

// ScenarioGetInput accepts either a scenario id (UUID) or a slug.
type ScenarioGetInput struct {
	ID string `path:"id" example:"tech-2024-q2" doc:"Scenario UUID or slug."`
}

// ScenarioGetOutput is the payload of GET /api/v1/scenarios/{id}.
type ScenarioGetOutput struct {
	Body struct {
		Scenario models.Scenario `json:"scenario"`
	}
}

// registerScenarios attaches the scenario routes to the huma API.
func (h *handlers) registerScenarios(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listScenarios",
		Method:      http.MethodGet,
		Path:        "/api/v1/scenarios",
		Summary:     "List available scenarios",
		Description: "Returns every scenario currently in status `ready` " +
			"or `archived`. Drafts (still being provisioned) are hidden.",
		Tags:     []string{"Scenarios"},
		Security: []map[string][]string{},
	}, h.listScenarios)

	huma.Register(api, huma.Operation{
		OperationID: "getScenario",
		Method:      http.MethodGet,
		Path:        "/api/v1/scenarios/{id}",
		Summary:     "Get one scenario by id or slug",
		Description: "Returns the full scenario including the universe, the " +
			"per-symbol slippage tiers, the leverage cap, and the date " +
			"window. The `id` path parameter accepts either the scenario's " +
			"UUID or its human-readable slug.",
		Tags:     []string{"Scenarios"},
		Security: []map[string][]string{},
	}, h.getScenario)
}

func (h *handlers) listScenarios(ctx context.Context, _ *struct{}) (*ScenarioListOutput, error) {
	rows, err := database.DB.Query(`
		SELECT id, slug, name, COALESCE(description,''), bar_resolution,
		       start_ts, end_ts, starting_cash, leverage_cap, short_enabled,
		       universe_json, slippage_json, benchmark_symbol, status,
		       current_version, created_at
		  FROM scenarios
		 WHERE status IN ('ready', 'archived')
		 ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, huma.Error500InternalServerError("db query failed: " + err.Error())
	}
	defer rows.Close()

	out := &ScenarioListOutput{}
	out.Body.Scenarios = []models.Scenario{}
	for rows.Next() {
		s, err := scanScenario(rows)
		if err != nil {
			return nil, huma.Error500InternalServerError("db scan failed: " + err.Error())
		}
		out.Body.Scenarios = append(out.Body.Scenarios, s)
	}
	return out, nil
}

func (h *handlers) getScenario(ctx context.Context, in *ScenarioGetInput) (*ScenarioGetOutput, error) {
	row := database.DB.QueryRow(`
		SELECT id, slug, name, COALESCE(description,''), bar_resolution,
		       start_ts, end_ts, starting_cash, leverage_cap, short_enabled,
		       universe_json, slippage_json, benchmark_symbol, status,
		       current_version, created_at
		  FROM scenarios
		 WHERE (id = ?1 OR slug = ?1)
		   AND status IN ('ready','archived')
	`, in.ID)
	s, err := scanScenario(row)
	if err != nil {
		return nil, huma.Error404NotFound("no scenario with id or slug " + in.ID)
	}
	out := &ScenarioGetOutput{}
	out.Body.Scenario = s
	return out, nil
}

// rowScanner is the intersection of *sql.Row and *sql.Rows. Lets us share
// the scan logic between list and get.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

// scanScenario reads one row out of either a sql.Row or sql.Rows.
func scanScenario(r rowScanner) (models.Scenario, error) {
	var s models.Scenario
	var startStr, endStr, createdStr, universeStr, slippageStr string
	var shortEnabled int
	if err := r.Scan(
		&s.ID, &s.Slug, &s.Name, &s.Description, &s.BarResolution,
		&startStr, &endStr, &s.StartingCash, &s.LeverageCap, &shortEnabled,
		&universeStr, &slippageStr, &s.BenchmarkSymbol, &s.Status,
		&s.CurrentVersion, &createdStr,
	); err != nil {
		return s, err
	}
	s.StartTs, _ = time.Parse(time.RFC3339, startStr)
	s.EndTs, _ = time.Parse(time.RFC3339, endStr)
	s.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
	s.ShortEnabled = shortEnabled != 0
	s.Universe, _ = models.UnmarshalUniverse(universeStr)
	s.SlippageBps, _ = models.UnmarshalSlippage(slippageStr)
	return s, nil
}
