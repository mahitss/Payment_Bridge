-- 000004_investigations.down.sql
DROP INDEX IF EXISTS idx_investigations_created_at;
DROP INDEX IF EXISTS idx_investigations_org;
DROP INDEX IF EXISTS idx_investigations_case;
DROP TABLE IF EXISTS payout_investigations;
