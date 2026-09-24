package repository

import (
	"fmt"
	"strings"

	"qa2a/internal/models"

	"github.com/jmoiron/sqlx"
)

// Repository инкапсулирует подключение к базе данных и методы работы с сущностями QA2A.
type Repository struct {
	db *sqlx.DB
}

// New создает новый экземпляр репозитория.
func New(db *sqlx.DB) *Repository {
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
		RETURNING id, tg_id, username, full_name, created_at`

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
	query := `SELECT id, tg_id, username, full_name, created_at FROM users WHERE tg_id = $1`
	err := r.db.Get(&user, query, tgID)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// ============================================================================
// ОРГАНИЗАЦИИ (КОМПАНИИ) И ЧЛЕНСТВО (RBAC)
// ============================================================================

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

// JoinCompanyByCode выполняет вступление сотрудника в заведение по промокоду.
func (r *Repository) JoinCompanyByCode(userID int, code string) (string, error) {
	var company struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	queryFind := `SELECT id, name FROM companies WHERE UPPER(invite_code) = UPPER($1) LIMIT 1`
	err := r.db.Get(&company, queryFind, strings.TrimSpace(code))
	if err != nil {
		return "", fmt.Errorf("код '%s' не найден в системе", code)
	}

	queryJoin := `
		INSERT INTO memberships (user_id, company_id, role) 
		VALUES ($1, $2, 'user') 
		ON CONFLICT (user_id, company_id) DO NOTHING`
	_, err = r.db.Exec(queryJoin, userID, company.ID)
	if err != nil {
		return "", fmt.Errorf("ошибка при вступлении в компанию: %w", err)
	}

	return company.Name, nil
}

// GetInviteCodeRaw извлекает инвайт-код заведения.
func (r *Repository) GetInviteCodeRaw(query string, userID, companyID int, dest *string) error {
	return r.db.Get(dest, query, userID, companyID)
}

// UpdateMember обновляет роль и пользовательскую должность сотрудника.
func (r *Repository) UpdateMember(companyID, userID int, role, title string) error {
	query := `
		UPDATE memberships 
		SET role = $1, custom_title = $2 
		WHERE company_id = $3 AND user_id = $4`
	_, err := r.db.Exec(query, role, title, companyID, userID)
	return err
}

// GetMembership возвращает параметры членства пользователя в конкретном заведении.
func (r *Repository) GetMembership(companyID, userID int) (*models.Membership, error) {
	var m models.Membership
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
		WHERE m.company_id = $1 AND m.user_id = $2`
	err := r.db.Get(&m, query, companyID, userID)
	return &m, err
}

// RemoveMember удаляет сотрудника из заведения.
func (r *Repository) RemoveMember(companyID, userID int) error {
	query := `DELETE FROM memberships WHERE company_id = $1 AND user_id = $2`
	_, err := r.db.Exec(query, companyID, userID)
	return err
}

// GetMembershipsByCompanyID возвращает список всех сотрудников заведения.
func (r *Repository) GetMembershipsByCompanyID(companyID int) ([]models.MemberInfo, error) {
	var members []models.MemberInfo
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
		ORDER BY m.role DESC, user_name ASC`

	err := r.db.Select(&members, query, companyID)
	return members, err
}

// GetAllActiveCompanyIDs возвращает ID компаний, у которых настроена интеграция с iiko.
func (r *Repository) GetAllActiveCompanyIDs() ([]int, error) {
	var ids []int
	query := `SELECT id FROM companies WHERE iiko_host != '' ORDER BY id ASC`
	err := r.db.Select(&ids, query)
	return ids, err
}

// ============================================================================
// СКЛАДСКИЕ ЛОКАЦИИ И НОМЕНКЛАТУРА
// ============================================================================

// CreateLocation создает новый склад компании.
func (r *Repository) CreateLocation(companyID int, name string) error {
	query := `INSERT INTO locations (company_id, name) VALUES ($1, $2)`
	_, err := r.db.Exec(query, companyID, name)
	return err
}

// GetLocations возвращает все склады компании.
func (r *Repository) GetLocations(companyID int) ([]models.Location, error) {
	var locs []models.Location
	query := `
		SELECT id, company_id, name, COALESCE(external_id, '') AS external_id 
		FROM locations 
		WHERE company_id = $1 
		ORDER BY name ASC`
	err := r.db.Select(&locs, query, companyID)
	return locs, err
}

// CreatePosition добавляет товарную позицию в каталог заведения.
func (r *Repository) CreatePosition(p *models.Position) error {
	if p.Type == "" {
		p.Type = "GOODS"
	}
	query := `
		INSERT INTO positions (company_id, name, unit, supplier, external_id, type, conception) 
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := r.db.Exec(query, p.CompanyID, p.Name, p.Unit, p.Supplier, p.ExternalID, p.Type, p.Conception)
	return err
}

