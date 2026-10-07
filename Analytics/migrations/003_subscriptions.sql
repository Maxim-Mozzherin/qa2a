-- Миграция: Добавление подписки и планировщика отчетов для заведений

ALTER TABLE analytics_restaurants 
ADD COLUMN IF NOT EXISTS is_subscribed BOOLEAN DEFAULT FALSE,
ADD COLUMN IF NOT EXISTS subscription_started_at TIMESTAMPTZ,
ADD COLUMN IF NOT EXISTS telegram_recipients TEXT DEFAULT '',
ADD COLUMN IF NOT EXISTS last_weekly_report_at TIMESTAMPTZ,
ADD COLUMN IF NOT EXISTS last_monthly_report_at TIMESTAMPTZ,
ADD COLUMN IF NOT EXISTS last_synced_at TIMESTAMPTZ,
ADD COLUMN IF NOT EXISTS last_sync_status VARCHAR(50) DEFAULT 'idle',
ADD COLUMN IF NOT EXISTS last_sync_error TEXT DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_analytics_restaurants_sub ON analytics_restaurants(is_subscribed) WHERE is_subscribed = true;
