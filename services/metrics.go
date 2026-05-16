package services

import "math"

// TradingDaysPerYear is the conventional annualization factor for daily returns.
const TradingDaysPerYear = 252.0

// MinSnapshotsForMetrics is the minimum number of snapshots required before
// risk-adjusted metrics are considered meaningful. Below this, the leaderboard
// reports the metric as invalid (null on the wire).
const MinSnapshotsForMetrics = 5

// PerformanceMetrics holds the risk-adjusted summary for a bot's portfolio
// value series. Valid is false when there are not enough datapoints to compute
// useful numbers; callers should render those as "n/a" rather than zero.
type PerformanceMetrics struct {
	Sharpe        float64 `json:"sharpe"`
	Sortino       float64 `json:"sortino"`
	MaxDrawdown   float64 `json:"max_drawdown"`
	Volatility    float64 `json:"volatility"`
	SnapshotCount int     `json:"snapshot_count"`
	Valid         bool    `json:"valid"`
}

// ComputeMetrics turns a chronologically-ordered series of portfolio values
// into a set of risk-adjusted performance metrics. Values must be positive.
func ComputeMetrics(values []float64) PerformanceMetrics {
	m := PerformanceMetrics{SnapshotCount: len(values)}
	if len(values) < MinSnapshotsForMetrics {
		return m
	}

	returns := dailyReturns(values)
	if len(returns) == 0 {
		return m
	}

	mean := meanOf(returns)
	std := stdDevOf(returns, mean)
	downside := downsideDeviation(returns)

	annualizer := math.Sqrt(TradingDaysPerYear)
	if std > 0 {
		m.Sharpe = (mean / std) * annualizer
	}
	if downside > 0 {
		m.Sortino = (mean / downside) * annualizer
	}
	m.Volatility = std * annualizer
	m.MaxDrawdown = maxDrawdown(values)
	m.Valid = true
	return m
}

// dailyReturns computes simple period-over-period returns. The result has
// length len(values) - 1.
func dailyReturns(values []float64) []float64 {
	if len(values) < 2 {
		return nil
	}
	out := make([]float64, 0, len(values)-1)
	for i := 1; i < len(values); i++ {
		prev := values[i-1]
		if prev <= 0 {
			continue
		}
		out = append(out, (values[i]-prev)/prev)
	}
	return out
}

func meanOf(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func stdDevOf(xs []float64, mean float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	sumSq := 0.0
	for _, x := range xs {
		d := x - mean
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(len(xs)-1))
}

// downsideDeviation is the std-dev variant used in Sortino: only negative
// returns contribute. Positive returns are treated as zero.
func downsideDeviation(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	sumSq := 0.0
	for _, x := range xs {
		if x < 0 {
			sumSq += x * x
		}
	}
	return math.Sqrt(sumSq / float64(len(xs)-1))
}

// maxDrawdown returns the largest peak-to-trough decline as a positive
// fraction (e.g. 0.42 = 42%). Walks the value series tracking the running
// peak.
func maxDrawdown(values []float64) float64 {
	peak := values[0]
	worst := 0.0
	for _, v := range values {
		if v > peak {
			peak = v
		}
		if peak <= 0 {
			continue
		}
		dd := (peak - v) / peak
		if dd > worst {
			worst = dd
		}
	}
	return worst
}
