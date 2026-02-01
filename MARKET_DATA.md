# Market Data Provider

## THIS PROJECT USES FINNHUB.IO

BotTrade uses **Finnhub.io** for real-time US stock market data.

## Getting Your FREE API Key

1. **Go to https://finnhub.io/register**
2. **Sign up** (just need email, takes 30 seconds)
3. **Copy your API key** from the dashboard
4. **Add it to `.env`:**
   ```
   MARKET_API_KEY=your_actual_finnhub_key_here
   ```
5. **Restart the server**

## API Details

**Endpoint Used:** `https://finnhub.io/api/v1/quote`

**Free Tier Limits:**
- 60 API calls per minute
- No credit card required
- Real-time US stock quotes

**Example API Call:**
```bash
curl "https://finnhub.io/api/v1/quote?symbol=AAPL&token=YOUR_KEY"
```

**Example Response:**
```json
{
  "c": 178.50,  // Current price
  "d": 2.46,    // Change
  "dp": 0.95,   // Percent change
  "h": 180.00,  // High
  "l": 177.00,  // Low
  "o": 177.50,  // Open
  "pc": 176.04, // Previous close
  "t": 1706803200  // Timestamp
}
```

## What Happens Without a Key?

If you don't set `MARKET_API_KEY` or leave it as `your_api_key_here`, the app will:
- ⚠️ Use **MOCK DATA** (fake prices)
- Still work for testing trading logic
- Not reflect real market movements

## Testing Real Market Data

After adding your Finnhub key to `.env`:

```bash
# Restart the server
go run main.go

# Test with a real quote
curl http://localhost:3000/api/market/quote/AAPL

# You should see real market data:
{
  "symbol": "AAPL",
  "price": 178.50,  // ← Real price from Finnhub
  "bid": 178.41,
  "ask": 178.59,
  "volume": 10000000,
  "change": 2.46,
  "change_percent": 0.95,
  "timestamp": "2024-01-31T14:30:00Z"
}
```

## Caching Strategy

To stay within rate limits, quotes are cached for 15 seconds:
- First request: Fetches from Finnhub API
- Subsequent requests (within 15s): Returns cached data
- After 15s: Fetches fresh data

This means you can make unlimited requests to the BotTrade API - it will only call Finnhub when the cache expires.

## Why Not Alpha Vantage?

Alpha Vantage's free tier only allows **25 API calls per day** - not enough for active trading bots. Finnhub gives you **60 per minute**.

## Why Not Polygon.io?

Polygon.io requires a paid plan ($29/month) for real-time data. Finnhub's free tier is perfect for development and testing.

## Production Considerations

For production with many bots:
- The 60 req/min limit is shared across all bots
- With 15-second caching, you can support ~50+ active bots
- For higher volume, consider Polygon.io's paid tier
