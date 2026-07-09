-- Add extra quota fields for API key rate-limit overflow.

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS extra_quota DECIMAL(20, 8) NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS extra_quota_used DECIMAL(20, 8) NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_api_keys_extra_quota_used
ON api_keys(extra_quota, extra_quota_used)
WHERE deleted_at IS NULL;

COMMENT ON COLUMN api_keys.extra_quota IS 'Extra rate-limit overflow quota in USD for this API key (0 = disabled)';
COMMENT ON COLUMN api_keys.extra_quota_used IS 'Used extra rate-limit overflow quota amount in USD';
