package repository

import (
	"fmt"
	"qa2a/internal/models"
	"github.com/jmoiron/sqlx"
)

type Repository struct {
	db *sqlx.DB
}

func New(db *sqlx.DB) *Repository {
	return &Repository{db: db}
}

// ===== USERS & AUTH =====

func (r *Repository) CreateUser(tgID int64, username, fullName string) (*models.User, error) {
	query := `INSERT INTO users (tg_id, username, full_name) 
	          VALUES ($1, $2, $3) 
	          ON CONFLICT (tg_id) DO UPDATE SET 
                  username = EXCLUDED.username, 
                  full_name = EXCLUDED.full_name 
	          RETURNING id, tg_id, username, full_name, created_at`
	
	var user models.User
	err := r.db.QueryRowx(query, tgID, username, fullName).StructScan(&user)
	return &user, err
}

func (r *Repository) GetUserByTgID(tgID int64) (*models.User, error) {
	var user models.User
	err := r.db.Get(&user, "SELECT * FROM users WHERE tg_id = $1", tgID)
	return &user, err
}

// ===== COMPANIES & MEMBERSHIPS =====

func (r *Repository) CreateCompany(name string) (int, error) {
	var id int
	err := r.db.QueryRow("INSERT INTO companies (name) VALUES ($1) RETURNING id", name).Scan(&id)
	return id, err
}

func (r *Repository) SetInviteCode(companyID int, code string) error {
	_, err := r.db.Exec("UPDATE companies SET invite_code = $1 WHERE id = $2", code, companyID)
	return err
}

func (r *Repository) AddMember(userID, companyID int, role string) error {
	_, err := r.db.Exec("INSERT INTO memberships (user_id, company_id, role) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING", userID, companyID, role)
	return err
}

func (r *Repository) GetMembershipsByUserID(userID int) ([]models.Membership, error) {
	m := []models.Membership{}
	query := `SELECT m.user_id, m.company_id, m.role, COALESCE(m.custom_title, '') as custom_title, c.name as company_name 
              FROM memberships m JOIN companies c ON m.company_id = c.id WHERE m.user_id = $1`
	err := r.db.Select(&m, query, userID)
	return m, err
}

func (r *Repository) JoinCompanyByCode(userID int, code string) (string, error) {
	var company struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	err := r.db.Get(&company, "SELECT id, name FROM companies WHERE UPPER(invite_code) = UPPER($1)", code)
	if err != nil {
		return "", fmt.Errorf("код '%s' не найден в системе", code)
	}

	_, err = r.db.Exec("INSERT INTO memberships (user_id, company_id, role) VALUES ($1, $2, 'user') ON CONFLICT DO NOTHING", userID, company.ID)
	if err != nil {
		return "", fmt.Errorf("ошибка при вступлении: %v", err)
	}
	
	return company.Name, nil
}

func (r *Repository) GetInviteCodeRaw(query string, userID, companyID int, dest *string) error {
	return r.db.Get(dest, query, userID, companyID)
}

// ===== OPERATIONS & BALANCES =====

func (r *Repository) ExecuteInTx(fn func(*sqlx.Tx) error) error {
	tx, err := r.db.Beginx()
	if err != nil { return err }
	defer tx.Rollback()
	if err := fn(tx); err != nil { return err }
	return tx.Commit()
}

func (r *Repository) CreateOperationTx(tx *sqlx.Tx, op *models.Operation) error {
	query := `INSERT INTO operations (company_id, location_id, user_id, type, position_name, quantity, unit, status, is_unlisted, comment) 
	          VALUES (:company_id, :location_id, :user_id, :type, :position_name, :quantity, :unit, :status, :is_unlisted, :comment)`
	_, err := tx.NamedExec(query, op)
	return err
}

func (r *Repository) UpdateBalanceTx(tx *sqlx.Tx, companyID int, locationID int, posName string, qty float64, unit string) error {
	query := `INSERT INTO balances (company_id, location_id, position_name, quantity, unit) 
	          VALUES ($1, $2, $3, $4, $5)
	          ON CONFLICT (company_id, location_id, position_name) 
	          DO UPDATE SET quantity = balances.quantity + EXCLUDED.quantity`
	_, err := tx.Exec(query, companyID, locationID, posName, qty, unit)
	return err
}

func (r *Repository) GetBalancesByCompany(companyID int) ([]models.Balance, error) {
	balances := []models.Balance{} 
	err := r.db.Select(&balances, "SELECT * FROM balances WHERE company_id = $1 ORDER BY position_name ASC", companyID)
	return balances, err
}

func (r *Repository) GetOperationsByCompany(companyID int, limit int) ([]models.Operation, error) {
	ops := []models.Operation{}
	query := `SELECT o.*, COALESCE(u.full_name, u.username, 'Система') as user_name 
              FROM operations o LEFT JOIN users u ON o.user_id = u.id 
              WHERE o.company_id = $1 ORDER BY o.created_at DESC LIMIT $2`
	err := r.db.Select(&ops, query, companyID, limit)
	return ops, err
}

