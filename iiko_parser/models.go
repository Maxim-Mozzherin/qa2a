package main

import (
	"encoding/xml"
	"time"
)

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
	VendorName         string   `json:"vendor_name"`
	VendorINN          string   `json:"vendor_inn"`
	DocNumber          string   `json:"doc_number"`
	DocDate            string   `json:"doc_date"`
	Consignee          string   `json:"consignee"`
	ConsigneeINN       string   `json:"consignee_inn"`
	Shipper            string   `json:"shipper"`
	DocPrintedTotalSum float64  `json:"doc_printed_total_sum"`
	Items              []AiItem `json:"items"`
	UsedModel          string   `json:"used_model"`
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
	OriginalPrefix string  `json:"original_prefix"` // Микро-якорь для защиты от галлюцинаций (первые 1-2 слова)
	Num            int     `json:"num"` // AI visual anchor
	Name           string  `json:"name"`
	CleanCategory string  `json:"clean_category"` // Базовая категория (для рынка)
	Brand         string  `json:"brand"`          // Производитель/Бренд (для рынка)
	Quantity      float64 `json:"quantity"`
	Unit          string  `json:"unit"`
	BaseUnit      string  `json:"base_unit"`
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
	Unit            string  `json:"unit"`
}

// Структуры выгрузки приходных накладных из iiko RMS (/resto/api/documents/export/incomingInvoice)
type IikoExportedInvoicesXML struct {
	XMLName   xml.Name               `xml:"incomingInvoiceDtoes"`
	Documents []IikoExportedDocument `xml:"document"`
}

type IikoExportedDocument struct {
	ID                     string                     `xml:"id" json:"id"`
	TransportInvoiceNumber string                     `xml:"transportInvoiceNumber" json:"transport_invoice_number,omitempty"`
	IncomingDocumentNumber string                     `xml:"incomingDocumentNumber" json:"incoming_document_number"`
	DocumentNumber         string                     `xml:"documentNumber" json:"document_number"`
	IncomingDate           string                     `xml:"incomingDate" json:"incoming_date"`
	DateIncoming           string                     `xml:"dateIncoming" json:"date_incoming"`
	DueDate                string                     `xml:"dueDate" json:"due_date,omitempty"`
	SupplierUUID           string                     `xml:"supplier" json:"supplier_uuid"`
	SupplierName           string                     `xml:"-" json:"supplier_name"`
	DefaultStoreUUID       string                     `xml:"defaultStore" json:"default_store_uuid"`
	DefaultStoreName       string                     `xml:"-" json:"default_store_name"`
	Status                 string                     `xml:"status" json:"status"`
	Comment                string                     `xml:"comment" json:"comment"`
	ConceptionUUID         string                     `xml:"conception" json:"conception_uuid,omitempty"`
	ConceptionCode         string                     `xml:"conceptionCode" json:"conception_code,omitempty"`
	DistributionAlgorithm  string                     `xml:"distributionAlgorithm" json:"distribution_algorithm,omitempty"`
	Items                  []IikoExportedDocumentItem `xml:"items>item" json:"items"`
	TotalSum               float64                    `xml:"-" json:"total_sum"`
	TotalVatSum            float64                    `xml:"-" json:"total_vat_sum"`
}

type IikoExportedDocumentItem struct {
	Num                 int     `xml:"num" json:"num"`
	ProductUUID         string  `xml:"product" json:"product_uuid"`
	ProductName         string  `xml:"-" json:"product_name"`
	ProductArticle      string  `xml:"productArticle" json:"product_article"`
	Code                string  `xml:"code" json:"code"`
	Amount              float64 `xml:"amount" json:"amount"`
	ActualAmount        float64 `xml:"actualAmount" json:"actual_amount"`
	AmountUnitUUID      string  `xml:"amountUnit" json:"amount_unit_uuid"`
	Price               float64 `xml:"price" json:"price"`
	PriceWithoutVat     float64 `xml:"priceWithoutVat" json:"price_without_vat"`
	Sum                 float64 `xml:"sum" json:"sum"`
	VatPercent          float64 `xml:"vatPercent" json:"vat_percent"`
	VatSum              float64 `xml:"vatSum" json:"vat_sum"`
	DiscountSum         float64 `xml:"discountSum" json:"discount_sum"`
	StoreUUID           string  `xml:"store" json:"store_uuid"`
	StoreName           string  `xml:"-" json:"store_name"`
	IsAdditionalExpense bool    `xml:"isAdditionalExpense" json:"is_additional_expense"`
}

type IikoInvoicesExportResponse struct {
	Status        string                 `json:"status"`
	TotalInvoices int                    `json:"total_invoices"`
	TotalItems    int                    `json:"total_items"`
	TotalSum      float64                `json:"total_sum"`
	FromDate      string                 `json:"from_date"`
	ToDate        string                 `json:"to_date"`
	SyncedToDB    int                    `json:"synced_to_db,omitempty"`
	Documents     []IikoExportedDocument `json:"documents"`
}
