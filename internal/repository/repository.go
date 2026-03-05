package repository

import (
	"qa2a/internal/models"
	"github.com/jmoiron/sqlx"
)

type Repository struct {
	db *sqlx.DB
}

func New(db *sqlx.DB) *Repository {
	return &Repository{db: db}
}
func (r *Repository) GetMembershipsByUserID(userID int) ([]models.Membership, error) {
	var m []models.Membership
	err := r.db.Select(&m, "SELECT user_id, company_id, role FROM memberships WHERE user_id = $1", userID)
	return m, err
}
func (r *Repository) CreateOperation(op *models.Operation) error {
    query := `INSERT INTO operations (company_id, user_id, type, position_name, quantity, unit, status) 
              VALUES (:company_id, :user_id, :type, :position_name, :quantity, :unit, :status)`
    _, err := r.db.NamedExec(query, op)
    return err
}
// CreateUser создает пользователя и возвращает его
func (r *Repository) CreateUser(tgID int64, username, fullName string) (*models.User, error) {
	// Исправленный SQL с учетом структуры таблицы
	query := `INSERT INTO users (tg_id, username, full_name) 
	          VALUES ($1, $2, $3) 
	          ON CONFLICT (tg_id) DO UPDATE SET username = EXCLUDED.username 
	          RETURNING id, tg_id, username, full_name, created_at`
	
	var user models.User
	err := r.db.QueryRowx(query, tgID, username, fullName).StructScan(&user)
	return &user, err
}

// CreateCompany создает компанию и возвращает ID
func (r *Repository) CreateCompany(name string) (int, error) {
	var id int
	err := r.db.QueryRow("INSERT INTO companies (name) VALUES ($1) RETURNING id", name).Scan(&id)
	return id, err
}
func (r *Repository) UpdateBalance(companyID int, posName string, quantity float64, unit string) error {
    query := `INSERT INTO balances (company_id, position_name, quantity, unit) 
              VALUES ($1, $2, $3, $4)
              ON CONFLICT (company_id, position_name) 
              DO UPDATE SET quantity = balances.quantity + EXCLUDED.quantity`
    _, err := r.db.Exec(query, companyID, posName, quantity, unit)
    return err
}
// AddMember добавляет юзера в компанию с ролью
func (r *Repository) AddMember(userID, companyID int, role string) error {
	_, err := r.db.Exec("INSERT INTO memberships (user_id, company_id, role) VALUES ($1, $2, $3)", userID, companyID, role)
	return err
}
