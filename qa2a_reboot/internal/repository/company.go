package repository

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"qa2a/internal/models"

	"github.com/jmoiron/sqlx"
)

// ============================================================================
// ОРГАНИЗАЦИИ (КОМПАНИИ) И ЧЛЕНСТВО (RBAC)
// ============================================================================

// JoinRequestDTO описывает заявку пользователя на вступление в заведение.
type JoinRequestDTO struct {
	ID          int    `json:"id" db:"id"`
	UserID      int    `json:"user_id" db:"user_id"`
	UserName    string `json:"user_name" db:"user_name"`
	CompanyName string `json:"company_name" db:"company_name"`
}

// CreateCompany создает новую компанию и возвращает ее сгенерированный ID.
func (r *Repository) CreateCompany(name string) (int, error) {
	var id int
	query := `INSERT INTO companies (name) VALUES ($1) RETURNING id`
	err := r.db.QueryRow(query, name).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ошибка создания компании: %w", err)
	}
	return id, nil
}

// SetInviteCode устанавливает уникальный инвайт-код для вступления сотрудников.
func (r *Repository) SetInviteCode(companyID int, code string) error {
	query := `UPDATE companies SET invite_code = $1 WHERE id = $2`
	_, err := r.db.Exec(query, code, companyID)
	return err
}

// AddMember добавляет пользователя в компанию с указанной ролью.
func (r *Repository) AddMember(userID, companyID int, role string) error {
	query := `
		INSERT INTO memberships (user_id, company_id, role) 
		VALUES ($1, $2, $3) 
		ON CONFLICT (user_id, company_id) DO NOTHING`
	_, err := r.db.Exec(query, userID, companyID, role)
	return err
}

// GetMembershipsByUserID возвращает все заведения, к которым привязан пользователь.
func (r *Repository) GetMembershipsByUserID(userID int) ([]models.Membership, error) {
	var list []models.Membership
	query := `
		SELECT 
			m.user_id, 
			m.company_id, 
			m.role, 
			COALESCE(m.custom_title, '') AS custom_title, 
			c.name AS company_name,
			COALESCE(u.full_name, u.username) AS user_name
		FROM memberships m 
		JOIN companies c ON m.company_id = c.id 
		JOIN users u ON m.user_id = u.id
		WHERE m.user_id = $1
		ORDER BY c.name ASC`

	err := r.db.Select(&list, query, userID)
	return list, err
}

