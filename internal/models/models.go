package models

import (
	"time"
)

// ============================================================================
// 1. ОРГАНИЗАЦИИ, ПОЛЬЗОВАТЕЛИ И РОЛЕВАЯ МОДЕЛЬ (RBAC)
// ============================================================================

// Company представляет юридическое лицо / заведение (ресторан, кафе, бар).
type Company struct {
	ID                   int       `json:"id" db:"id"`
	Name                 string    `json:"name" db:"name"`
	InviteCode           string    `json:"invite_code" db:"invite_code"`
	IikoHost             string    `json:"iiko_host,omitempty" db:"iiko_host"`
	IikoApiLogin         string    `json:"iiko_api_login,omitempty" db:"iiko_api_login"`
	IikoApiPassword      string    `json:"-" db:"iiko_api_password"` // Никогда не отдается в API в открытом виде
	IikoWriteoffAccount  string    `json:"iiko_writeoff_account,omitempty" db:"iiko_writeoff_account"`
	CreatedAt            time.Time `json:"created_at" db:"created_at"`
}

// User представляет пользователя Telegram WebApp.
type User struct {
	ID        int       `json:"id" db:"id"`
	TgID      int64     `json:"tg_id" db:"tg_id"`
	Username  string    `json:"username" db:"username"`
	FullName  string    `json:"full_name" db:"full_name"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// Membership отражает членство пользователя в конкретной компании и его права.
type Membership struct {
	UserID      int    `json:"user_id" db:"user_id"`
	CompanyID   int    `json:"company_id" db:"company_id"`
	CompanyName string `json:"company_name" db:"company_name"`
	UserName    string `json:"user_name,omitempty" db:"user_name"`
	Role        string `json:"role" db:"role"`                 // owner, admin, manager, user
	CustomTitle string `json:"custom_title" db:"custom_title"` // Напр: "Су-шеф", "Барменеджер"
}

// MemberInfo используется при отображении списка сотрудников в панели администратора.
type MemberInfo struct {
	UserID      int    `json:"user_id" db:"user_id"`
	CompanyID   int    `json:"company_id" db:"company_id"`
	Role        string `json:"role" db:"role"`
	CustomTitle string `json:"custom_title" db:"custom_title"`
	CompanyName string `json:"company_name" db:"company_name"`
	UserName    string `json:"user_name" db:"user_name"`
}

// ============================================================================
// 2. СКЛАДСКОЙ УЧЕТ И НОМЕНКЛАТУРА
// ============================================================================

// Location представляет физический или виртуальный склад (Бар, Кухня и т.д.).
type Location struct {
	ID         int    `json:"id" db:"id"`
	CompanyID  int    `json:"company_id" db:"company_id"`
	Name       string `json:"name" db:"name"`
	ExternalID string `json:"external_id" db:"external_id"` // UUID склада в iiko
}

// Position описывает товарную единицу, сырье или полуфабрикат.
type Position struct {
	ID         int    `json:"id" db:"id"`
	CompanyID  int    `json:"company_id" db:"company_id"`
	Name       string `json:"name" db:"name"`
	Unit       string `json:"unit" db:"unit"`
	Supplier   string `json:"supplier" db:"supplier"`
	ExternalID string `json:"external_id" db:"external_id"` // UUID товара в iiko
	Type       string `json:"type" db:"type"`               // GOODS, PREPARED, DISH, MODIFIER
	Conception string `json:"conception" db:"conception"`   // Иерархия категорий ("Бар / Сиропы")
}

// Balance отражает текущий количественный остаток позиции на локации.
type Balance struct {
	CompanyID    int     `json:"company_id" db:"company_id"`
	LocationID   int     `json:"location_id" db:"location_id"`
	PositionName string  `json:"position_name" db:"position_name"`
	Quantity     float64 `json:"quantity" db:"quantity"`
	Unit         string  `json:"unit" db:"unit"`
	AvgPrice     float64 `json:"avg_price,omitempty" db:"avg_price"`
}

// Operation фиксирует движение товара (списание, перемещение, приготовление полуфабриката).
type Operation struct {
	ID             int       `json:"id" db:"id"`
	CompanyID      int       `json:"company_id" db:"company_id"`
	LocationID     int       `json:"location_id" db:"location_id"`
	ToLocationID   int       `json:"to_location_id" db:"to_location_id"`
	UserID         int       `json:"user_id" db:"user_id"`
	UserName       string    `json:"user_name" db:"user_name"`
	Type           string    `json:"type" db:"type"` // writeoff, transfer_in/out, assembly_in/out
	PositionName   string    `json:"position_name" db:"position_name"`
	Quantity       float64   `json:"quantity" db:"quantity"`
	Unit           string    `json:"unit" db:"unit"`
	Status         string    `json:"status" db:"status"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	IsUnlisted     bool      `json:"is_unlisted" db:"is_unlisted"`
	Comment        string    `json:"comment" db:"comment"`
	AccountID      string    `json:"account_id" db:"account_id"`
	ExportedToIiko bool      `json:"exported_to_iiko" db:"exported_to_iiko"`
}

