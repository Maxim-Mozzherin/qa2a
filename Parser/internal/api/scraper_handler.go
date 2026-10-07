package api

import (
	"context"
	"encoding/json"
	"net/http"

	"leads_monster/internal/config"
	"leads_monster/internal/db"
	"leads_monster/internal/scraper"
)

type ScraperHandler struct {
	db  *db.DB
	cfg *config.Config
}

func NewScraperHandler(database *db.DB, cfg *config.Config) *ScraperHandler {
	return &ScraperHandler{
		db:  database,
		cfg: cfg,
	}
}

type StartScraperRequest struct {
	City     string  `json:"city"`
	CityID   string  `json:"city_id"`
	Query    string  `json:"query"`
	APIKey   string  `json:"api_key"`
	MaxPages int     `json:"max_pages"`
	MinBill  float64 `json:"min_bill"`
}

// HandleStart initiates the background 2GIS scraping worker
func (h *ScraperHandler) HandleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req StartScraperRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}

	opts := scraper.ScrapeOptions{
		City:     req.City,
		CityID:   req.CityID,
		Query:    req.Query,
		APIKey:   req.APIKey,
		MaxPages: req.MaxPages,
		MinBill:  req.MinBill,
	}

	// Use background context for scraper worker
	err := scraper.StartScraper(context.Background(), h.db, h.cfg, opts)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "started",
		"message": "Парсер 2ГИС запущен в фоновом режиме",
		"options": opts,
	})
}

// HandleStop terminates the running scraper
func (h *ScraperHandler) HandleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	scraper.StopScraper()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "stopped",
		"message": "Сигнал остановки отправлен",
	})
}

// HandleStatus returns active scraper progress and metrics
func (h *ScraperHandler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	status := scraper.GetStatus()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}
