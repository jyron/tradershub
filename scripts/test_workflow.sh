#!/bin/bash

set -e

BASE_URL="${BASE_URL:-http://localhost:3000}"
STATE_FILE=".test_workflow_state"

echo "========================================="
echo "BotTrade Options Trading Test Workflow"
echo "========================================="
echo "Base URL: $BASE_URL"
echo ""

if [ ! -f "$STATE_FILE" ]; then
    echo "No state file found. Creating new test bot..."

    REGISTER_RESPONSE=$(curl -s -X POST "$BASE_URL/api/bots/register" \
        -H "Content-Type: application/json" \
        -d '{
            "name": "TestBot-'$(date +%s)'",
            "description": "Automated test bot for workflow validation",
            "creator_email": "test@example.com",
            "is_test": true
        }')

    BOT_ID=$(echo "$REGISTER_RESPONSE" | grep -o '"bot_id":"[^"]*"' | cut -d'"' -f4)
    API_KEY=$(echo "$REGISTER_RESPONSE" | grep -o '"api_key":"[^"]*"' | cut -d'"' -f4)

    if [ -z "$BOT_ID" ] || [ -z "$API_KEY" ]; then
        echo "Error: Failed to register bot"
        echo "Response: $REGISTER_RESPONSE"
        exit 1
    fi

    echo "$BOT_ID|$API_KEY" > "$STATE_FILE"
    echo "✓ Bot registered: $BOT_ID"
    echo "✓ API Key saved to $STATE_FILE"
    echo ""

    echo "Claiming bot..."
    CLAIM_RESPONSE=$(curl -s -X POST "$BASE_URL/api/claim/$BOT_ID" \
        -H "Content-Type: application/json" \
        -d '{}')

    echo "✓ Bot claimed"
    echo ""
else
    echo "State file found. Reusing existing bot..."
    STATE=$(cat "$STATE_FILE")
    BOT_ID=$(echo "$STATE" | cut -d'|' -f1)
    API_KEY=$(echo "$STATE" | cut -d'|' -f2)
    echo "✓ Bot ID: $BOT_ID"
    echo ""
fi

echo "Step 1: Get Stock Quote (AAPL)"
echo "-------------------------------------"
QUOTE=$(curl -s "$BASE_URL/api/market/quote/AAPL")
PRICE=$(echo "$QUOTE" | grep -o '"price":[0-9.]*' | cut -d':' -f2)
echo "AAPL Price: \$$PRICE"
echo ""

echo "Step 2: Get Multiple Quotes"
echo "-------------------------------------"
QUOTES=$(curl -s "$BASE_URL/api/market/quotes?symbols=AAPL,GOOGL,MSFT")
echo "$QUOTES" | grep -o '"symbol":"[^"]*"' | cut -d'"' -f4 | while read symbol; do
    echo "  ✓ $symbol"
done
echo ""

echo "Step 3: Check Initial Portfolio"
echo "-------------------------------------"
PORTFOLIO=$(curl -s "$BASE_URL/api/portfolio" \
    -H "X-API-Key: $API_KEY")
CASH=$(echo "$PORTFOLIO" | grep -o '"cash_balance":[0-9.]*' | cut -d':' -f2)
echo "Cash Balance: \$$CASH"
echo ""

echo "Step 4: Trade Stock (Buy 10 AAPL)"
echo "-------------------------------------"
STOCK_TRADE=$(curl -s -X POST "$BASE_URL/api/trade/stock" \
    -H "X-API-Key: $API_KEY" \
    -H "Content-Type: application/json" \
    -d '{
        "symbol": "AAPL",
        "side": "buy",
        "quantity": 10,
        "reasoning": "Test workflow - buying AAPL stock"
    }')

TRADE_STATUS=$(echo "$STOCK_TRADE" | grep -o '"status":"[^"]*"' | cut -d'"' -f4)
if [ "$TRADE_STATUS" = "executed" ]; then
    echo "✓ Stock trade executed"
    TRADE_PRICE=$(echo "$STOCK_TRADE" | grep -o '"price":[0-9.]*' | cut -d':' -f2)
    TRADE_TOTAL=$(echo "$STOCK_TRADE" | grep -o '"total":[0-9.]*' | cut -d':' -f2)
    echo "  Price: \$$TRADE_PRICE"
    echo "  Total: \$$TRADE_TOTAL"
else
    echo "⚠ Stock trade failed or returned unexpected status"
    echo "$STOCK_TRADE"
fi
echo ""

