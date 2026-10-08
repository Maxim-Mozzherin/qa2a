-- 005_analytics_store_balances.sql
-- Таблица снимков складских остатков ресторана на дату начала периода

CREATE TABLE IF NOT EXISTS analytics_store_balances (
    id SERIAL PRIMARY KEY,
    restaurant_id INT NOT NULL REFERENCES analytics_restaurants(id) ON DELETE CASCADE,
    balance_date DATE NOT NULL,
    timestamp_str VARCHAR(50) NOT NULL,
    store_uuid VARCHAR(64) NOT NULL,
    product_uuid VARCHAR(64) NOT NULL,
    product_name VARCHAR(255) NOT NULL,
    amount NUMERIC(14, 4) DEFAULT 0,
    cost_rub NUMERIC(14, 2) DEFAULT 0,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_store_balances_rest_date ON analytics_store_balances(restaurant_id, balance_date);
CREATE INDEX IF NOT EXISTS idx_store_balances_prod ON analytics_store_balances(restaurant_id, product_name);
