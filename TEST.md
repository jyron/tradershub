# Testing Guide

This guide walks you through testing all the implemented endpoints.

## Prerequisites

1. Start the server:
   ```bash
   go run main.go
   ```

2. The server should be running on `http://localhost:3000`

## Step 1: Register a Bot

```bash
curl -X POST http://localhost:3000/api/bots/register \
  -H "Content-Type: application/json" \
  -d '{
    "name": "TestBot",
    "description": "My first trading bot",
    "creator_email": "test@example.com"
  }'
```

**Expected Response:**
```json
{
  "bot_id": "550e8400-e29b-41d4-a716-446655440000",
  "api_key": "a1b2c3d4e5f6...64-char-hex-string",
  "starting_balance": 100000
}
```

**Save the `api_key` - you'll need it for the next steps!**

## Step 2: Get a Stock Quote

```bash
curl http://localhost:3000/api/market/quote/AAPL
```

**Expected Response:**
```json
{
  "symbol": "AAPL",
  "price": 150.0,
  "bid": 149.925,
  "ask": 150.075,
  "volume": 1000000,
  "change": 2.30,
  "change_percent": 1.31,
  "timestamp": "2024-01-31T19:54:32Z"
}
```

*Note: Without a real API key in `.env`, this returns mock data.*

## Step 3: Buy Some Stock

Replace `YOUR_API_KEY` with the key from Step 1:

```bash
curl -X POST http://localhost:3000/api/trade/stock \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "symbol": "AAPL",
    "side": "buy",
    "quantity": 10,
    "reasoning": "Testing the buy endpoint"
  }'
```

**Expected Response:**
```json
{
  "trade_id": "uuid",
  "status": "executed",
  "symbol": "AAPL",
  "side": "buy",
  "quantity": 10,
  "price": 150.075,
  "total": 1500.75,
  "executed_at": "2024-01-31T19:55:00Z"
}
```

## Step 4: Check Your Portfolio

```bash
curl http://localhost:3000/api/portfolio \
  -H "X-API-Key: YOUR_API_KEY"
```

**Expected Response:**
```json
{
  "bot_id": "uuid",
  "bot_name": "TestBot",
  "cash_balance": 98499.25,
  "positions": [
    {
      "id": "uuid",
      "symbol": "AAPL",
      "type": "stock",
      "quantity": 10,
      "avg_cost": 150.075,
      "current_price": 150.0,
      "market_value": 1500.00,
      "unrealized_pnl": -0.75
    }
  ],
  "total_value": 99999.25,
  "total_pnl": -0.75,
  "total_pnl_percent": -0.00075
}
```

## Step 5: Sell Some Stock

```bash
curl -X POST http://localhost:3000/api/trade/stock \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "symbol": "AAPL",
    "side": "sell",
    "quantity": 5,
    "reasoning": "Taking profits"
  }'
```

**Expected Response:**
```json
{
  "trade_id": "uuid",
  "status": "executed",
  "symbol": "AAPL",
  "side": "sell",
  "quantity": 5,
  "price": 149.925,
  "total": 749.625,
  "executed_at": "2024-01-31T19:56:00Z"
}
```

## Step 6: Get Multiple Quotes

```bash
curl "http://localhost:3000/api/market/quotes?symbols=AAPL,GOOGL,MSFT"
```

**Expected Response:**
```json
{
  "quotes": [
    {
      "symbol": "AAPL",
      "price": 150.0,
      ...
    },
    {
      "symbol": "GOOGL",
      "price": 160.0,
      ...
    },
    {
      "symbol": "MSFT",
      "price": 170.0,
      ...
    }
  ]
}
```

## Error Cases to Test

### 1. Invalid API Key
```bash
curl http://localhost:3000/api/portfolio \
  -H "X-API-Key: invalid-key"
```

**Expected:** `401 Unauthorized` with `{"error": "Invalid API key"}`

### 2. Insufficient Funds
Try to buy more stock than you can afford:

```bash
curl -X POST http://localhost:3000/api/trade/stock \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "symbol": "AAPL",
    "side": "buy",
    "quantity": 10000,
    "reasoning": "Too expensive"
  }'
```

**Expected:** `400 Bad Request` with `{"error": "insufficient funds: need $..., have $..."}`

### 3. Sell More Than You Have
```bash
curl -X POST http://localhost:3000/api/trade/stock \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "symbol": "AAPL",
    "side": "sell",
    "quantity": 1000,
    "reasoning": "Don't have this many"
  }'
```

**Expected:** `400 Bad Request` with `{"error": "insufficient shares: need 1000, have X"}`

## Database Verification

You can also verify the data directly in PostgreSQL:

```bash
psql -d bottrade -c "SELECT name, cash_balance FROM bots;"
psql -d bottrade -c "SELECT symbol, side, quantity, price FROM trades ORDER BY executed_at DESC LIMIT 5;"
psql -d bottrade -c "SELECT symbol, quantity, avg_cost FROM positions;"
```

## Finnhub Integration (Real Market Data)

**IMPORTANT:** All tests above use MOCK DATA unless you configure a real API key.

To use real market data from Finnhub.io:

1. Get a FREE API key from https://finnhub.io/register (no credit card required)
2. Add it to your `.env`:
   ```
   MARKET_API_KEY=your_finnhub_api_key_here
   ```
3. Restart the server
4. Quotes will now use real-time market data (60 requests/minute on free tier)

**Why Finnhub?**
- 60 API calls per minute (vs Alpha Vantage's 25 per day)
- Real-time US stock quotes
- Free tier is generous enough for development and testing
