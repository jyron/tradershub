package jobs

import (
	"bottrade/database"
	"bottrade/services"
	"fmt"
	"log"
	"strings"
	"time"
)

// BarIngestJob fetches the most recent hour of bars for every symbol in
// services.BenchmarkUniverse and upserts them into market.bars. The ingester
// is the only writer to that table; it's intentionally narrow.
//
// Re-pulling Alpaca for already-known bars is a no-op (PK conflict → skipped),
// so running this on a coarse interval is safe and self-healing.
type BarIngestJob struct{}

func NewBarIngestJob() *BarIngestJob {
	return &BarIngestJob{}
}

func (j *BarIngestJob) Name() string {
	return "BarIngest"
}

func (j *BarIngestJob) Interval() time.Duration {
	// Hourly: scenario_bars are frozen ahead of time anyway. This job's
	// only purpose is keeping the rolling working set fresh in case
	// future scenarios want to extend their window.
	return 1 * time.Hour
}

func (j *BarIngestJob) Run() error {
	if database.MarketDB == nil {
		return fmt.Errorf("market DB not initialized")
	}
	ac := services.GetAlpacaClient()
	if ac == nil {
		return fmt.Errorf("alpaca client not initialized")
	}

	// Pull the last 36 hours so we cover the prior trading session even
	// if the ticker fired during a long weekend. Alpaca returns nothing
	// for hours outside RTH, which is fine.
	end := time.Now().UTC().Add(-15 * time.Minute) // dodge free-tier SIP block
	start := end.Add(-36 * time.Hour)

	total := 0
	for _, symbol := range services.BenchmarkUniverse {
		bars, err := ac.GetHistoricalCandles(symbol, "1Hour", start, end)
		if err != nil {
			log.Printf("BarIngest: %s: %v", symbol, err)
			continue
		}
		n, err := UpsertBars(symbol, bars)
		if err != nil {
			log.Printf("BarIngest: %s: upsert failed: %v", symbol, err)
			continue
		}
		total += n
	}
	if total > 0 {
		log.Printf("BarIngest: upserted %d bars across %d symbols", total, len(services.BenchmarkUniverse))
	}
	return nil
}

// upsertBatchSize is rows per multi-VALUES INSERT. 50 × 7 columns = 350
// bound parameters, well under SQLite's default SQLITE_MAX_VARIABLE_NUMBER
// (999) and libsql's per-statement budget. Reducing round-trips by ~50x
// versus one-exec-per-row is the difference between a full-year backfill
// taking ~4 hours vs ~5 minutes against remote Turso.
const upsertBatchSize = 50

// UpsertBars writes the given candles into market.bars idempotently. Exposed
// (capitalized) so the backfill CLI can call it directly without going
// through the scheduled job path.
func UpsertBars(symbol string, bars []services.Candle) (int, error) {
	if len(bars) == 0 {
		return 0, nil
	}
	tx, err := database.MarketDB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	for offset := 0; offset < len(bars); offset += upsertBatchSize {
		end := offset + upsertBatchSize
		if end > len(bars) {
			end = len(bars)
		}
		chunk := bars[offset:end]

		placeholders := make([]string, len(chunk))
		args := make([]interface{}, 0, len(chunk)*7)
		for i, b := range chunk {
			placeholders[i] = "(?,?,?,?,?,?,?)"
			args = append(args,
				symbol,
				b.Timestamp.UTC().Format(time.RFC3339),
				b.Open, b.High, b.Low, b.Close, b.Volume,
			)
		}
		query := `INSERT INTO bars (symbol, ts, open, high, low, close, volume) VALUES ` +
			strings.Join(placeholders, ",") +
			` ON CONFLICT(symbol, ts) DO UPDATE SET
				open = excluded.open, high = excluded.high, low = excluded.low,
				close = excluded.close, volume = excluded.volume,
				ingested_at = CURRENT_TIMESTAMP`

		if _, err := tx.Exec(query, args...); err != nil {
			return 0, fmt.Errorf("batched upsert %s offset %d: %w", symbol, offset, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return len(bars), nil
}
