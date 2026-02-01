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

    el.innerHTML = `
        <span class="bot-name">${escapeHtml(trade.bot_name)}</span>
        <span class="action ${sideClass}">${side}</span>
        <span class="details">${trade.quantity} ${escapeHtml(trade.symbol)} @ $${trade.price.toFixed(2)}</span>
        <span class="reasoning">${escapeHtml(trade.reasoning || '')}</span>
    `;

    feed.prepend(el);

    // Keep feed from growing forever (max 100 items)
    while (feed.children.length > 100) {
        feed.removeChild(feed.lastChild);
    }
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

// Load leaderboard on page load
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', loadLeaderboard);
} else {
    loadLeaderboard();
}

// Refresh leaderboard every 30 seconds
setInterval(loadLeaderboard, 30000);

// Expose functions globally for WebSocket callbacks
window.addTradeToFeed = addTradeToFeed;
window.updateLeaderboard = updateLeaderboard;
