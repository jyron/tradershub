package services

import (
	"fmt"
	"time"

	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
)

type Candle struct {
	Timestamp time.Time `json:"timestamp"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	// Volume is float64 so crypto bars (fractional coin volume, e.g. 12.3456 BTC)
	// survive without truncation. Equity volumes are whole but fit fine.
	Volume float64 `json:"volume"`
}

// parseTimeFrame maps our string resolution (e.g. "1Hour", "5Min") to an
// Alpaca TimeFrame. Shared by the stock and crypto fetchers. Unknown values
// fall back to daily.
func parseTimeFrame(timeframe string) marketdata.TimeFrame {
	switch timeframe {
	case "1Min":
		return marketdata.OneMin
	case "5Min":
		return marketdata.NewTimeFrame(5, marketdata.Min)
	case "15Min":
		return marketdata.NewTimeFrame(15, marketdata.Min)
	case "1Hour":
		return marketdata.OneHour
	case "1Day":
		return marketdata.OneDay
	default:
		return marketdata.OneDay
	}
}

// GetHistoricalCandles fetches OHLC candle data for an equity symbol from Alpaca.
func (ac *AlpacaClient) GetHistoricalCandles(symbol string, timeframe string, start time.Time, end time.Time) ([]Candle, error) {
	if ac == nil || ac.marketData == nil {
		return nil, fmt.Errorf("Alpaca client not initialized")
	}

	// Fetch bars from Alpaca. Adjustment MUST be split: raw bars ship stock
	// splits as giant overnight "crashes" (NVDA 10:1 read as -90% and
	// margin-called every agent holding it, 2026-07 incident).
	bars, err := ac.marketData.GetBars(symbol, marketdata.GetBarsRequest{
		TimeFrame:  parseTimeFrame(timeframe),
		Start:      start,
		End:        end,
		Feed:       marketdata.IEX, // Use IEX feed for free tier
		Adjustment: marketdata.Split,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to fetch bars from Alpaca: %w", err)
	}

	if len(bars) == 0 {
		return []Candle{}, nil
	}

	candles := make([]Candle, 0, len(bars))
	for _, bar := range bars {
		candles = append(candles, Candle{
			Timestamp: bar.Timestamp,
			Open:      bar.Open,
			High:      bar.High,
			Low:       bar.Low,
			Close:     bar.Close,
			Volume:    float64(bar.Volume),
		})
	}

	return candles, nil
}

// GetHistoricalCryptoCandles fetches OHLCV candles for a crypto pair (e.g.
// "BTC/USD") from Alpaca's crypto feed. Crypto is served continuously (24/7)
// from a free, separate feed — there is no IEX/SIP feed split and no real-time
// delay to dodge, so this is a plain range pull. CryptoBar.Volume is already
// float64, so fractional coin volume is preserved.
func (ac *AlpacaClient) GetHistoricalCryptoCandles(symbol string, timeframe string, start time.Time, end time.Time) ([]Candle, error) {
	if ac == nil || ac.marketData == nil {
		return nil, fmt.Errorf("Alpaca client not initialized")
	}

	bars, err := ac.marketData.GetCryptoBars(symbol, marketdata.GetCryptoBarsRequest{
		TimeFrame: parseTimeFrame(timeframe),
		Start:     start,
		End:       end,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch crypto bars from Alpaca: %w", err)
	}

	if len(bars) == 0 {
		return []Candle{}, nil
	}

	candles := make([]Candle, 0, len(bars))
	for _, bar := range bars {
		candles = append(candles, Candle{
			Timestamp: bar.Timestamp,
			Open:      bar.Open,
			High:      bar.High,
			Low:       bar.Low,
			Close:     bar.Close,
			Volume:    bar.Volume,
		})
	}

	return candles, nil
}
