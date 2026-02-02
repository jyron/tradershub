/**
 * Homepage dashboard: loads leaderboard, renders leaderboard chart and per-bot portfolio mini charts.
 * Depends on Chart, ChartUtils, createLeaderboardBarChart, createPortfolioMiniChart.
 */
(function (global) {
  'use strict';

  var DASHBOARD_LEADERBOARD_LIMIT = 8;
  var DASHBOARD_BOT_CARDS_COUNT = 6;
  var DEFAULT_TIME_SCALE = '1Y';

  var leaderboardChartInstance = null;
  var botChartInstances = [];
  var botDetailsCache = [];
  var currentTimeScale = DEFAULT_TIME_SCALE;

  function escapeHtml(text) {
    if (!text) return '';
    var div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
  }

  function renderLeaderboardChart(rankings) {
    var canvas = document.getElementById('dashboard-leaderboard-chart');
    if (!canvas) return;
    if (leaderboardChartInstance) {
      leaderboardChartInstance.destroy();
      leaderboardChartInstance = null;
    }
    if (typeof createLeaderboardBarChart !== 'undefined' && rankings && rankings.length > 0) {
      leaderboardChartInstance = createLeaderboardBarChart(canvas, rankings);
    }
  }

  function renderBotCards(rankings, botDetailsList) {
    var container = document.getElementById('dashboard-bot-cards');
    if (!container) return;

    container.innerHTML = '';
    botChartInstances.forEach(function (inst) {
      if (inst && inst.destroy) inst.destroy();
    });
    botChartInstances = [];

    if (!rankings || !botDetailsList || botDetailsList.length === 0) {
      container.innerHTML = '<p class="text-muted text-center p-24">No bot data to display.</p>';
      return;
    }

    for (var i = 0; i < botDetailsList.length; i++) {
      var entry = rankings[i];
      var data = botDetailsList[i];
      if (!entry || !data) continue;

      var card = document.createElement('div');
      card.className = 'dashboard-bot-card card card-chart';

      var totalValue = (data.portfolio && data.portfolio.total_value) ? data.portfolio.total_value : 0;
      var pnlPercent = (data.portfolio && data.portfolio.total_pnl_percent != null) ? data.portfolio.total_pnl_percent : 0;
      var pnlClass = pnlPercent >= 0 ? 'positive' : 'negative';
      var pnlSign = pnlPercent >= 0 ? '+' : '';

      // Get top positions
      var positions = (data.portfolio && data.portfolio.positions) ? data.portfolio.positions : [];
      var sortedPositions = positions.slice().sort(function(a, b) { return b.market_value - a.market_value; });
      var topPositions = sortedPositions.slice(0, 3);

      var positionsHtml = '';
      if (topPositions.length > 0) {
        positionsHtml = '<div style="margin-top: 12px; padding-top: 12px; border-top: 1px solid var(--border);">' +
          '<div style="font-size: 12px; color: var(--muted); margin-bottom: 8px; font-weight: 600;">TOP HOLDINGS</div>';

        topPositions.forEach(function(pos) {
          var percent = totalValue > 0 ? (pos.market_value / totalValue * 100) : 0;
          var pnl = pos.unrealized_pnl || 0;
          var posClass = pnl >= 0 ? 'positive' : 'negative';
          var posSign = pnl >= 0 ? '+' : '';

          positionsHtml +=
            '<div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 6px; font-size: 13px;">' +
              '<div style="display: flex; align-items: center; gap: 8px;">' +
                '<a href="/chart.html?symbol=' + encodeURIComponent(pos.symbol) + '" style="font-weight: 600; text-decoration: none; color: inherit;">' +
                  escapeHtml(pos.symbol) +
                '</a>' +
                '<span style="color: var(--muted); font-size: 11px;">' + percent.toFixed(1) + '%</span>' +
              '</div>' +
              '<div class="' + posClass + '" style="font-size: 12px; font-weight: 600;">' +
                posSign + '$' + Math.abs(pnl).toLocaleString('en-US', { minimumFractionDigits: 0, maximumFractionDigits: 0 }) +
              '</div>' +
            '</div>';
        });

        positionsHtml += '</div>';
      }

      card.innerHTML =
        '<h3 class="dashboard-bot-card-title">' +
          '<a href="/bot.html?id=' + encodeURIComponent(entry.bot_id) + '">' + escapeHtml(data.name || entry.bot_name) + '</a>' +
        '</h3>' +
        '<p class="dashboard-bot-card-stat ' + pnlClass + '">' +
          '$' + totalValue.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) +
          ' <span class="text-muted">(' + pnlSign + pnlPercent.toFixed(2) + '%)</span>' +
        '</p>' +
        '<div class="dashboard-bot-chart-wrapper chart-wrapper"></div>' +
        positionsHtml;

      container.appendChild(card);

      var chartWrapper = card.querySelector('.dashboard-bot-chart-wrapper');
      var canvas = document.createElement('canvas');
      chartWrapper.appendChild(canvas);

      var snapshots = data.portfolio_snapshots || [];
      var trades = data.recent_trades || [];
      if (typeof createPortfolioMiniChart !== 'undefined') {
        var chartInstance = createPortfolioMiniChart(canvas, snapshots, totalValue, currentTimeScale, trades);
        if (chartInstance) botChartInstances.push(chartInstance);
      }
    }
  }

  function reRenderAllMiniCharts() {
    var container = document.getElementById('dashboard-bot-cards');
    if (!container) return;

    var cards = container.querySelectorAll('.dashboard-bot-card');
    var chartWrappers = container.querySelectorAll('.dashboard-bot-chart-wrapper');
    botChartInstances.forEach(function (inst) {
      if (inst && inst.destroy) inst.destroy();
    });
    botChartInstances = [];

    for (var i = 0; i < chartWrappers.length && i < botDetailsCache.length; i++) {
      var data = botDetailsCache[i];
      var totalValue = (data.portfolio && data.portfolio.total_value) ? data.portfolio.total_value : 0;
      var snapshots = data.portfolio_snapshots || [];
      var trades = data.recent_trades || [];
      var wrapper = chartWrappers[i];
      wrapper.innerHTML = '';
      var canvas = document.createElement('canvas');
      wrapper.appendChild(canvas);
      if (typeof createPortfolioMiniChart !== 'undefined') {
        var chartInstance = createPortfolioMiniChart(canvas, snapshots, totalValue, currentTimeScale, trades);
        if (chartInstance) botChartInstances.push(chartInstance);
      }
    }
  }

  function bindTimeScaleButtons() {
    var selector = document.querySelector('.dashboard-time-scale-selector');
    if (!selector) return;
    selector.addEventListener('click', function (e) {
      var btn = e.target.closest('.time-scale-btn');
      if (!btn || !btn.dataset.scale) return;
      currentTimeScale = btn.dataset.scale;
      selector.querySelectorAll('.time-scale-btn').forEach(function (b) { b.classList.remove('active'); });
      btn.classList.add('active');
      reRenderAllMiniCharts();
    });
  }

  function loadDashboard() {
    var leaderboardWrapper = document.getElementById('dashboard-leaderboard');
    var botCardsContainer = document.getElementById('dashboard-bot-cards');
    if (!leaderboardWrapper && !botCardsContainer) return;

    if (leaderboardWrapper) {
      var chartEl = leaderboardWrapper.querySelector('.chart-wrapper');
      if (chartEl) chartEl.innerHTML = '<div class="chart-loading">Loading leaderboard...</div>';
    }

    fetch('/api/leaderboard?limit=' + DASHBOARD_LEADERBOARD_LIMIT)
      .then(function (res) { return res.json(); })
      .then(function (apiData) {
        var rankings = apiData.rankings || [];
        if (leaderboardWrapper) {
          var chartWrapper = leaderboardWrapper.querySelector('.chart-wrapper');
          if (chartWrapper) {
            chartWrapper.innerHTML = '<canvas id="dashboard-leaderboard-chart"></canvas>';
            renderLeaderboardChart(rankings);
          }
        }

        if (!botCardsContainer || rankings.length === 0) return;

        botCardsContainer.innerHTML = '<div class="chart-loading text-center p-24">Loading bot charts...</div>';
        var topBots = rankings.slice(0, DASHBOARD_BOT_CARDS_COUNT);
        var fetchPromises = topBots.map(function (r) {
          return fetch('/api/bots/' + encodeURIComponent(r.bot_id) + '?limit=200').then(function (res) { return res.json(); });
        });

        return Promise.all(fetchPromises).then(function (botDetailsList) {
          botDetailsCache = botDetailsList;
          renderBotCards(rankings, botDetailsList);
        });
      })
      .catch(function (err) {
        console.error('Dashboard load failed:', err);
        if (leaderboardWrapper) {
          var w = leaderboardWrapper.querySelector('.chart-wrapper');
          if (w) w.innerHTML = '<p class="text-muted p-24">Failed to load leaderboard.</p>';
        }
        var botCards = document.getElementById('dashboard-bot-cards');
        if (botCards) botCards.innerHTML = '<p class="text-muted text-center p-24">Failed to load bot data.</p>';
      })
      .then(function () {
        bindTimeScaleButtons();
      });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', loadDashboard);
  } else {
    loadDashboard();
  }
})(typeof window !== 'undefined' ? window : this);
