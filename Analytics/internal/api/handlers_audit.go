package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"analytics_service/internal/audit"
	"analytics_service/internal/engine"
)

// HandleAudit формирует экономический аудит переплат с плавающими когортами,
// рассчитывает переплаты к рыночной средней и к минимуму когорты, ценовой спред и AI-заключение
func (s *Server) HandleAudit(w http.ResponseWriter, r *http.Request) {
	restIDStr := r.URL.Query().Get("restaurant_id")
	if restIDStr == "" {
		http.Error(w, "Параметр restaurant_id обязателен", http.StatusBadRequest)
		return
	}
	restID, err := strconv.Atoi(restIDStr)
	if err != nil || restID <= 0 {
		http.Error(w, "Некорректный restaurant_id", http.StatusBadRequest)
		return
	}

	days := 60
	if dStr := r.URL.Query().Get("days"); dStr != "" {
		if d, err := strconv.Atoi(dStr); err == nil && d > 0 {
			days = d
		}
	}

	// Настройка плавающих когорт из параметров запроса
	cohortCfg := engine.DefaultCohortConfig()
	if sVal := r.URL.Query().Get("small_threshold"); sVal != "" {
		if st, err := strconv.ParseFloat(sVal, 64); err == nil && st > 0 {
			cohortCfg.SmallMaxThreshold = st
		}
	}
	if mVal := r.URL.Query().Get("medium_threshold"); mVal != "" {
		if mt, err := strconv.ParseFloat(mVal, 64); err == nil && mt > cohortCfg.SmallMaxThreshold {
			cohortCfg.MediumMaxThreshold = mt
		}
	}

	auditData, err := audit.ExecuteAudit(r.Context(), s.db, s.llmClient, restID, days, cohortCfg)
	if err != nil {
		http.Error(w, "Ошибка расчета аудита: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Применение Freemium маскирования (mode=demo по умолчанию, mode=full для полного отчета)
	mode := r.URL.Query().Get("mode")
	unblurCount := 3
	if ucStr := r.URL.Query().Get("unblur_count"); ucStr != "" {
		if uc, err := strconv.Atoi(ucStr); err == nil && uc >= 0 {
			unblurCount = uc
		}
	}
	engine.ApplyFreemiumMask(auditData, mode, unblurCount)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(auditData)
}
