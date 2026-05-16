package services

import (
	"math"
	"testing"
)

func TestComputeMetricsTooFewSnapshots(t *testing.T) {
	m := ComputeMetrics([]float64{100000, 101000})
	if m.Valid {
		t.Fatalf("expected Valid=false with only 2 snapshots, got %+v", m)
	}
}

func TestSteadyGrowthBeatsVolatileGrowth(t *testing.T) {
	steady := []float64{100, 101, 102, 103, 104, 105, 106, 107}
	wild := []float64{100, 80, 130, 90, 140, 80, 120, 107}

	a := ComputeMetrics(steady)
	b := ComputeMetrics(wild)

	if !a.Valid || !b.Valid {
		t.Fatalf("both should be valid; got %+v / %+v", a, b)
	}
	if a.Sharpe <= b.Sharpe {
		t.Errorf("steady Sharpe %v should exceed wild Sharpe %v", a.Sharpe, b.Sharpe)
	}
	if a.MaxDrawdown >= b.MaxDrawdown {
		t.Errorf("steady drawdown %v should be less than wild %v", a.MaxDrawdown, b.MaxDrawdown)
	}
}

func TestMaxDrawdown(t *testing.T) {
	// peak 200, trough 80 after the peak -> 60%
	dd := maxDrawdown([]float64{100, 200, 150, 80, 90, 120})
	if math.Abs(dd-0.6) > 1e-9 {
		t.Fatalf("expected drawdown 0.6, got %v", dd)
	}
}

func TestSortinoOnlyPenalizesDownside(t *testing.T) {
	// Bot A and Bot B have the same mean return, same total volatility, but
	// A's volatility is entirely upside (good) and B's is entirely downside.
	// Sortino should rank A >> B.
	a := []float64{100, 90, 120, 100, 130, 110, 140}
	b := []float64{100, 110, 80, 100, 70, 90, 60}
	ma := ComputeMetrics(a)
	mb := ComputeMetrics(b)
	if !ma.Valid || !mb.Valid {
		t.Fatalf("both should be valid")
	}
	if ma.Sortino <= mb.Sortino {
		t.Errorf("upside-volatile Sortino %v should exceed downside-volatile %v", ma.Sortino, mb.Sortino)
	}
}
