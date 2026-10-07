package main

import (
	"encoding/json"
	"log"
	"net/http"

	"iiko_parser/crypto"
	"iiko_parser/pkg/netutil"
)

func handleCompanies(w http.ResponseWriter, r *http.Request) {
	user := GetAuthUser(r)
	if user == nil {
		http.Error(w, "Неавторизованный доступ", http.StatusUnauthorized)
		return
	}

	query := "SELECT id, name FROM companies WHERE iiko_host != '' ORDER BY name ASC"
	var args []interface{}

	if user.Role != "superadmin" && user.Role != "global_accountant" {
		if user.AccountingFirmID != nil {
			query = "SELECT id, name FROM companies WHERE accounting_firm_id = $1 ORDER BY name ASC"
			args = append(args, *user.AccountingFirmID)
		} else {
			query = "SELECT id, name FROM companies WHERE 1 = 0"
		}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		log.Printf("❌ [Settings] Error querying companies: %v", err)
		http.Error(w, "Ошибка чтения списка заведений", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var list []Company
	for rows.Next() {
		var c Company
		if err := rows.Scan(&c.ID, &c.Name); err == nil {
			list = append(list, c)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

func handleCatalog(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CompanyID int `json:"company_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	user := GetAuthUser(r)
	if !checkAccountantAccessUser(user, req.CompanyID) {
		http.Error(w, "Доступ к данному заведению запрещен", http.StatusForbidden)
		return
	}

	var host, login, encryptedPass string
	err := db.QueryRow("SELECT iiko_host, iiko_api_login, iiko_api_password FROM companies WHERE id = $1", req.CompanyID).
		Scan(&host, &login, &encryptedPass)
	if err != nil {
		http.Error(w, "Заведение с указанным ID не найдено в базе", http.StatusNotFound)
		return
	}

	if host == "" {
		http.Error(w, "iiko не подключена. Настройте интеграцию в Telegram-боте.", http.StatusUnauthorized)
		return
	}

	if err := netutil.ValidateHost(host); err != nil {
		log.Printf("❌ [Settings] SSRF host validation error for host '%s': %v", host, err)
		http.Error(w, "Недопустимый адрес сервера iiko RMS (заблокировано политикой безопасности)", http.StatusBadRequest)
		return
	}

	password, err := crypto.Decrypt(encryptedPass, encryptionKey)
	if err != nil {
		log.Printf("❌ [Settings] Password decryption error: %v", err)
		http.Error(w, "Ошибка дешифрования пароля iiko RMS. Проверьте настройки в боте.", http.StatusInternalServerError)
		return
	}

	iikoToken, err := authIiko(host, login, password)
	if err != nil {
		log.Printf("❌ [Settings] iiko auth error for host '%s': %v", host, err)
		http.Error(w, "Ошибка авторизации на сервере iiko. Проверьте настройки подключения в боте.", http.StatusUnauthorized)
		return
	}

	catalog, err := fetchIikoCatalog(host, iikoToken)
	if err != nil {
		log.Printf("❌ [Settings] iiko catalog fetch error: %v", err)
		http.Error(w, "Ошибка загрузки каталога iiko", http.StatusInternalServerError)
		return
	}

	stores, _ := fetchIikoStores(host, iikoToken)
	suppliers, _ := fetchIikoSuppliers(host, iikoToken)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token":     iikoToken,
		"catalog":   catalog,
		"stores":    stores,
		"suppliers": suppliers,
	})
}

