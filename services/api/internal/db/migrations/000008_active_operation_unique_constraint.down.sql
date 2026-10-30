-- 000008_active_operation_unique_constraint.down.sql
-- Mission 033.8.1A: Rollback active operation partial unique index.

DROP INDEX IF EXISTS uq_payout_ops_case_active;

