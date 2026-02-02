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
	Volume    uint64    `json:"volume"`
}

// GetHistoricalCandles fetches OHLC candle data from Alpaca
func (ac *AlpacaClient) GetHistoricalCandles(symbol string, timeframe string, start time.Time, end time.Time) ([]Candle, error) {
	if ac == nil || ac.marketData == nil {
		return nil, fmt.Errorf("Alpaca client not initialized")
	}

	// Parse timeframe (e.g., "1Day", "1Hour", "5Min")
	var tf marketdata.TimeFrame
	switch timeframe {
	case "1Min":
		tf = marketdata.OneMin
	case "5Min":
		tf = marketdata.NewTimeFrame(5, marketdata.Min)
	case "15Min":
		tf = marketdata.NewTimeFrame(15, marketdata.Min)
	case "1Hour":
		tf = marketdata.OneHour
	case "1Day":
		tf = marketdata.OneDay
	default:
		tf = marketdata.OneDay
	}

	// Fetch bars from Alpaca
	bars, err := ac.marketData.GetBars(symbol, marketdata.GetBarsRequest{
		TimeFrame: tf,
		Start:     start,
		End:       end,
		Feed:      marketdata.IEX, // Use IEX feed for free tier
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
			Volume:    bar.Volume,
		})
	}

	return candles, nil
}
