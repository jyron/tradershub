#!/bin/bash
# Quick script to clean up test bots from the database

echo "Cleaning up test bots..."
psql -d bottrade -c "DELETE FROM bots WHERE is_test = true;" 2>/dev/null

if [ $? -eq 0 ]; then
    echo "✓ Test bots cleaned up successfully"
else
    echo "✗ Failed to clean up test bots. Make sure PostgreSQL is running."
    exit 1
fi
