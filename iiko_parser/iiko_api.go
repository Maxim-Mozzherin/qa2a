package main

import (
	"encoding/xml"
	"fmt"
	"iiko_parser/crypto"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func authIiko(host, login, pass string) (string, error) {
	passHash := crypto.HashPasswordSHA1(pass)
	authURL := fmt.Sprintf("%s/resto/api/auth?login=%s&pass=%s", strings.TrimSuffix(host, "/"), url.QueryEscape(login), url.QueryEscape(passHash))

	resp, err := iikoHTTPClient.Get(authURL)
	if err != nil {
		return "", fmt.Errorf("ошибка соединения с сервером iiko: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ошибка авторизации (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return strings.TrimSpace(string(body)), nil
}

func fetchIikoCatalog(host, token string) ([]IikoProduct, error) {
	url := fmt.Sprintf("%s/resto/api/products?key=%s", strings.TrimSuffix(host, "/"), token)
	resp, err := iikoHTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("ошибка запроса товаров: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iiko вернул статус %d при загрузке каталога", resp.StatusCode)
	}

	var data XMLProducts
	if err := xml.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("ошибка разбора XML каталога: %w", err)
	}

	var goods, prepared, dishes, modifiers, others []IikoProduct

	for _, p := range data.List {
		pType := strings.ToUpper(strings.TrimSpace(p.ProductType))
		if pType == "" {
			pType = strings.ToUpper(strings.TrimSpace(p.Type))
		}

		prod := IikoProduct{
			UUID: p.ID,
			Name: p.Name,
			Type: pType,
		}

		switch pType {
		case "GOODS":
			goods = append(goods, prod)
		case "PREPARED":
			prepared = append(prepared, prod)
		case "DISH":
			dishes = append(dishes, prod)
		case "MODIFIER":
			modifiers = append(modifiers, prod)
		default:
			others = append(others, prod)
		}
	}

	var sortedProducts []IikoProduct
	sortedProducts = append(sortedProducts, goods...)
	sortedProducts = append(sortedProducts, prepared...)
	sortedProducts = append(sortedProducts, dishes...)
	sortedProducts = append(sortedProducts, modifiers...)
	sortedProducts = append(sortedProducts, others...)

	return sortedProducts, nil
}

func fetchIikoStores(host, token string) ([]IikoStore, error) {
	url := fmt.Sprintf("%s/resto/api/corporation/stores?key=%s", strings.TrimSuffix(host, "/"), token)
	resp, err := iikoHTTPClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("статус %d при загрузке складов", resp.StatusCode)
	}

	var stores []IikoStore
	decoder := xml.NewDecoder(resp.Body)
	for {
		t, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		switch se := t.(type) {
		case xml.StartElement:
			if se.Name.Local == "corporateItemDto" {
				var item struct {
					ID   string `xml:"id"`
					Name string `xml:"name"`
				}
				if err := decoder.DecodeElement(&item, &se); err == nil && item.ID != "" {
					stores = append(stores, IikoStore{UUID: item.ID, Name: item.Name})
				}
			}
		}
	}
	return stores, nil
}

func fetchIikoSuppliers(host, token string) ([]IikoSupplier, error) {
	url := fmt.Sprintf("%s/resto/api/suppliers?key=%s", strings.TrimSuffix(host, "/"), token)
	resp, err := iikoHTTPClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("статус %d при запросе поставщиков", resp.StatusCode)
	}

	var data struct {
		XMLName xml.Name `xml:"employees"`
		List    []struct {
			ID       string `xml:"id"`
			Name     string `xml:"name"`
			Supplier bool   `xml:"supplier"`
			Deleted  bool   `xml:"deleted"`
		} `xml:"employee"`
	}

	if err := xml.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("ошибка парсинга поставщиков: %w", err)
	}

	var suppliers []IikoSupplier
	for _, emp := range data.List {
		if emp.Supplier && !emp.Deleted {
			suppliers = append(suppliers, IikoSupplier{
				UUID: emp.ID,
				Name: emp.Name,
			})
		}
	}
	return suppliers, nil
}

// fetchIikoIncomingInvoices выгружает список приходных накладных из iiko RMS за интервал дат.
// Если supplierID пустой, выгружаются накладные по всем поставщикам без исключения.
func fetchIikoIncomingInvoices(host, token, from, to, supplierID string) (*IikoExportedInvoicesXML, error) {
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
	resp, err := iikoHTTPClient.Get(urlStr)
	if err != nil {
		return nil, fmt.Errorf("ошибка соединения с сервером iiko: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("iiko вернул статус %d: %s", resp.StatusCode, string(body))
	}

	var result IikoExportedInvoicesXML
	// Ограничиваем чтение 100 МБ для защиты от неконтролируемого расхода оперативной памяти
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 100<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("ошибка парсинга XML накладных iiko: %w", err)
	}

	return &result, nil
}