// WriteoffAccount представляет статью расходов (счет списания в iiko).
type WriteoffAccount struct {
	ID         int    `json:"id" db:"id"`
	CompanyID  int    `json:"company_id" db:"company_id"`
	Name       string `json:"name" db:"name"`
	ExternalID string `json:"external_id" db:"external_id"`
}

// ============================================================================
// 3. ЗАЯВКИ НА ЗАКУПКУ (PROCUREMENT)
// ============================================================================

// ProcurementItem представляет отдельную строчку в заявке на поставку.
type ProcurementItem struct {
	ID           int     `json:"id,omitempty" db:"id"`
	RequestID    int     `json:"request_id,omitempty" db:"request_id"`
	PositionName string  `json:"position_name" db:"position_name"`
	Quantity     float64 `json:"quantity" db:"quantity"`
	Unit         string  `json:"unit" db:"unit"`
	IsUnlisted   bool    `json:"is_unlisted" db:"is_unlisted"`
	Supplier     string  `json:"supplier" db:"supplier"` // UUID|Имя поставщика
}

// ProcurementRequest объединяет строчки заказа для согласования руководством.
type ProcurementRequest struct {
	ID         int               `json:"id" db:"id"`
	CompanyID  int               `json:"company_id" db:"company_id"`
	UserID     int               `json:"user_id" db:"user_id"`
	UserName   string            `json:"user_name" db:"user_name"`
	ApprovedBy *int              `json:"approved_by,omitempty" db:"approved_by"`
	Status     string            `json:"status" db:"status"` // pending, approved, rejected
	CreatedAt  time.Time         `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at" db:"updated_at"`
	Items      []ProcurementItem `json:"items"`
}

// ============================================================================
// 4. ИНВЕНТАРИЗАЦИЯ И ШАБЛОНЫ
// ============================================================================

// InventoryItem хранит данные пересчета по конкретному товару.
type InventoryItem struct {
	PositionName   string  `json:"position_name" db:"position_name"`
	ExternalID     string  `json:"external_id" db:"external_id"`
	ExpectedAmount float64 `json:"expected_amount" db:"expected_amount"`
	ActualAmount   float64 `json:"actual_amount" db:"actual_amount"`
}

// InventoryAct — акт инвентаризации остатков.
type InventoryAct struct {
	ID             int             `json:"id" db:"id"`
	CompanyID      int             `json:"company_id" db:"company_id"`
	LocationID     int             `json:"location_id" db:"location_id"`
	LocationName   string          `json:"location_name,omitempty" db:"location_name"`
	UserID         int             `json:"user_id" db:"user_id"`
	UserName       string          `json:"user_name,omitempty" db:"user_name"`
	Status         string          `json:"status" db:"status"` // in_progress, completed
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
	IikoDocumentID string          `json:"iiko_document_id" db:"iiko_document_id"`
	DocumentNumber string          `json:"document_number" db:"document_number"`
	Items          []InventoryItem `json:"items"`
}

