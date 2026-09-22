package service

import (
	"database/sql"
		"qa2a/internal/repository"
)

type AccountantService struct {
	repo *repository.Repository
}

func NewAccountantService(repo *repository.Repository) *AccountantService {
	return &AccountantService{repo: repo}
}

func (s *AccountantService) GetFirmIDByTgID(tgID int64) (int, error) {
	var firmID int
	err := s.repo.GetDb().Get(&firmID, "SELECT accounting_firm_id FROM accounting_users WHERE tg_id = $1 AND is_active = true LIMIT 1", tgID)
	return firmID, err
}

func (s *AccountantService) RegisterFirm(name string) (int, error) {
	var firmID int
	err := s.repo.GetDb().QueryRow("INSERT INTO accounting_firms (name) VALUES ($1) RETURNING id", name).Scan(&firmID)
	return firmID, err
}

func (s *AccountantService) AddUserToFirm(firmID int, tgID int64, username string, role string) error {
	_, err := s.repo.GetDb().Exec(`
		INSERT INTO accounting_users (accounting_firm_id, tg_id, tg_username, role) 
		VALUES ($1, $2, $3, $4) 
		ON CONFLICT (tg_id) DO UPDATE SET accounting_firm_id = EXCLUDED.accounting_firm_id, role = EXCLUDED.role
	`, firmID, tgID, username, role)
	return err
}

type FirmCompany struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
	Code string `json:"invite_code" db:"invite_code"`
}

func (s *AccountantService) GetFirmCompanies(firmID int) ([]FirmCompany, error) {
	var companies []FirmCompany
	err := s.repo.GetDb().Select(&companies, "SELECT id, name, invite_code FROM companies WHERE accounting_firm_id = $1 ORDER BY name", firmID)
	if err == sql.ErrNoRows {
		return []FirmCompany{}, nil
	}
	return companies, err
}

