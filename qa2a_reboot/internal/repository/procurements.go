package repository

import (
	"strings"

	"qa2a/internal/models"

	"github.com/jmoiron/sqlx"
)

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
			COALESCE(NULLIF(u.full_name, ''), NULLIF(u.username, ''), 'Сотрудник') AS user_name,
			COALESCE(u.username, '') AS username,
			COALESCE(m.role, 'user') AS user_role,
			COALESCE(m.custom_title, '') AS custom_title
		FROM procurement_requests pr 
		JOIN users u ON pr.user_id = u.id 
		LEFT JOIN memberships m ON m.user_id = pr.user_id AND m.company_id = pr.company_id
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
		SELECT pi.position_name, pi.quantity, pi.unit, COALESCE(pi.supplier, '') AS supplier 
		FROM procurement_items pi
		JOIN procurement_requests pr ON pi.request_id = pr.id
		WHERE pi.request_id = $1 AND pr.company_id = $2`
	err := r.db.Select(&rows, query, requestID, companyID)
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
			parts := strings.SplitN(row.Supplier, "|", 2)
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
