/**
 * Renders a horizontal bar chart for leaderboard rankings (Top 10 Return %).
 * Accepts a canvas element and rankings array; no fetch. Caller must destroy previous instance.
 */
(function (global) {
  'use strict';

  function createLeaderboardBarChart(canvas, rankings) {
    if (!canvas || !rankings || rankings.length === 0) return null;
    if (typeof Chart === 'undefined') return null;

    var top10 = rankings.slice(0, 10);
    var ctx = canvas.getContext('2d');

    return new Chart(ctx, {
      type: 'bar',
      data: {
        labels: top10.map(function (bot) { return bot.bot_name; }),
        datasets: [{
          label: 'Return %',
          data: top10.map(function (bot) { return bot.pnl_percent; }),
          backgroundColor: top10.map(function (bot, idx) {
            var opacity = 1 - (idx * 0.07);
            return bot.pnl_percent >= 0
              ? 'rgba(16, 185, 129, ' + opacity + ')'
              : 'rgba(239, 68, 68, ' + opacity + ')';
          }),
          borderColor: top10.map(function (bot) {
            return bot.pnl_percent >= 0 ? '#10b981' : '#ef4444';
          }),
          borderWidth: 2,
          borderRadius: 6
        }]
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        indexAxis: 'y',
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
              title: function (context) {
                return top10[context[0].dataIndex].bot_name;
              },
              label: function (context) {
                var bot = top10[context.dataIndex];
                return [
                  'Return: ' + (context.parsed.x >= 0 ? '+' : '') + context.parsed.x.toFixed(2) + '%',
                  'Portfolio: $' + bot.total_value.toLocaleString(),
                  'P&L: ' + (bot.pnl >= 0 ? '+' : '') + '$' + bot.pnl.toLocaleString(),
                  'Trades: ' + bot.trade_count
                ];
              }
            }
          }
        },
        scales: {
          x: {
            grid: {
              color: 'rgba(255, 255, 255, 0.05)',
              drawBorder: false
            },
            ticks: {
              color: '#9ca3af',
              callback: function (value) {
                return (value >= 0 ? '+' : '') + value + '%';
              }
            }
          },
          y: {
            grid: { display: false },
            ticks: {
              color: '#9ca3af',
              font: { size: 13, weight: '500' }
            }
          }
        }
      }
    });
  }

  global.createLeaderboardBarChart = createLeaderboardBarChart;
})(typeof window !== 'undefined' ? window : this);
