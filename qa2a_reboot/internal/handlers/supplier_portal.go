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

// GetSupplierOffersHandler возвращает список торговых предложений (офферов) текущего поставщика.
func (h *Handler) GetSupplierOffersHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	supplierID := middleware.GetSupplierID(req.Context())
	if supplierID == 0 {
		respondError(w, http.StatusUnauthorized, "Неавторизованный доступ к кабинету поставщика")
		return
	}

	offers, err := h.marketplaceService.GetSupplierOffers(supplierID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка получения списка офферов")
		return
	}

	respondJSON(w, http.StatusOK, offers)
}

// extractToken извлекает Bearer или X-Telegram-ID токен из заголовков HTTP-запроса.
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

// SaveSupplierOfferHandler создает или обновляет торговое предложение поставщика.
func (h *Handler) SaveSupplierOfferHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	req.Body = http.MaxBytesReader(w, req.Body, 1<<20)
	var offer service.CreateOfferReq
	if err := json.NewDecoder(req.Body).Decode(&offer); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат данных оффера")
		return
	}

	supplierID := middleware.GetSupplierID(req.Context())
	if supplierID == 0 {
		respondError(w, http.StatusUnauthorized, "Неавторизованный доступ к кабинету поставщика")
		return
	}

	err := h.marketplaceService.SaveSupplierOffer(supplierID, offer)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) authenticateSupplierTgUser(req *http.Request) (int64, error) {
	tokenStr := extractToken(req)
	tgID, tokenVersion := middleware.VerifySignedTokenWithVersion(tokenStr, h.botToken)
	if tgID <= 0 {
		return 0, fmt.Errorf("Unauthorized")
	}
	if h.inventoryService != nil && h.inventoryService.GetRepo() != nil {
		user, err := h.inventoryService.GetRepo().GetUserByTgID(tgID)
		if err != nil || user == nil {
			return 0, fmt.Errorf("User not found")
		}
		if user.TokenVersion != tokenVersion {
			return 0, fmt.Errorf("Session invalidated")
		}
	}
	return tgID, nil
}

func (h *Handler) RegisterSupplierHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	tgID, err := h.authenticateSupplierTgUser(req)
	if err != nil {
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

	_, err = h.marketplaceService.RegisterSupplier(tgID, reqBody.CompanyName)

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

	tgID, err := h.authenticateSupplierTgUser(req)
	if err != nil {
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

	err = h.marketplaceService.JoinSupplier(tgID, reqBody.Code)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.joinLimiter.RecordSuccess(rateKey)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "ok"}`))
}

// GetSupplierMeHandler возвращает профиль текущего поставщика (название компании и код приглашения).
func (h *Handler) GetSupplierMeHandler(w http.ResponseWriter, req *http.Request) {
	tgID, err := h.authenticateSupplierTgUser(req)
	if err != nil {
		respondError(w, http.StatusUnauthorized, "Неавторизованный запрос")
		return
	}

	db := h.marketplaceService.GetDb()
	var res struct {
		CompanyName string `json:"company_name"`
		InviteCode  string `json:"invite_code"`
	}
	err = db.QueryRow("SELECT s.company_name, COALESCE(s.invite_code, '') FROM marketplace_suppliers s JOIN marketplace_supplier_users u ON s.id = u.supplier_id WHERE u.tg_id = $1 LIMIT 1", tgID).Scan(&res.CompanyName, &res.InviteCode)
	if err != nil {
		respondError(w, http.StatusNotFound, "Поставщик не найден")
		return
	}

	respondJSON(w, http.StatusOK, res)
}

// DeleteSupplierOfferHandler удаляет торговое предложение поставщика.
func (h *Handler) DeleteSupplierOfferHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	supplierID := middleware.GetSupplierID(req.Context())
	if supplierID == 0 {
		respondError(w, http.StatusUnauthorized, "Неавторизованный доступ к кабинету поставщика")
		return
	}

	idStr := req.URL.Query().Get("id")
	if idStr == "" {
		respondError(w, http.StatusBadRequest, "Не указан ID предложения (?id=)")
		return
	}

	var offerID int
	if _, err := fmt.Sscanf(idStr, "%d", &offerID); err != nil || offerID <= 0 {
		respondError(w, http.StatusBadRequest, "Некорректный ID предложения")
		return
	}

	err := h.marketplaceService.DeleteSupplierOffer(supplierID, offerID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка удаления оффера")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
