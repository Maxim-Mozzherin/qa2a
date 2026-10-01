package db

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"analytics_service/internal/config"
	_ "github.com/lib/pq"
)

func Connect(cfg *config.Config) (*sql.DB, error) {
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable connect_timeout=10",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPass, cfg.DBName)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("ошибка открытия пула PostgreSQL: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(15 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ошибка пинга PostgreSQL: %w", err)
	}

	log.Printf("✅ [Analytics DB] Успешное подключение к PostgreSQL (%s:%s/%s)", cfg.DBHost, cfg.DBPort, cfg.DBName)
	return db, nil
}
