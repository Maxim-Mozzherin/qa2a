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

// handleTriggerIikoExport инициирует экстренную ручную выгрузку списаний заведения в iiko RMS
func handleTriggerIikoExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Только POST метод", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CompanyID int `json:"company_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CompanyID <= 0 {
		http.Error(w, "Требуется корректный company_id", http.StatusBadRequest)
		return
	}

	user := GetAuthUser(r)
	if !checkAccountantAccessUser(user, req.CompanyID) {
		http.Error(w, "Доступ к выгрузке для данного заведения запрещен", http.StatusForbidden)
		return
	}

	targetQA2A := qa2aBaseURL
	if targetQA2A == "" {
		targetQA2A = "http://127.0.0.1:8082"
	}
	qa2aURL := fmt.Sprintf("%s/api/external/export-iiko", strings.TrimSuffix(targetQA2A, "/"))

	payloadBytes, _ := json.Marshal(map[string]int{"company_id": req.CompanyID})
	qa2aReq, err := http.NewRequest("POST", qa2aURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		http.Error(w, "Ошибка формирования запроса к бэкенду: "+err.Error(), http.StatusInternalServerError)
		return
	}
	qa2aReq.Header.Set("Content-Type", "application/json")
	qa2aReq.Header.Set("Authorization", "Bearer "+externalApiKey)

	client := &http.Client{Timeout: 60 * time.Second}
	res, err := client.Do(qa2aReq)
	if err != nil {
		http.Error(w, "Сбой связи с сервером QA2A: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer res.Body.Close()

	bodyBytes, _ := io.ReadAll(res.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.StatusCode)
	_, _ = w.Write(bodyBytes)
}
