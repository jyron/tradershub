-- Extend symbol columns to support option contract symbols
-- Option contract symbols (OCC format) are 20 characters: AAPL260202C00175000
-- Stock symbols are typically 1-5 characters
--
-- SQLite Note: TEXT columns have no length limit, so no ALTER needed.
-- The initial migration already created symbol as TEXT which supports any length.

-- No-op: columns already created as TEXT in migration 001
SELECT 1;
