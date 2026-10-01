-- Миграция: Схема данных отдельного сервиса ресторанной аналитики (Analytics Service)

CREATE TABLE IF NOT EXISTS analytics_restaurants (
    id SERIAL PRIMARY KEY,
    company_id INT UNIQUE, -- связка с существующей таблицей companies при необходимости
    name VARCHAR(255) NOT NULL,
    cuisine_type VARCHAR(100) DEFAULT 'other',
    city VARCHAR(100) DEFAULT 'Пермь',
    iiko_host VARCHAR(255) NOT NULL,
    iiko_login VARCHAR(255) NOT NULL,
    iiko_password_enc TEXT NOT NULL,
    monthly_revenue NUMERIC(14,2) DEFAULT 3500000.00, -- оценочная выручка для расчета Food Cost Impact
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS analytics_invoices (
    id SERIAL PRIMARY KEY,
    restaurant_id INT REFERENCES analytics_restaurants(id) ON DELETE CASCADE,
    iiko_doc_id UUID NOT NULL,
    doc_number VARCHAR(100),
    incoming_number VARCHAR(100),
    doc_date DATE NOT NULL,
    supplier_uuid UUID,
    supplier_name VARCHAR(255),
    default_store_uuid UUID,
    default_store_name VARCHAR(255),
    total_sum NUMERIC(14,2) DEFAULT 0,
    status VARCHAR(50),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(restaurant_id, iiko_doc_id)
);

CREATE TABLE IF NOT EXISTS analytics_invoice_items (
    id SERIAL PRIMARY KEY,
    invoice_id INT REFERENCES analytics_invoices(id) ON DELETE CASCADE,
    restaurant_id INT NOT NULL,
    doc_date DATE NOT NULL,
    product_uuid UUID NOT NULL,
    product_name VARCHAR(500) NOT NULL,
    product_article VARCHAR(100),
    supplier_product_name VARCHAR(500),
    supplier_product_article VARCHAR(100),
    is_commodity BOOLEAN DEFAULT FALSE,
    detected_brand VARCHAR(100) DEFAULT '',
    canonical_category VARCHAR(100) DEFAULT 'Прочее',
    quantity NUMERIC(12,4) NOT NULL,
    unit VARCHAR(50) DEFAULT 'кг',
    price_per_unit NUMERIC(12,2) NOT NULL,
    total_sum NUMERIC(14,2) NOT NULL,
    vat_sum NUMERIC(14,2) DEFAULT 0
);

-- Индексы для сверхбыстрой аналитики и когортных расчетов
CREATE INDEX IF NOT EXISTS idx_analytics_items_rest_date ON analytics_invoice_items(restaurant_id, doc_date DESC);
CREATE INDEX IF NOT EXISTS idx_analytics_items_cat ON analytics_invoice_items(canonical_category, doc_date DESC);
CREATE INDEX IF NOT EXISTS idx_analytics_items_brand ON analytics_invoice_items(detected_brand, doc_date DESC) WHERE detected_brand <> '';
CREATE INDEX IF NOT EXISTS idx_analytics_items_commodity ON analytics_invoice_items(is_commodity, doc_date DESC);
CREATE INDEX IF NOT EXISTS idx_analytics_items_lookup ON analytics_invoice_items(restaurant_id, product_name, doc_date DESC);