// GetPositions возвращает весь активный справочник номенклатуры компании.
func (r *Repository) GetPositions(companyID int) ([]models.Position, error) {
	var pos []models.Position
	query := `
		SELECT 
			id, company_id, name, unit, 
			COALESCE(supplier, '') AS supplier, 
			COALESCE(external_id, '') AS external_id, 
			type, 
			COALESCE(conception, '') AS conception 
		FROM positions 
		WHERE company_id = $1 
		ORDER BY name ASC`

	err := r.db.Select(&pos, query, companyID)
	return pos, err
}

// GetPositionByName находит позицию по ее наименованию.
func (r *Repository) GetPositionByName(companyID int, name string) (*models.Position, error) {
	var p models.Position
	query := `
		SELECT 
			id, company_id, name, unit, 
			COALESCE(supplier, '') AS supplier, 
			COALESCE(external_id, '') AS external_id, 
			type,
			COALESCE(conception, '') AS conception
		FROM positions 
		WHERE company_id = $1 AND name = $2 
		LIMIT 1`
	err := r.db.Get(&p, query, companyID, name)
	return &p, err
}

// GetGhostItems находит товары из списаний, которых нет в справочнике номенклатуры (неучтенка).
func (r *Repository) GetGhostItems(companyID int) ([]string, error) {
	var items []string
	query := `
		SELECT DISTINCT position_name 
		FROM operations 
		WHERE company_id = $1 
		  AND is_unlisted = true
		  AND position_name NOT IN (SELECT name FROM positions WHERE company_id = $1)
		ORDER BY position_name ASC`
	err := r.db.Select(&items, query, companyID)
	return items, err
}

// GetBalancesByCompany возвращает текущие складские балансы по всем складам компании.
func (r *Repository) GetBalancesByCompany(companyID int) ([]models.Balance, error) {
	var balances []models.Balance
	query := `
		SELECT 
			p.company_id, 
			COALESCE(b.location_id, 0) AS location_id, 
			p.name AS position_name, 
			COALESCE(b.quantity, 0) AS quantity, 
			p.unit 
		FROM positions p
		LEFT JOIN balances b ON p.name = b.position_name AND p.company_id = b.company_id
		WHERE p.company_id = $1 
		ORDER BY p.name ASC`

	err := r.db.Select(&balances, query, companyID)
	return balances, err
}

// ============================================================================
// ОПЕРАЦИИ (СПИСАНИЯ, ПЕРЕМЕЩЕНИЯ, ЖУРНАЛ ДВИЖЕНИЙ)
// ============================================================================

// CreateOperationTx записывает складскую операцию в рамках транзакции.
func (r *Repository) CreateOperationTx(tx *sqlx.Tx, op *models.Operation) error {
	query := `
		INSERT INTO operations (
			company_id, location_id, to_location_id, user_id, type, 
			position_name, quantity, unit, status, is_unlisted, comment, account_id, created_at
		) VALUES (
			:company_id, :location_id, :to_location_id, :user_id, :type, 
			:position_name, :quantity, :unit, :status, :is_unlisted, :comment, :account_id, :created_at
		)`
	_, err := tx.NamedExec(query, op)
	return err
}

// GetOperationByID возвращает конкретную операцию по ID.
func (r *Repository) GetOperationByID(companyID, id int) (*models.Operation, error) {
	var op models.Operation
	query := `SELECT * FROM operations WHERE company_id = $1 AND id = $2`
	err := r.db.Get(&op, query, companyID, id)
	return &op, err
}

// UpdateWriteoffTx обновляет параметры списания в рамках транзакции.
func (r *Repository) UpdateWriteoffTx(tx *sqlx.Tx, op *models.Operation) error {
	query := `
		UPDATE operations SET 
			quantity = :quantity,
			location_id = :location_id,
			account_id = :account_id,
			comment = :comment,
			created_at = :created_at
		WHERE id = :id AND company_id = :company_id`
	_, err := tx.NamedExec(query, op)
	return err
}

