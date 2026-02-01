// WebSocket connection manager
let ws = null;
let reconnectTimeout = null;
const RECONNECT_DELAY = 3000;

function connectWebSocket() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws`;

    ws = new WebSocket(wsUrl);

    ws.onopen = () => {
        console.log('WebSocket connected');
        updateConnectionStatus(true);
        if (reconnectTimeout) {
            clearTimeout(reconnectTimeout);
            reconnectTimeout = null;
        }
    };

    ws.onmessage = (event) => {
        try {
            const message = JSON.parse(event.data);
            handleWebSocketMessage(message);
        } catch (error) {
            console.error('Failed to parse WebSocket message:', error);
        }
    };

    ws.onerror = (error) => {
        console.error('WebSocket error:', error);
        updateConnectionStatus(false);
    };

    ws.onclose = () => {
        console.log('WebSocket closed');
        updateConnectionStatus(false);
        // Attempt to reconnect after delay
        reconnectTimeout = setTimeout(connectWebSocket, RECONNECT_DELAY);
    };
}

function handleWebSocketMessage(message) {
    const { event, data } = message;

    switch (event) {
        case 'trade':
            if (typeof window.addTradeToFeed === 'function') {
                window.addTradeToFeed(data);
            }
            break;
        case 'leaderboard_update':
            if (typeof window.updateLeaderboard === 'function') {
                window.updateLeaderboard(data.rankings);
            }
            break;
        default:
            console.log('Unknown event type:', event);
    }
}

function updateConnectionStatus(connected) {
    const statusEl = document.getElementById('connection-status');
    if (!statusEl) return;

    if (connected) {
        statusEl.textContent = 'Live';
        statusEl.style.color = 'var(--positive)';
    } else {
        statusEl.textContent = 'Disconnected';
        statusEl.style.color = 'var(--negative)';
    }
}

// Connect on page load
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', connectWebSocket);
} else {
    connectWebSocket();
}

// Cleanup on page unload
window.addEventListener('beforeunload', () => {
    if (ws) {
        ws.close();
    }
    if (reconnectTimeout) {
        clearTimeout(reconnectTimeout);
    }
});
