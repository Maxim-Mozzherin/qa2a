-- Migration 002: Analytics Audit Summaries
CREATE TABLE IF NOT EXISTS analytics_audit_summaries (
    id SERIAL PRIMARY KEY,
    restaurant_id INT REFERENCES analytics_restaurants(id) ON DELETE CASCADE,
    period_days INT NOT NULL,
    summary_text TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(restaurant_id, period_days)
);
