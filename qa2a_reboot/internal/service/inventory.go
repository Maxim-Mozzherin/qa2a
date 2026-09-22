package service

import (
	"fmt"
	"strings"
	"time"

	"qa2a/internal/models"
	"qa2a/internal/repository"

	"github.com/jmoiron/sqlx"
)

// SupplierInfo описывает поставщика с его контактом в Telegram для быстрых заказов.
type SupplierInfo struct {
	UUID       string `json:"uuid"`
	Name       string `json:"name"`
	TgUsername string `json:"tg_username"`
}

// PositionSuppliersResult возвращает список поставщиков товара и флаг, является ли список общим (fallback).
type PositionSuppliersResult struct {
	IsFallback bool           `json:"is_fallback"`
	Suppliers  []SupplierInfo `json:"suppliers"`
}

// InventoryService управляет складскими операциями, инвентаризациями и заявками на поставку.
type InventoryService struct {
	repo    *repository.Repository
	iikoSvc *IikoService
}

// NewInventoryService создает новый экземпляр InventoryService.
func NewInventoryService(repo *repository.Repository, iikoSvc *IikoService) *InventoryService {
	return &InventoryService{
		repo:    repo,
		iikoSvc: iikoSvc,
	}
}

// GetRepo возвращает экземпляр низкоуровневого репозитория.
func (s *InventoryService) GetRepo() *repository.Repository {
	return s.repo
}

// ============================================================================
// 1. СПИСАНИЯ ТОВАРОВ (WRITEOFF)
// ============================================================================

// WriteOff выполняет регистрацию списания товара и атомарно уменьшает складской баланс.
func (s *InventoryService) WriteOff(
	userID, companyID int,
	posName string,
	qty float64,
	unit string,
	locationID int,
	isUnlisted bool,
	comment string,
	accountID string,
	opDate time.Time,
) error {
	trimmedPos := strings.TrimSpace(posName)
	if trimmedPos == "" {
		return fmt.Errorf("наименование списываемого товара не указано")
	}
	if locationID <= 0 {
		return fmt.Errorf("не выбран склад списания")
	}

	cleanAccountID := strings.TrimSpace(accountID)
	if cleanAccountID == "" {
		cleanAccountID = "97036ddb-b2e1-cd47-1669-c145daa9f9c5" // Дефолтная статья "Расход продуктов"
	}

	return s.repo.ExecuteInTx(func(tx *sqlx.Tx) error {
		op := &models.Operation{
			CompanyID:    companyID,
			UserID:       userID,
			Type:         "writeoff",
			PositionName: trimmedPos,
			Quantity:     qty,
			Unit:         unit,
			Status:       "approved",
			LocationID:   locationID,
			IsUnlisted:   isUnlisted,
			Comment:      strings.TrimSpace(comment),
			AccountID:    cleanAccountID,
			CreatedAt:    opDate,
		}

		if err := s.repo.CreateOperationTx(tx, op); err != nil {
			return fmt.Errorf("ошибка создания записи списания: %w", err)
		}

		// Для неучтенных товаров баланс не корректируется (их нет в справочнике)
		if !isUnlisted {
			if err := s.repo.UpdateBalanceTx(tx, companyID, locationID, trimmedPos, -qty, unit); err != nil {
				return fmt.Errorf("ошибка корректировки баланса при списании: %w", err)
			}
		}
		return nil
	})
}

