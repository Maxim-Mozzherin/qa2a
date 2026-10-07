package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/xuri/excelize/v2"
	"leads_monster/internal/db"
)

type LeadsHandler struct {
	db *db.DB
}

func NewLeadsHandler(database *db.DB) *LeadsHandler {
	return &LeadsHandler{db: database}
}

type Lead struct {
	ID            int            `json:"id"`
	TwoGisID      string         `json:"two_gis_id"`
	Name          string         `json:"name"`
	LegalName     string         `json:"legal_name"`
	LegalType     string         `json:"legal_type"`
	Address       string         `json:"address"`
	City          string         `json:"city"`
	Rubrics       pq.StringArray `json:"rubrics"`
	AvgBillRaw    string         `json:"avg_bill_raw"`
	AvgBillVal    float64        `json:"avg_bill_val"`
	Phones        pq.StringArray `json:"phones"`
	Website       string         `json:"website"`
	VkURL         string         `json:"vk_url"`
	TgURL         string         `json:"tg_url"`
	Rating        float64        `json:"rating"`
	ReviewsCount  int            `json:"reviews_count"`
	TwoGisURL     string         `json:"two_gis_url"`
	RusprofileURL string         `json:"rusprofile_url"`
	Status        string         `json:"status"`
	Priority      string         `json:"priority"`
	Notes         string         `json:"notes"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type FilterParams struct {
	Search      string
	City        string
	BillRange   string
	MinBill     float64
	MaxBill     float64
	LegalOnly   bool
	LegalType   string
	ReviewsMin  int
	Rubrics     []string
	Status      string
	Priority    string
	SortBy      string
	SortOrder   string
	Page        int
	PageSize    int
}

type DashboardStats struct {
	TotalCount       int `json:"total_count"`
	EnrichedCount    int `json:"enriched_count"`
	HighTicketCount  int `json:"high_ticket_count"`
	InPipelineCount  int `json:"in_pipeline_count"`
}

// HandleGetLeads handles GET /api/leads with advanced filtering, pagination, and KPI stats
func (h *LeadsHandler) HandleGetLeads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	params := parseFilterParams(r)
	leads, total, err := h.queryLeads(params)
	if err != nil {
		log.Printf("⚠️ [API] Ошибка запроса лидов: %v", err)
		http.Error(w, fmt.Sprintf(`{"error":"Query failed: %v"}`, err), http.StatusInternalServerError)
		return
	}

	stats, err := h.getDashboardStats()
	if err != nil {
		log.Printf("⚠️ [API] Ошибка получения статистики: %v", err)
	}

	totalPages := 0
	if params.PageSize > 0 {
		totalPages = (total + params.PageSize - 1) / params.PageSize
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "success",
		"leads":       leads,
		"total":       total,
		"page":        params.Page,
		"page_size":   params.PageSize,
		"total_pages": totalPages,
		"stats":       stats,
	})
}

// HandleUpdateLead handles PATCH /api/leads/:id
func (h *LeadsHandler) HandleUpdateLead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Extract ID from path e.g. /api/leads/123 or /api/leads/123/status
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		http.Error(w, `{"error":"ID лида не указан"}`, http.StatusBadRequest)
		return
	}
	id, err := strconv.Atoi(parts[2])
	if err != nil || id <= 0 {
		http.Error(w, `{"error":"Некорректный ID лида"}`, http.StatusBadRequest)
		return
	}

	var req struct {
		Status   *string `json:"status"`
		Priority *string `json:"priority"`
		Notes    *string `json:"notes"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Неверный формат JSON"}`, http.StatusBadRequest)
		return
	}

	setClauses := []string{"updated_at = NOW()"}
	var args []interface{}
	argIdx := 1

	if req.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *req.Status)
		argIdx++
	}
	if req.Priority != nil {
		setClauses = append(setClauses, fmt.Sprintf("priority = $%d", argIdx))
		args = append(args, *req.Priority)
		argIdx++
	}
	if req.Notes != nil {
		setClauses = append(setClauses, fmt.Sprintf("notes = $%d", argIdx))
		args = append(args, *req.Notes)
		argIdx++
	}

	if len(args) == 0 {
		http.Error(w, `{"error":"Нет данных для обновления"}`, http.StatusBadRequest)
		return
	}

	query := fmt.Sprintf("UPDATE leads_restaurants SET %s WHERE id = $%d RETURNING id, status, priority, notes, updated_at",
		strings.Join(setClauses, ", "), argIdx)
	args = append(args, id)

	var lead Lead
	err = h.db.QueryRow(query, args...).Scan(&lead.ID, &lead.Status, &lead.Priority, &lead.Notes, &lead.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"Лид не найден"}`, http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"Update failed: %v"}`, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
		"lead":   lead,
	})
}