// InventoryTemplate — шаблон (бланк) инвентаризации, сформированный бухгалтером.
type InventoryTemplate struct {
	ID         int       `json:"id" db:"id"`
	CompanyID  int       `json:"company_id" db:"company_id"`
	LocationID int       `json:"location_id" db:"location_id"`
	Name       string    `json:"name" db:"name"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

// InventoryTemplateItem — связь шаблона с позицией пересчета.
type InventoryTemplateItem struct {
	TemplateID   int    `json:"template_id" db:"template_id"`
	PositionName string `json:"position_name" db:"position_name"`
}

// ============================================================================
// 5. ИНТЕГРАЦИЯ IIKO RMS И ЭКСПОРТ ДАННЫХ
// ============================================================================

// IikoSettings содержит параметры подключения к REST API сервера iiko RMS.
type IikoSettings struct {
	CompanyID int    `json:"company_id" db:"id"`
	Host      string `json:"iiko_host" db:"iiko_host"`
	Login     string `json:"iiko_api_login" db:"iiko_api_login"`
	Password  string `json:"iiko_api_password" db:"iiko_api_password"`
}

// IikoDraftInventory описывает черновик бланка инвентаризации из iiko.
type IikoDraftInventory struct {
	ID             string `json:"id"`
	DocumentNumber string `json:"documentNumber"`
	DateIncoming   string `json:"dateIncoming"`
	StoreID        string `json:"storeId"`
	Status         string `json:"status"`
	Comment        string `json:"comment"`
	Items          []struct {
		ProductID string  `json:"productId"`
		Amount    float64 `json:"amount"`
	} `json:"items"`
}

// OlapRequest — структура запроса к OLAP-отчетам iiko RMS v2.
type OlapRequest struct {
	ReportType       string                 `json:"reportType"`
	GroupByRowFields []string               `json:"groupByRowFields"`
	AggregateFields  []string               `json:"aggregateFields"`
	Filters          map[string]interface{} `json:"filters"`
}

// OlapStockResponse — ответ отчета транзакций по остаткам из iiko RMS.
type OlapStockResponse struct {
	Data []struct {
		ProductID    string  `json:"Product.Id"`
		ProductName  string  `json:"Product.Name"`
		FinalBalance float64 `json:"FinalBalance.Amount"`
	} `json:"data"`
}

// ExportOperationDTO агрегирует операции для батч-выгрузки в iiko.
type ExportOperationDTO struct {
	OpIDs       string  `db:"op_ids"`
	ProductID   string  `db:"product_id"`
	StoreFromID string  `db:"store_from"`
	StoreToID   string  `db:"store_to"`
	TotalAmount float64 `db:"total_qty"`
	AccountID   string  `db:"account_id"`
	OpDate      string  `db:"op_date"`
}

// WriteoffCommentDTO используется для извлечения комментариев и авторства списаний.
type WriteoffCommentDTO struct {
	PositionName string `db:"position_name"`
	Comment      string `db:"comment"`
	Username     string `db:"username"`
}

// ============================================================================
// 6. МАППИНГИ И СВЯЗИ С ПОСТАВЩИКАМИ (ДЛЯ IIKO_PARSER И QA2A)
// ============================================================================

// SupplierContact — контакт ответственного поставщика в Telegram.
type SupplierContact struct {
	CompanyID    int       `json:"company_id" db:"company_id"`
	SupplierUUID string    `json:"supplier_uuid" db:"supplier_uuid"`
	TgUsername   string    `json:"tg_username" db:"tg_username"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// SupplierMapping — связка имени контрагента из накладной с UUID в iiko.
type SupplierMapping struct {
	CompanyID        int    `json:"company_id" db:"company_id"`
	VendorName       string `json:"vendor_name" db:"vendor_name"`
	IikoSupplierUUID string `json:"iiko_supplier_uuid" db:"iiko_supplier_uuid"`
}

// StoreMapping — связка грузополучателя из УПД со складом оприходования в iiko.
type StoreMapping struct {
	CompanyID     int    `json:"company_id" db:"company_id"`
	Consignee     string `json:"consignee" db:"consignee"`
	IikoStoreUUID string `json:"iiko_store_uuid" db:"iiko_store_uuid"`
}

// ProductMapping — сопоставление строки номенклатуры поставщика с позицией в iiko.
type ProductMapping struct {
	CompanyID       int       `json:"company_id" db:"company_id"`
	VendorName      string    `json:"vendor_name" db:"vendor_name"`
	VendorItemName  string    `json:"vendor_item_name" db:"vendor_item_name"`
	IikoProductUUID string    `json:"iiko_product_uuid" db:"iiko_product_uuid"`
	IikoProductName string    `json:"iiko_product_name" db:"iiko_product_name"`
	Multiplier      float64   `json:"multiplier" db:"multiplier"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

