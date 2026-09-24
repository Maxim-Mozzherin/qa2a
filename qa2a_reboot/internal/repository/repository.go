package repository

import (
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// Repository инкапсулирует подключение к базе данных и методы работы с сущностями QA2A.
type Repository struct {
	db *sqlx.DB
}

// New создает новый экземпляр репозитория.
func New(db *sqlx.DB) *Repository {
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS join_requests (
			id SERIAL PRIMARY KEY,
			user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			company_id INT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			CONSTRAINT uq_join_request UNIQUE (user_id, company_id)
		);
	`)
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS token_version INT NOT NULL DEFAULT 1;`)
	return &Repository{db: db}
}

// GetDb возвращает низкоуровневый дескриптор sqlx.DB (для специализированных raw-запросов).
func (r *Repository) GetDb() *sqlx.DB {
	return r.db
}

// ExecuteInTx выполняет переданную функцию внутри единой транзакции БД.
// Гарантирует автоматический откат (Rollback) при ошибке или panic, и Commit при успехе.
func (r *Repository) ExecuteInTx(fn func(*sqlx.Tx) error) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return fmt.Errorf("ошибка старта транзакции: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p) // Пробрасываем панику дальше после гарантированного отката
		}
	}()

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ошибка фиксации транзакции: %w", err)
	}
	return nil
}

// Select является универсальной оберткой для безопасного выполнения SELECT-запросов.
func (r *Repository) Select(dest interface{}, query string, args ...interface{}) error {
	return r.db.Select(dest, query, args...)
}

// ============================================================================
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ РАСЧЕТА БИЗНЕС-ДНЯ И СМЕН (ЕКАТЕРИНБУРГ / YEKT)
// ============================================================================

var yektLocation *time.Location

func init() {
	loc, err := time.LoadLocation("Asia/Yekaterinburg")
	if err != nil {
		loc = time.FixedZone("YEKT", 5*60*60)
	}
	yektLocation = loc
}

func GetYekaterinburgLocation() *time.Location {
	return yektLocation
}

func CalculateYektBusinessDate(t time.Time) string {
	inYekt := t.In(yektLocation)
	if inYekt.Hour() < 6 {
		return inYekt.AddDate(0, 0, -1).Format("2006-01-02")
	}
	return inYekt.Format("2006-01-02")
}

// CalculateBusinessDate возвращает дату смены с учетом границы в 06:00 YEKT.
func CalculateBusinessDate(t time.Time) string {
	return CalculateYektBusinessDate(t)
}