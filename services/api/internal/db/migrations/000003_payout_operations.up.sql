-- 000003_payout_operations.up.sql
-- Mission 003: Payout Operations, Dual-Custody Approval, and Status Snapshots

CREATE TABLE IF NOT EXISTS payout_cases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    created_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    reference VARCHAR(64) NOT NULL,
    recipient_email VARCHAR(255) NOT NULL,
    recipient_name VARCHAR(255) NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
    status VARCHAR(32) NOT NULL DEFAULT 'DRAFT',
    risk_score NUMERIC(5, 2) NOT NULL DEFAULT 0.0,
    dual_approval_required BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_payout_cases_org_reference UNIQUE (organization_id, reference)
);

CREATE INDEX IF NOT EXISTS idx_payout_cases_org ON payout_cases(organization_id);
CREATE INDEX IF NOT EXISTS idx_payout_cases_status ON payout_cases(status);
CREATE INDEX IF NOT EXISTS idx_payout_cases_created_at ON payout_cases(created_at DESC);

CREATE TABLE IF NOT EXISTS payout_operations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payout_case_id UUID NOT NULL REFERENCES payout_cases(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    initiated_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    sender_batch_id VARCHAR(128) NOT NULL UNIQUE,
    sender_item_id VARCHAR(128) NOT NULL UNIQUE,
    amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    status VARCHAR(32) NOT NULL DEFAULT 'PREPARED',
    paypal_payout_batch_id VARCHAR(128),
    paypal_batch_status VARCHAR(64),
    paypal_payout_item_id VARCHAR(128),
    paypal_item_status VARCHAR(64),
    error_message TEXT,
    raw_provider_response JSONB,
    requires_dual_approval BOOLEAN NOT NULL DEFAULT false,
    approved_by_user_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    approved_at TIMESTAMPTZ,
    submitted_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_payout_ops_case ON payout_operations(payout_case_id);
CREATE INDEX IF NOT EXISTS idx_payout_ops_org ON payout_operations(organization_id);
CREATE INDEX IF NOT EXISTS idx_payout_ops_batch ON payout_operations(paypal_payout_batch_id);
CREATE INDEX IF NOT EXISTS idx_payout_ops_item ON payout_operations(paypal_payout_item_id);
CREATE INDEX IF NOT EXISTS idx_payout_ops_status ON payout_operations(status);

CREATE TABLE IF NOT EXISTS payout_status_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payout_operation_id UUID NOT NULL REFERENCES payout_operations(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    provider VARCHAR(32) NOT NULL DEFAULT 'PAYPAL',
    batch_status VARCHAR(64) NOT NULL,
    item_status VARCHAR(64),
    raw_response JSONB NOT NULL DEFAULT '{}'::jsonb,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_snapshots_operation ON payout_status_snapshots(payout_operation_id);
CREATE INDEX IF NOT EXISTS idx_snapshots_org ON payout_status_snapshots(organization_id);
