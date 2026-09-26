-- ============================================================================
-- QA2A RESTAURANT ERP & WAREHOUSE AUTOMATION SYSTEM
-- СУБД: PostgreSQL 14+
-- Описание: Полная архитектурная схема базы данных для бэкенда QA2A 
--           и микросервиса автоматического парсинга накладных iiko_parser.
-- ============================================================================

-- Включаем расширение для генерации UUID, если потребуется в будущем
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ============================================================================
-- 1. ОРГАНИЗАЦИИ (КОМПАНИИ / РЕСТОРАНЫ)
-- ============================================================================
CREATE TABLE IF NOT EXISTS companies (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    invite_code VARCHAR(50) NOT NULL DEFAULT '',
    iiko_host VARCHAR(255) NOT NULL DEFAULT '',
    iiko_api_login VARCHAR(255) NOT NULL DEFAULT '',
    iiko_api_password VARCHAR(255) NOT NULL DEFAULT '',
    iiko_writeoff_account VARCHAR(255) NOT NULL DEFAULT '97036ddb-b2e1-cd47-1669-c145daa9f9c5',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_companies_invite_code 
    ON companies (UPPER(invite_code)) 
    WHERE invite_code != '';
CREATE INDEX IF NOT EXISTS idx_companies_iiko_host ON companies(iiko_host) WHERE iiko_host != '';

COMMENT ON TABLE companies IS 'Юридические лица / заведения общественного питания';
COMMENT ON COLUMN companies.iiko_api_password IS 'Зашифрованный AES-256 пароль API iiko RMS';

-- ============================================================================
-- 2. ПОЛЬЗОВАТЕЛИ И АВТОРИЗАЦИЯ TELEGRAM
-- ============================================================================
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    tg_id BIGINT UNIQUE NOT NULL,
    username VARCHAR(255) NOT NULL DEFAULT '',
    full_name VARCHAR(255) NOT NULL DEFAULT '',
    token_version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_tg_id ON users(tg_id);

COMMENT ON TABLE users IS 'Пользователи экосистемы Telegram WebApp';

-- ============================================================================
-- 3. ЧЛЕНСТВО В ЗАВЕДЕНИЯХ И РОЛИ (RBAC)
-- ============================================================================
CREATE TABLE IF NOT EXISTS memberships (
    user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    role VARCHAR(50) NOT NULL DEFAULT 'user', -- 'owner', 'admin', 'manager', 'user'
    custom_title VARCHAR(255) NOT NULL DEFAULT '',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, company_id)
);

CREATE INDEX IF NOT EXISTS idx_memberships_company_user ON memberships(company_id, user_id);

COMMENT ON TABLE memberships IS 'Связь сотрудников с заведениями и их полномочия';

CREATE TABLE IF NOT EXISTS join_requests (
    id SERIAL PRIMARY KEY,
    user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_join_request UNIQUE (user_id, company_id)
);

CREATE INDEX IF NOT EXISTS idx_join_requests_company_id ON join_requests(company_id);

-- ============================================================================
-- 4. СКЛАДСКИЕ ЛОКАЦИИ
-- ============================================================================
CREATE TABLE IF NOT EXISTS locations (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    external_id VARCHAR(255) NOT NULL DEFAULT '' -- UUID склада в iiko RMS
);

CREATE INDEX IF NOT EXISTS idx_locations_company_id ON locations(company_id);
CREATE INDEX IF NOT EXISTS idx_locations_external_id ON locations(company_id, external_id);

COMMENT ON TABLE locations IS 'Складские помещения заведений (Бары, Кухни, Основные склады)';

