-- Migration: writeoff_and_security_reform.sql
-- 1. Operations approval tracking
ALTER TABLE operations 
ADD COLUMN IF NOT EXISTS approved_by INT REFERENCES users(id) ON DELETE SET NULL,
ADD COLUMN IF NOT EXISTS approved_at TIMESTAMP WITH TIME ZONE;

CREATE INDEX IF NOT EXISTS idx_operations_pending_writeoffs 
ON operations (company_id, type, status) 
WHERE type = 'writeoff' AND status = 'pending';

-- 2. Shift notes per expense account and business day
CREATE TABLE IF NOT EXISTS writeoff_shift_notes (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    account_id VARCHAR(255) NOT NULL,
    business_date DATE NOT NULL,
    chef_user_id INT REFERENCES users(id) ON DELETE SET NULL,
    note VARCHAR(200) NOT NULL DEFAULT '',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_writeoff_shift_note UNIQUE (company_id, account_id, business_date)
);

CREATE INDEX IF NOT EXISTS idx_writeoff_shift_notes_lookup 
ON writeoff_shift_notes (company_id, account_id, business_date);

-- 3. Composite index for purchase_history price lookups
CREATE INDEX IF NOT EXISTS idx_purchase_history_median_lookup 
ON purchase_history (company_id, iiko_product_uuid, invoice_date);

-- Ensure idempotency for writeoff_shift_notes if table already exists without all columns/constraints
ALTER TABLE writeoff_shift_notes ADD COLUMN IF NOT EXISTS id SERIAL;
ALTER TABLE writeoff_shift_notes ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE writeoff_shift_notes ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

DO $$ 
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'uq_writeoff_shift_note') THEN
        ALTER TABLE writeoff_shift_notes ADD CONSTRAINT uq_writeoff_shift_note UNIQUE (company_id, account_id, business_date);
    END IF;
END $$;
