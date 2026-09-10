package handlers

import (
	"encoding/json"
	"net/http"
)

type AccountantAuthReq struct {
	TgID     int64  `json:"tg_id"`
	Username string `json:"username"`
	Code     string `json:"code"`
}

func (h *Handler) JoinAccountantHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var parsed AccountantAuthReq
	if err := json.NewDecoder(req.Body).Decode(&parsed); err != nil {
		http.Error(w, `{"error": "Bad request"}`, http.StatusBadRequest)
		return
	}

	// TODO: Verify the code mapping to a firm. For now, if code is "ACC-TEST", we link to firm ID 1.
	// We will implement dynamic firm code mapping later.
	firmID := 1
	if parsed.Code == "ACC-TEST" {
		// Ensure firm 1 exists
		h.accountantService.RegisterFirm("Тестовая Бухгалтерия") // Ignore error if exists
		err := h.accountantService.AddUserToFirm(firmID, parsed.TgID, parsed.Username, "admin")
		if err != nil {
			http.Error(w, `{"error": "Failed to add user"}`, http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "ok", "firm_id": 1}`))
		return
	}

	http.Error(w, `{"error": "Invalid code"}`, http.StatusUnauthorized)
}

func (h *Handler) GetAccountantCompaniesHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	firmID, _ := req.Context().Value("firm_id").(int)
	if firmID == 0 {
		http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	companies, err := h.accountantService.GetFirmCompanies(firmID)
	if err != nil {
		http.Error(w, `{"error": "Internal Error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(companies)
}
