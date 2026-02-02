/**
 * Renders a mini "Portfolio Value Over Time" line chart for dashboard bot cards.
 * Uses ChartUtils for time filtering. Accepts canvas, snapshots, currentValue, timeScale;
 * optional trades array for fallback when no snapshots. Returns Chart instance for cleanup.
 */
(function (global) {
  'use strict';

  var ChartUtils = global.ChartUtils;
  if (!ChartUtils) {
    console.warn('portfolio-mini-chart: ChartUtils required');
  }

  function buildPointsFromSnapshots(snapshots, currentValue, timeScale) {
    var filtered = ChartUtils.filterSnapshotsByTimeScale(snapshots || [], timeScale);
    if (!filtered.length) return null;
    var points = filtered.map(function (s) {
      return { time: new Date(s.snapshot_at), value: Number(s.total_value) };
    });
    var last = filtered[filtered.length - 1];
    if (new Date(last.snapshot_at).getTime() < Date.now() - 60 * 60 * 1000) {
      points.push({ time: new Date(), value: currentValue });
    }
    return points;
  }

  function buildPointsFromTrades(trades, currentValue, timeScale) {
    var filtered = ChartUtils.filterTradesByTimeScale(trades || [], timeScale);
    if (!filtered || filtered.length === 0) return null;
    var startingBalance = 100000;
    var chronological = filtered.slice().reverse();
    var points = [];
    var cashBalance = startingBalance;
    var holdings = {};

    points.push({
      time: new Date(chronological[0].executed_at),
      value: startingBalance
    });

    chronological.forEach(function (trade) {
      var total = trade.total != null ? trade.total : trade.total_value;
      if (trade.side === 'buy') {
        cashBalance -= total;
        holdings[trade.symbol] = (holdings[trade.symbol] || 0) + trade.quantity;
      } else {
        cashBalance += total;
        holdings[trade.symbol] = (holdings[trade.symbol] || 0) - trade.quantity;
        if (holdings[trade.symbol] <= 0) delete holdings[trade.symbol];
      }
      points.push({
        time: new Date(trade.executed_at),
        value: cashBalance + (startingBalance - cashBalance)
      });
    });
    points.push({ time: new Date(), value: currentValue });
    return points;
  }

  function createGradient(ctx, isProfit) {
    var gradient = ctx.createLinearGradient(0, 0, 0, 200);
    if (isProfit) {
      gradient.addColorStop(0, 'rgba(16, 185, 129, 0.4)');
      gradient.addColorStop(0.5, 'rgba(16, 185, 129, 0.15)');
      gradient.addColorStop(1, 'rgba(16, 185, 129, 0.01)');
    } else {
      gradient.addColorStop(0, 'rgba(239, 68, 68, 0.4)');
      gradient.addColorStop(0.5, 'rgba(239, 68, 68, 0.15)');
      gradient.addColorStop(1, 'rgba(239, 68, 68, 0.01)');
    }
    return gradient;
  }

  function createPortfolioMiniChart(canvas, snapshots, currentValue, timeScale, tradesOptional) {
    if (!canvas || typeof Chart === 'undefined') return null;
    if (!ChartUtils) return null;

    var points = buildPointsFromSnapshots(snapshots, currentValue, timeScale);
    if (!points || points.length === 0) {
      points = buildPointsFromTrades(tradesOptional, currentValue, timeScale);
    }
    if (!points || points.length === 0) {
      points = [
        { time: ChartUtils.getTimeRangeStart(timeScale), value: 100000 },
        { time: new Date(), value: currentValue }
      ];
    }

    var ctx = canvas.getContext('2d');
    var isProfit = currentValue >= 100000;
    var gradient = createGradient(ctx, isProfit);
    var granularity = ChartUtils.getChartGranularity(timeScale);

    return new Chart(ctx, {
      type: 'line',
      data: {
        labels: points.map(function (p) { return p.time; }),
        datasets: [{
          label: 'Portfolio Value',
          data: points.map(function (p) { return p.value; }),
          borderColor: isProfit ? '#10b981' : '#ef4444',
          backgroundColor: gradient,
          borderWidth: 2,
          fill: true,
          tension: 0.4,
          pointRadius: 0,
          pointHoverRadius: 4
        }]
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        interaction: { mode: 'index', intersect: false },
        plugins: {
          legend: { display: false },
          tooltip: {
            backgroundColor: 'rgba(17, 24, 39, 0.95)',
            titleColor: '#e5e5e5',
            bodyColor: '#e5e5e5',
            borderColor: '#374151',
            borderWidth: 1,
            padding: 12,
            displayColors: false,
            callbacks: {
              label: function (context) {
                return '$' + context.parsed.y.toLocaleString('en-US', {
                  minimumFractionDigits: 2,
                  maximumFractionDigits: 2
                });
              }
            }
          }
        },
        scales: {
          x: {
            type: 'time',
            time: {
              unit: granularity,
              displayFormats: {
                minute: 'HH:mm',
                hour: 'MMM d, HH:mm',
                day: 'MMM d'
              }
            },
            grid: { color: 'rgba(255, 255, 255, 0.05)', drawBorder: false },
            ticks: { color: '#9ca3af', maxRotation: 0, autoSkipPadding: 20 }
          },
          y: {
            beginAtZero: false,
            grid: { color: 'rgba(255, 255, 255, 0.05)', drawBorder: false },
            ticks: {
              color: '#9ca3af',
              callback: function (value) {
                return '$' + value.toLocaleString('en-US', {
                  minimumFractionDigits: 0,
                  maximumFractionDigits: 0
                });
              }
            }
          }
        }
      }
    });
  }

  global.createPortfolioMiniChart = createPortfolioMiniChart;
})(typeof window !== 'undefined' ? window : this);
