-- 000007_recovery_claim.down.sql

DROP INDEX IF EXISTS idx_payout_ops_recovery_claim;

ALTER TABLE payout_operations
    DROP COLUMN IF EXISTS recovery_attempt_id,
    DROP COLUMN IF EXISTS recovery_claimed_at,
    DROP COLUMN IF EXISTS recovery_lease_expires_at,
    ALTER COLUMN submission_fingerprint TYPE VARCHAR(64);
