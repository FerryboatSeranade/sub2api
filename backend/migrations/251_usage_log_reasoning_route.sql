-- Additive nullable column: old blue/green versions remain compatible.
SET LOCAL lock_timeout = '5s';
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS reasoning_route TEXT;
