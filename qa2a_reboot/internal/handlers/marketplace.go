package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

func (h *Handler) GetMarketplaceOffersHandler(w http.ResponseWriter, req *http.Request) {
	// Handle CORS preflight
	if req.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	companyID := h.getCompanyID(req)
	if companyID == 0 {
		http.Error(w, `{"error": "Доступ к заведению запрещен или отсутствует"}`, http.StatusForbidden)
		return
	}

	offers, err := h.marketplaceService.GetOffersForCompany(companyID)
	if err != nil {
		http.Error(w, `{"error": "Internal Server Error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(offers)
}

func (h *Handler) RecordOfferViewsHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var ids []int
	if err := json.NewDecoder(req.Body).Decode(&ids); err != nil {
		http.Error(w, "Invalid body", http.StatusBadRequest)
		return
	}
	if len(ids) > 100 {
		ids = ids[:100]
	}
	h.marketplaceService.RecordOfferViews(ids)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) RecordOfferClickHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	idStr := mux.Vars(req)["id"]
	id, _ := strconv.Atoi(idStr)
	if id > 0 {
		h.marketplaceService.RecordOfferClick(id)
	}
	w.WriteHeader(http.StatusOK)
}