// GetOperationsByCompany возвращает журнал последних операций заведения.
func (r *Repository) GetOperationsByCompany(companyID int, limit int) ([]models.Operation, error) {
	var ops []models.Operation
	query := `
		SELECT 
			o.*, 
			COALESCE(u.full_name, u.username, 'Система') AS user_name 
		FROM operations o 
		LEFT JOIN users u ON o.user_id = u.id 
		WHERE o.company_id = $1 
		ORDER BY o.created_at DESC 
		LIMIT $2`
	err := r.db.Select(&ops, query, companyID, limit)
	return ops, err
}

// GetWriteoffComments возвращает DTO с комментариями и именами пользователей для группы операций.
func (r *Repository) GetWriteoffComments(ids []string) ([]models.WriteoffCommentDTO, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	var results []models.WriteoffCommentDTO
	query, args, err := sqlx.In(`
		SELECT 
			o.position_name, 
			COALESCE(o.comment, '') AS comment, 
			COALESCE(u.username, u.full_name, '') AS username 
		FROM operations o 
		LEFT JOIN users u ON o.user_id = u.id 
		WHERE o.id IN (?)`, ids)
	if err != nil {
		return nil, fmt.Errorf("ошибка формирования IN-запроса: %w", err)
	}

	query = r.db.Rebind(query)
	err = r.db.Select(&results, query, args...)
	return results, err
}

// ============================================================================
// АГРЕГАЦИЯ И ВЫГРУЗКА В IIKO RMS
// ============================================================================

// GetGroupedWriteoffs группирует невыгруженные списания по складу, статье и минуте создания.
func (r *Repository) GetGroupedWriteoffs(companyID int) ([]models.ExportOperationDTO, error) {
	var results []models.ExportOperationDTO
	query := `
		SELECT 
			STRING_AGG(o.id::text, ',') AS op_ids,
			p.external_id AS product_id,
			l.external_id AS store_from,
			'' AS store_to,
			o.account_id,
			TO_CHAR(o.created_at, 'YYYY-MM-DD"T"HH24:MI') AS op_date,
			SUM(o.quantity) AS total_qty
		FROM operations o
		JOIN positions p ON o.position_name = p.name AND o.company_id = p.company_id
		JOIN locations l ON o.location_id = l.id
		WHERE o.company_id = $1 AND o.type = 'writeoff' AND o.exported_to_iiko = false 
		  AND p.external_id != '' AND l.external_id != '' AND o.is_unlisted = false
		GROUP BY p.external_id, l.external_id, o.account_id, TO_CHAR(o.created_at, 'YYYY-MM-DD"T"HH24:MI')`
	err := r.db.Select(&results, query, companyID)
	return results, err
}

// GetGroupedTransfers группирует невыгруженные перемещения товаров между складами.
func (r *Repository) GetGroupedTransfers(companyID int) ([]models.ExportOperationDTO, error) {
	var results []models.ExportOperationDTO
	query := `
		SELECT 
			STRING_AGG(o.id::text, ',') AS op_ids,
			p.external_id AS product_id,
			l2.external_id AS store_from,
			l1.external_id AS store_to,
			TO_CHAR(o.created_at, 'YYYY-MM-DD"T"HH24:MI') AS op_date,
			SUM(o.quantity) AS total_qty
		FROM operations o
		JOIN positions p ON o.position_name = p.name AND o.company_id = p.company_id
		JOIN locations l1 ON o.location_id = l1.id
		JOIN locations l2 ON o.to_location_id = l2.id
		WHERE o.company_id = $1 AND o.type = 'transfer_in' AND o.exported_to_iiko = false 
		  AND p.external_id != '' AND l1.external_id != '' AND l2.external_id != ''
		GROUP BY p.external_id, l2.external_id, l1.external_id, TO_CHAR(o.created_at, 'YYYY-MM-DD"T"HH24:MI')`
	err := r.db.Select(&results, query, companyID)
	return results, err
}

