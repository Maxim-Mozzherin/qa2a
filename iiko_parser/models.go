package main

import "time"

type Company struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Mapping struct {
	InternalUUID string
	InternalName string
	Multiplier   float64
}

type AiResponse struct {
	VendorName string   `json:"vendor_name"`
	DocNumber  string   `json:"doc_number"`
	DocDate    string   `json:"doc_date"`
	Consignee  string   `json:"consignee"`
	Shipper    string   `json:"shipper"`
	Items      []AiItem `json:"items"`
}

type PromptPreset struct {
	ID          int       `json:"id"`
	CompanyID   int       `json:"company_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Prompt      string    `json:"prompt"`
	IsDefault   bool      `json:"is_default"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type AiItem struct {
	Name          string  `json:"name"`
	CleanCategory string  `json:"clean_category"` // Базовая категория (для рынка)
	Brand         string  `json:"brand"`          // Производитель/Бренд (для рынка)
	Quantity      float64 `json:"quantity"`
	Price         float64 `json:"price"`
	Sum           float64 `json:"sum"`
	SumWithoutNds float64 `json:"sum_without_nds"`
	NdsPercent    float64 `json:"nds_percent"`
	AiMultiplier  float64 `json:"ai_multiplier"`
	AiTip         string  `json:"ai_tip"`
	DocNumber     string  `json:"doc_number"`
	DocDate       string  `json:"doc_date"`
	Amount        float64 `json:"amount"`
}

type XMLProducts struct {
	List []struct {
		ID          string `xml:"id"`
		Name        string `xml:"name"`
		ProductType string `xml:"productType"`
		Type        string `xml:"type"`
	} `xml:"productDto"`
}

type IikoProduct struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type IikoStore struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

type IikoSupplier struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

type UnlistedOperation struct {
	ID           int       `json:"id"`
	PositionName string    `json:"position_name"`
	Quantity     float64   `json:"quantity"`
	Unit         string    `json:"unit"`
	Comment      string    `json:"comment"`
	CreatedAt    time.Time `json:"created_at"`
	UserName     string    `json:"user_name"`
}

// Запись о закупке для локального дашборда бухгалтера
type PurchaseRecord struct {
	InvoiceDate     string  `json:"invoice_date"`
	InvoiceNum      string  `json:"invoice_number"`
	SupplierName    string  `json:"supplier_name"`
	ProductUUID     string  `json:"iiko_product_uuid"`
	IikoProductName string  `json:"iiko_product_name"`
	ProductName     string  `json:"product_name"`
	Quantity        float64 `json:"quantity"`
	Unit            string  `json:"unit"`
	Multiplier      float64 `json:"multiplier"`
	TotalSum        float64 `json:"total_sum"`
	PricePerUnit    float64 `json:"price_per_base_unit"`
}

// Запись для рыночной сводки (Суперадмин)
type MarketRecord struct {
	RestaurantName  string  `json:"restaurant_name"`
	InvoiceDate     string  `json:"invoice_date"`
	SupplierName    string  `json:"supplier_name"`
	IikoProductName string  `json:"iiko_product_name"`
	ProductName     string  `json:"product_name_in_invoice"`
	CleanCategory   string  `json:"clean_category"`
	Brand           string  `json:"brand"`
	PricePerUnit    float64 `json:"price_per_base_unit"`
}
