package repository

import (
	"qa2a/internal/models"

	"github.com/jmoiron/sqlx"
)

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
