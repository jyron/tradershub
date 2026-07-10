-- Rename the scenario_versions.bars_frozen_at column to bars_captured_at.
-- Purely a naming change (metadata-only) — the value is still the UTC timestamp
-- at which a scenario version's historic bars were snapshotted. No data changes.
ALTER TABLE scenario_versions RENAME COLUMN bars_frozen_at TO bars_captured_at;
