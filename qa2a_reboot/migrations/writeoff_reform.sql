-- Migration: writeoff_reform.sql
-- Multi-Tier Approval System & Shift Notes for writeoffs

ALTER TABLE operations ADD COLUMN IF NOT EXISTS approved_by INT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE operations ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_operations_pending_writeoffs 
ON operations (company_id, type, status) 
WHERE type = 'writeoff' AND status = 'pending';

CREATE TABLE IF NOT EXISTS writeoff_shift_notes (
    company_id INT NOT NULL,
    account_id VARCHAR(255) NOT NULL,
    business_date VARCHAR(20) NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    chef_user_id INT REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (company_id, account_id, business_date)
);
