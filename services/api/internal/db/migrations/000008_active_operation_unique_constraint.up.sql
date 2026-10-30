-- 000008_active_operation_unique_constraint.up.sql
-- Mission 033.8.1A: Enforce at most one active payout operation per payout case at database level.
-- Active statuses are any status other than 'CANCELLED' and 'FAILED'.

-- Account for existing data without automatically deleting or rewriting records.
-- If duplicates exist, fail the migration with a descriptive error.
DO $$
DECLARE
    dup_count INTEGER;
BEGIN
    SELECT COUNT(*) INTO dup_count
    FROM (
        SELECT payout_case_id
        FROM payout_operations
        WHERE status NOT IN ('CANCELLED', 'FAILED')
        GROUP BY payout_case_id
        HAVING COUNT(*) > 1
    ) dups;

    IF dup_count > 0 THEN
        RAISE EXCEPTION 'Cannot create unique index uq_payout_ops_case_active: found % case(s) with multiple active payout operations. Manual remediation required per ADR-005 without deleting or rewriting records.', dup_count;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS uq_payout_ops_case_active
    ON payout_operations(payout_case_id)
    WHERE status NOT IN ('CANCELLED', 'FAILED');

