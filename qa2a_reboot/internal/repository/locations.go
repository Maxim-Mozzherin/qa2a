package repository

import (
	"qa2a/internal/models"
)

// ============================================================================
// СКЛАДЫ И НОМЕНКЛАТУРНЫЕ ПОЗИЦИИ
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
