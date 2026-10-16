-- Versioned probe metadata is stored on each result, independent of later plan edits.
ALTER TABLE scheduled_test_plans ADD COLUMN IF NOT EXISTS test_kind VARCHAR(40) NOT NULL DEFAULT 'connectivity';
ALTER TABLE scheduled_test_results ADD COLUMN IF NOT EXISTS test_kind VARCHAR(40) NOT NULL DEFAULT 'connectivity';
ALTER TABLE scheduled_test_results ADD COLUMN IF NOT EXISTS model_id VARCHAR(100) NOT NULL DEFAULT '';