// JoinCompanyByCode выполняет вступление сотрудника в заведение по промокоду/инвайту с защитой от race conditions.
func (r *Repository) JoinCompanyByCode(userID int, code string) (string, error) {
	cleanCode := strings.ToUpper(strings.TrimSpace(code))
	if cleanCode == "" {
		return "", fmt.Errorf("укажите код доступа")
	}

	var companyName string

	err := r.ExecuteInTx(func(tx *sqlx.Tx) error {
		// 1. Сначала проверяем, является ли код инвайтом на создание нового заведения (company_invites)
		var invite struct {
			Code             string    `db:"code"`
			Name             string    `db:"name"`
			AccountingFirmID *int      `db:"accounting_firm_id"`
			ExpiresAt        time.Time `db:"expires_at"`
			IsUsed           bool      `db:"is_used"`
		}

		errInvite := tx.Get(&invite, `
			SELECT code, name, accounting_firm_id, expires_at, is_used 
			FROM company_invites 
			WHERE UPPER(code) = $1 
			FOR UPDATE
		`, cleanCode)

		if errInvite == nil {
			// Инвайт найден в company_invites
			if invite.IsUsed {
				return fmt.Errorf("инвайт-код уже активирован")
			}
			if time.Now().After(invite.ExpiresAt) {
				return fmt.Errorf("срок действия инвайт-кода истек")
			}

			// Генерируем криптостойкий командный инвайт-код для создаваемой компании
			b := make([]byte, 8)
			if _, err := rand.Read(b); err != nil {
				return fmt.Errorf("ошибка генератора случайных чисел: %w", err)
			}
			teamCode := "QA-" + strings.ToUpper(hex.EncodeToString(b))

			var newCompanyID int
			queryCreate := `
				INSERT INTO companies (name, invite_code, accounting_firm_id) 
				VALUES ($1, $2, $3) 
				RETURNING id`
			if err := tx.QueryRow(queryCreate, invite.Name, teamCode, invite.AccountingFirmID).Scan(&newCompanyID); err != nil {
				return fmt.Errorf("ошибка создания компании по инвайту: %w", err)
			}

			// Создаем стартовую локацию (склад)
			if _, err := tx.Exec("INSERT INTO locations (company_id, name) VALUES ($1, $2)", newCompanyID, "Основной склад"); err != nil {
				return fmt.Errorf("ошибка создания локации: %w", err)
			}

			// Назначаем пользователя владельцем
			queryOwner := `
				INSERT INTO memberships (user_id, company_id, role) 
				VALUES ($1, $2, 'owner') 
				ON CONFLICT (user_id, company_id) DO UPDATE SET role = 'owner'`
			if _, err := tx.Exec(queryOwner, userID, newCompanyID); err != nil {
				return fmt.Errorf("ошибка назначения роли владельца: %w", err)
			}

			// Атомарно помечаем инвайт как использованный
			queryMark := `
				UPDATE company_invites 
				SET is_used = TRUE, used_by_user_id = $1, company_id = $2 
				WHERE code = $3`
			if _, err := tx.Exec(queryMark, userID, newCompanyID, invite.Code); err != nil {
				return fmt.Errorf("ошибка обновления статуса инвайта: %w", err)
			}

			companyName = invite.Name
			return nil
		}

		// 2. Если в company_invites не найдено, проверяем существующие компании по invite_code
		var company struct {
			ID               int    `db:"id"`
			Name             string `db:"name"`
			AccountingFirmID *int   `db:"accounting_firm_id"`
		}

		queryFind := `SELECT id, name, accounting_firm_id FROM companies WHERE UPPER(invite_code) = $1 FOR UPDATE`
		errComp := tx.Get(&company, queryFind, cleanCode)
		if errComp != nil {
			return fmt.Errorf("код '%s' не найден в системе", code)
		}

		// Проверяем, состоит ли уже пользователь в компании
		var existingRole string
		_ = tx.Get(&existingRole, "SELECT role FROM memberships WHERE user_id = $1 AND company_id = $2", userID, company.ID)
		if existingRole != "" {
			return fmt.Errorf("вы уже являетесь участником заведения '%s'", company.Name)
		}

		var memberCount int
		if err := tx.Get(&memberCount, "SELECT COUNT(*) FROM memberships WHERE company_id = $1", company.ID); err != nil {
			return fmt.Errorf("ошибка проверки участников: %w", err)
		}

		if memberCount == 0 {
			// В компании еще нет участников (например, старая shell-компания без владельца)
			queryJoin := `
				INSERT INTO memberships (user_id, company_id, role) 
				VALUES ($1, $2, 'owner') 
				ON CONFLICT (user_id, company_id) DO NOTHING`
			if _, err := tx.Exec(queryJoin, userID, company.ID); err != nil {
				return fmt.Errorf("ошибка при вступлении в компанию: %w", err)
			}
			companyName = company.Name
			return nil
		}

		// Иначе создаем заявку на вступление (join_requests)
		queryJoinReq := `
			INSERT INTO join_requests (user_id, company_id) 
			VALUES ($1, $2) 
			ON CONFLICT (user_id, company_id) DO NOTHING`
		if _, err := tx.Exec(queryJoinReq, userID, company.ID); err != nil {
			return fmt.Errorf("ошибка при создании заявки на вступление: %w", err)
		}

		companyName = company.Name
		return nil
	})

	if err != nil {
		return "", err
	}
	return companyName, nil
}

