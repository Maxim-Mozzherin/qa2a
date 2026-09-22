package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func handleSaveTemplateProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Только POST метод", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		StoreUUID string   `json:"store_uuid"`
		Name      string   `json:"name"`
		Items     []string `json:"items"`
		CompanyID int      `json:"company_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	if req.StoreUUID == "" || req.Name == "" || len(req.Items) == 0 || req.CompanyID <= 0 {
		http.Error(w, "Не все обязательные поля заполнены (store_uuid, name, items, company_id)", http.StatusBadRequest)
		return
	}

	user := GetAuthUser(r)
	if !checkAccountantAccessUser(user, req.CompanyID) {
		http.Error(w, "Доступ к сохранению бланков для данного заведения запрещен", http.StatusForbidden)
		return
	}

	payloadBytes, err := json.Marshal(req)
	if err != nil {
		http.Error(w, "Ошибка сериализации", http.StatusInternalServerError)
		return
	}

	qa2aURL := fmt.Sprintf("%s/api/external/inventory-templates", strings.TrimSuffix(qa2aBaseURL, "/"))
	qa2aReq, err := http.NewRequest("POST", qa2aURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		http.Error(w, "Ошибка формирования запроса", http.StatusInternalServerError)
		return
	}
	qa2aReq.Header.Set("Content-Type", "application/json")
	qa2aReq.Header.Set("Authorization", "Bearer "+externalApiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(qa2aReq)
	if err != nil {
		http.Error(w, "Сбой связи с сервером QA2A: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer res.Body.Close()

	respBody, _ := io.ReadAll(res.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.StatusCode)
	_, _ = w.Write(respBody)
}
