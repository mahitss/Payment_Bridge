-- 000005_fixture_provenance.up.sql
-- Mission 013: Demo Fixture Provenance, Synthetic Isolation, and Idempotent Seeding

ALTER TABLE payout_cases 
    ADD COLUMN IF NOT EXISTS is_synthetic_fixture BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_payout_cases_synthetic 
    ON payout_cases(is_synthetic_fixture);

ALTER TABLE payout_operations 
    ADD COLUMN IF NOT EXISTS is_synthetic_fixture BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_payout_ops_synthetic 
    ON payout_operations(is_synthetic_fixture);

ALTER TABLE payout_status_snapshots 
    ADD COLUMN IF NOT EXISTS is_synthetic_fixture BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_snapshots_synthetic 
    ON payout_status_snapshots(is_synthetic_fixture);
