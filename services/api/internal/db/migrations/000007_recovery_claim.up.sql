-- 000007_recovery_claim.up.sql
-- Mission 031: Time-Bounded Replay, Atomic Recovery Claims, and Unambiguous Fingerprints

ALTER TABLE payout_operations
    ALTER COLUMN submission_fingerprint TYPE VARCHAR(128),
    ADD COLUMN IF NOT EXISTS recovery_attempt_id VARCHAR(64),
    ADD COLUMN IF NOT EXISTS recovery_claimed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS recovery_lease_expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_payout_ops_recovery_claim
    ON payout_operations(recovery_attempt_id)
    WHERE recovery_attempt_id IS NOT NULL;
