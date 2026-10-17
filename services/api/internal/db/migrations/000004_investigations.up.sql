-- 000004_investigations.up.sql
-- Mission 004: AI Investigation Engine & Evidence-Grounded Recommendations

CREATE TABLE IF NOT EXISTS payout_investigations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payout_case_id UUID NOT NULL REFERENCES payout_cases(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    requested_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    provider VARCHAR(64) NOT NULL,
    model VARCHAR(128) NOT NULL,
    prompt_version VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    confidence VARCHAR(32) NOT NULL DEFAULT 'INSUFFICIENT_EVIDENCE',
    requires_human_review BOOLEAN NOT NULL DEFAULT true,
    observed_facts JSONB NOT NULL DEFAULT '[]'::jsonb,
    findings JSONB NOT NULL DEFAULT '[]'::jsonb,
    recommended_next_steps JSONB NOT NULL DEFAULT '[]'::jsonb,
    missing_information JSONB NOT NULL DEFAULT '[]'::jsonb,
    risk_flags JSONB NOT NULL DEFAULT '[]'::jsonb,
    limitations JSONB NOT NULL DEFAULT '[]'::jsonb,
    raw_response JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_investigations_case ON payout_investigations(payout_case_id);
CREATE INDEX IF NOT EXISTS idx_investigations_org ON payout_investigations(organization_id);
CREATE INDEX IF NOT EXISTS idx_investigations_created_at ON payout_investigations(created_at DESC);
