-- Extend symbol columns to support option contract symbols
-- Option contract symbols (OCC format) are 20 characters: AAPL260202C00175000
-- Stock symbols are typically 1-5 characters
-- Using VARCHAR(30) to provide headroom

ALTER TABLE positions ALTER COLUMN symbol TYPE VARCHAR(30);
ALTER TABLE trades ALTER COLUMN symbol TYPE VARCHAR(30);
