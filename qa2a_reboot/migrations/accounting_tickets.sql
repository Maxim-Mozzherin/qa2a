CREATE TABLE IF NOT EXISTS accounting_tickets (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category VARCHAR(100) NOT NULL DEFAULT 'other',
    priority VARCHAR(50) NOT NULL DEFAULT 'normal',
    description TEXT NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'new',
    accountant_comment TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_accounting_tickets_comp ON accounting_tickets(company_id, status);
CREATE INDEX IF NOT EXISTS idx_accounting_tickets_created ON accounting_tickets(created_at DESC);

ALTER TABLE accounting_tickets ADD COLUMN IF NOT EXISTS image_path VARCHAR(500) DEFAULT '';
ALTER TABLE accounting_tickets RENAME COLUMN image_path TO media_paths;
ALTER TABLE accounting_tickets ALTER COLUMN media_paths TYPE TEXT;
ALTER TABLE accounting_tickets ALTER COLUMN media_paths SET DEFAULT '[]';
