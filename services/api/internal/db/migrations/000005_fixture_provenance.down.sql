-- 000005_fixture_provenance.down.sql
-- Mission 013: Rollback Demo Fixture Provenance Columns

DROP INDEX IF EXISTS idx_snapshots_synthetic;
ALTER TABLE payout_status_snapshots 
    DROP COLUMN IF EXISTS is_synthetic_fixture;

DROP INDEX IF EXISTS idx_payout_ops_synthetic;
ALTER TABLE payout_operations 
    DROP COLUMN IF EXISTS is_synthetic_fixture;

DROP INDEX IF EXISTS idx_payout_cases_synthetic;
ALTER TABLE payout_cases 
    DROP COLUMN IF EXISTS is_synthetic_fixture;
