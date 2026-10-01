package iiko

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"analytics_service/internal/crypto"
	"analytics_service/internal/netutil"
)

type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout:   45 * time.Second,
			Transport: netutil.NewSafeHTTPTransport(15 * time.Second),
		},
	}
}

type ExportedInvoicesXML struct {
	XMLName   xml.Name           `xml:"incomingInvoiceDtoes"`
	Documents []ExportedDocument `xml:"document"`
}

type ExportedDocument struct {
	ID                     string                 `xml:"id"`
	TransportInvoiceNumber string                 `xml:"transportInvoiceNumber"`
	IncomingDocumentNumber string                 `xml:"incomingDocumentNumber"`
	DocumentNumber         string                 `xml:"documentNumber"`
	IncomingDate           string                 `xml:"incomingDate"`
	DateIncoming           string                 `xml:"dateIncoming"`
	DueDate                string                 `xml:"dueDate"`
	SupplierUUID           string                 `xml:"supplier"`
	DefaultStoreUUID       string                 `xml:"defaultStore"`
	Status                 string                 `xml:"status"`
	Comment                string                 `xml:"comment"`
	ConceptionUUID         string                 `xml:"conception"`
	ConceptionCode         string                 `xml:"conceptionCode"`
	Items                  []ExportedDocumentItem `xml:"items>item"`
}

type ExportedDocumentItem struct {
	Num                    int     `xml:"num"`
	ProductUUID            string  `xml:"product"`
	ProductArticle         string  `xml:"productArticle"`
	Code                   string  `xml:"code"`
	Amount                 float64 `xml:"amount"`
	ActualAmount           float64 `xml:"actualAmount"`
	AmountUnitUUID         string  `xml:"amountUnit"`
	Price                  float64 `xml:"price"`
	PriceWithoutVat        float64 `xml:"priceWithoutVat"`
	Sum                    float64 `xml:"sum"`
	VatPercent             float64 `xml:"vatPercent"`
	VatSum                 float64 `xml:"vatSum"`
	DiscountSum            float64 `xml:"discountSum"`
	StoreUUID              string  `xml:"store"`
	SupplierProductUUID    string  `xml:"supplierProduct"`
	SupplierProductArticle string  `xml:"supplierProductArticle"`
	IsAdditionalExpense    bool    `xml:"isAdditionalExpense"`
}

type SimpleItem struct {
	UUID string
	Name string
}

func (c *Client) Auth(host, login, pass string) (string, error) {
	cleanHost := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(host), "/"), "/resto")
	passHash := crypto.HashPasswordSHA1(pass)
	authURL := fmt.Sprintf("%s/resto/api/auth?login=%s&pass=%s", cleanHost, url.QueryEscape(login), url.QueryEscape(passHash))

	resp, err := c.httpClient.Get(authURL)
	if err != nil {
		return "", fmt.Errorf("ошибка соединения с iiko: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ошибка авторизации iiko (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return strings.TrimSpace(string(body)), nil
}

func (c *Client) Logout(host, token string) error {
	if token == "" {
		return nil
	}
	cleanHost := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(host), "/"), "/resto")
	logoutURL := fmt.Sprintf("%s/resto/api/logout?key=%s", cleanHost, url.QueryEscape(token))
	resp, err := c.httpClient.Get(logoutURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (c *Client) FetchIncomingInvoices(host, token, from, to, supplierID string) (*ExportedInvoicesXML, error) {
	cleanHost := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(host), "/"), "/resto")
	params := url.Values{}
	params.Set("key", token)
	if from != "" {
		params.Set("from", from)
	}
	if to != "" {
		params.Set("to", to)
	}
	if supplierID != "" {
		params.Set("supplierId", supplierID)
	}

	urlStr := fmt.Sprintf("%s/resto/api/documents/export/incomingInvoice?%s", cleanHost, params.Encode())
	resp, err := c.httpClient.Get(urlStr)
	if err != nil {
		return nil, fmt.Errorf("ошибка запроса выгрузки: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("iiko вернул статус %d: %s", resp.StatusCode, string(body))
	}

	var result ExportedInvoicesXML
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 100<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("ошибка парсинга XML: %w", err)
	}
	return &result, nil
}

func (c *Client) FetchSuppliers(host, token string) (map[string]string, error) {
	cleanHost := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(host), "/"), "/resto")
	urlStr := fmt.Sprintf("%s/resto/api/suppliers?key=%s", cleanHost, token)
	resp, err := c.httpClient.Get(urlStr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data struct {
		List []struct {
			ID       string `xml:"id"`
			Name     string `xml:"name"`
			Supplier bool   `xml:"supplier"`
			Deleted  bool   `xml:"deleted"`
		} `xml:"employee"`
	}
	_ = xml.NewDecoder(resp.Body).Decode(&data)

	res := make(map[string]string)
	for _, emp := range data.List {
		if emp.Supplier && !emp.Deleted {
			res[emp.ID] = emp.Name
		}
	}
	return res, nil
}

func (c *Client) FetchStores(host, token string) (map[string]string, error) {
	cleanHost := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(host), "/"), "/resto")
	urlStr := fmt.Sprintf("%s/resto/api/corporation/stores?key=%s", cleanHost, token)
	resp, err := c.httpClient.Get(urlStr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	res := make(map[string]string)
	decoder := xml.NewDecoder(resp.Body)
	for {
		t, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		if se, ok := t.(xml.StartElement); ok && se.Name.Local == "corporateItemDto" {
			var item struct {
				ID   string `xml:"id"`
				Name string `xml:"name"`
			}
			if err := decoder.DecodeElement(&item, &se); err == nil && item.ID != "" {
				res[item.ID] = item.Name
			}
		}
	}
	return res, nil
}

func (c *Client) FetchCatalog(host, token string) (map[string]string, error) {
	cleanHost := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(host), "/"), "/resto")
	urlStr := fmt.Sprintf("%s/resto/api/products?key=%s", cleanHost, token)
	resp, err := c.httpClient.Get(urlStr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data struct {
		List []struct {
			ID   string `xml:"id"`
			Name string `xml:"name"`
		} `xml:"productDto"`
	}
	_ = xml.NewDecoder(resp.Body).Decode(&data)

	res := make(map[string]string)
	for _, p := range data.List {
		if p.ID != "" && p.Name != "" {
			res[p.ID] = p.Name
		}
	}
	return res, nil
}