// EditWriteoff позволяет скорректировать операцию списания до ее отправки в iiko.
func (s *InventoryService) EditWriteoff(
	userID, companyID, opID int,
	qty float64,
	locID int,
	comment, accountID string,
	opDate time.Time,
) error {
	if qty <= 0 {
		return fmt.Errorf("количество товара должно быть больше нуля")
	}
	if locID <= 0 {
		return fmt.Errorf("укажите корректный склад")
	}

	return s.repo.ExecuteInTx(func(tx *sqlx.Tx) error {
		oldOp, err := s.repo.GetOperationByID(companyID, opID)
		if err != nil {
			return fmt.Errorf("операция #%d не найдена: %w", opID, err)
		}
		if oldOp.Type != "writeoff" {
			return fmt.Errorf("редактирование разрешено только для операций списания")
		}
		if oldOp.ExportedToIiko {
			return fmt.Errorf("нельзя редактировать операцию, которая уже выгружена в iiko RMS")
		}

		if !oldOp.IsUnlisted {
			// 1. Возвращаем старый объем на прежний склад
			if err = s.repo.UpdateBalanceTx(tx, companyID, oldOp.LocationID, oldOp.PositionName, oldOp.Quantity, oldOp.Unit); err != nil {
				return fmt.Errorf("ошибка отката прежнего остатка: %w", err)
			}
			// 2. Списываем новый объем с нового выбранного склада
			if err = s.repo.UpdateBalanceTx(tx, companyID, locID, oldOp.PositionName, -qty, oldOp.Unit); err != nil {
				return fmt.Errorf("ошибка списания нового остатка: %w", err)
			}
		}

		oldOp.Quantity = qty
		oldOp.LocationID = locID
		oldOp.Comment = strings.TrimSpace(comment)
		oldOp.AccountID = strings.TrimSpace(accountID)
		oldOp.CreatedAt = opDate

		return s.repo.UpdateWriteoffTx(tx, oldOp)
	})
}

// ============================================================================
// 2. ПЕРЕМЕЩЕНИЕ И ПРИГОТОВЛЕНИЕ ПОЛУФАБРИКАТОВ
// ============================================================================

// Transfer оформляет внутреннее перемещение товара либо акт приготовления заготовки (PREPARED).
func (s *InventoryService) Transfer(
	userID, companyID int,
	posName string,
	qty float64,
	unit string,
	fromLoc, toLoc int,
	comment string,
	opDate time.Time,
) error {
	if fromLoc <= 0 || toLoc <= 0 {
		return fmt.Errorf("необходимо выбрать склад-отправитель и склад-получатель")
	}
	if fromLoc == toLoc {
		return fmt.Errorf("склады отправления и назначения должны различаться")
	}
	if qty <= 0 {
		return fmt.Errorf("количество для перемещения должно быть строго больше нуля")
	}

	trimmedPos := strings.TrimSpace(posName)
	pos, err := s.repo.GetPositionByName(companyID, trimmedPos)
	if err != nil {
		return fmt.Errorf("позиция '%s' не найдена в номенклатуре: %w", trimmedPos, err)
	}

	typeOut := "transfer_out"
	typeIn := "transfer_in"

	// Если тип товара - полуфабрикат, в iiko регистрируется акт переработки/приготовления
	if strings.ToUpper(pos.Type) == "PREPARED" {
		typeOut = "assembly_out"
		typeIn = "assembly_in"
	}

	cleanComment := strings.TrimSpace(comment)

	return s.repo.ExecuteInTx(func(tx *sqlx.Tx) error {
		// Расход со склада-отправителя
		opOut := &models.Operation{
			CompanyID:    companyID,
			UserID:       userID,
			LocationID:   fromLoc,
			ToLocationID: toLoc,
			Type:         typeOut,
			PositionName: trimmedPos,
			Quantity:     -qty,
			Unit:         unit,
			Status:       "approved",
			Comment:      cleanComment,
			CreatedAt:    opDate,
		}
		if err := s.repo.CreateOperationTx(tx, opOut); err != nil {
			return fmt.Errorf("ошибка создания записи расхода перемещения: %w", err)
		}
		if err := s.repo.UpdateBalanceTx(tx, companyID, fromLoc, trimmedPos, -qty, unit); err != nil {
			return fmt.Errorf("ошибка списания со склада-отправителя: %w", err)
		}

		// Приход на склад-получатель
		opIn := &models.Operation{
			CompanyID:    companyID,
			UserID:       userID,
			LocationID:   toLoc,
			ToLocationID: fromLoc,
			Type:         typeIn,
			PositionName: trimmedPos,
			Quantity:     qty,
			Unit:         unit,
			Status:       "approved",
			Comment:      cleanComment,
			CreatedAt:    opDate,
		}
		if err := s.repo.CreateOperationTx(tx, opIn); err != nil {
			return fmt.Errorf("ошибка создания записи прихода перемещения: %w", err)
		}
		if err := s.repo.UpdateBalanceTx(tx, companyID, toLoc, trimmedPos, qty, unit); err != nil {
			return fmt.Errorf("ошибка начисления на склад-получатель: %w", err)
		}

		return nil
	})
}

