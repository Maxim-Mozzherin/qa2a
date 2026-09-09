package database

import (
	"context"
	"fmt"
	"time"

	"qa2a/internal/config"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

// New инициализирует подключение к PostgreSQL по переданной DSN-строке
// и устанавливает сбалансированные параметры пула соединений по умолчанию.
func New(dsn string) (*sqlx.DB, error) {
	db, err := sqlx.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("ошибка открытия дескриптора БД: %w", err)
	}

	// Дефолтные параметры пула соединений
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(15 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	// Проверяем физическое подключение к СУБД с жестким таймаутом
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("не удалось установить соединение с PostgreSQL (Ping timeout): %w", err)
	}

	return db, nil
}

// NewWithConfig создает подключение к БД, применяя все параметры пула соединений из конфигурации приложения.
func NewWithConfig(cfg *config.Config) (*sqlx.DB, error) {
	db, err := sqlx.Open("postgres", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("ошибка открытия дескриптора БД: %w", err)
	}

	// Применяем тонкие настройки пула из Config
	db.SetMaxOpenConns(cfg.DBMaxOpenConns)
	db.SetMaxIdleConns(cfg.DBMaxIdleConns)
	db.SetConnMaxLifetime(cfg.DBConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.DBConnMaxIdleTime)

	// Проверяем доступность базы данных
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ошибка проверки соединения с БД (%s:%s/%s): %w", cfg.DBHost, cfg.DBPort, cfg.DBName, err)
	}

	return db, nil
}

