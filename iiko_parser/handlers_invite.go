
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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
	if req.Name == "" {
		req.Name = "Новое заведение"
	}

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

	b := make([]byte, 3)
	rand.Read(b)
	inviteCode := strings.ToUpper(hex.EncodeToString(b))

	// Insert shell company
	_, err := db.Exec("INSERT INTO companies (name, invite_code, accounting_firm_id) VALUES ($1, $2, $3)", req.Name, inviteCode, firmID)
	if err != nil {
		http.Error(w, "Ошибка БД", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"invite_code": inviteCode,
	})
}