// ============================================================================
// 3. ИНВЕНТАРИЗАЦИЯ ОСТАТКОВ
// ============================================================================

// StartInventory открывает акт сплошной инвентаризации склада, подгружая расчетный остаток из iiko OLAP.
func (s *InventoryService) StartInventory(companyID, userID, locationID int) (*models.InventoryAct, error) {
	locs, err := s.repo.GetLocations(companyID)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения складов: %w", err)
	}

	var targetLoc *models.Location
	for _, l := range locs {
		if l.ID == locationID {
			targetLoc = &l
			break
		}
	}
	if targetLoc == nil {
		return nil, fmt.Errorf("склад #%d не найден", locationID)
	}
	if targetLoc.ExternalID == "" {
		return nil, fmt.Errorf("у склада '%s' отсутствует UUID iiko RMS. Выполните синхронизацию", targetLoc.Name)
	}

	stockMap, err := s.iikoSvc.FetchExpectedStock(companyID, targetLoc.ExternalID)
	if err != nil {
		return nil, fmt.Errorf("не удалось загрузить расчетные остатки из iiko OLAP: %w", err)
	}

	goodsMap, err := s.iikoSvc.FetchGoodsCatalog(companyID)
	if err != nil {
		return nil, fmt.Errorf("ошибка проверки каталога товаров iiko: %w", err)
	}

	positions, err := s.repo.GetPositions(companyID)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения локальной номенклатуры: %w", err)
	}

	var items []models.InventoryItem
	for _, p := range positions {
		if p.ExternalID == "" {
			continue
		}

		extIDLower := strings.ToLower(p.ExternalID)
		if !goodsMap[extIDLower] {
			continue
		}

		expectedQty := stockMap[strings.ToUpper(p.ExternalID)]

		items = append(items, models.InventoryItem{
			PositionName:   p.Name,
			ExternalID:     p.ExternalID,
			ExpectedAmount: expectedQty,
			ActualAmount:   0,
		})
	}

	act := &models.InventoryAct{
		CompanyID:  companyID,
		LocationID: locationID,
		UserID:     userID,
		Status:     "in_progress",
		Items:      items,
	}

	err = s.repo.ExecuteInTx(func(tx *sqlx.Tx) error {
		return s.repo.CreateInventoryActTx(tx, act)
	})

	return act, err
}

// StartInventoryFromDraft создает акт инвентаризации на основе пустого бланка, открытого бухгалтером в iiko RMS.
func (s *InventoryService) StartInventoryFromDraft(companyID, userID, locationID int, draftID string) (*models.InventoryAct, error) {
	locs, err := s.repo.GetLocations(companyID)
	if err != nil {
		return nil, err
	}
	var targetLoc *models.Location
	for _, l := range locs {
		if l.ID == locationID {
			targetLoc = &l
			break
		}
	}
	if targetLoc == nil || targetLoc.ExternalID == "" {
		return nil, fmt.Errorf("склад не найден или не привязан к iiko")
	}

	drafts, err := s.iikoSvc.FetchDraftInventories(companyID, targetLoc.ExternalID)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения бланков из iiko: %w", err)
	}

	var selectedDraft *models.IikoDraftInventory
	for _, d := range drafts {
		if d.ID == draftID {
			selectedDraft = &d
			break
		}
	}
	if selectedDraft == nil {
		return nil, fmt.Errorf("черновой бланк '%s' не найден на сервере iiko", draftID)
	}

	positions, err := s.repo.GetPositions(companyID)
	if err != nil {
		return nil, err
	}

	posMap := make(map[string]string, len(positions))
	for _, p := range positions {
		if p.ExternalID != "" {
			posMap[strings.ToLower(p.ExternalID)] = p.Name
		}
	}

	var items []models.InventoryItem
	for _, item := range selectedDraft.Items {
		extID := strings.ToLower(item.ProductID)
		posName, exists := posMap[extID]
		if !exists {
			continue
		}

		items = append(items, models.InventoryItem{
			PositionName:   posName,
			ExternalID:     item.ProductID,
			ExpectedAmount: 0,
			ActualAmount:   0,
		})
	}

	act := &models.InventoryAct{
		CompanyID:      companyID,
		LocationID:     locationID,
		UserID:         userID,
		Status:         "in_progress",
		IikoDocumentID: selectedDraft.ID,
		DocumentNumber: selectedDraft.DocumentNumber,
		Items:          items,
	}

	err = s.repo.ExecuteInTx(func(tx *sqlx.Tx) error {
		return s.repo.CreateInventoryActTx(tx, act)
	})

	return act, err
}