// GetGroupedAssemblies группирует операции приготовления полуфабрикатов для актов переработки iiko.
func (r *Repository) GetGroupedAssemblies(companyID int) ([]models.ExportOperationDTO, error) {
	var results []models.ExportOperationDTO
	query := `
		SELECT 
			STRING_AGG(o.id::text, ',') AS op_ids,
			p.external_id AS product_id,
			l2.external_id AS store_from,
			l1.external_id AS store_to,
			TO_CHAR(o.created_at, 'YYYY-MM-DD"T"HH24:MI') AS op_date,
			SUM(o.quantity) AS total_qty
		FROM operations o
		JOIN positions p ON o.position_name = p.name AND o.company_id = p.company_id
		JOIN locations l1 ON o.location_id = l1.id
		JOIN locations l2 ON o.to_location_id = l2.id
		WHERE o.company_id = $1 AND o.type = 'assembly_in' AND o.exported_to_iiko = false 
		  AND p.external_id != '' AND l1.external_id != '' AND l2.external_id != ''
		GROUP BY p.external_id, l2.external_id, l1.external_id, TO_CHAR(o.created_at, 'YYYY-MM-DD"T"HH24:MI')`
	err := r.db.Select(&results, query, companyID)
	return results, err
}

// MarkOperationsExported помечает пачку операций как успешно выгруженные в iiko RMS.
func (r *Repository) MarkOperationsExported(companyID int, opIDs []string) error {
	if len(opIDs) == 0 {
		return nil
	}

	query, args, err := sqlx.In(`
		UPDATE operations 
		SET exported_to_iiko = true 
		WHERE company_id = ? AND id IN (?)`, companyID, opIDs)
	if err != nil {
		return fmt.Errorf("ошибка построения запроса MarkOperationsExported: %w", err)
	}

	query = r.db.Rebind(query)
	_, err = r.db.Exec(query, args...)
	return err
}

// ============================================
// СТАТЬИ СПИСАНИЯ (СЧЕТА РАСХОДОВ)
// ============================================

// GetWriteoffAccounts возвращает список доступных статей списаний заведения.
func (r *Repository) GetWriteoffAccounts(companyID int) ([]models.WriteoffAccount, error) {
	var accs []models.WriteoffAccount
	query := `
		SELECT id, company_id, name, external_id 
		FROM writeoff_accounts 
		WHERE company_id = $1 
		ORDER BY name ASC`
	err := r.db.Select(&accs, query, companyID)
	return accs, err
}

// CreateWriteoffAccount добавляет статью списания.
func (r *Repository) CreateWriteoffAccount(acc models.WriteoffAccount) error {
	query := `INSERT INTO writeoff_accounts (company_id, name, external_id) VALUES ($1, $2, $3)`
	_, err := r.db.Exec(query, acc.CompanyID, acc.Name, acc.ExternalID)
	return err
}

// DeleteWriteoffAccount удаляет статью списания.
func (r *Repository) DeleteWriteoffAccount(companyID, id int) error {
	query := `DELETE FROM writeoff_accounts WHERE company_id = $1 AND id = $2`
	_, err := r.db.Exec(query, companyID, id)
	return err
}

// GetWriteoffAccount возвращает дефолтный счет списания из настроек компании.
func (r *Repository) GetWriteoffAccount(companyID int) (string, error) {
	var accountID string
	query := `SELECT iiko_writeoff_account FROM companies WHERE id = $1`
	err := r.db.Get(&accountID, query, companyID)
	return accountID, err
}

// ============================================================================
// ЗАЯВКИ НА ЗАКУПКУ (PROCUREMENT)
// ============================================================================

// ProcurementItemWithSupplier расширяет строчку заявки данными о поставщике и его Telegram.
type ProcurementItemWithSupplier struct {
	PositionName string  `db:"position_name" json:"position_name"`
	Quantity     float64 `db:"quantity" json:"quantity"`
	Unit         string  `db:"unit" json:"unit"`
	SupplierUUID string  `db:"supplier_uuid" json:"supplier_uuid"`
	SupplierName string  `db:"supplier_name" json:"supplier_name"`
	TgUsername   string  `db:"tg_username" json:"tg_username"`
}