// HandleExportExcel handles GET /api/leads/export
func (h *LeadsHandler) HandleExportExcel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	params := parseFilterParams(r)
	// For export, disable pagination to dump all filtered records (max 5000)
	params.Page = 1
	params.PageSize = 5000

	leads, _, err := h.queryLeads(params)
	if err != nil {
		http.Error(w, fmt.Sprintf("Ошибка выборки данных: %v", err), http.StatusInternalServerError)
		return
	}

	f := excelize.NewFile()
	sheet := "Лиды HoReCa"
	f.SetSheetName("Sheet1", sheet)

	// Headers
	headers := []string{
		"ID", "Название заведения", "Юрлицо (ООО/ИП)", "Тип юрлица", "Адрес", "Город",
		"Категории / Рубрики", "Средний чек (₽)", "Исходный чек", "Телефоны",
		"Сайт", "VK", "Telegram", "Рейтинг", "Кол-во отзывов",
		"Статус", "Приоритет", "Ссылка 2ГИС", "Проверка Rusprofile", "Заметки", "Дата добавления",
	}

	for colIdx, hText := range headers {
		cell, _ := excelize.CoordinatesToCellName(colIdx+1, 1)
		_ = f.SetCellValue(sheet, cell, hText)
	}

	// Style Header Row (Dark Slate / Emerald)
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#FFFFFF", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#1e222d"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	_ = f.SetRowStyle(sheet, 1, 1, headerStyle)
	_ = f.SetRowHeight(sheet, 1, 26)

	// Populate Rows
	for rowIdx, lead := range leads {
		row := rowIdx + 2
		phonesStr := strings.Join(lead.Phones, ", ")
		rubricsStr := strings.Join(lead.Rubrics, ", ")

		_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", row), lead.ID)
		_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", row), lead.Name)
		_ = f.SetCellValue(sheet, fmt.Sprintf("C%d", row), lead.LegalName)
		_ = f.SetCellValue(sheet, fmt.Sprintf("D%d", row), lead.LegalType)
		_ = f.SetCellValue(sheet, fmt.Sprintf("E%d", row), lead.Address)
		_ = f.SetCellValue(sheet, fmt.Sprintf("F%d", row), lead.City)
		_ = f.SetCellValue(sheet, fmt.Sprintf("G%d", row), rubricsStr)
		_ = f.SetCellValue(sheet, fmt.Sprintf("H%d", row), lead.AvgBillVal)
		_ = f.SetCellValue(sheet, fmt.Sprintf("I%d", row), lead.AvgBillRaw)
		_ = f.SetCellValue(sheet, fmt.Sprintf("J%d", row), phonesStr)
		_ = f.SetCellValue(sheet, fmt.Sprintf("K%d", row), lead.Website)
		_ = f.SetCellValue(sheet, fmt.Sprintf("L%d", row), lead.VkURL)
		_ = f.SetCellValue(sheet, fmt.Sprintf("M%d", row), lead.TgURL)
		_ = f.SetCellValue(sheet, fmt.Sprintf("N%d", row), lead.Rating)
		_ = f.SetCellValue(sheet, fmt.Sprintf("O%d", row), lead.ReviewsCount)
		_ = f.SetCellValue(sheet, fmt.Sprintf("P%d", row), lead.Status)
		_ = f.SetCellValue(sheet, fmt.Sprintf("Q%d", row), lead.Priority)
		_ = f.SetCellValue(sheet, fmt.Sprintf("R%d", row), lead.TwoGisURL)
		_ = f.SetCellValue(sheet, fmt.Sprintf("S%d", row), lead.RusprofileURL)
		_ = f.SetCellValue(sheet, fmt.Sprintf("T%d", row), lead.Notes)
		_ = f.SetCellValue(sheet, fmt.Sprintf("U%d", row), lead.CreatedAt.Format("2006-01-02 15:04"))
	}

	// Auto fit column widths
	_ = f.SetColWidth(sheet, "A", "A", 8)
	_ = f.SetColWidth(sheet, "B", "B", 28)
	_ = f.SetColWidth(sheet, "C", "C", 26)
	_ = f.SetColWidth(sheet, "D", "D", 14)
	_ = f.SetColWidth(sheet, "E", "E", 32)
	_ = f.SetColWidth(sheet, "G", "G", 30)
	_ = f.SetColWidth(sheet, "H", "H", 16)
	_ = f.SetColWidth(sheet, "J", "J", 24)
	_ = f.SetColWidth(sheet, "K", "M", 25)
	_ = f.SetColWidth(sheet, "P", "Q", 16)
	_ = f.SetColWidth(sheet, "R", "S", 35)

	filename := fmt.Sprintf("leads_monster_%s.xlsx", time.Now().Format("2006-01-02_1504"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	if err := f.Write(w); err != nil {
		log.Printf("⚠️ [Excel] Ошибка записи потока xlsx: %v", err)
	}
}

func parseFilterParams(r *http.Request) FilterParams {
	q := r.URL.Query()

	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}

	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	reviewsMin, _ := strconv.Atoi(q.Get("reviews_min"))
	minBill, _ := strconv.ParseFloat(q.Get("min_bill"), 64)
	maxBill, _ := strconv.ParseFloat(q.Get("max_bill"), 64)

	var rubrics []string
	if rub := strings.TrimSpace(q.Get("rubric")); rub != "" {
		for _, item := range strings.Split(rub, ",") {
			item = strings.TrimSpace(item)
			if item != "" {
				rubrics = append(rubrics, item)
			}
		}
	}

	return FilterParams{
		Search:     strings.TrimSpace(q.Get("search")),
		City:       strings.TrimSpace(q.Get("city")),
		BillRange:  strings.TrimSpace(q.Get("bill_range")),
		MinBill:    minBill,
		MaxBill:    maxBill,
		LegalOnly:  q.Get("legal_only") == "true" || q.Get("legal_only") == "1",
		LegalType:  strings.TrimSpace(q.Get("legal_type")),
		ReviewsMin: reviewsMin,
		Rubrics:    rubrics,
		Status:     strings.TrimSpace(q.Get("status")),
		Priority:   strings.TrimSpace(q.Get("priority")),
		SortBy:     strings.TrimSpace(q.Get("sort_by")),
		SortOrder:  strings.TrimSpace(q.Get("sort_order")),
		Page:       page,
		PageSize:   pageSize,
	}
}