// StartInventoryFromTemplate открывает инвентаризацию по локальному шаблону (срезу).
func (s *InventoryService) StartInventoryFromTemplate(companyID, userID, locationID, templateID int) (*models.InventoryAct, error) {
	templateItems, err := s.repo.GetTemplateItems(templateID)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения позиций шаблона: %w", err)
	}
	if len(templateItems) == 0 {
		return nil, fmt.Errorf("выбранный шаблон инвентаризации пуст")
	}

	positions, err := s.repo.GetPositions(companyID)
	if err != nil {
		return nil, err
	}

	posMap := make(map[string]string, len(positions))
	for _, p := range positions {
		if p.ExternalID != "" {
			posMap[strings.ToLower(p.Name)] = p.ExternalID
		}
	}

	var items []models.InventoryItem
	for _, name := range templateItems {
		extID := posMap[strings.ToLower(name)]
		items = append(items, models.InventoryItem{
			PositionName:   name,
			ExternalID:     extID,
			ExpectedAmount: 0,
			ActualAmount:   0,
		})
	}

	act := &models.InventoryAct{
		CompanyID:  companyID,
		LocationID: locationID,
		UserID:     userID,
		Status:     "in_progress",
		Items:      items,
	}

	err = s.repo.ExecuteInTx(func(tx *sqlx.Tx) error {
		return s.repo.CreateInventoryActTx(tx, act)
	})

	return act, err
}

// SaveInventory сохраняет промежуточный прогресс пересчета (черновик).
func (s *InventoryService) SaveInventory(companyID, actID int, items []models.InventoryItem) error {
	act, err := s.repo.GetInventoryActByID(companyID, actID)
	if err != nil {
		return err
	}
	if act.Status == "completed" {
		return fmt.Errorf("нельзя изменять уже завершенный акт инвентаризации")
	}

	return s.repo.ExecuteInTx(func(tx *sqlx.Tx) error {
		return s.repo.UpdateInventoryItemsTx(tx, actID, items)
	})
}

// FinalizeInventory завершает пересчет и выгружает итоговый документ в iiko RMS.
func (s *InventoryService) FinalizeInventory(companyID, actID int) error {
	act, err := s.repo.GetInventoryActByID(companyID, actID)
	if err != nil {
		return err
	}
	if act.Status == "completed" {
		return fmt.Errorf("акт инвентаризации #%d уже был завершен ранее", actID)
	}

	locs, _ := s.repo.GetLocations(companyID)
	var storeExtID string
	for _, l := range locs {
		if l.ID == act.LocationID {
			storeExtID = l.ExternalID
			break
		}
	}
	if storeExtID == "" {
		return fmt.Errorf("склад инвентаризации не привязан к iiko RMS")
	}

	// Экспорт документа в формате XML в iiko RMS
	err = s.iikoSvc.ExportInventoryAct(companyID, storeExtID, act.IikoDocumentID, act.DocumentNumber, act.Items)
	if err != nil {
		return fmt.Errorf("ошибка выгрузки инвентаризации в iiko: %w", err)
	}

	return s.repo.ExecuteInTx(func(tx *sqlx.Tx) error {
		return s.repo.UpdateInventoryStatusTx(tx, actID, "completed")
	})
}

