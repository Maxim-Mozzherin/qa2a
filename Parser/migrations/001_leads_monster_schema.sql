-- ============================================================================
-- Leads Monster: B2B HoReCa Lead Intelligence & Scraper Platform
-- Migration: 001_leads_monster_schema.sql
-- ============================================================================

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Try creating pg_trgm for fast fuzzy search if allowed
DO $$
BEGIN
    CREATE EXTENSION IF NOT EXISTS pg_trgm;
EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'pg_trgm extension could not be installed, falling back to standard text index';
END $$;

CREATE TABLE IF NOT EXISTS leads_restaurants (
    id SERIAL PRIMARY KEY,
    two_gis_id VARCHAR(100) UNIQUE NOT NULL,
    name VARCHAR(255) NOT NULL,
    legal_name VARCHAR(255) DEFAULT '',             -- Extracted ООО / ИП from items.org.name
    legal_type VARCHAR(20) DEFAULT 'UNKNOWN',       -- 'OOO', 'IP', 'OTHER', 'NONE'
    address TEXT NOT NULL,
    city VARCHAR(100) DEFAULT 'Пермь',
    rubrics TEXT[] DEFAULT '{}',                    -- Array of tags: Ресторан, Бар, Паназия, etc.
    avg_bill_raw VARCHAR(100) DEFAULT '',           -- Raw string, e.g., "Чек 1600 ₽"
    avg_bill_val NUMERIC(10,2) DEFAULT 0.00,        -- Numeric extracted value for slider/range filtering
    phones TEXT[] DEFAULT '{}',                     -- Array of cleaned phone numbers
    website VARCHAR(500) DEFAULT '',
    vk_url VARCHAR(500) DEFAULT '',
    tg_url VARCHAR(500) DEFAULT '',
    rating NUMERIC(3,2) DEFAULT 0.00,
    reviews_count INT DEFAULT 0,
    two_gis_url VARCHAR(500) DEFAULT '',
    rusprofile_url VARCHAR(500) DEFAULT '',         -- Auto-generated direct link to search company by legal_name
    status VARCHAR(50) DEFAULT 'new',               -- 'new', 'verified_rusprofile', 'contacted', 'pilot_sent', 'signed_1rub', 'rejected'
    priority VARCHAR(20) DEFAULT 'medium',          -- 'high' (bill > 1200 + reviews > 80), 'medium', 'low'
    notes TEXT DEFAULT '',                          -- Manual notes for the operator
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- B-Tree and GIN indexes for complex multi-filtering
CREATE INDEX IF NOT EXISTS idx_leads_city ON leads_restaurants(city);
CREATE INDEX IF NOT EXISTS idx_leads_bill ON leads_restaurants(avg_bill_val DESC);
CREATE INDEX IF NOT EXISTS idx_leads_reviews ON leads_restaurants(reviews_count DESC);
CREATE INDEX IF NOT EXISTS idx_leads_legal_type ON leads_restaurants(legal_type);
CREATE INDEX IF NOT EXISTS idx_leads_status ON leads_restaurants(status);
CREATE INDEX IF NOT EXISTS idx_leads_rubrics ON leads_restaurants USING GIN(rubrics);

-- Try to create trigram index if pg_trgm is present, else standard btree
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm') THEN
        CREATE INDEX IF NOT EXISTS idx_leads_name_trgm ON leads_restaurants USING gin (name gin_trgm_ops);
    ELSE
        CREATE INDEX IF NOT EXISTS idx_leads_name ON leads_restaurants(name);
    END IF;
END $$;

-- Session table for super-admin cookie authentication
CREATE TABLE IF NOT EXISTS leads_sessions (
    token VARCHAR(128) PRIMARY KEY,
    user_id INT NOT NULL,
    login VARCHAR(255) NOT NULL,
    role VARCHAR(50) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_leads_sessions_expires ON leads_sessions(expires_at);