func (r *Repository) CreateLocation(companyID int, name string) error {
	_, err := r.db.Exec("INSERT INTO locations (company_id, name) VALUES ($1, $2)", companyID, name)
	return err
}

func (r *Repository) CreatePosition(p *models.Position) error {
	query := `INSERT INTO positions (company_id, name, unit, supplier) 
              VALUES ($1, $2, $3, $4)`
	_, err := r.db.Exec(query, p.CompanyID, p.Name, p.Unit, p.Supplier)
	return err
}

func (r *Repository) GetLocations(companyID int) ([]models.Location, error) {
	locs := []models.Location{}
	err := r.db.Select(&locs, "SELECT * FROM locations WHERE company_id = $1", companyID)
	return locs, err
}

func (r *Repository) GetPositions(companyID int) ([]models.Position, error) {
	pos := []models.Position{}
	// ВАЖНО: Заменяем SELECT * на явный выбор полей с защитой от NULL в поставщике
	query := `SELECT id, company_id, name, unit, COALESCE(supplier, '') as supplier 
              FROM positions 
              WHERE company_id = $1 ORDER BY name`
              
	err := r.db.Select(&pos, query, companyID)
	return pos, err
}

func (r *Repository) GetMembershipsByCompanyID(companyID int) ([]models.MemberInfo, error) {
	var members []models.MemberInfo
	query := `
		SELECT 
			m.user_id, 
			m.company_id, 
			m.role, 
			COALESCE(m.custom_title, '') as custom_title, 
			c.name as company_name, 
			COALESCE(u.full_name, u.username) as user_name
		FROM memberships m 
		JOIN companies c ON m.company_id = c.id 
		JOIN users u ON m.user_id = u.id
		WHERE m.company_id = $1`
		
	err := r.db.Select(&members, query, companyID)
	return members, err
}

// ===== PROCUREMENT (ЗАЯВКИ) =====

func (r *Repository) CreateProcurementRequest(companyID, userID int, items []models.ProcurementItem) error {
	return r.ExecuteInTx(func(tx *sqlx.Tx) error {
		var reqID int
		err := tx.QueryRow("INSERT INTO procurement_requests (company_id, user_id, status) VALUES ($1, $2, 'pending') RETURNING id", companyID, userID).Scan(&reqID)
		if err != nil { return err }

		for _, item := range items {
			_, err = tx.Exec("INSERT INTO procurement_items (request_id, position_name, quantity, unit, is_unlisted) VALUES ($1, $2, $3, $4, $5)", 
				reqID, item.PositionName, item.Quantity, item.Unit, item.IsUnlisted)
			if err != nil { return err }
		}
		return nil
	})
}

func (r *Repository) Select(dest interface{}, query string, args ...interface{}) error {
	return r.db.Select(dest, query, args...)
}

func (r *Repository) GetProcurementRequests(companyID int, status string) ([]models.ProcurementRequest, error) {
	var requests []models.ProcurementRequest
	query := `SELECT pr.id, pr.company_id, pr.user_id, pr.status, pr.created_at, COALESCE(u.full_name, u.username) as user_name 
	          FROM procurement_requests pr JOIN users u ON pr.user_id = u.id 
	          WHERE pr.company_id = $1 AND pr.status = $2 ORDER BY pr.created_at DESC`
	
	err := r.db.Select(&requests, query, companyID, status)
	if err != nil { return nil, err }

	for i := range requests {
		itemsQuery := `SELECT pi.position_name, pi.quantity, pi.unit, pi.is_unlisted
		               FROM procurement_items pi WHERE pi.request_id = $1`
		var items []models.ProcurementItem
		err := r.db.Select(&items, itemsQuery, requests[i].ID)
		if err == nil { requests[i].Items = items }
	}
	return requests, nil
}

func (r *Repository) UpdateProcurementStatus(requestID int, status string, adminID int) error {
	_, err := r.db.Exec("UPDATE procurement_requests SET status = $1, approved_by = $2, updated_at = NOW() WHERE id = $3", status, adminID, requestID)
	return err
}

func (r *Repository) GetGhostItems(companyID int) ([]string, error) {
	var items []string
	query := `SELECT DISTINCT position_name FROM operations 
              WHERE company_id = $1 
              AND position_name NOT IN (SELECT name FROM positions WHERE company_id = $1)`
	err := r.db.Select(&items, query, companyID)
	return items, err
}

func (r *Repository) UpdateMember(companyID, userID int, role, title string) error {
    _, err := r.db.Exec("UPDATE memberships SET role = $1, custom_title = $2 WHERE company_id = $3 AND user_id = $4", 
        role, title, companyID, userID)
    return err
}

func (r *Repository) GetMembership(companyID, userID int) (*models.Membership, error) {
    var m models.Membership
	query := "SELECT user_id, company_id, role, COALESCE(custom_title, '') as custom_title FROM memberships WHERE company_id=$1 AND user_id=$2"
    err := r.db.Get(&m, query, companyID, userID)
    return &m, err
}

func (r *Repository) RemoveMember(companyID, userID int) error {
    _, err := r.db.Exec("DELETE FROM memberships WHERE company_id = $1 AND user_id = $2", companyID, userID)
    return err
}