// DeleteInventory удаляет черновик акта пересчета.
func (s *InventoryService) DeleteInventory(companyID, actID int) error {
	act, err := s.repo.GetInventoryActByID(companyID, actID)
	if err != nil {
		return err
	}
	if act.Status == "completed" {
		return fmt.Errorf("нельзя удалить завершенный акт инвентаризации")
	}
	return s.repo.DeleteInventoryAct(companyID, actID)
}

// GetInventories возвращает историю актов компании.
func (s *InventoryService) GetInventories(companyID int) ([]models.InventoryAct, error) {
	return s.repo.GetInventoryActs(companyID)
}

// GetInventory возвращает акт по его ID.
func (s *InventoryService) GetInventory(companyID, actID int) (*models.InventoryAct, error) {
	return s.repo.GetInventoryActByID(companyID, actID)
}

// CreateTemplateFromExternal создает бланк пересчета по запросу из микросервиса бухгалтера (iiko_parser).
func (s *InventoryService) CreateTemplateFromExternal(companyID, locationID int, name string, items []string) error {
	return s.repo.ExecuteInTx(func(tx *sqlx.Tx) error {
		return s.repo.CreateInventoryTemplateTx(tx, companyID, locationID, name, items)
	})
}

// GetTemplates возвращает шаблоны инвентаризаций для склада.
func (s *InventoryService) GetTemplates(companyID, locationID int) ([]models.InventoryTemplate, error) {
	return s.repo.GetInventoryTemplatesByLocation(companyID, locationID)
}

// ============================================================================
// 4. СТАТЬИ СПИСАНИЯ, СКЛАДЫ, ПОЗИЦИИ, ОСТАТКИ
// ============================================================================

func (s *InventoryService) GetAccounts(companyID int) ([]models.WriteoffAccount, error) {
	return s.repo.GetWriteoffAccounts(companyID)
}

func (s *InventoryService) CreateAccount(companyID int, name, externalID string) error {
	return s.repo.CreateWriteoffAccount(models.WriteoffAccount{
		CompanyID:  companyID,
		Name:       strings.TrimSpace(name),
		ExternalID: strings.TrimSpace(externalID),
	})
}

func (s *InventoryService) DeleteAccount(companyID, id int) error {
	return s.repo.DeleteWriteoffAccount(companyID, id)
}

func (s *InventoryService) GetBalances(companyID int) ([]models.Balance, error) {
	return s.repo.GetBalancesByCompany(companyID)
}

func (s *InventoryService) GetHistory(companyID int, limit int) ([]models.Operation, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	return s.repo.GetOperationsByCompany(companyID, limit)
}

func (s *InventoryService) CreateLocation(companyID int, name string) error {
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		return fmt.Errorf("название склада не может быть пустым")
	}
	return s.repo.CreateLocation(companyID, cleanName)
}

func (s *InventoryService) GetLocations(companyID int) ([]models.Location, error) {
	return s.repo.GetLocations(companyID)
}

func (s *InventoryService) CreatePosition(p *models.Position) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return fmt.Errorf("наименование товара обязательно")
	}
	return s.repo.CreatePosition(p)
}

func (s *InventoryService) GetPositions(companyID int) ([]models.Position, error) {
	return s.repo.GetPositions(companyID)
}

func (s *InventoryService) GetGhostItems(companyID int) ([]string, error) {
	return s.repo.GetGhostItems(companyID)
}

// ============================================================================
// 5. ЗАЯВКИ НА ЗАКУПКУ (PROCUREMENT)
// ============================================================================

func (s *InventoryService) CreateProcurementRequest(companyID, userID int, items []models.ProcurementItem) error {
	if len(items) == 0 {
		return fmt.Errorf("заявка на закупку не может быть пустой")
	}
	return s.repo.CreateProcurementRequest(companyID, userID, items)
}

