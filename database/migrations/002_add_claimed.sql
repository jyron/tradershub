-- Add claimed status to bots table
ALTER TABLE bots ADD COLUMN IF NOT EXISTS claimed BOOLEAN DEFAULT false;