func (h *LeadsHandler) queryLeads(p FilterParams) ([]Lead, int, error) {
	whereClauses := []string{"1=1"}
	var args []interface{}
	argIdx := 1

	// Search filter: fuzzy search across Venue Name, Legal Entity Name, Street Address
	if p.Search != "" {
		searchPattern := "%" + p.Search + "%"
		whereClauses = append(whereClauses, fmt.Sprintf("(name ILIKE $%d OR legal_name ILIKE $%d OR address ILIKE $%d)", argIdx, argIdx, argIdx))
		args = append(args, searchPattern)
		argIdx++
	}

	// City filter
	if p.City != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("city = $%d", argIdx))
		args = append(args, p.City)
		argIdx++
	}

	// Bill range filter
	switch p.BillRange {
	case "under_800":
		whereClauses = append(whereClauses, "avg_bill_val > 0 AND avg_bill_val < 800")
	case "800_1200":
		whereClauses = append(whereClauses, "avg_bill_val >= 800 AND avg_bill_val <= 1200")
	case "1200_1800":
		whereClauses = append(whereClauses, "avg_bill_val >= 1200 AND avg_bill_val <= 1800")
	case "over_1800":
		whereClauses = append(whereClauses, "avg_bill_val > 1800")
	default:
		if p.MinBill > 0 {
			whereClauses = append(whereClauses, fmt.Sprintf("avg_bill_val >= $%d", argIdx))
			args = append(args, p.MinBill)
			argIdx++
		}
		if p.MaxBill > 0 {
			whereClauses = append(whereClauses, fmt.Sprintf("avg_bill_val <= $%d", argIdx))
			args = append(args, p.MaxBill)
			argIdx++
		}
	}

	// Legal only filter (filters out kiosks)
	if p.LegalOnly {
		whereClauses = append(whereClauses, "legal_type IN ('OOO', 'IP') AND legal_name != ''")
	} else if p.LegalType != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("legal_type = $%d", argIdx))
		args = append(args, p.LegalType)
		argIdx++
	}

	// Reviews threshold filter
	if p.ReviewsMin > 0 {
		whereClauses = append(whereClauses, fmt.Sprintf("reviews_count >= $%d", argIdx))
		args = append(args, p.ReviewsMin)
		argIdx++
	}

	// Rubrics multi-tag filter
	if len(p.Rubrics) > 0 {
		whereClauses = append(whereClauses, fmt.Sprintf("rubrics && $%d", argIdx))
		args = append(args, pq.Array(p.Rubrics))
		argIdx++
	}

	// Status filter
	if p.Status != "" && p.Status != "all" {
		whereClauses = append(whereClauses, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, p.Status)
		argIdx++
	}

	// Priority filter
	if p.Priority != "" && p.Priority != "all" {
		whereClauses = append(whereClauses, fmt.Sprintf("priority = $%d", argIdx))
		args = append(args, p.Priority)
		argIdx++
	}

	whereSQL := strings.Join(whereClauses, " AND ")

	// Count total records
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM leads_restaurants WHERE %s", whereSQL)
	var total int
	err := h.db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Sorting
	sortByCol := "avg_bill_val"
	switch p.SortBy {
	case "name":
		sortByCol = "name"
	case "reviews_count":
		sortByCol = "reviews_count"
	case "rating":
		sortByCol = "rating"
	case "created_at":
		sortByCol = "created_at"
	case "priority":
		sortByCol = "priority"
	}

	sortOrder := "DESC"
	if strings.ToUpper(p.SortOrder) == "ASC" {
		sortOrder = "ASC"
	}

	offset := (p.Page - 1) * p.PageSize
	querySQL := fmt.Sprintf(`
		SELECT id, two_gis_id, name, legal_name, legal_type, address, city, rubrics,
		       avg_bill_raw, avg_bill_val, phones, website, vk_url, tg_url,
		       rating, reviews_count, two_gis_url, rusprofile_url, status, priority, notes,
		       created_at, updated_at
		FROM leads_restaurants
		WHERE %s
		ORDER BY %s %s, id DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, sortByCol, sortOrder, argIdx, argIdx+1)

	args = append(args, p.PageSize, offset)

	rows, err := h.db.Query(querySQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var leads []Lead
	for rows.Next() {
		var l Lead
		err := rows.Scan(
			&l.ID, &l.TwoGisID, &l.Name, &l.LegalName, &l.LegalType, &l.Address, &l.City, &l.Rubrics,
			&l.AvgBillRaw, &l.AvgBillVal, &l.Phones, &l.Website, &l.VkURL, &l.TgURL,
			&l.Rating, &l.ReviewsCount, &l.TwoGisURL, &l.RusprofileURL, &l.Status, &l.Priority, &l.Notes,
			&l.CreatedAt, &l.UpdatedAt,
		)
		if err != nil {
			log.Printf("⚠️ [API] Scan error: %v", err)
			continue
		}
		leads = append(leads, l)
	}

	if leads == nil {
		leads = []Lead{}
	}

	return leads, total, nil
}

func (h *LeadsHandler) getDashboardStats() (DashboardStats, error) {
	var s DashboardStats
	query := `
		SELECT 
			COUNT(*),
			COUNT(*) FILTER (WHERE legal_type IN ('OOO', 'IP') AND legal_name != ''),
			COUNT(*) FILTER (WHERE avg_bill_val >= 1000),
			COUNT(*) FILTER (WHERE status IN ('contacted', 'pilot_sent', 'signed_1rub'))
		FROM leads_restaurants
	`
	err := h.db.QueryRow(query).Scan(&s.TotalCount, &s.EnrichedCount, &s.HighTicketCount, &s.InPipelineCount)
	return s, err
}
