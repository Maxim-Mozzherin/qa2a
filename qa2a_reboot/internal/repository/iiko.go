package repository

import (
	"fmt"
	"strings"

	"qa2a/internal/models"

	"github.com/jmoiron/sqlx"
)

// UpdateIikoSettings обновляет хост, логин и пароль подключения к API iiko RMS.
// Если передан пустой пароль или маска "********", текущий зашифрованный пароль в базе сохраняется.
func (r *Repository) UpdateIikoSettings(companyID int, host, login, password string) error {
	query := `
		UPDATE companies 
		SET 
			iiko_host = $1, 
			iiko_api_login = $2, 
			iiko_api_password = CASE 
				WHEN $3 = '' OR $3 = '********' THEN iiko_api_password 
				ELSE $3 
			END 
		WHERE id = $4`
	_, err := r.db.Exec(query, strings.TrimSpace(host), strings.TrimSpace(login), password, companyID)
	if err != nil {
		return fmt.Errorf("ошибка обновления настроек iiko в БД: %w", err)
	}
	return nil
}

// GetIikoSettings возвращает сохраненные параметры подключения к API iiko RMS заведения.
func (r *Repository) GetIikoSettings(companyID int) (*models.IikoSettings, error) {
	var settings models.IikoSettings
	query := `
		SELECT 
			id, 
			COALESCE(iiko_host, '') AS iiko_host, 
			COALESCE(iiko_api_login, '') AS iiko_api_login, 
			COALESCE(iiko_api_password, '') AS iiko_api_password 
		FROM companies 
		WHERE id = $1`
	err := r.db.QueryRow(query, companyID).Scan(
		&settings.CompanyID,
		&settings.Host,
		&settings.Login,
		&settings.Password,
	)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения настроек iiko для заведения #%d: %w", companyID, err)
	}
	return &settings, nil
}

// SaveIikoUnitsTx сохраняет справочник единиц измерения iiko в рамках транзакции пакетом.
func (r *Repository) SaveIikoUnitsTx(tx *sqlx.Tx, companyID int, units map[string]string) error {
	if _, err := tx.Exec("DELETE FROM iiko_units WHERE company_id = $1", companyID); err != nil {
		return fmt.Errorf("ошибка очистки старых единиц измерения: %w", err)
	}

	if len(units) == 0 {
		return nil
	}

	// Формируем пакетную вставку multi-row VALUES
	valueStrings := make([]string, 0, len(units))
	valueArgs := make([]interface{}, 0, len(units)*3)
	i := 1

	for id, name := range units {
		valueStrings = append(valueStrings, fmt.Sprintf("($%d, $%d, $%d)", i, i+1, i+2))
		valueArgs = append(valueArgs, id, companyID, name)
		i += 3
	}

	stmt := fmt.Sprintf(
		"INSERT INTO iiko_units (id, company_id, name) VALUES %s ON CONFLICT (id, company_id) DO UPDATE SET name = EXCLUDED.name",
		strings.Join(valueStrings, ","),
	)

	if _, err := tx.Exec(stmt, valueArgs...); err != nil {
		return fmt.Errorf("ошибка пакетной вставки единиц измерения: %w", err)
	}

	return nil
}

// UpsertPositionTx атомарно создает или обновляет позицию номенклатуры при синхронизации с iiko RMS.
func (r *Repository) UpsertPositionTx(tx *sqlx.Tx, p models.Position) error {
	query := `
		INSERT INTO positions (
			company_id, name, unit, supplier, external_id, type, conception
		) VALUES (
			:company_id, :name, :unit, :supplier, :external_id, :type, :conception
		)
		ON CONFLICT (company_id, external_id) WHERE external_id != ''
		DO UPDATE SET 
			name = EXCLUDED.name, 
			unit = EXCLUDED.unit, 
			supplier = EXCLUDED.supplier,
			type = EXCLUDED.type, 
			conception = EXCLUDED.conception`
	_, err := tx.NamedExec(query, p)
	if err != nil {
		return fmt.Errorf("ошибка UpsertPositionTx для '%s' (UUID: %s): %w", p.Name, p.ExternalID, err)
	}
	return nil
}

// UpdateBalanceTx выполняет атомарный инкремент/декремент остатка товара на складе.
func (r *Repository) UpdateBalanceTx(tx *sqlx.Tx, companyID int, locationID int, posName string, qty float64, unit string) error {
	query := `
		INSERT INTO balances (company_id, location_id, position_name, quantity, unit) 
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (company_id, location_id, position_name) 
		DO UPDATE SET 
			quantity = balances.quantity + EXCLUDED.quantity,
			unit = EXCLUDED.unit`
	_, err := tx.Exec(query, companyID, locationID, posName, qty, unit)
	if err != nil {
		return fmt.Errorf("ошибка обновления баланса позиции '%s' (склад #%d): %w", posName, locationID, err)
	}
	return nil
}

// UpdateBalanceByExternalIDTx обновляет остаток позиции по внешнему UUID iiko склада и товара.
func (r *Repository) UpdateBalanceByExternalIDTx(tx *sqlx.Tx, companyID int, storeExternalID string, productExternalID string, qty float64) error {
	var locID int
	err := tx.Get(&locID, "SELECT id FROM locations WHERE company_id = $1 AND external_id = $2 LIMIT 1", companyID, storeExternalID)
	if err != nil {
		return fmt.Errorf("склад iiko с UUID '%s' не найден в базе: %w", storeExternalID, err)
	}

	var pos struct {
		Name string `db:"name"`
		Unit string `db:"unit"`
	}
	err = tx.Get(&pos, "SELECT name, unit FROM positions WHERE company_id = $1 AND external_id = $2 LIMIT 1", companyID, productExternalID)
	if err != nil {
		return fmt.Errorf("товар iiko с UUID '%s' не найден в номенклатуре: %w", productExternalID, err)
	}

	query := `
		INSERT INTO balances (company_id, location_id, position_name, quantity, unit) 
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (company_id, location_id, position_name) 
		DO UPDATE SET 
			quantity = EXCLUDED.quantity,
			unit = EXCLUDED.unit`
	_, err = tx.Exec(query, companyID, locID, pos.Name, qty, pos.Unit)
	if err != nil {
		return fmt.Errorf("ошибка установки абсолютного остатка товара '%s': %w", pos.Name, err)
	}
	return nil
}