-- ============================================================================
-- 5. СПРАВОЧНИК ТОВАРОВ И НОМЕНКЛАТУРЫ (ПОЗИЦИИ)
-- ============================================================================
CREATE TABLE IF NOT EXISTS positions (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    unit VARCHAR(50) NOT NULL DEFAULT 'ед.',
    supplier VARCHAR(255) NOT NULL DEFAULT '',
    external_id VARCHAR(255) NOT NULL DEFAULT '', -- UUID товара в iiko
    type VARCHAR(50) NOT NULL DEFAULT 'GOODS',    -- GOODS, PREPARED, DISH, MODIFIER
    conception VARCHAR(500) NOT NULL DEFAULT '',  -- Дерево родительских групп/категорий
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Уникальность товара в рамках компании по external_id iiko (для исключения дубликатов при синхронизации)
CREATE UNIQUE INDEX IF NOT EXISTS idx_positions_company_ext_id 
    ON positions (company_id, external_id) 
    WHERE external_id != '';

CREATE INDEX IF NOT EXISTS idx_positions_company_name ON positions (company_id, name);

COMMENT ON TABLE positions IS 'Каталог сырья, полуфабрикатов и товаров';

-- ============================================================================
-- 6. ЕДИНИЦЫ ИЗМЕРЕНИЯ IIKO
-- ============================================================================
CREATE TABLE IF NOT EXISTS iiko_units (
    id VARCHAR(255) NOT NULL,
    company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    PRIMARY KEY (id, company_id)
);

-- ============================================================================
-- 7. ОСТАТКИ ТОВАРОВ (ТЕКУЩИЕ БАЛАНСЫ)
-- ============================================================================
CREATE TABLE IF NOT EXISTS balances (
    company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    location_id INT NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
    position_name VARCHAR(255) NOT NULL,
    quantity NUMERIC(12, 3) NOT NULL DEFAULT 0.000,
    unit VARCHAR(50) NOT NULL DEFAULT 'ед.',
    avg_price NUMERIC(12, 2) NOT NULL DEFAULT 0.00,
    PRIMARY KEY (company_id, location_id, position_name)
);

CREATE INDEX IF NOT EXISTS idx_balances_lookup 
    ON balances(company_id, location_id, position_name);

-- ============================================================================
-- 8. ОПЕРАЦИИ СКЛАДСКОГО УЧЕТА (СПИСАНИЯ, ПЕРЕМЕЩЕНИЯ, ПРИГОТОВЛЕНИЕ)
-- ============================================================================
CREATE TABLE IF NOT EXISTS operations (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    location_id INT NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
    to_location_id INT NOT NULL DEFAULT 0,
    user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type VARCHAR(50) NOT NULL, -- writeoff, transfer_in, transfer_out, assembly_in, assembly_out
    position_name VARCHAR(255) NOT NULL,
    quantity NUMERIC(12, 3) NOT NULL,
    unit VARCHAR(50) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'approved',
    is_unlisted BOOLEAN NOT NULL DEFAULT FALSE,
    comment TEXT NOT NULL DEFAULT '',
    account_id VARCHAR(255) NOT NULL DEFAULT '97036ddb-b2e1-cd47-1669-c145daa9f9c5',
    exported_to_iiko BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_operations_company_export 
    ON operations (company_id, exported_to_iiko, type) 
    WHERE exported_to_iiko = FALSE;

CREATE INDEX IF NOT EXISTS idx_operations_created_at 
    ON operations (company_id, created_at DESC);

COMMENT ON TABLE operations IS 'Журнал складских проводок и списаний';

-- ============================================================================
-- 9. СТАТЬИ СПИСАНИЙ (СЧЕТА РАСХОДОВ)
-- ============================================================================
CREATE TABLE IF NOT EXISTS writeoff_accounts (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    external_id VARCHAR(255) NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_writeoff_accounts_company ON writeoff_accounts(company_id);

-- ============================================================================
-- 10. ЗАЯВКИ НА ЗАКУПКУ (PROCUREMENT)
-- ============================================================================
CREATE TABLE IF NOT EXISTS procurement_requests (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    approved_by INT REFERENCES users(id) ON DELETE SET NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending', -- 'pending', 'approved', 'rejected'
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_procurement_requests_status 
    ON procurement_requests (company_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS procurement_items (
    id SERIAL PRIMARY KEY,
    request_id INT NOT NULL REFERENCES procurement_requests(id) ON DELETE CASCADE,
    position_name VARCHAR(255) NOT NULL,
    quantity NUMERIC(12, 3) NOT NULL,
    unit VARCHAR(50) NOT NULL,
    is_unlisted BOOLEAN NOT NULL DEFAULT FALSE,
    supplier VARCHAR(255) NOT NULL DEFAULT '' -- UUID|Название поставщика
);

CREATE INDEX IF NOT EXISTS idx_procurement_items_request ON procurement_items(request_id);

-- ============================================================================
-- 11. АКТЫ ИНВЕНТАРИЗАЦИИ И СВЕРКИ ОСТАТКОВ
-- ============================================================================
CREATE TABLE IF NOT EXISTS inventories (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    location_id INT NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
    user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status VARCHAR(50) NOT NULL DEFAULT 'in_progress', -- 'in_progress', 'completed'
    iiko_document_id VARCHAR(255) NOT NULL DEFAULT '',
    document_number VARCHAR(255) NOT NULL DEFAULT '',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_inventories_company_status 
    ON inventories(company_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS inventory_items (
    id SERIAL PRIMARY KEY,
    inventory_id INT NOT NULL REFERENCES inventories(id) ON DELETE CASCADE,
    position_name VARCHAR(255) NOT NULL,
    external_id VARCHAR(255) NOT NULL DEFAULT '',
    expected_amount NUMERIC(12, 3) NOT NULL DEFAULT 0.000,
    actual_amount NUMERIC(12, 3) NOT NULL DEFAULT 0.000
);

CREATE INDEX IF NOT EXISTS idx_inventory_items_inv_id ON inventory_items(inventory_id);

-- ============================================================================
-- 12. ШАБЛОНЫ / БЛАНКИ ИНВЕНТАРИЗАЦИИ (ОТ БУХГАЛТЕРА)
-- ============================================================================
CREATE TABLE IF NOT EXISTS inventory_templates (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    location_id INT NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_inventory_templates_loc 
    ON inventory_templates(company_id, location_id);

CREATE TABLE IF NOT EXISTS inventory_template_items (
    id SERIAL PRIMARY KEY,
    template_id INT NOT NULL REFERENCES inventory_templates(id) ON DELETE CASCADE,
    position_name VARCHAR(255) NOT NULL,
    CONSTRAINT uq_template_position UNIQUE (template_id, position_name)
);

CREATE INDEX IF NOT EXISTS idx_inventory_template_items_tpl 
    ON inventory_template_items(template_id);

-- ============================================================================
-- 13. ИНТЕГРАЦИЯ С ПОСТАВЩИКАМИ (TELEGRAM & IIKO MAPPINGS)
-- ============================================================================
CREATE TABLE IF NOT EXISTS supplier_contacts (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    supplier_uuid VARCHAR(255) NOT NULL,
    tg_username VARCHAR(255) NOT NULL DEFAULT '',
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_company_supplier_contact UNIQUE (company_id, supplier_uuid)
);

CREATE TABLE IF NOT EXISTS supplier_mappings (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    vendor_name VARCHAR(255) NOT NULL,
    iiko_supplier_uuid VARCHAR(255) NOT NULL,
    CONSTRAINT uq_company_vendor_mapping UNIQUE (company_id, vendor_name)
);

CREATE TABLE IF NOT EXISTS store_mappings (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    consignee TEXT NOT NULL,
    iiko_store_uuid VARCHAR(255) NOT NULL,
    CONSTRAINT uq_company_store_mapping UNIQUE (company_id, consignee)
);

CREATE TABLE IF NOT EXISTS product_mappings (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    vendor_name VARCHAR(255) NOT NULL,
    vendor_item_name VARCHAR(500) NOT NULL,
    iiko_product_uuid VARCHAR(255) NOT NULL,
    iiko_product_name VARCHAR(500) NOT NULL,
    multiplier NUMERIC(12, 4) NOT NULL DEFAULT 1.0000,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_company_vendor_item_mapping UNIQUE (company_id, vendor_name, vendor_item_name)
);

CREATE INDEX IF NOT EXISTS idx_product_mappings_lookup 
    ON product_mappings (company_id, vendor_name);


-- Marketplace tables

CREATE TABLE IF NOT EXISTS marketplace_suppliers (
    id SERIAL PRIMARY KEY,
    company_name VARCHAR(255) NOT NULL,
    contact_phone VARCHAR(50) NOT NULL DEFAULT '',
    contact_email VARCHAR(255) NOT NULL DEFAULT '',
    invite_code VARCHAR(50) UNIQUE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS marketplace_offers (
    id SERIAL PRIMARY KEY,
    supplier_id INT NOT NULL REFERENCES marketplace_suppliers(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    price_type VARCHAR(50) NOT NULL DEFAULT 'exact', -- exact, from, request
    price_value NUMERIC(12, 2) NOT NULL DEFAULT 0.00,
    keywords JSONB NOT NULL DEFAULT '[]'::jsonb,
    views_count INTEGER NOT NULL DEFAULT 0,
    clicks_count INTEGER NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_marketplace_offers_active ON marketplace_offers(is_active);
CREATE INDEX IF NOT EXISTS idx_marketplace_offers_keywords ON marketplace_offers USING gin(keywords);


CREATE TABLE IF NOT EXISTS marketplace_supplier_users (
    id SERIAL PRIMARY KEY,
    supplier_id INT NOT NULL REFERENCES marketplace_suppliers(id) ON DELETE CASCADE,
    tg_id BIGINT NOT NULL UNIQUE,
    tg_username VARCHAR(255) NOT NULL DEFAULT '',
    first_name VARCHAR(255) NOT NULL DEFAULT '',
    last_name VARCHAR(255) NOT NULL DEFAULT '',
    role VARCHAR(50) NOT NULL DEFAULT 'rep', -- admin, rep
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_supplier_users_active ON marketplace_supplier_users(supplier_id, is_active);


INSERT INTO marketplace_suppliers (company_name, contact_phone) VALUES ('Демо-Поставщик', '+79991234567') ON CONFLICT DO NOTHING;
INSERT INTO marketplace_offers (supplier_id, title, description, price_type, price_value, keywords) VALUES ((SELECT id FROM marketplace_suppliers LIMIT 1), 'Сливки Чудское 33%', 'Свежая поставка, от 10 коробок.', 'from', 320.00, '["сливки", "молоко"]'::jsonb);
INSERT INTO marketplace_offers (supplier_id, title, description, price_type, price_value, keywords) VALUES ((SELECT id FROM marketplace_suppliers LIMIT 1), 'Угорь жареный (уннаги)', 'Премиум качество, коробки по 5кг', 'exact', 1150.00, '["угорь", "суши", "рыба"]'::jsonb);

-- ============================================================================
-- 14. ЗАЯВКИ В БУХГАЛТЕРИЮ (SERVICE DESK)
-- ============================================================================
CREATE TABLE IF NOT EXISTS accounting_tickets (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category VARCHAR(100) NOT NULL DEFAULT 'other',
    priority VARCHAR(50) NOT NULL DEFAULT 'normal',
    description TEXT NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'new',
    accountant_comment TEXT NOT NULL DEFAULT '',
    media_paths TEXT NOT NULL DEFAULT '[]',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_accounting_tickets_comp ON accounting_tickets(company_id, status);
CREATE INDEX IF NOT EXISTS idx_accounting_tickets_created ON accounting_tickets(created_at DESC);

-- ============================================================================
-- 15. МУЛЬТИТЕНАНТНОСТЬ БУХГАЛТЕРИИ (ACCOUNTING FIRMS & USERS)
-- ============================================================================
CREATE TABLE IF NOT EXISTS accounting_firms (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    max_companies INT NOT NULL DEFAULT 10,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS accounting_users (
    id SERIAL PRIMARY KEY,
    accounting_firm_id INT REFERENCES accounting_firms(id) ON DELETE CASCADE,
    login VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL DEFAULT '',
    role VARCHAR(50) NOT NULL DEFAULT 'accountant',
    access_token VARCHAR(255) NOT NULL DEFAULT '',
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_accounting_users_token ON accounting_users(access_token) WHERE access_token != '';

CREATE TABLE IF NOT EXISTS accountant_invites (
    id SERIAL PRIMARY KEY,
    code VARCHAR(64) UNIQUE NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_by INT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

ALTER TABLE companies ADD COLUMN IF NOT EXISTS accounting_firm_id INT REFERENCES accounting_firms(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_companies_accounting_firm ON companies(accounting_firm_id);


