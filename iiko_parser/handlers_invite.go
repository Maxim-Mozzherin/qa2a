
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func handleGenerateInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Только POST метод", http.StatusMethodNotAllowed)
		return
	}

	user := GetAuthUser(r)
	if user == nil {
		http.Error(w, "Неавторизованный доступ", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name             string `json:"name"`
		AccountingFirmID *int   `json:"accounting_firm_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if strings.TrimSpace(req.Name) == "" {
		req.Name = "Новое заведение"
	}
	req.Name = strings.TrimSpace(req.Name)

	var firmID *int = user.AccountingFirmID
	if user.Role == "superadmin" || user.Role == "global_accountant" {
		if req.AccountingFirmID != nil && *req.AccountingFirmID > 0 {
			firmID = req.AccountingFirmID
		} else if qFirm := r.URL.Query().Get("accounting_firm_id"); qFirm != "" {
			var fid int
			if _, err := fmt.Sscanf(qFirm, "%d", &fid); err == nil && fid > 0 {
				firmID = &fid
			} else {
				firmID = nil
			}
		} else {
			firmID = nil
		}
	}

	// 16 bytes = 128-bit cryptographic entropy
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		http.Error(w, "Ошибка генерации инвайт-кода", http.StatusInternalServerError)
		return
	}
	inviteCode := strings.ToUpper(hex.EncodeToString(b))
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	// Вставляем инвайт в таблицу company_invites для безопасной атомарной активации
	_, err := db.Exec(`
		INSERT INTO company_invites (code, name, accounting_firm_id, expires_at, is_used) 
		VALUES ($1, $2, $3, $4, FALSE)
	`, inviteCode, req.Name, firmID, expiresAt)
	if err != nil {
		http.Error(w, "Ошибка БД: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"invite_code": inviteCode,
		"expires_at":  expiresAt.Format(time.RFC3339),
	})
}
