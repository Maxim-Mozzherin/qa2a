package repository

import (
	"qa2a/internal/models"
)

// ============================================================================
// ЗАЯВКИ В БУХГАЛТЕРИЮ (SERVICE DESK)
// ============================================================================

// CreateAccountingTicket создает новую заявку в бухгалтерию от имени заведения.
func (r *Repository) CreateAccountingTicket(companyID, userID int, category, priority, description, mediaPaths string) error {
	query := `
		INSERT INTO accounting_tickets (company_id, user_id, category, priority, description, media_paths)
		VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := r.db.Exec(query, companyID, userID, category, priority, description, mediaPaths)
	return err
}

// GetCompanyTickets возвращает список последних 50 заявок заведения в бухгалтерию.
func (r *Repository) GetCompanyTickets(companyID int) ([]models.AccountingTicket, error) {
	var tickets []models.AccountingTicket
	query := `
		SELECT 
			t.id, t.company_id, t.user_id, t.category, t.priority, t.description,
			t.status, t.accountant_comment, t.media_paths, t.created_at, t.updated_at,
			COALESCE(u.full_name, u.username) AS user_name
		FROM accounting_tickets t
		JOIN users u ON t.user_id = u.id
		WHERE t.company_id = $1
		ORDER BY t.created_at DESC
		LIMIT 50`
	err := r.db.Select(&tickets, query, companyID)
	return tickets, err
}
