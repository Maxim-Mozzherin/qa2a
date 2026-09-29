package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"qa2a/pkg/ratelimit"

	"github.com/gorilla/mux"
)

// ============================================================================
// B2B МАРКЕТПЛЕЙС: ОФФЕРЫ И АНАЛИТИКА
// ============================================================================

// GetMarketplaceOffersHandler возвращает релевантные спецпредложения от поставщиков для текущего заведения.
func (h *Handler) GetMarketplaceOffersHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	companyID := h.getCompanyID(req)
	if companyID == 0 {
		respondError(w, http.StatusForbidden, "Доступ к заведению запрещен или отсутствует")
		return
	}

	offers, err := h.marketplaceService.GetOffersForCompany(companyID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка загрузки спецпредложений")
		return
	}

	respondJSON(w, http.StatusOK, offers)
}

// RecordOfferViewsHandler инкрементирует счетчик показов (views_count) для списка ID офферов.
// Ограничен лимитом 100 ID за один запрос и rate-limiter.
func (h *Handler) RecordOfferViewsHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	userID := h.getUserID(req)
	clientIP := ratelimit.GetClientIP(req)
	rateKey := fmt.Sprintf("views_%d_%s", userID, clientIP)
	if h.actionLimiter != nil {
		if allowed, _ := h.actionLimiter.Allow(rateKey); !allowed {
			respondError(w, http.StatusTooManyRequests, "Слишком много запросов")
			return
		}
	}

	req.Body = http.MaxBytesReader(w, req.Body, 1<<20)
	var ids []int
	if err := json.NewDecoder(req.Body).Decode(&ids); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат массива ID")
		return
	}
	if len(ids) > 100 {
		ids = ids[:100]
	}
	_ = h.marketplaceService.RecordOfferViews(ids)
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RecordOfferClickHandler инкрементирует счетчик кликов (переходов) по конкретному спецпредложению.
func (h *Handler) RecordOfferClickHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	userID := h.getUserID(req)
	clientIP := ratelimit.GetClientIP(req)
	rateKey := fmt.Sprintf("click_%d_%s", userID, clientIP)
	if h.actionLimiter != nil {
		if allowed, _ := h.actionLimiter.Allow(rateKey); !allowed {
			respondError(w, http.StatusTooManyRequests, "Слишком много запросов")
			return
		}
	}

	idStr := mux.Vars(req)["id"]
	id, _ := strconv.Atoi(idStr)
	if id <= 0 {
		respondError(w, http.StatusBadRequest, "Некорректный ID предложения")
		return
	}
	_ = h.marketplaceService.RecordOfferClick(id)
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
