-- Миграция: Акты списания iiko RMS (Analytics Service)

CREATE TABLE IF NOT EXISTS analytics_writeoffs (
    id SERIAL PRIMARY KEY,
    restaurant_id INT REFERENCES analytics_restaurants(id) ON DELETE CASCADE,
    iiko_doc_id UUID NOT NULL,
    doc_number VARCHAR(100),
    doc_date DATE NOT NULL,
    date_incoming VARCHAR(50),
    status VARCHAR(50),
    store_id UUID,
    account_id UUID,
    comment TEXT,
    total_cost NUMERIC(14,2) DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(restaurant_id, iiko_doc_id)
);

CREATE TABLE IF NOT EXISTS analytics_writeoff_items (
    id SERIAL PRIMARY KEY,
    writeoff_id INT REFERENCES analytics_writeoffs(id) ON DELETE CASCADE,
    restaurant_id INT NOT NULL,
    doc_date DATE NOT NULL,
    product_uuid UUID NOT NULL,
    product_name VARCHAR(500) NOT NULL,
    amount NUMERIC(12,4) NOT NULL,
    unit VARCHAR(50) DEFAULT 'кг',
    cost NUMERIC(14,2) NOT NULL DEFAULT 0,
    store_id UUID,
    account_id UUID,
    comment TEXT
);

CREATE INDEX IF NOT EXISTS idx_analytics_writeoffs_rest_date ON analytics_writeoff_items(restaurant_id, doc_date DESC);
CREATE INDEX IF NOT EXISTS idx_analytics_writeoffs_product ON analytics_writeoff_items(restaurant_id, product_name);
