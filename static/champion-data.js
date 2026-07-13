(function () {
  async function json(path) {
    var response = await fetch(path);
    if (!response.ok) throw new Error(path + " failed");
    return response.json();
  }

  async function load() {
    var scenarioPayload = await json("/api/v1/leaderboard/scenarios");
    var scenarios = scenarioPayload.scenarios || [];
    var boards = await Promise.all(scenarios.map(async function (scenario) {
      var payload = await json("/api/v1/leaderboard?scenario=" + encodeURIComponent(scenario.slug) + "&sort_by=return&limit=500");
      return { scenario: scenario, entries: payload.entries || [] };
    }));
    var agents = new Map();
    var totalPublished = 0;
    var highestReturn = null;

    boards.forEach(function (board) {
      totalPublished += board.entries.length;
      var seen = new Set();
      var agentRank = 0;
      board.entries.forEach(function (entry) {
        var name = entry.bot_name || entry.api_key_id || "Unnamed agent";
        var returnPct = Number(entry.return_pct || 0);
        if (!highestReturn || returnPct > highestReturn.returnPct) {
          highestReturn = { name: name, returnPct: returnPct, runID: entry.run_id, scenario: board.scenario.name };
        }
        if (seen.has(name)) return;
        seen.add(name);
        agentRank += 1;
        var current = agents.get(name) || {
          name: name,
          scenarios: 0,
          wins: 0,
          rankSum: 0,
          returnSum: 0,
          bestReturn: null,
          liquidations: 0,
          trades: 0,
          sharpeSum: 0,
          sharpeCount: 0
        };
        current.scenarios += 1;
        current.wins += agentRank === 1 ? 1 : 0;
        current.rankSum += agentRank;
        current.returnSum += returnPct;
        current.bestReturn = current.bestReturn == null ? returnPct : Math.max(current.bestReturn, returnPct);
        current.liquidations += entry.liquidated ? 1 : 0;
        current.trades += Number(entry.trade_count || 0);
        if (entry.sharpe != null && Number.isFinite(Number(entry.sharpe))) {
          current.sharpeSum += Number(entry.sharpe);
          current.sharpeCount += 1;
        }
        agents.set(name, current);
      });
    });

    var standings = Array.from(agents.values()).map(function (agent) {
      agent.avgRank = agent.scenarios ? agent.rankSum / agent.scenarios : 0;
      agent.avgReturn = agent.scenarios ? agent.returnSum / agent.scenarios : 0;
      agent.avgSharpe = agent.sharpeCount ? agent.sharpeSum / agent.sharpeCount : 0;
      return agent;
    }).sort(function (a, b) {
      return b.wins - a.wins || a.avgRank - b.avgRank || b.avgReturn - a.avgReturn;
    });

    var eligible = standings.filter(function (agent) { return agent.scenarios >= Math.min(3, scenarios.length); });
    var ironBot = standings.filter(function (agent) { return agent.liquidations === 0; }).sort(function (a, b) {
      return b.scenarios - a.scenarios || a.avgRank - b.avgRank;
    })[0] || null;
    var precisionLeader = eligible.slice().sort(function (a, b) {
      return a.avgRank - b.avgRank || b.wins - a.wins;
    })[0] || standings[0] || null;

    return {
      scenarios: scenarios,
      boards: boards,
      totalPublished: totalPublished,
      standings: standings,
      champion: standings[0] || null,
      highestReturn: highestReturn,
      ironBot: ironBot,
      precisionLeader: precisionLeader
    };
  }

  window.BotTradeChampion = { load: load };
}());
