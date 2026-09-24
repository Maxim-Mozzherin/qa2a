package repository

import (
	"fmt"
	"time"

	"qa2a/internal/models"

	"github.com/jmoiron/sqlx"
)

// ============================================================================
// ОПЕРАЦИИ (СПИСАНИЯ, ПЕРЕМЕЩЕНИЯ, ЖУРНАЛ ДВИЖЕНИЙ, ВЫГРУЗКА В IIKO)
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
	query := `
		SELECT 
			o.*, 
			COALESCE(NULLIF(u.full_name, ''), NULLIF(u.username, ''), 'Система') AS user_name,
			COALESCE(u.username, '') AS username,
			COALESCE(m.role, 'user') AS user_role,
			COALESCE(m.custom_title, '') AS custom_title
		FROM operations o 
		LEFT JOIN users u ON o.user_id = u.id 
		LEFT JOIN memberships m ON m.user_id = o.user_id AND m.company_id = o.company_id
		WHERE o.company_id = $1 AND o.id = $2`
	err := r.db.Get(&op, query, companyID, id)
	return &op, err
}

// GetOperationByIDTx возвращает конкретную операцию по ID в рамках транзакции.
func (r *Repository) GetOperationByIDTx(tx *sqlx.Tx, companyID, id int) (*models.Operation, error) {
	var op models.Operation
	query := `
		SELECT 
			o.*, 
			COALESCE(NULLIF(u.full_name, ''), NULLIF(u.username, ''), 'Система') AS user_name,
			COALESCE(u.username, '') AS username,
			COALESCE(m.role, 'user') AS user_role,
			COALESCE(m.custom_title, '') AS custom_title
		FROM operations o 
		LEFT JOIN users u ON o.user_id = u.id 
		LEFT JOIN memberships m ON m.user_id = o.user_id AND m.company_id = o.company_id
		WHERE o.company_id = $1 AND o.id = $2`
	err := tx.Get(&op, query, companyID, id)
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
			COALESCE(NULLIF(u.full_name, ''), NULLIF(u.username, ''), 'Система') AS user_name,
			COALESCE(u.username, '') AS username,
			COALESCE(m.role, 'user') AS user_role,
			COALESCE(m.custom_title, '') AS custom_title
		FROM operations o 
		LEFT JOIN users u ON o.user_id = u.id 
		LEFT JOIN memberships m ON m.user_id = o.user_id AND m.company_id = o.company_id
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

// GetPendingWriteoffs возвращает список списаний в ожидании подтверждения шефа/управляющего.
func (r *Repository) GetPendingWriteoffs(companyID int) ([]models.PendingWriteoffDTO, error) {
	results := []models.PendingWriteoffDTO{}
	query := `
		SELECT 
			o.id,
			o.company_id,
			o.location_id,
			COALESCE(l.name, '') AS location_name,
			o.user_id,
			COALESCE(NULLIF(u.full_name, ''), NULLIF(u.username, ''), 'Повар') AS user_name,
			COALESCE(u.username, '') AS username,
			COALESCE(m.role, 'user') AS user_role,
			COALESCE(m.custom_title, '') AS custom_title,
			o.position_name,
			o.quantity,
			o.unit,
			o.status,
			o.created_at,
			CASE 
				WHEN EXTRACT(HOUR FROM o.created_at AT TIME ZONE 'Asia/Yekaterinburg') < 6 
				THEN TO_CHAR((o.created_at AT TIME ZONE 'Asia/Yekaterinburg') - INTERVAL '1 day', 'YYYY-MM-DD')
				ELSE TO_CHAR(o.created_at AT TIME ZONE 'Asia/Yekaterinburg', 'YYYY-MM-DD')
			END AS business_date,
			o.comment,
			o.account_id,
			COALESCE(wa.name, 'Расход продуктов') AS account_name,
			o.is_unlisted
		FROM operations o
		LEFT JOIN locations l ON o.location_id = l.id
		LEFT JOIN users u ON o.user_id = u.id
		LEFT JOIN memberships m ON m.user_id = o.user_id AND m.company_id = o.company_id
		LEFT JOIN writeoff_accounts wa ON o.account_id = wa.external_id AND wa.company_id = o.company_id
		WHERE o.company_id = $1 
		  AND o.type = 'writeoff' 
		  AND o.status = 'pending'
		ORDER BY o.created_at DESC`
	err := r.db.Select(&results, query, companyID)
	return results, err
}

// GetPendingWriteoffsCount возвращает количество неподтвержденных списаний для бейджа-счетчика.
func (r *Repository) GetPendingWriteoffsCount(companyID int) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM operations WHERE company_id = $1 AND type = 'writeoff' AND status = 'pending'`
	err := r.db.Get(&count, query, companyID)
	return count, err
}

// ApproveWriteoff переводит списание в статус утвержденного шефом.
func (r *Repository) ApproveWriteoff(companyID, operationID, chefUserID int) error {
	query := `
		UPDATE operations 
		SET status = 'approved', approved_by = $1, approved_at = NOW() 
		WHERE company_id = $2 AND id = $3 AND type = 'writeoff' AND status = 'pending'`
	res, err := r.db.Exec(query, chefUserID, companyID, operationID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("операция #%d не найдена или уже утверждена", operationID)
	}
	return nil
}

// RejectWriteoffTx переводит списание в статус отклоненного в рамках транзакции.
func (r *Repository) RejectWriteoffTx(tx *sqlx.Tx, companyID, operationID, chefUserID int) error {
	query := `
		UPDATE operations 
		SET status = 'rejected', approved_by = $1, approved_at = NOW() 
		WHERE company_id = $2 AND id = $3 AND type = 'writeoff'`
	res, err := tx.Exec(query, chefUserID, companyID, operationID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("операция #%d не найдена", operationID)
	}
	return nil
}

// UpsertShiftNote сохраняет или обновляет комментарий шефа к смене и статье расходов.
func (r *Repository) UpsertShiftNote(note *models.WriteoffShiftNote) error {
	query := `
		INSERT INTO writeoff_shift_notes (company_id, account_id, business_date, note, chef_user_id, updated_at)
		VALUES ($1, $2, $3::date, $4, $5, NOW())
		ON CONFLICT (company_id, account_id, business_date)
		DO UPDATE SET note = EXCLUDED.note, chef_user_id = EXCLUDED.chef_user_id, updated_at = NOW()`
	_, err := r.db.Exec(query, note.CompanyID, note.AccountID, note.BusinessDate, note.Note, note.ChefUserID)
	return err
}

// GetShiftNote возвращает заметку шефа для конкретной статьи и бизнес-даты смены.
func (r *Repository) GetShiftNote(companyID int, accountID, businessDate string) (*models.WriteoffShiftNote, error) {
	var note models.WriteoffShiftNote
	query := `SELECT id, company_id, account_id, TO_CHAR(business_date, 'YYYY-MM-DD') AS business_date, note, chef_user_id, updated_at 
	          FROM writeoff_shift_notes 
	          WHERE company_id = $1 AND account_id = $2 AND business_date = $3::date`
	err := r.db.Get(&note, query, companyID, accountID, businessDate)
	if err != nil {
		return nil, err
	}
	return &note, nil
}

// GetShiftNotesByCompany возвращает все заметки к сменам для компании.
func (r *Repository) GetShiftNotesByCompany(companyID int) ([]models.WriteoffShiftNote, error) {
	var notes []models.WriteoffShiftNote
	query := `SELECT id, company_id, account_id, TO_CHAR(business_date, 'YYYY-MM-DD') AS business_date, note, chef_user_id, updated_at 
	          FROM writeoff_shift_notes 
	          WHERE company_id = $1`
	err := r.db.Select(&notes, query, companyID)
	return notes, err
}

// GetWriteoffExportDetails возвращает статус утверждения и данные шефов для пачки ID списаний.
func (r *Repository) GetWriteoffExportDetails(companyID int, opIDs []string) ([]models.WriteoffExportDetailsDTO, error) {
	if len(opIDs) == 0 {
		return nil, nil
	}
	query, args, err := sqlx.In(`
		SELECT 
			o.id,
			o.status,
			o.approved_by,
			u.full_name AS chef_full_name,
			u.username AS chef_username
		FROM operations o
		LEFT JOIN users u ON o.approved_by = u.id
		WHERE o.company_id = ? AND o.id IN (?)`, companyID, opIDs)
	if err != nil {
		return nil, fmt.Errorf("ошибка построения запроса GetWriteoffExportDetails: %w", err)
	}
	query = r.db.Rebind(query)
	var details []models.WriteoffExportDetailsDTO
	err = r.db.Select(&details, query, args...)
	return details, err
}

// GetGroupedWriteoffs группирует невыгруженные списания по складу, статье и дате смены (граница 06:00 YEKT).
func (r *Repository) GetGroupedWriteoffs(companyID int, cutoffTime *time.Time) ([]models.ExportOperationDTO, error) {
	var results []models.ExportOperationDTO
	query := `
		SELECT 
			STRING_AGG(o.id::text, ',') AS op_ids,
			p.external_id AS product_id,
			l.external_id AS store_from,
			'' AS store_to,
			o.account_id,
			CASE 
				WHEN EXTRACT(HOUR FROM o.created_at AT TIME ZONE 'Asia/Yekaterinburg') < 6 
				THEN TO_CHAR((o.created_at AT TIME ZONE 'Asia/Yekaterinburg') - INTERVAL '1 day', 'YYYY-MM-DD')
				ELSE TO_CHAR(o.created_at AT TIME ZONE 'Asia/Yekaterinburg', 'YYYY-MM-DD')
			END AS op_date,
			SUM(o.quantity) AS total_qty
		FROM operations o
		JOIN positions p ON o.position_name = p.name AND o.company_id = p.company_id
		JOIN locations l ON o.location_id = l.id
		WHERE o.company_id = $1 
		  AND o.type = 'writeoff' 
		  AND o.exported_to_iiko = false 
		  AND o.status IN ('approved', 'pending')
		  AND p.external_id != '' AND l.external_id != '' AND o.is_unlisted = false`

	var args []interface{}
	args = append(args, companyID)

	if cutoffTime != nil {
		query += ` AND o.created_at < $2`
		args = append(args, *cutoffTime)
	}

	query += `
		GROUP BY p.external_id, l.external_id, o.account_id, 
			CASE 
				WHEN EXTRACT(HOUR FROM o.created_at AT TIME ZONE 'Asia/Yekaterinburg') < 6 
				THEN TO_CHAR((o.created_at AT TIME ZONE 'Asia/Yekaterinburg') - INTERVAL '1 day', 'YYYY-MM-DD')
				ELSE TO_CHAR(o.created_at AT TIME ZONE 'Asia/Yekaterinburg', 'YYYY-MM-DD')
			END`

	err := r.db.Select(&results, query, args...)
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
	return r.ExecuteInTx(func(tx *sqlx.Tx) error {
		// 1. Mark target operations (writeoffs, transfer_in, assembly_in)
		query, args, err := sqlx.In(`UPDATE operations SET exported_to_iiko = true WHERE company_id = ? AND id IN (?)`, companyID, opIDs)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(tx.Rebind(query), args...); err != nil {
			return err
		}

		// 2. Mark corresponding transfer_out / assembly_out operations to avoid ghost rows
		queryPairs, argsPairs, err := sqlx.In(`
			UPDATE operations out_op
			SET exported_to_iiko = true
			FROM operations in_op
			WHERE in_op.id IN (?)
			  AND in_op.company_id = ?
			  AND out_op.company_id = in_op.company_id
			  AND out_op.exported_to_iiko = false
			  AND (
				  (in_op.type = 'transfer_in' AND out_op.type = 'transfer_out') OR
				  (in_op.type = 'assembly_in' AND out_op.type = 'assembly_out')
			  )
			  AND out_op.position_name = in_op.position_name
			  AND out_op.location_id = in_op.to_location_id
			  AND out_op.to_location_id = in_op.location_id
			  AND ABS(EXTRACT(EPOCH FROM (out_op.created_at - in_op.created_at))) < 120
		`, opIDs, companyID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(tx.Rebind(queryPairs), argsPairs...)
		return err
	})
}