func (s *InventoryService) GetProcurementRequests(companyID int, status string) ([]models.ProcurementRequest, error) {
	cleanStatus := strings.ToLower(strings.TrimSpace(status))
	if cleanStatus == "" {
		cleanStatus = "pending"
	}
	return s.repo.GetProcurementRequests(companyID, cleanStatus)
}

func (s *InventoryService) UpdateProcurementStatus(requestID int, status string, adminID int) error {
	cleanStatus := strings.ToLower(strings.TrimSpace(status))
	if cleanStatus != "approved" && cleanStatus != "rejected" && cleanStatus != "pending" {
		return fmt.Errorf("недопустимый статус заявки: %s", status)
	}
	return s.repo.UpdateProcurementStatus(requestID, cleanStatus, adminID)
}

func (s *InventoryService) GetProcurementItemsWithSuppliers(companyID, requestID int) ([]repository.ProcurementItemWithSupplier, error) {
	return s.repo.GetProcurementItemsWithSuppliers(companyID, requestID)
}

// ============================================================================
// 6. ПОСТАВЩИКИ И ИХ TELEGRAM-КОНТАКТЫ
// ============================================================================

func (s *InventoryService) SaveSupplierContact(companyID int, supplierUUID, tgUsername string) error {
	cleanUsername := strings.TrimPrefix(strings.TrimSpace(tgUsername), "@")
	return s.repo.SaveSupplierContact(companyID, strings.TrimSpace(supplierUUID), cleanUsername)
}

// GetAllSuppliersWithContacts объединяет поставщиков из iiko и локальной БД с их Telegram-контактами.
func (s *InventoryService) GetAllSuppliersWithContacts(companyID int) ([]SupplierInfo, error) {
	var suppliers []models.WriteoffAccount
	var err error

	iikoSups, err := s.iikoSvc.FetchIikoSuppliers(companyID)
	if err == nil && len(iikoSups) > 0 {
		suppliers = iikoSups
	} else {
		suppliers, err = s.repo.GetAllCompanySuppliers(companyID)
		if err != nil {
			return nil, fmt.Errorf("ошибка чтения списка поставщиков: %w", err)
		}
	}

	contacts, err := s.repo.GetSupplierContacts(companyID)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения контактов: %w", err)
	}

	mappings, _ := s.repo.GetSupplierNameMappings(companyID)

	var list []SupplierInfo
	for _, sup := range suppliers {
		name := sup.Name
		if mappedName, exists := mappings[strings.ToUpper(sup.ExternalID)]; exists && mappedName != "" {
			name = mappedName
		}

		list = append(list, SupplierInfo{
			UUID:       sup.ExternalID,
			Name:       name,
			TgUsername: contacts[sup.ExternalID],
		})
	}
	return list, nil
}

// GetPositionSuppliers подбирает поставщика под конкретный товар.
func (s *InventoryService) GetPositionSuppliers(companyID int, productUUID string) (*PositionSuppliersResult, error) {
	suppliers, err := s.repo.GetPositionSuppliers(companyID, productUUID)
	if err != nil {
		return nil, err
	}

	contacts, err := s.repo.GetSupplierContacts(companyID)
	if err != nil {
		return nil, err
	}

	mappings, _ := s.repo.GetSupplierNameMappings(companyID)

	isFallback := false
	if len(suppliers) == 0 {
		isFallback = true
		allSups, err := s.GetAllSuppliersWithContacts(companyID)
		if err == nil && len(allSups) > 0 {
			return &PositionSuppliersResult{
				IsFallback: true,
				Suppliers:  allSups,
			}, nil
		}
	}

	var list []SupplierInfo
	for _, sup := range suppliers {
		name := sup.Name
		if mappedName, exists := mappings[strings.ToUpper(sup.ExternalID)]; exists && mappedName != "" {
			name = mappedName
		}
		list = append(list, SupplierInfo{
			UUID:       sup.ExternalID,
			Name:       name,
			TgUsername: contacts[sup.ExternalID],
		})
	}

	return &PositionSuppliersResult{
		IsFallback: isFallback,
		Suppliers:  list,
	}, nil
}