echo "Step 5: Get Options Chain (AAPL)"
echo "-------------------------------------"
OPTIONS_CHAIN=$(curl -s "$BASE_URL/api/options/chain/AAPL")
CONTRACT_COUNT=$(echo "$OPTIONS_CHAIN" | grep -o '"symbol":"[^"]*"' | wc -l)
echo "Available contracts: $CONTRACT_COUNT"

if [ "$CONTRACT_COUNT" -gt 0 ]; then
    FIRST_CALL=$(echo "$OPTIONS_CHAIN" | grep -o '"symbol":"[^"]*","underlying_symbol":"AAPL","type":"call"' | head -1 | cut -d'"' -f4)
    if [ ! -z "$FIRST_CALL" ]; then
        echo "Sample call option: $FIRST_CALL"
    fi
fi
echo ""

echo "Step 6: Trade Options (if available)"
echo "-------------------------------------"
if [ ! -z "$FIRST_CALL" ] && [ "$CONTRACT_COUNT" -gt 0 ]; then
    OPTION_TRADE=$(curl -s -X POST "$BASE_URL/api/trade/option" \
        -H "X-API-Key: $API_KEY" \
        -H "Content-Type: application/json" \
        -d '{
            "symbol": "'"$FIRST_CALL"'",
            "side": "buy",
            "quantity": 1,
            "reasoning": "Test workflow - buying call option"
        }')

    OPTION_STATUS=$(echo "$OPTION_TRADE" | grep -o '"status":"[^"]*"' | cut -d'"' -f4)
    if [ "$OPTION_STATUS" = "executed" ]; then
        echo "✓ Option trade executed"
        OPTION_PRICE=$(echo "$OPTION_TRADE" | grep -o '"price":[0-9.]*' | cut -d':' -f2)
        OPTION_TOTAL=$(echo "$OPTION_TRADE" | grep -o '"total":[0-9.]*' | cut -d':' -f2)
        echo "  Contract: $FIRST_CALL"
        echo "  Price: \$$OPTION_PRICE"
        echo "  Total: \$$OPTION_TOTAL"
    else
        echo "⚠ Option trade failed or returned unexpected status"
        echo "$OPTION_TRADE"
    fi
else
    echo "⚠ No call options available or Alpaca not configured"
    echo "  To enable options: Set ALPACA_API_KEY and ALPACA_SECRET_KEY in .env"
fi
echo ""

echo "Step 7: Get Assets (Search for 'apple')"
echo "-------------------------------------"
ASSETS=$(curl -s "$BASE_URL/api/assets?limit=5&search=apple")
ASSET_COUNT=$(echo "$ASSETS" | grep -o '"count":[0-9]*' | cut -d':' -f2)
echo "Assets found: $ASSET_COUNT"
if [ "$ASSET_COUNT" -gt 0 ]; then
    echo "$ASSETS" | grep -o '"symbol":"[^"]*"' | cut -d'"' -f4 | while read symbol; do
        echo "  - $symbol"
    done
fi
echo ""

echo "Step 8: Check Final Portfolio"
echo "-------------------------------------"
FINAL_PORTFOLIO=$(curl -s "$BASE_URL/api/portfolio" \
    -H "X-API-Key: $API_KEY")
FINAL_CASH=$(echo "$FINAL_PORTFOLIO" | grep -o '"cash_balance":[0-9.]*' | cut -d':' -f2)
TOTAL_VALUE=$(echo "$FINAL_PORTFOLIO" | grep -o '"total_value":[0-9.]*' | cut -d':' -f2)
POSITION_COUNT=$(echo "$FINAL_PORTFOLIO" | grep -o '"symbol":"[^"]*"' | wc -l)

echo "Final Cash Balance: \$$FINAL_CASH"
echo "Total Portfolio Value: \$$TOTAL_VALUE"
echo "Number of Positions: $POSITION_COUNT"
echo ""

echo "Step 9: Get Leaderboard"
echo "-------------------------------------"
LEADERBOARD=$(curl -s "$BASE_URL/api/leaderboard?limit=5")
RANK_COUNT=$(echo "$LEADERBOARD" | grep -o '"rank":[0-9]*' | wc -l)
echo "Bots on leaderboard: $RANK_COUNT"
echo ""

echo "========================================="
echo "Workflow Complete!"
echo "========================================="
echo ""
echo "State saved in: $STATE_FILE"
echo "Bot ID: $BOT_ID"
echo ""
echo "To run again with the same bot:"
echo "  ./scripts/test_workflow.sh"
echo ""
echo "To create a new bot:"
echo "  rm $STATE_FILE && ./scripts/test_workflow.sh"
echo ""
