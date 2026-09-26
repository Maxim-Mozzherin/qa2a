package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"qa2a/internal/middleware"
	"qa2a/internal/service"
	"qa2a/pkg/ratelimit"
	"strings"
)

// ============================================================================
// ПОРТАЛ ПОСТАВЩИКА (B2B MARKETPLACE SUPPLIER PORTAL)
// ============================================================================

func (h *Handler) GetSupplierOffersHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	supplierID, _ := req.Context().Value("supplier_id").(int)
	if supplierID == 0 {
		http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	offers, err := h.marketplaceService.GetSupplierOffers(supplierID)
	if err != nil {
		http.Error(w, `{"error": "Internal Server Error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(offers)
}

func extractToken(req *http.Request) string {
	token := strings.TrimSpace(req.Header.Get("X-Telegram-ID"))
	if token == "" {
		authHeader := strings.TrimSpace(req.Header.Get("Authorization"))
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}
	return token
}

func (h *Handler) SaveSupplierOfferHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	req.Body = http.MaxBytesReader(w, req.Body, 1<<20)
	var offer service.CreateOfferReq
	if err := json.NewDecoder(req.Body).Decode(&offer); err != nil {
		http.Error(w, `{"error": "Bad request"}`, http.StatusBadRequest)
		return
	}

	supplierID, _ := req.Context().Value("supplier_id").(int)
	if supplierID == 0 {
		http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	err := h.marketplaceService.SaveSupplierOffer(supplierID, offer)
	if err != nil {
		http.Error(w, `{"error": "Server error"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (h *Handler) RegisterSupplierHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Read auth token manually supporting both X-Telegram-ID and Authorization Bearer
	tokenStr := extractToken(req)
	tgID := middleware.VerifySignedTokenExported(tokenStr, h.botToken)
	if tgID == 0 {
		http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	clientIP := ratelimit.GetClientIP(req)
	rateKey := fmt.Sprintf("sup_reg_%d_%s", tgID, clientIP)
	if allowed, remaining := h.joinLimiter.Allow(rateKey); !allowed {
		respondError(w, http.StatusTooManyRequests, fmt.Sprintf("Слишком много запросов регистрации. Подождите %d сек.", int(remaining.Seconds())+1))
		return
	}

	req.Body = http.MaxBytesReader(w, req.Body, 1<<20)
	var reqBody struct {
		CompanyName string `json:"company_name"`
	}
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		http.Error(w, `{"error": "Invalid request body"}`, http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(reqBody.CompanyName) == "" {
		reqBody.CompanyName = "Demo Supplier" // fallback
	}

	_, err := h.marketplaceService.RegisterSupplier(tgID, reqBody.CompanyName)

	if err != nil {
		http.Error(w, `{"error": "Server error"}`, http.StatusInternalServerError)
		return
	}
	h.joinLimiter.RecordSuccess(rateKey)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "ok"}`))
}

func (h *Handler) JoinSupplierHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	tokenStr := extractToken(req)
	tgID := middleware.VerifySignedTokenExported(tokenStr, h.botToken)
	if tgID == 0 {
		http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	clientIP := ratelimit.GetClientIP(req)
	rateKey := fmt.Sprintf("sup_join_%d_%s", tgID, clientIP)
	if allowed, remaining := h.joinLimiter.Allow(rateKey); !allowed {
		respondError(w, http.StatusTooManyRequests, fmt.Sprintf("Слишком много попыток ввода кода. Подождите %d сек.", int(remaining.Seconds())+1))
		return
	}

	req.Body = http.MaxBytesReader(w, req.Body, 1<<20)
	var reqBody struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		http.Error(w, `{"error": "Invalid request body"}`, http.StatusBadRequest)
		return
	}

	err := h.marketplaceService.JoinSupplier(tgID, reqBody.Code)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.joinLimiter.RecordSuccess(rateKey)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "ok"}`))
}

func (h *Handler) GetSupplierMeHandler(w http.ResponseWriter, req *http.Request) {
	tokenStr := extractToken(req)
	tgID := middleware.VerifySignedTokenExported(tokenStr, h.botToken)
	if tgID == 0 {
		http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	db := h.marketplaceService.GetDb() // We will add GetDb() to marketplaceService
	var res struct {
		CompanyName string `json:"company_name"`
		InviteCode  string `json:"invite_code"`
	}
	err := db.QueryRow("SELECT s.company_name, COALESCE(s.invite_code, '') FROM marketplace_suppliers s JOIN marketplace_supplier_users u ON s.id = u.supplier_id WHERE u.tg_id = $1 LIMIT 1", tgID).Scan(&res.CompanyName, &res.InviteCode)
	if err != nil {
		http.Error(w, `{"error": "Not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func (h *Handler) DeleteSupplierOfferHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	supplierID, _ := req.Context().Value("supplier_id").(int)
	if supplierID == 0 {
		http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// Parse offer ID from query or body? Let just take from query
	idStr := req.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, `{"error": "Missing ID"}`, http.StatusBadRequest)
		return
	}

	var offerID int
	if _, err := fmt.Sscanf(idStr, "%d", &offerID); err != nil || offerID <= 0 {
		http.Error(w, `{"error": "Invalid ID"}`, http.StatusBadRequest)
		return
	}

	err := h.marketplaceService.DeleteSupplierOffer(supplierID, offerID)
	if err != nil {
		http.Error(w, `{"error": "Internal Server Error"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
