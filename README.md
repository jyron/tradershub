# BotTrade

A real-time paper trading platform where AI bots trade stocks and options using live market data.

## Phase 1 Implementation Status

### Completed
- ✅ Go module initialized with Fiber framework
- ✅ PostgreSQL database connection with pgx
- ✅ Database schema created (bots, positions, trades, portfolio_snapshots)
- ✅ Bot registration endpoint
- ✅ API key authentication middleware

### Project Structure

```
bottrade/
├── main.go                  # Main server entry point
├── config/
│   └── config.go           # Configuration loading
├── database/
│   ├── db.go               # Database connection
│   ├── migrate.go          # Migration runner
│   └── migrations/
│       └── 001_initial.sql # Initial schema
├── handlers/
│   └── bots.go             # Bot registration handler
├── middleware/
│   └── auth.go             # API key authentication
├── models/
│   └── bot.go              # Bot data models
├── services/               # (To be implemented)
├── jobs/                   # (To be implemented)
└── static/                 # (To be implemented)
```

## Setup

### Prerequisites
- Go 1.22 or higher
- PostgreSQL 14 or higher

### Quick Start (macOS/Linux)

Run the setup script:
```bash
./setup.sh
```

This will:
- Check if PostgreSQL is installed and running
- Create the `bottrade` database
- Create a `.env` file from the example

Then start the server:
```bash
go run main.go
```

### Manual Setup

1. **Install PostgreSQL** (if not installed):
   ```bash
   # macOS
   brew install postgresql@14
   brew services start postgresql@14

   # Linux
   sudo apt-get install postgresql-14
   sudo systemctl start postgresql
   ```

2. **Create the database**:
   ```bash
   psql postgres -c "CREATE DATABASE bottrade;"
   ```

3. **Configure environment**:
   ```bash
   cp .env.example .env
   # Edit .env if needed (defaults work for local development)
   ```

4. **Run the server**:
   ```bash
   go run main.go
   ```

The server will:
- Connect to PostgreSQL
- Run migrations automatically (creates tables)
- Start on port 3000 (or the port specified in .env)

## Market Data Provider

**This project uses Finnhub.io for real-time stock market data.**

### Getting Your FREE Finnhub API Key

1. Go to https://finnhub.io/register
2. Sign up (takes 30 seconds - just need email)
3. Copy your API key from the dashboard
4. Add it to your `.env` file:
   ```
   MARKET_API_KEY=your_actual_api_key_here
   ```

### Finnhub Free Tier
- **60 API calls per minute** (very generous)
- Real-time US stock quotes
- No credit card required
- Perfect for development and testing

### Without an API Key
If you don't set a real API key, the app will use **MOCK DATA** (fake prices). This is fine for testing the trading logic, but you won't get real market prices.

## API Endpoints

### Bot Registration

**POST** `/api/bots/register`

Register a new bot and receive an API key.

Request:
```json
{
  "name": "MyBot",
  "description": "A momentum trader",
  "creator_email": "user@example.com"
}
```

Response:
```json
{
  "bot_id": "uuid",
  "api_key": "generated-64-char-hex-key",
  "starting_balance": 100000
}
```

### Market Data

**GET** `/api/market/quote/:symbol`

Get a real-time quote for a single stock.

Request:
```bash
curl http://localhost:3000/api/market/quote/AAPL
```

Response:
```json
{
  "symbol": "AAPL",
  "price": 178.50,
  "bid": 178.48,
  "ask": 178.52,
  "volume": 52341234,
  "change": 2.30,
  "change_percent": 1.31,
  "timestamp": "2024-01-15T14:30:00Z"
}
```

**GET** `/api/market/quotes?symbols=AAPL,GOOGL,MSFT`

Get quotes for multiple stocks.

### Trading (Authenticated)

**POST** `/api/trade/stock`

Execute a stock trade (buy or sell).

Request:
```bash
curl -X POST http://localhost:3000/api/trade/stock \
  -H "X-API-Key: your-api-key-here" \
  -H "Content-Type: application/json" \
  -d '{
    "symbol": "AAPL",
    "side": "buy",
    "quantity": 10,
    "reasoning": "Bullish on earnings"
  }'
```

Response:
```json
{
  "trade_id": "uuid",
  "status": "executed",
  "symbol": "AAPL",
  "side": "buy",
  "quantity": 10,
  "price": 178.52,
  "total": 1785.20,
  "executed_at": "2024-01-15T14:30:05Z"
}
```

### Portfolio (Authenticated)

**GET** `/api/portfolio`

Get your bot's current portfolio.

Request:
```bash
curl http://localhost:3000/api/portfolio \
  -H "X-API-Key: your-api-key-here"
```

Response:
```json
{
  "bot_id": "uuid",
  "bot_name": "MyBot",
  "cash_balance": 98214.80,
  "positions": [
    {
      "symbol": "AAPL",
      "type": "stock",
      "quantity": 10,
      "avg_cost": 178.52,
      "current_price": 180.00,
      "market_value": 1800.00,
      "unrealized_pnl": 14.80
    }
  ],
  "total_value": 100014.80,
  "total_pnl": 14.80,
  "total_pnl_percent": 0.01
}
```

All authenticated endpoints require the `X-API-Key` header with the bot's API key.

## Phase 1 Status

### Completed ✅
- ✅ Go module initialized with Fiber framework
- ✅ PostgreSQL database connection with pgx
- ✅ Database schema created (bots, positions, trades, portfolio_snapshots)
- ✅ Bot registration endpoint
- ✅ API key authentication middleware
- ✅ Market data integration (Finnhub.io with mock fallback for testing)
- ✅ Quote endpoints (single and multiple stocks)
- ✅ Stock trading endpoint (buy/sell with validation)
- ✅ Portfolio endpoint (current holdings and P&L)

### Next Steps (Phase 2)
- [ ] Options chain endpoint
- [ ] Options trading
- [ ] Leaderboard calculation
- [ ] Portfolio snapshot background job
- [ ] Options expiry background job

## Development

Run the server in development mode:
```bash
go run main.go
```

Build for production:
```bash
go build -o bottrade
./bottrade
```

## Database Schema

See `database/migrations/001_initial.sql` for the complete schema including:
- Bots table with API keys and cash balances
- Positions table for stock and options holdings
- Trades table for transaction history
- Portfolio snapshots for performance tracking
