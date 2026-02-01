#!/bin/bash

echo "BotTrade Setup Script"
echo "===================="

# Prefer postgresql@16 in PATH if present (matches common Homebrew install)
if [ -x "/opt/homebrew/opt/postgresql@16/bin/psql" ]; then
  export PATH="/opt/homebrew/opt/postgresql@16/bin:$PATH"
elif [ -x "/usr/local/opt/postgresql@16/bin/psql" ]; then
  export PATH="/usr/local/opt/postgresql@16/bin:$PATH"
fi

# Connect via TCP so we're not dependent on Unix socket path
export PGHOST=localhost

# Check if PostgreSQL client is available
if ! command -v psql &> /dev/null; then
    echo "❌ PostgreSQL client (psql) is not installed"
    echo "   Install with: brew install postgresql@16"
    exit 1
fi

# Check if PostgreSQL is accepting connections
if ! pg_isready -h localhost &> /dev/null; then
    echo "❌ PostgreSQL is not running"
    echo "   Start it with: brew services start postgresql@16"
    exit 1
fi

echo "✅ PostgreSQL is running"

# Create 'postgres' role if missing (app expects DB_USER=postgres; Homebrew default superuser is your OS user)
echo "Ensuring role 'postgres' exists..."
psql -d postgres -c "DO \$\$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='postgres') THEN CREATE ROLE postgres WITH LOGIN PASSWORD 'postgres' SUPERUSER; END IF; END \$\$;" 2>/dev/null || true

# Create database if it doesn't exist
echo "Creating database 'bottrade'..."
psql -d postgres -c "CREATE DATABASE bottrade;" 2>/dev/null || echo "   Database already exists"

echo "✅ Database ready"

# Create .env file if it doesn't exist
if [ ! -f .env ]; then
    echo "Creating .env file..."
    cp .env.example .env
    echo "✅ .env file created (edit as needed)"
else
    echo "✅ .env file already exists"
fi

echo ""
echo "Setup complete! Run the app with:"
echo "  go run main.go"
echo ""
echo "Test bot registration:"
echo '  curl -X POST http://localhost:3000/api/bots/register \'
echo '    -H "Content-Type: application/json" \'
echo '    -d '"'"'{"name":"TestBot","description":"My first bot","creator_email":"test@example.com"}'"'"