// CreateProcurementRequest создает новую заявку с перечнем позиций.
func (r *Repository) CreateProcurementRequest(companyID, userID int, items []models.ProcurementItem) error {
	return r.ExecuteInTx(func(tx *sqlx.Tx) error {
		var reqID int
		queryHeader := `
			INSERT INTO procurement_requests (company_id, user_id, status) 
			VALUES ($1, $2, 'pending') 
			RETURNING id`
		err := tx.QueryRow(queryHeader, companyID, userID).Scan(&reqID)
		if err != nil {
			return err
		}

		queryItem := `
			INSERT INTO procurement_items (request_id, position_name, quantity, unit, is_unlisted, supplier) 
			VALUES ($1, $2, $3, $4, $5, $6)`
		for _, item := range items {
			_, err = tx.Exec(queryItem, reqID, item.PositionName, item.Quantity, item.Unit, item.IsUnlisted, item.Supplier)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// GetProcurementRequests возвращает заявки на закупку с фильтрацией по статусу.
func (r *Repository) GetProcurementRequests(companyID int, status string) ([]models.ProcurementRequest, error) {
	var requests []models.ProcurementRequest
	query := `
		SELECT 
			pr.id, pr.company_id, pr.user_id, pr.status, pr.created_at, pr.updated_at,
			COALESCE(u.full_name, u.username) AS user_name 
		FROM procurement_requests pr 
		JOIN users u ON pr.user_id = u.id 
		WHERE pr.company_id = $1 AND pr.status = $2 
		ORDER BY pr.created_at DESC`

	err := r.db.Select(&requests, query, companyID, status)
	if err != nil {
		return nil, err
	}

	for i := range requests {
		itemsQuery := `
			SELECT position_name, quantity, unit, is_unlisted, supplier
			FROM procurement_items 
			WHERE request_id = $1`
		var items []models.ProcurementItem
		if err := r.db.Select(&items, itemsQuery, requests[i].ID); err == nil {
			requests[i].Items = items
		}
	}
	return requests, nil
}

// UpdateProcurementStatus изменяет статус заявки (согласована/отклонена).
func (r *Repository) UpdateProcurementStatus(requestID int, status string, adminID int) error {
	query := `
		UPDATE procurement_requests 
		SET status = $1, approved_by = $2, updated_at = NOW() 
		WHERE id = $3`
	_, err := r.db.Exec(query, status, adminID, requestID)
	return err
}

// CleanOldProcurements удаляет архивные заявки старше 30 дней.
func (r *Repository) CleanOldProcurements() error {
	query := `DELETE FROM procurement_requests WHERE created_at < NOW() - INTERVAL '30 days'`
	_, err := r.db.Exec(query)
	return err
}

// GetProcurementItemsWithSuppliers агрегирует строчки заявки с привязанными поставщиками.
func (r *Repository) GetProcurementItemsWithSuppliers(companyID, requestID int) ([]ProcurementItemWithSupplier, error) {
	type RawRow struct {
		PositionName string  `db:"position_name"`
		Quantity     float64 `db:"quantity"`
		Unit         string  `db:"unit"`
		Supplier     string  `db:"supplier"`
	}
	var rows []RawRow
	query := `
		SELECT position_name, quantity, unit, COALESCE(supplier, '') AS supplier 
		FROM procurement_items 
		WHERE request_id = $1`
	err := r.db.Select(&rows, query, requestID)
	if err != nil {
		return nil, err
	}

	contacts, err := r.GetSupplierContacts(companyID)
	if err != nil {
		return nil, err
	}

	var results []ProcurementItemWithSupplier
	for _, row := range rows {
		uuid := ""
		name := "Без поставщика"

		if row.Supplier != "" {
			parts := strings.Split(row.Supplier, "|")
			if len(parts) == 2 {
				uuid = parts[0]
				name = parts[1]
			} else {
				name = row.Supplier
			}
		}

		results = append(results, ProcurementItemWithSupplier{
			PositionName: row.PositionName,
			Quantity:     row.Quantity,
			Unit:         row.Unit,
			SupplierUUID: uuid,
			SupplierName: name,
			TgUsername:   contacts[uuid],
		})
	}

	return results, nil
}

// ============================================================================
// ИНВЕНТАРИЗАЦИЯ И ШАБЛОНЫ
// ============================================================================

// CreateInventoryActTx создает черновик акта инвентаризации в рамках транзакции.
func (r *Repository) CreateInventoryActTx(tx *sqlx.Tx, act *models.InventoryAct) error {
	query := `
		INSERT INTO inventories (company_id, location_id, user_id, status, iiko_document_id, document_number) 
		VALUES ($1, $2, $3, $4, $5, $6) 
		RETURNING id`
	err := tx.QueryRow(query, act.CompanyID, act.LocationID, act.UserID, act.Status, act.IikoDocumentID, act.DocumentNumber).Scan(&act.ID)
	if err != nil {
		return err
	}

	queryItem := `
		INSERT INTO inventory_items (inventory_id, position_name, external_id, expected_amount, actual_amount) 
		VALUES ($1, $2, $3, $4, $5)`
	for _, item := range act.Items {
		_, err = tx.Exec(queryItem, act.ID, item.PositionName, item.ExternalID, item.ExpectedAmount, item.ActualAmount)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetInventoryActs возвращает историю актов инвентаризации компании.
func (r *Repository) GetInventoryActs(companyID int) ([]models.InventoryAct, error) {
	var acts []models.InventoryAct
	query := `
		SELECT 
			i.id, i.company_id, i.location_id, l.name AS location_name, 
			i.user_id, COALESCE(u.full_name, u.username) AS user_name, 
			i.status, i.created_at, 
			COALESCE(i.iiko_document_id, '') AS iiko_document_id, 
			COALESCE(i.document_number, '') AS document_number 
		FROM inventories i 
		JOIN locations l ON i.location_id = l.id 
		JOIN users u ON i.user_id = u.id 
		WHERE i.company_id = $1 
		ORDER BY i.created_at DESC`
	err := r.db.Select(&acts, query, companyID)
	return acts, err
}

// GetInventoryActByID возвращает акт инвентаризации со всеми позициями.
func (r *Repository) GetInventoryActByID(companyID, actID int) (*models.InventoryAct, error) {
	var act models.InventoryAct
	query := `
		SELECT 
			i.id, i.company_id, i.location_id, l.name AS location_name, 
			i.user_id, COALESCE(u.full_name, u.username) AS user_name, 
			i.status, i.created_at, 
			COALESCE(i.iiko_document_id, '') AS iiko_document_id, 
			COALESCE(i.document_number, '') AS document_number 
		FROM inventories i 
		JOIN locations l ON i.location_id = l.id 
		JOIN users u ON i.user_id = u.id 
		WHERE i.company_id = $1 AND i.id = $2`
	err := r.db.Get(&act, query, companyID, actID)
	if err != nil {
		return nil, err
	}

	itemsQuery := `
		SELECT position_name, external_id, expected_amount, actual_amount 
		FROM inventory_items 
		WHERE inventory_id = $1`
	err = r.db.Select(&act.Items, itemsQuery, act.ID)
	return &act, err
}

// UpdateInventoryItemsTx обновляет фактические остатки в черновике акта.
func (r *Repository) UpdateInventoryItemsTx(tx *sqlx.Tx, actID int, items []models.InventoryItem) error {
	_, err := tx.Exec("DELETE FROM inventory_items WHERE inventory_id = $1", actID)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO inventory_items (inventory_id, position_name, external_id, expected_amount, actual_amount) 
		VALUES ($1, $2, $3, $4, $5)`
	for _, item := range items {
		_, err = tx.Exec(query, actID, item.PositionName, item.ExternalID, item.ExpectedAmount, item.ActualAmount)
		if err != nil {
			return err
		}
	}
	return nil
}

// UpdateInventoryStatusTx изменяет статус инвентаризации (например, на completed).
func (r *Repository) UpdateInventoryStatusTx(tx *sqlx.Tx, actID int, status string) error {
	query := `UPDATE inventories SET status = $1 WHERE id = $2`
	_, err := tx.Exec(query, status, actID)
	return err
}

// DeleteInventoryAct удаляет черновик инвентаризации.
func (r *Repository) DeleteInventoryAct(companyID, actID int) error {
	query := `DELETE FROM inventories WHERE company_id = $1 AND id = $2 AND status = 'in_progress'`
	_, err := r.db.Exec(query, companyID, actID)
	return err
}

// CreateInventoryTemplateTx сохраняет сформированный шаблон пересчета от бухгалтера.
func (r *Repository) CreateInventoryTemplateTx(tx *sqlx.Tx, companyID, locationID int, name string, items []string) error {
	var templateID int
	query := `
		INSERT INTO inventory_templates (company_id, location_id, name) 
		VALUES ($1, $2, $3) 
		RETURNING id`
	err := tx.QueryRow(query, companyID, locationID, name).Scan(&templateID)
	if err != nil {
		return err
	}

	queryItem := `
		INSERT INTO inventory_template_items (template_id, position_name) 
		VALUES ($1, $2) 
		ON CONFLICT DO NOTHING`
	for _, item := range items {
		if _, err = tx.Exec(queryItem, templateID, item); err != nil {
			return err
		}
	}
	return nil
}

// GetInventoryTemplatesByLocation возвращает бланки инвентаризации по локации.
func (r *Repository) GetInventoryTemplatesByLocation(companyID, locationID int) ([]models.InventoryTemplate, error) {
	var list []models.InventoryTemplate
	query := `
		SELECT id, company_id, location_id, name, created_at 
		FROM inventory_templates 
		WHERE company_id = $1 AND location_id = $2 
		ORDER BY created_at DESC`
	err := r.db.Select(&list, query, companyID, locationID)
	return list, err
}

// GetTemplateItems возвращает список позиций внутри шаблона.
func (r *Repository) GetTemplateItems(templateID int) ([]string, error) {
	var items []string
	query := `SELECT position_name FROM inventory_template_items WHERE template_id = $1 ORDER BY position_name ASC`
	err := r.db.Select(&items, query, templateID)
	return items, err
}

// ============================================================================
// ПОСТАВЩИКИ И МАППИНГИ
// ============================================================================

// GetPositionSuppliers находит поставщиков товара по связкам накладных.
func (r *Repository) GetPositionSuppliers(companyID int, productUUID string) ([]models.WriteoffAccount, error) {
	var results []models.WriteoffAccount
	query := `
		SELECT DISTINCT s.iiko_supplier_uuid AS external_id, s.vendor_name AS name
		FROM supplier_mappings s
		JOIN product_mappings p ON s.company_id = p.company_id AND s.vendor_name = p.vendor_name
		WHERE p.company_id = $1 AND p.iiko_product_uuid = $2
		ORDER BY s.vendor_name ASC`
	err := r.db.Select(&results, query, companyID, productUUID)
	return results, err
}

// GetAllCompanySuppliers возвращает всех известных поставщиков компании из маппингов.
func (r *Repository) GetAllCompanySuppliers(companyID int) ([]models.WriteoffAccount, error) {
	var results []models.WriteoffAccount
	query := `
		SELECT DISTINCT iiko_supplier_uuid AS external_id, vendor_name AS name 
		FROM supplier_mappings 
		WHERE company_id = $1 
		ORDER BY vendor_name ASC`
	err := r.db.Select(&results, query, companyID)
	return results, err
}

// GetSupplierContacts возвращает карту контактов поставщиков в Telegram {SupplierUUID: TgUsername}.
func (r *Repository) GetSupplierContacts(companyID int) (map[string]string, error) {
	type ContactRow struct {
		SupplierUUID string `db:"supplier_uuid"`
		TgUsername   string `db:"tg_username"`
	}
	var rows []ContactRow
	query := `SELECT supplier_uuid, tg_username FROM supplier_contacts WHERE company_id = $1`
	if err := r.db.Select(&rows, query, companyID); err != nil {
		return nil, err
	}

	contacts := make(map[string]string, len(rows))
	for _, row := range rows {
		contacts[row.SupplierUUID] = row.TgUsername
	}
	return contacts, nil
}

// SaveSupplierContact привязывает Telegram username к поставщику iiko.
func (r *Repository) SaveSupplierContact(companyID int, supplierUUID, tgUsername string) error {
	query := `
		INSERT INTO supplier_contacts (company_id, supplier_uuid, tg_username)
		VALUES ($1, $2, $3)
		ON CONFLICT (company_id, supplier_uuid)
		DO UPDATE SET tg_username = EXCLUDED.tg_username, updated_at = NOW()`
	_, err := r.db.Exec(query, companyID, supplierUUID, tgUsername)
	return err
}

// GetSupplierNameMappings возвращает соответствие UUID поставщика iiko его коммерческому имени {UUID: VendorName}.
func (r *Repository) GetSupplierNameMappings(companyID int) (map[string]string, error) {
	type MapRow struct {
		UUID string `db:"iiko_supplier_uuid"`
		Name string `db:"vendor_name"`
	}
	var rows []MapRow
	query := `SELECT iiko_supplier_uuid, vendor_name FROM supplier_mappings WHERE company_id = $1`
	if err := r.db.Select(&rows, query, companyID); err != nil {
		return nil, err
	}

	mappings := make(map[string]string, len(rows))
	for _, row := range rows {
		mappings[strings.ToUpper(row.UUID)] = row.Name
	}
	return mappings, nil
}

