-- Add is_test flag to bots table for test data management
ALTER TABLE bots ADD COLUMN IF NOT EXISTS is_test BOOLEAN DEFAULT false;

-- Create index for filtering test bots
CREATE INDEX IF NOT EXISTS idx_bots_is_test ON bots(is_test);
