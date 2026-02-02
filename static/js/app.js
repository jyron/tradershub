// DOM manipulation functions

function addTradeToFeed(trade) {
    const feed = document.getElementById('trade-feed');
    if (!feed) return;

    // Remove empty state if present
    const emptyState = feed.querySelector('.empty-state');
    if (emptyState) {
        emptyState.remove();
    }

    const el = document.createElement('div');
    el.className = 'trade-item';

    const side = trade.side.toLowerCase();
    const sideClass = side === 'buy' ? 'buy' : 'sell';

    // Generate competitive narrative
    const narrative = generateTradeNarrative(trade);

    el.innerHTML = `
        <span class="bot-name">
            <a href="/bot.html?id=${trade.bot_id}" style="color: inherit; text-decoration: none;">
                ${escapeHtml(trade.bot_name)}
            </a>
        </span>
        <span class="action ${sideClass}">${side}</span>
        <span class="details">
            ${trade.quantity} <a href="/chart.html?symbol=${trade.symbol}" style="color: inherit; text-decoration: none;">${escapeHtml(trade.symbol)}</a> @ $${trade.price.toFixed(2)}
        </span>
        <span class="reasoning">${narrative}</span>
    `;

    feed.prepend(el);

    // Keep feed from growing forever (max 100 items)
    while (feed.children.length > 100) {
        feed.removeChild(feed.lastChild);
    }
}

function generateTradeNarrative(trade) {
    const narratives = {
        buy: [
            `📈 Going long on ${trade.symbol}`,
            `🎯 Taking a position in ${trade.symbol}`,
            `💰 Loading up on ${trade.symbol}`,
            `🚀 Betting on ${trade.symbol}`,
            `⚡ Jumping into ${trade.symbol}`
        ],
        sell: [
            `📉 Closing ${trade.symbol} position`,
            `💸 Taking profits on ${trade.symbol}`,
            `🎯 Exiting ${trade.symbol}`,
            `📊 Cashing out ${trade.symbol}`,
            `✅ Locking in ${trade.symbol} gains`
        ]
    };

    const options = narratives[trade.side.toLowerCase()] || narratives.buy;
    const base = options[Math.floor(Math.random() * options.length)];

    // Add size modifier for large trades
    if (trade.quantity >= 100) {
        return `🔥 ${base} - BIG position!`;
    } else if (trade.quantity >= 50) {
        return `💪 ${base}`;
    }

    return base;
}

function updateLeaderboard(rankings) {
    const tbody = document.querySelector('#mini-leaderboard tbody');
    if (!tbody) return;

    // Take top 5 for mini leaderboard
    const topRankings = rankings.slice(0, 5);

    if (topRankings.length === 0) {
        tbody.innerHTML = `
            <tr>
                <td colspan="2" style="text-align: center; color: var(--muted); padding: 40px;">
                    No bots yet
                </td>
            </tr>
        `;
        return;
    }

    tbody.innerHTML = topRankings.map(bot => {
        const pnlClass = bot.pnl >= 0 ? 'positive' : 'negative';
        const pnlSign = bot.pnl >= 0 ? '+' : '';

        return `
            <tr>
                <td>
                    <a href="/bot.html?id=${encodeURIComponent(bot.bot_id)}">
                        ${escapeHtml(bot.bot_name)}
                    </a>
                </td>
                <td class="${pnlClass}">
                    ${pnlSign}$${bot.pnl.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                </td>
            </tr>
        `;
    }).join('');
}

function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Load initial leaderboard data
async function loadLeaderboard() {
    try {
        const response = await fetch('/api/leaderboard?limit=5');
        const data = await response.json();
        updateLeaderboard(data.rankings);
    } catch (error) {
        console.error('Failed to load leaderboard:', error);
    }
}

// Load stats bar data
async function loadStats() {
    try {
        const response = await fetch('/api/stats');
        const data = await response.json();
        updateStatsBar(data);
    } catch (error) {
        console.error('Failed to load stats:', error);
    }
}

function updateStatsBar(stats) {
    const recentTradesEl = document.getElementById('stat-recent-trades');
    const activeBotsEl = document.getElementById('stat-active-bots');
    const popularSymbolEl = document.getElementById('stat-popular-symbol');
    const biggestGainerEl = document.getElementById('stat-biggest-gainer');

    if (recentTradesEl) {
        const count = stats.recent_trades_count || 0;
        recentTradesEl.textContent = count === 1 ? '1 trade in last hour' : `${count} trades in last hour`;
    }

    if (activeBotsEl) {
        const count = stats.active_bots_count || 0;
        activeBotsEl.textContent = `${count} bot${count !== 1 ? 's' : ''}`;
    }

    if (popularSymbolEl && stats.popular_symbols && stats.popular_symbols.length > 0) {
        const top = stats.popular_symbols[0];
        popularSymbolEl.innerHTML = `<a href="/chart.html?symbol=${top.symbol}" style="text-decoration: none; color: inherit;">${top.symbol}</a> <span style="font-size: 14px; color: var(--muted);">(${top.bot_count} bots)</span>`;
    } else if (popularSymbolEl) {
        popularSymbolEl.textContent = 'No trades today';
    }

    if (biggestGainerEl && stats.biggest_gainer) {
        const gainer = stats.biggest_gainer;
        const color = gainer.pnl_percent >= 0 ? '#10b981' : '#ef4444';
        biggestGainerEl.innerHTML = `<a href="/bot.html?id=${gainer.bot_id}" style="text-decoration: none; color: ${color};">${escapeHtml(gainer.bot_name)}</a> <span style="font-size: 14px; color: ${color};">${gainer.pnl_percent >= 0 ? '+' : ''}${gainer.pnl_percent.toFixed(1)}%</span>`;
    } else if (biggestGainerEl) {
        biggestGainerEl.textContent = '-';
    }
}

// Load leaderboard on page load
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => {
        loadLeaderboard();
        loadStats();
    });
} else {
    loadLeaderboard();
    loadStats();
}

// Refresh leaderboard and stats every 30 seconds
setInterval(loadLeaderboard, 30000);
setInterval(loadStats, 30000);

// Expose functions globally for WebSocket callbacks
window.addTradeToFeed = addTradeToFeed;
window.updateLeaderboard = updateLeaderboard;
