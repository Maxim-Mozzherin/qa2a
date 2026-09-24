package repository

import (
	"qa2a/internal/models"
)

// ============================================================================
// СТАТЬИ СПИСАНИЯ (СЧЕТА РАСХОДОВ)
// ============================================================================

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
