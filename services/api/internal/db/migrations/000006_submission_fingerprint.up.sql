-- 000006_submission_fingerprint.up.sql
-- Mission 030: Submission Fingerprint for Safe Ambiguous Recovery

ALTER TABLE payout_operations 
    ADD COLUMN IF NOT EXISTS submission_fingerprint VARCHAR(64);

CREATE INDEX IF NOT EXISTS idx_payout_ops_fingerprint 
    ON payout_operations(submission_fingerprint);
