package service
import (
	"qa2a/internal/models"
	"qa2a/internal/repository"
	"github.com/jmoiron/sqlx"
	"fmt"
)
type InventoryService struct { repo *repository.Repository }
func NewInventoryService(repo *repository.Repository) *InventoryService { return &InventoryService{repo: repo} }

func (s *InventoryService) WriteOff(userID, companyID int, posName string, qty float64, unit string, locationID int, isUnlisted bool, comment string) error {
	return s.repo.ExecuteInTx(func(tx *sqlx.Tx) error {
		op := &models.Operation{
			CompanyID: companyID, UserID: userID, Type: "writeoff", 
			PositionName: posName, Quantity: qty, Unit: unit, Status: "approved", 
			LocationID: locationID, IsUnlisted: isUnlisted, Comment: comment, // <-- Сохраняем "призрака"
		}
		if err := s.repo.CreateOperationTx(tx, op); err != nil { return err }
		
		// Если товар официальный - обновляем баланс. Если "Призрак" - баланс не трогаем (его нет в БД)
		if !isUnlisted {
			return s.repo.UpdateBalanceTx(tx, companyID, locationID, posName, -qty, unit)
		}
		return nil
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
	if fromLoc == toLoc {
		return fmt.Errorf("склады отправления и назначения должны различаться")
	}

	return s.repo.ExecuteInTx(func(tx *sqlx.Tx) error {
		// 1. Операция списания с исходного склада
		opOut := &models.Operation{
			CompanyID: companyID, UserID: userID, LocationID: fromLoc,
			Type: "transfer_out", PositionName: posName, Quantity: -qty, Unit: unit, Status: "approved",
		}
		if err := s.repo.CreateOperationTx(tx, opOut); err != nil {
			return err
		}
		if err := s.repo.UpdateBalanceTx(tx, companyID, fromLoc, posName, -qty, unit); err != nil {
			return err
		}

		// 2. Операция зачисления на целевой склад
		opIn := &models.Operation{
			CompanyID: companyID, UserID: userID, LocationID: toLoc,
			Type: "transfer_in", PositionName: posName, Quantity: qty, Unit: unit, Status: "approved",
		}
		if err := s.repo.CreateOperationTx(tx, opIn); err != nil {
			return err
		}
		if err := s.repo.UpdateBalanceTx(tx, companyID, toLoc, posName, qty, unit); err != nil {
			return err
		}

		return nil
	})
}
// --- PROCUREMENT ---

func (s *InventoryService) CreateProcurementRequest(companyID, userID int, items []models.ProcurementItem) error {
	if len(items) == 0 {
		return fmt.Errorf("заявка не может быть пустой")
	}
	return s.repo.CreateProcurementRequest(companyID, userID, items)
}

func (s *InventoryService) GetProcurementRequests(companyID int, status string) ([]models.ProcurementRequest, error) {
	return s.repo.GetProcurementRequests(companyID, status)
}

func (s *InventoryService) UpdateProcurementStatus(requestID int, status string, adminID int) error {
	return s.repo.UpdateProcurementStatus(requestID, status, adminID)
}
func (s *InventoryService) GetGhostItems(companyID int) ([]string, error) {
	return s.repo.GetGhostItems(companyID)
}