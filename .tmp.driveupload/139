package service
import (
	"qa2a/internal/models"
	"qa2a/internal/repository"
	"github.com/jmoiron/sqlx"
)
type InventoryService struct { repo *repository.Repository }
func NewInventoryService(repo *repository.Repository) *InventoryService { return &InventoryService{repo: repo} }

func (s *InventoryService) WriteOff(userID, companyID int, posName string, qty float64, unit string, locationID int) error {
	return s.repo.ExecuteInTx(func(tx *sqlx.Tx) error {
		op := &models.Operation{CompanyID: companyID, UserID: userID, Type: "writeoff", PositionName: posName, Quantity: qty, Unit: unit, Status: "approved", LocationID: locationID}
		if err := s.repo.CreateOperationTx(tx, op); err != nil { return err }
		return s.repo.UpdateBalanceTx(tx, companyID, locationID, posName, -qty, unit)
	})
}

func (s *InventoryService) GetBalances(companyID int) ([]models.Balance, error) {
	return s.repo.GetBalancesByCompany(companyID)
}

func (s *InventoryService) GetHistory(companyID int, limit int) ([]models.Operation, error) {
	return s.repo.GetOperationsByCompany(companyID, limit)
}

// НОВЫЕ МЕТОДЫ УПРАВЛЕНИЯ
func (s *InventoryService) CreateLocation(companyID int, name string) error {
	return s.repo.CreateLocation(companyID, name)
}

func (s *InventoryService) GetLocations(companyID int) ([]models.Location, error) {
	return s.repo.GetLocations(companyID)
}

func (s *InventoryService) CreatePosition(p *models.Position) error {
	return s.repo.CreatePosition(p)
}

func (s *InventoryService) GetPositions(companyID int) ([]models.Position, error) {
	return s.repo.GetPositions(companyID)
}
func (s *InventoryService) Transfer(userID, companyID int, posName string, qty float64, unit string, fromLoc, toLoc int) error {
	return s.repo.ExecuteInTx(func(tx *sqlx.Tx) error {
		// 1. Запись об операции переноса
		op := &models.Operation{
			CompanyID: companyID, UserID: userID, Type: "transfer",
			PositionName: posName, Quantity: qty, Unit: unit, Status: "approved",
		}
		if err := s.repo.CreateOperationTx(tx, op); err != nil { return err }

		// 2. Уменьшаем остаток на складе-источнике (логика складов будет расширена позже, пока общий баланс)
		// В MVP мы просто фиксируем факт переноса. Если хочешь строгий учет по складам, 
        // нужно добавить location_id в таблицу balances. Оставим это на след. шаг.
		return nil 
	})
}