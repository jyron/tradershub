package jobs

import (
	"bottrade/database"
	"bottrade/services"
	"log"
	"time"

	"github.com/google/uuid"
)

// PortfolioSnapshotJob writes a row to portfolio_snapshots for every active
// bot. The resulting time series is what feeds Sharpe / Sortino / drawdown on
// the leaderboard.
type PortfolioSnapshotJob struct {
	portfolio *services.PortfolioService
}

func NewPortfolioSnapshotJob() *PortfolioSnapshotJob {
	return &PortfolioSnapshotJob{
		portfolio: services.NewPortfolioService(),
	}
}

func (j *PortfolioSnapshotJob) Name() string {
	return "PortfolioSnapshot"
}

// Interval is intentionally short (1h) so that newly-created bots accumulate
// enough datapoints for risk-adjusted metrics within a day or two of running.
func (j *PortfolioSnapshotJob) Interval() time.Duration {
	return time.Hour
}

func (j *PortfolioSnapshotJob) Run() error {
	rows, err := database.DB.Query(`SELECT id FROM bots WHERE is_active = 1`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var botIDs []uuid.UUID
	for rows.Next() {
		var idStr string
		if err := rows.Scan(&idStr); err != nil {
			continue
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}
		botIDs = append(botIDs, id)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	saved := 0
	for _, botID := range botIDs {
		portfolio, err := j.portfolio.GetPortfolio(botID)
		if err != nil {
			continue
		}
		positionsValue := portfolio.TotalValue - portfolio.CashBalance
		_, err = database.DB.Exec(
			`INSERT INTO portfolio_snapshots
			 (id, bot_id, total_value, cash_balance, positions_value, daily_pnl, total_pnl, snapshot_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			uuid.NewString(),
			botID.String(),
			portfolio.TotalValue,
			portfolio.CashBalance,
			positionsValue,
			0.0,
			portfolio.TotalPnL,
			now,
		)
		if err != nil {
			log.Printf("PortfolioSnapshot: failed for bot %s: %v", botID, err)
			continue
		}
		saved++
	}
	log.Printf("PortfolioSnapshot: wrote %d/%d", saved, len(botIDs))
	return nil
}
