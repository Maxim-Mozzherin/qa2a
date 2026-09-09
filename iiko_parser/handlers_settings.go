package main

import (
	"encoding/json"
	"iiko_parser/crypto"
	"net/http"
)

func handleCompanies(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, name FROM companies WHERE iiko_host != '' ORDER BY name ASC")
	if err != nil {
		http.Error(w, "Ошибка чтения списка заведений: "+err.Error(), http.StatusInternalServerError)
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	var host, login, encryptedPass string
	err := db.QueryRow("SELECT iiko_host, iiko_api_login, iiko_api_password FROM companies WHERE id = $1", req.CompanyID).
		Scan(&host, &login, &encryptedPass)
	if err != nil {
		http.Error(w, "Заведение с указанным ID не найдено в базе", http.StatusNotFound)
		return
	}

	password, err := crypto.Decrypt(encryptedPass, encryptionKey)
	if err != nil {
		http.Error(w, "Ошибка дешифрования пароля iiko RMS: "+err.Error(), http.StatusInternalServerError)
		return
	}

	token, err := authIiko(host, login, password)
	if err != nil {
		http.Error(w, "Ошибка авторизации на сервере iiko: "+err.Error(), http.StatusUnauthorized)
		return
	}

	catalog, err := fetchIikoCatalog(host, token)
	if err != nil {
		http.Error(w, "Ошибка загрузки каталога iiko: "+err.Error(), http.StatusInternalServerError)
		return
	}

	stores, _ := fetchIikoStores(host, token)
	suppliers, _ := fetchIikoSuppliers(host, token)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token":     token,
		"catalog":   catalog,
		"stores":    stores,
		"suppliers": suppliers,
	})
}
