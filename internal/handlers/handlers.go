package handlers

import (
	"encoding/json"
	
	"log"
	"net/http"
	"net/url"
	"strconv"
	

	"github.com/gorilla/mux"
	"qa2a/internal/models"
	"qa2a/internal/service"
)

type Handler struct {
	authService      *service.AuthService
	inventoryService *service.InventoryService
	reportService    *service.ReportService // Добавляем поле для ReportService
	botToken         string
}

func New(as *service.AuthService, is *service.InventoryService, rs *service.ReportService, t string) *Handler {
    if rs == nil {
        panic("CRITICAL: reportService is NIL in handlers.New!")
    }
    return &Handler{
        authService:      as,
        inventoryService: is,
        reportService:    rs,
        botToken:         t,
    }
}
func (h *Handler) DownloadProcurementPDFHandler(w http.ResponseWriter, r *http.Request) {
	if h.reportService == nil {
		log.Printf("❌ CRITICAL: reportService is nil!")
		http.Error(w, "PDF service not configured", http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(r)
	reqID, _ := strconv.Atoi(vars["id"])
	cID, _ := strconv.Atoi(r.URL.Query().Get("c_id"))
	if cID == 0 {
		cID, _ = strconv.Atoi(r.Header.Get("X-Company-ID"))
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "inline; filename=\"request.pdf\"")

	err := h.reportService.GenerateProcurementPDF(reqID, cID, w)
	if err != nil {
		log.Printf("❌ ГЕНЕРАЦИЯ PDF ПРОВАЛИЛАСЬ: %v", err)
		return
	}
}

func (h *Handler) AuthHandler(w http.ResponseWriter, r *http.Request) {
var req struct { InitData string `json:"initData"`; DemoID int64 `json:"demo_id"`; DemoName string `json:"demo_name"` }
json.NewDecoder(r.Body).Decode(&req)
var tgID int64
var name string
if req.InitData != "" {
params, _ := url.ParseQuery(req.InitData)
var u struct { ID int64 `json:"id"`; FN string `json:"first_name"` }
json.Unmarshal([]byte(params.Get("user")), &u)
tgID, name = u.ID, u.FN
} else { tgID, name = req.DemoID, req.DemoName }
res, _ := h.authService.LoginOrRegister(tgID, "user", name)
w.Header().Set("Content-Type", "application/json")
json.NewEncoder(w).Encode(res)
}
func (h *Handler) CreateOperationHandler(w http.ResponseWriter, r *http.Request) {
	cID, _ := strconv.Atoi(r.Header.Get("X-Company-ID"))
	tID, _ := strconv.ParseInt(r.Header.Get("X-Telegram-ID"), 10, 64)
	user, _ := h.authService.GetUserByTgID(tID)

	var req struct {
		Pos        string  `json:"position_name"`
		Qty        float64 `json:"quantity"`
		Unit       string  `json:"unit"`
		Type       string  `json:"type"`
		Loc    	   int     `json:"location_id"`
		ToLoc 	   int     `json:"to_location_id"` // <-- Добавили целевой склад
		IsUnlisted bool    `json:"is_unlisted"` // <-- Флаг призрака
		Comment    string  `json:"comment"`     // <-- Причина
	}
	json.NewDecoder(r.Body).Decode(&req)

	var err error
	if req.Type == "transfer" {
		err = h.inventoryService.Transfer(user.ID, cID, req.Pos, req.Qty, req.Unit, req.Loc, req.ToLoc)
	} else {
		// По умолчанию считаем, что это writeoff (списание)
		err = h.inventoryService.WriteOff(user.ID, cID, req.Pos, req.Qty, req.Unit, req.Loc, req.IsUnlisted, req.Comment)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
}
func (h *Handler) GetBalancesHandler(w http.ResponseWriter, r *http.Request) {
cID, _ := strconv.Atoi(r.Header.Get("X-Company-ID"))
res, _ := h.inventoryService.GetBalances(cID)
json.NewEncoder(w).Encode(res)
}
func (h *Handler) GetOperationsHandler(w http.ResponseWriter, r *http.Request) {
cID, _ := strconv.Atoi(r.Header.Get("X-Company-ID"))
res, _ := h.inventoryService.GetHistory(cID, 20)
json.NewEncoder(w).Encode(res)
}
func (h *Handler) GetLocationsHandler(w http.ResponseWriter, r *http.Request) {
cID, _ := strconv.Atoi(r.Header.Get("X-Company-ID"))
res, _ := h.inventoryService.GetLocations(cID)
json.NewEncoder(w).Encode(res)
}
func (h *Handler) CreateLocationHandler(w http.ResponseWriter, r *http.Request) {
cID, _ := strconv.Atoi(r.Header.Get("X-Company-ID"))
var req struct { Name string `json:"name"` }
json.NewDecoder(r.Body).Decode(&req)
h.inventoryService.CreateLocation(cID, req.Name)
w.WriteHeader(201)
}
func (h *Handler) GetPositionsHandler(w http.ResponseWriter, r *http.Request) {
cID, _ := strconv.Atoi(r.Header.Get("X-Company-ID"))
res, _ := h.inventoryService.GetPositions(cID)
json.NewEncoder(w).Encode(res)
}
func (h *Handler) CreatePositionHandler(w http.ResponseWriter, r *http.Request) {
	cID, _ := strconv.Atoi(r.Header.Get("X-Company-ID"))
	tID, _ := strconv.ParseInt(r.Header.Get("X-Telegram-ID"), 10, 64)
	user, _ := h.authService.GetUserByTgID(tID)

	// ДОБАВЛЕНО ПОЛЕ Supplier
	var req struct {
		Name     string  `json:"name"`
		Unit     string  `json:"unit"`
		Supplier string  `json:"supplier"`
		InitQty  float64 `json:"initial_quantity"`
		Loc      int     `json:"location_id"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	// ТЕПЕРЬ МЫ ПЕРЕДАЕМ ИМЯ ПОСТАВЩИКА В БАЗУ
	h.inventoryService.CreatePosition(&models.Position{
		CompanyID: cID, 
		Name:      req.Name, 
		Unit:      req.Unit, 
		Supplier:  req.Supplier, // <-- Вот оно!
	})
	
	if req.InitQty > 0 && req.Loc > 0 {
		h.inventoryService.WriteOff(user.ID, cID, req.Name, -req.InitQty, req.Unit, req.Loc, false, "Начальный остаток при создании")
	}
	w.WriteHeader(http.StatusCreated)
}
func (h *Handler) CreateCompanyHandler(w http.ResponseWriter, r *http.Request) {
tID, _ := strconv.ParseInt(r.Header.Get("X-Telegram-ID"), 10, 64)
user, _ := h.authService.GetUserByTgID(tID)
var req struct { Name string `json:"name"` }
json.NewDecoder(r.Body).Decode(&req)
id, _ := h.authService.CreateCompany(user.ID, req.Name)
json.NewEncoder(w).Encode(map[string]int{"id": id})
}
func (h *Handler) JoinCompanyHandler(w http.ResponseWriter, r *http.Request) {
	tID, _ := strconv.ParseInt(r.Header.Get("X-Telegram-ID"), 10, 64)
	user, _ := h.authService.GetUserByTgID(tID)

	// Ожидаем поле "code" как в app.js
	var req struct {
		Code string `json:"code"` 
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", 400)
		return
	}

	if req.Code == "" {
		http.Error(w, "Code is required", 400)
		return
	}

	// Вызываем сервис
	name, err := h.authService.JoinCompanyByCode(user.ID, req.Code)
	if err != nil {
		// Если код не найден или ошибка БД — вернем 400
		http.Error(w, err.Error(), 400)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"company": name})
}
func (h *Handler) GetInviteCodeHandler(w http.ResponseWriter, r *http.Request) {
    tID, _ := strconv.ParseInt(r.Header.Get("X-Telegram-ID"), 10, 64)
    cID, _ := strconv.Atoi(r.Header.Get("X-Company-ID")) // Получаем ID компании
    user, _ := h.authService.GetUserByTgID(tID)
    
    // Передаем cID в сервис
    code, err := h.authService.GetInviteCode(user.ID, cID)
    if err != nil {
        http.Error(w, "Код не найден", 404)
        return
    }
    json.NewEncoder(w).Encode(map[string]string{"code": code})
}
func (h *Handler) GetMembersHandler(w http.ResponseWriter, r *http.Request) {
    // 1. Получаем ID компании из заголовка
    cIDStr := r.Header.Get("X-Company-ID")
    cID, err := strconv.Atoi(cIDStr)
    if err != nil {
        http.Error(w, "Invalid X-Company-ID", 400)
        return
    }

    // 2. Получаем список сотрудников через репозиторий
    // (Используем прямой доступ к repo, так как мы внутри Handler)
    members, err := h.authService.GetCompanyMembers(cID)
    if err != nil {
        log.Printf("❌ Ошибка получения участников: %v", err)
        http.Error(w, "Database error", 500)
        return
    }

    // 3. Отдаем JSON
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(members)
}
// ===== ЗАЯВКИ (PROCUREMENT) =====

func (h *Handler) CreateProcurementHandler(w http.ResponseWriter, r *http.Request) {
	cID, _ := strconv.Atoi(r.Header.Get("X-Company-ID"))
	tID, _ := strconv.ParseInt(r.Header.Get("X-Telegram-ID"), 10, 64)
	user, _ := h.authService.GetUserByTgID(tID)

	var req struct {
		Items []models.ProcurementItem `json:"items"` // ProcurementItem должен иметь поле IsUnlisted
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid body", http.StatusBadRequest)
		return
	}

	if err := h.inventoryService.CreateProcurementRequest(cID, user.ID, req.Items); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) GetProcurementsHandler(w http.ResponseWriter, r *http.Request) {
	cID, _ := strconv.Atoi(r.Header.Get("X-Company-ID"))
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending" // По умолчанию отдаем ожидающие
	}

	requests, err := h.inventoryService.GetProcurementRequests(cID, status)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(requests)
}

func (h *Handler) UpdateProcurementStatusHandler(w http.ResponseWriter, r *http.Request) {
	tID, _ := strconv.ParseInt(r.Header.Get("X-Telegram-ID"), 10, 64)
	admin, _ := h.authService.GetUserByTgID(tID)

	var req struct {
		RequestID int    `json:"request_id"`
		Status    string `json:"status"` // "approved" или "rejected"
	}
	json.NewDecoder(r.Body).Decode(&req)

	if err := h.inventoryService.UpdateProcurementStatus(req.RequestID, req.Status, admin.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
func (h *Handler) GetUnlistedItemsHandler(w http.ResponseWriter, r *http.Request) {
    cIDStr := r.Header.Get("X-Company-ID")
    cID, _ := strconv.Atoi(cIDStr)
    
    // Используем сервис, а не repo!
    items, err := h.inventoryService.GetGhostItems(cID)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(items)
}
func (h *Handler) UpdateMemberRoleHandler(w http.ResponseWriter, r *http.Request) {
    cID, _ := strconv.Atoi(r.Header.Get("X-Company-ID"))
    tID, _ := strconv.ParseInt(r.Header.Get("X-Telegram-ID"), 10, 64)
    actor, _ := h.authService.GetUserByTgID(tID)

    var req struct {
        UserID      int    `json:"user_id"`
        Role        string `json:"role"`
        CustomTitle string `json:"custom_title"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid JSON", 400)
        return
    }

    // Вызываем сервис с проверкой прав
    err := h.authService.UpdateMemberRole(cID, actor.ID, req.UserID, req.Role, req.CustomTitle)
    if err != nil {
        http.Error(w, err.Error(), 403) // 403 Forbidden если прав не хватило
        return
    }
    w.WriteHeader(http.StatusOK)
}
func (h *Handler) RemoveMemberHandler(w http.ResponseWriter, r *http.Request) {
    cID, _ := strconv.Atoi(r.Header.Get("X-Company-ID"))
    tID, _ := strconv.ParseInt(r.Header.Get("X-Telegram-ID"), 10, 64)
    actor, _ := h.authService.GetUserByTgID(tID)
    
    // Получаем ID из URL
    vars := mux.Vars(r)
    targetUserID, _ := strconv.Atoi(vars["id"])

    err := h.authService.RemoveMember(cID, actor.ID, targetUserID)
    if err != nil {
        http.Error(w, err.Error(), 403)
        return
    }
    w.WriteHeader(http.StatusOK)
}