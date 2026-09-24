package repository

import (
	"strings"

	"qa2a/internal/models"
)

// ============================================================================
// ПОСТАВЩИКИ И МАППИНГИ
// ============================================================================

// GetPositionSuppliers находит поставщиков товара по связкам накладных.
func (r *Repository) GetPositionSuppliers(companyID int, productUUID string) ([]models.WriteoffAccount, error) {
	var results []models.WriteoffAccount
	query := `
		SELECT supplier_uuid AS external_id, supplier_name AS name
		FROM purchase_history
		WHERE company_id = $1 AND iiko_product_uuid = $2 AND supplier_uuid != ''
		ORDER BY invoice_date DESC, id DESC
		LIMIT 1`
		
	err := r.db.Select(&results, query, companyID, productUUID)
	if err != nil {
		// If no rows are found, it's not a fatal error; fallback is handled by the service
		return nil, nil 
	}
	return results, nil
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
