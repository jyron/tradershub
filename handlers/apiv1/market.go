package apiv1

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// GetMarket returns bars up to (and including) the run's sim_time for the
// requested symbols.
//   GET /v1/runs/:id/market?symbols=AAPL,MSFT&lookback=24
func (h *Handlers) GetMarket(c *fiber.Ctx) error {
	runID := c.Params("id")
	if err := h.assertRunOwner(c, runID); err != nil {
		return err
	}

	run, err := h.Engine.LoadRun(runID)
	if err != nil {
		return jsonError(c, fiber.StatusNotFound, "run_not_found", "no such run")
	}

	symbolsParam := c.Query("symbols")
	if symbolsParam == "" {
		return jsonError(c, fiber.StatusBadRequest, "missing_symbols", "symbols query param required (comma-separated)")
	}
	symbols := strings.Split(symbolsParam, ",")
	for i := range symbols {
		symbols[i] = strings.TrimSpace(symbols[i])
	}

	lookback := 50
	if lb := c.Query("lookback"); lb != "" {
		if n, err := strconv.Atoi(lb); err == nil && n > 0 {
			lookback = n
		}
	}
	// Hard cap to keep response size bounded.
	if lookback > 1000 {
		lookback = 1000
	}

	bars := h.Engine.Bars()
	out := map[string]interface{}{}
	for _, sym := range symbols {
		series := bars.Lookback(run.ScenarioID, run.ScenarioVersion, sym, run.SimTime, lookback)
		simple := make([]map[string]interface{}, 0, len(series))
		for _, b := range series {
			simple = append(simple, map[string]interface{}{
				"ts":     b.Ts.UTC().Format("2006-01-02T15:04:05Z"),
				"open":   b.Open,
				"high":   b.High,
				"low":    b.Low,
				"close":  b.Close,
				"volume": b.Volume,
			})
		}
		out[sym] = simple
	}
	return c.JSON(fiber.Map{
		"sim_time": run.SimTime.UTC().Format("2006-01-02T15:04:05Z"),
		"bars":     out,
	})
}
