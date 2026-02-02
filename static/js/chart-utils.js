/**
 * Shared time filtering and chart granularity for portfolio/snapshot charts.
 * Pure functions only (no DOM). Used by dashboard and bot profile.
 */
(function (global) {
  'use strict';

  function filterTradesByTimeScale(trades, timeScale) {
    if (timeScale === 'ALL' || !trades || trades.length === 0) {
      return trades;
    }
    const now = new Date();
    let cutoffTime;
    switch (timeScale) {
      case '1D':
        cutoffTime = new Date(now - 24 * 60 * 60 * 1000);
        break;
      case '1W':
        cutoffTime = new Date(now - 7 * 24 * 60 * 60 * 1000);
        break;
      case '1M':
        cutoffTime = new Date(now - 30 * 24 * 60 * 60 * 1000);
        break;
      case '1Y':
        cutoffTime = new Date(now - 365 * 24 * 60 * 60 * 1000);
        break;
      default:
        return trades;
    }
    return trades.filter(function (trade) {
      return new Date(trade.executed_at) >= cutoffTime;
    });
  }

  function getChartGranularity(timeScale) {
    switch (timeScale) {
      case '1D': return 'minute';
      case '1W': return 'hour';
      case '1M':
      case '1Y':
      case 'ALL': return 'day';
      default: return 'minute';
    }
  }

  function getTimeRangeStart(timeScale) {
    const now = new Date();
    switch (timeScale) {
      case '1D':
        return new Date(now - 24 * 60 * 60 * 1000);
      case '1W':
        return new Date(now - 7 * 24 * 60 * 60 * 1000);
      case '1M':
        return new Date(now - 30 * 24 * 60 * 60 * 1000);
      case '1Y':
        return new Date(now - 365 * 24 * 60 * 60 * 1000);
      default:
        return new Date(now - 3600000);
    }
  }

  function filterSnapshotsByTimeScale(snapshots, timeScale) {
    if (!snapshots || snapshots.length === 0) return [];
    if (timeScale === 'ALL') return snapshots;
    const now = new Date();
    let cutoffTime;
    if (timeScale === '1D') cutoffTime = new Date(now - 24 * 60 * 60 * 1000);
    else if (timeScale === '1W') cutoffTime = new Date(now - 7 * 24 * 60 * 60 * 1000);
    else if (timeScale === '1M') cutoffTime = new Date(now - 30 * 24 * 60 * 60 * 1000);
    else if (timeScale === '1Y') cutoffTime = new Date(now - 365 * 24 * 60 * 60 * 1000);
    else return snapshots;
    return snapshots.filter(function (s) {
      return new Date(s.snapshot_at) >= cutoffTime;
    });
  }

  global.ChartUtils = {
    filterTradesByTimeScale: filterTradesByTimeScale,
    getChartGranularity: getChartGranularity,
    getTimeRangeStart: getTimeRangeStart,
    filterSnapshotsByTimeScale: filterSnapshotsByTimeScale
  };
})(typeof window !== 'undefined' ? window : this);
