package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "github.com/lib/pq"
	"leads_monster/internal/config"
)

type DB struct {
	*sql.DB
}

// Connect establishes a connection pool to PostgreSQL with retries and timeout
func Connect(cfg *config.Config) (*DB, error) {
	dsn := cfg.DatabaseDSN()
	var sqlDB *sql.DB
	var err error

	maxRetries := 5
	backoff := 1 * time.Second

	for i := 1; i <= maxRetries; i++ {
		sqlDB, err = sql.Open("postgres", dsn)
		if err == nil {
			sqlDB.SetMaxOpenConns(25)
			sqlDB.SetMaxIdleConns(10)
			sqlDB.SetConnMaxLifetime(15 * time.Minute)

			pingErr := sqlDB.Ping()
			if pingErr == nil {
				log.Printf("✅ [PostgreSQL] Успешно подключено к базе данных %s:%s/%s", cfg.DBHost, cfg.DBPort, cfg.DBName)
				dbInstance := &DB{sqlDB}
				// Auto-apply migrations
				if migErr := dbInstance.AutoMigrate(); migErr != nil {
					log.Printf("⚠️ [Migration] Предупреждение при выполнении миграции: %v", migErr)
				} else {
					log.Println("✅ [Migration] Схема базы данных leads_restaurants актуализирована")
				}
				return dbInstance, nil
			}
			err = pingErr
		}

		log.Printf("⏳ [PostgreSQL] Попытка %d/%d не удалась (%v), повтор через %v...", i, maxRetries, err, backoff)
		time.Sleep(backoff)
		backoff *= 2
	}

	return nil, fmt.Errorf("не удалось подключиться к базе данных после %d попыток: %w", maxRetries, err)
}

// AutoMigrate applies migrations/001_leads_monster_schema.sql
func (db *DB) AutoMigrate() error {
	migrationPaths := []string{
		"migrations/001_leads_monster_schema.sql",
		"../../migrations/001_leads_monster_schema.sql",
		"Z:/Parser/migrations/001_leads_monster_schema.sql",
	}

	var ddl string
	for _, p := range migrationPaths {
		if content, err := os.ReadFile(filepath.Clean(p)); err == nil {
			ddl = string(content)
			break
		}
	}

	if ddl == "" {
		// Fallback embedded schema if file not found
		ddl = `
		CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

		CREATE TABLE IF NOT EXISTS leads_restaurants (
			id SERIAL PRIMARY KEY,
			two_gis_id VARCHAR(100) UNIQUE NOT NULL,
			name VARCHAR(255) NOT NULL,
			legal_name VARCHAR(255) DEFAULT '',
			legal_type VARCHAR(20) DEFAULT 'UNKNOWN',
			address TEXT NOT NULL,
			city VARCHAR(100) DEFAULT 'Пермь',
			rubrics TEXT[] DEFAULT '{}',
			avg_bill_raw VARCHAR(100) DEFAULT '',
			avg_bill_val NUMERIC(10,2) DEFAULT 0.00,
			phones TEXT[] DEFAULT '{}',
			website VARCHAR(500) DEFAULT '',
			vk_url VARCHAR(500) DEFAULT '',
			tg_url VARCHAR(500) DEFAULT '',
			rating NUMERIC(3,2) DEFAULT 0.00,
			reviews_count INT DEFAULT 0,
			two_gis_url VARCHAR(500) DEFAULT '',
			rusprofile_url VARCHAR(500) DEFAULT '',
			status VARCHAR(50) DEFAULT 'new',
			priority VARCHAR(20) DEFAULT 'medium',
			notes TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_leads_city ON leads_restaurants(city);
		CREATE INDEX IF NOT EXISTS idx_leads_bill ON leads_restaurants(avg_bill_val DESC);
		CREATE INDEX IF NOT EXISTS idx_leads_reviews ON leads_restaurants(reviews_count DESC);
		CREATE INDEX IF NOT EXISTS idx_leads_legal_type ON leads_restaurants(legal_type);
		CREATE INDEX IF NOT EXISTS idx_leads_status ON leads_restaurants(status);
		CREATE INDEX IF NOT EXISTS idx_leads_rubrics ON leads_restaurants USING GIN(rubrics);

		CREATE TABLE IF NOT EXISTS leads_sessions (
			token VARCHAR(128) PRIMARY KEY,
			user_id INT NOT NULL,
			login VARCHAR(255) NOT NULL,
			role VARCHAR(50) NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			expires_at TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_leads_sessions_expires ON leads_sessions(expires_at);
		`
	}

	_, err := db.Exec(ddl)
	return err
}