// GetJoinRequests возвращает список заявок на вступление в заведение.
func (r *Repository) GetJoinRequests(companyID int) ([]JoinRequestDTO, error) {
	var list []JoinRequestDTO
	query := `
		SELECT jr.id, jr.user_id, COALESCE(u.full_name, u.username) AS user_name, c.name AS company_name
		FROM join_requests jr
		JOIN users u ON jr.user_id = u.id
		JOIN companies c ON jr.company_id = c.id
		WHERE jr.company_id = $1
		ORDER BY jr.created_at ASC`
	err := r.db.Select(&list, query, companyID)
	return list, err
}

// ApproveJoinRequest переводит заявку на вступление в статус полноправного сотрудника заведения.
func (r *Repository) ApproveJoinRequest(requestID, companyID int) error {
	return r.ExecuteInTx(func(tx *sqlx.Tx) error {
		var userID int
		err := tx.QueryRow("SELECT user_id FROM join_requests WHERE id = $1 AND company_id = $2", requestID, companyID).Scan(&userID)
		if err != nil {
			return err
		}

		_, err = tx.Exec("INSERT INTO memberships (user_id, company_id, role) VALUES ($1, $2, 'user') ON CONFLICT DO NOTHING", userID, companyID)
		if err != nil {
			return err
		}

		_, err = tx.Exec("DELETE FROM join_requests WHERE id = $1", requestID)
		return err
	})
}

// RejectJoinRequest удаляет отклоненную заявку на вступление.
func (r *Repository) RejectJoinRequest(requestID, companyID int) error {
	_, err := r.db.Exec("DELETE FROM join_requests WHERE id = $1 AND company_id = $2", requestID, companyID)
	return err
}

// GetInviteCodeRaw возвращает инвайт-код с проверкой доступа.
func (r *Repository) GetInviteCodeRaw(query string, userID, companyID int, dest *string) error {
	return r.db.Get(dest, query, userID, companyID)
}

// UpdateMember обновляет роль и должность сотрудника.
func (r *Repository) UpdateMember(companyID, userID int, role, title string) error {
	query := `
		UPDATE memberships 
		SET role = $1, custom_title = $2 
		WHERE company_id = $3 AND user_id = $4`
	_, err := r.db.Exec(query, role, title, companyID, userID)
	return err
}

// GetMembership возвращает информацию о членстве пользователя в заведении.
func (r *Repository) GetMembership(companyID, userID int) (*models.Membership, error) {
	var m models.Membership
	query := `
		SELECT 
			m.user_id, 
			m.company_id, 
			m.role, 
			COALESCE(m.custom_title, '') AS custom_title, 
			c.name AS company_name 
		FROM memberships m 
		JOIN companies c ON m.company_id = c.id 
		WHERE m.company_id = $1 AND m.user_id = $2`
	err := r.db.Get(&m, query, companyID, userID)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// RemoveMember удаляет сотрудника из заведения.
func (r *Repository) RemoveMember(companyID, userID int) error {
	query := `DELETE FROM memberships WHERE company_id = $1 AND user_id = $2`
	_, err := r.db.Exec(query, companyID, userID)
	return err
}

// GetMembershipsByCompanyID возвращает всех сотрудников заведения.
func (r *Repository) GetMembershipsByCompanyID(companyID int) ([]models.MemberInfo, error) {
	var list []models.MemberInfo
	query := `
		SELECT 
			m.user_id, 
			m.company_id, 
			m.role, 
			COALESCE(m.custom_title, '') AS custom_title, 
			c.name AS company_name, 
			COALESCE(u.full_name, u.username) AS user_name 
		FROM memberships m 
		JOIN companies c ON m.company_id = c.id 
		JOIN users u ON m.user_id = u.id 
		WHERE m.company_id = $1 
		ORDER BY m.role ASC, user_name ASC`
	err := r.db.Select(&list, query, companyID)
	return list, err
}

// GetAllActiveCompanyIDs возвращает ID всех заведений с настроенной интеграцией iiko.
func (r *Repository) GetAllActiveCompanyIDs() ([]int, error) {
	var ids []int
	query := `SELECT id FROM companies WHERE iiko_host != '' ORDER BY id ASC`
	err := r.db.Select(&ids, query)
	return ids, err
}
