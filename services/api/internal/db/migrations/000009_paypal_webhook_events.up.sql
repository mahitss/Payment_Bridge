-- 000009_paypal_webhook_events.up.sql
-- Mission 037: Genuine PayPal Payout Webhooks & Durable Reconciliation

CREATE TABLE IF NOT EXISTS paypal_webhook_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    paypal_event_id VARCHAR(128) NOT NULL UNIQUE,
    event_type VARCHAR(128) NOT NULL,
    resource_type VARCHAR(64) NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    transmission_id VARCHAR(128) NOT NULL,
    transmission_time TIMESTAMPTZ,
    webhook_id VARCHAR(128) NOT NULL,
    payout_batch_id VARCHAR(128),
    payout_item_id VARCHAR(128),
    signature_verified BOOLEAN NOT NULL DEFAULT false,
    processing_status VARCHAR(64) NOT NULL DEFAULT 'RECEIVED',
    processing_error TEXT,
    payload_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    organization_id UUID REFERENCES organizations(id) ON DELETE SET NULL,
    payout_operation_id UUID REFERENCES payout_operations(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_paypal_webhook_events_type ON paypal_webhook_events(event_type);
CREATE INDEX IF NOT EXISTS idx_paypal_webhook_events_batch ON paypal_webhook_events(payout_batch_id);
CREATE INDEX IF NOT EXISTS idx_paypal_webhook_events_item ON paypal_webhook_events(payout_item_id);
CREATE INDEX IF NOT EXISTS idx_paypal_webhook_events_status ON paypal_webhook_events(processing_status);
CREATE INDEX IF NOT EXISTS idx_paypal_webhook_events_created ON paypal_webhook_events(created_at DESC);
