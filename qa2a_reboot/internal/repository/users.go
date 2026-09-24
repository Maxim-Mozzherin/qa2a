package repository

import (
	"fmt"

	"qa2a/internal/models"
)

// ============================================================================
// ПОЛЬЗОВАТЕЛИ И АВТОРИЗАЦИЯ TELEGRAM
// ============================================================================

// CreateUser создает нового пользователя Telegram или обновляет имя/юзернейм существующего.
func (r *Repository) CreateUser(tgID int64, username, fullName string) (*models.User, error) {
	query := `
		INSERT INTO users (tg_id, username, full_name) 
		VALUES ($1, $2, $3) 
		ON CONFLICT (tg_id) DO UPDATE SET 
			username = EXCLUDED.username, 
			full_name = EXCLUDED.full_name 
		RETURNING id, tg_id, username, full_name, token_version, created_at`

	var user models.User
	err := r.db.QueryRowx(query, tgID, username, fullName).StructScan(&user)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания/обновления пользователя: %w", err)
	}
	return &user, nil
}

// GetUserByTgID находит пользователя по его уникальному Telegram ID.
func (r *Repository) GetUserByTgID(tgID int64) (*models.User, error) {
	var user models.User
	query := `SELECT id, tg_id, username, full_name, token_version, created_at FROM users WHERE tg_id = $1`
	err := r.db.Get(&user, query, tgID)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// IncrementTokenVersion увеличивает версию токена пользователя, инвалидируя все ранее выпущенные токены.
func (r *Repository) IncrementTokenVersion(userID int) error {
	query := `UPDATE users SET token_version = token_version + 1 WHERE id = $1`
	_, err := r.db.Exec(query, userID)
	if err != nil {
		return fmt.Errorf("ошибка инвалидации сессии пользователя: %w", err)
	}
	return nil
}

// IncrementTokenVersionByTgID инвалидирует все токены пользователя по его Telegram ID.
func (r *Repository) IncrementTokenVersionByTgID(tgID int64) error {
	query := `UPDATE users SET token_version = token_version + 1 WHERE tg_id = $1`
	_, err := r.db.Exec(query, tgID)
	if err != nil {
		return fmt.Errorf("ошибка инвалидации сессии по tg_id: %w", err)
	}
	return nil
}

