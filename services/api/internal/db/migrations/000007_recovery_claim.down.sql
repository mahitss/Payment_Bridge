-- 000007_recovery_claim.down.sql
-- Mission 032 Finding 3: Safe migration rollback with stored v2 fingerprints

DROP INDEX IF EXISTS idx_payout_ops_recovery_claim;

ALTER TABLE payout_operations
    DROP COLUMN IF EXISTS recovery_attempt_id,
    DROP COLUMN IF EXISTS recovery_claimed_at,
    DROP COLUMN IF EXISTS recovery_lease_expires_at;

-- NOTE: submission_fingerprint is deliberately NOT narrowed back to VARCHAR(64).
-- In Mission 031+, canonical v2 fingerprints are 67-71 characters (e.g. v2:sha256:<64hex>).
-- Attempting to alter the column to VARCHAR(64) on rollback would fail with:
-- "ERROR: value too long for type character varying(64)" if any v2 fingerprints exist,
-- or risk data loss/truncation of cryptographic integrity evidence.
-- Retaining VARCHAR(128) safely preserves existing cryptographic evidence across rollback
-- without data loss or rollback failures.
