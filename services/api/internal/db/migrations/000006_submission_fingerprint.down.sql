-- 000006_submission_fingerprint.down.sql

DROP INDEX IF EXISTS idx_payout_ops_fingerprint;
ALTER TABLE payout_operations DROP COLUMN IF EXISTS submission_fingerprint;
