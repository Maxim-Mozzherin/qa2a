package handlers
import (
	"encoding/json"
	"log" 
	"net/http"
	"net/url"
	"strconv"
	"qa2a/internal/models"
	"qa2a/internal/service"
)
type Handler struct {
authService *service.AuthService
inventoryService *service.InventoryService
botToken string
}
func New(as *service.AuthService, is *service.InventoryService, t string) *Handler {
return &Handler{authService: as, inventoryService: is, botToken: t}
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
var req struct { Pos string `json:"position_name"`; Qty float64 `json:"quantity"`; Unit string `json:"unit"`; Type string `json:"type"`; Loc int `json:"location_id"` }
json.NewDecoder(r.Body).Decode(&req)
h.inventoryService.WriteOff(user.ID, cID, req.Pos, req.Qty, req.Unit, req.Loc)
w.WriteHeader(201)
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
var req struct { Name string; Unit string; InitQty float64 `json:"initial_quantity"`; Loc int `json:"location_id"` }
json.NewDecoder(r.Body).Decode(&req)
h.inventoryService.CreatePosition(&models.Position{CompanyID: cID, Name: req.Name, Unit: req.Unit})
if req.InitQty > 0 && req.Loc > 0 { h.inventoryService.WriteOff(user.ID, cID, req.Name, -req.InitQty, req.Unit, req.Loc) }
w.WriteHeader(201)
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
var req struct { Code string `json:"code"` }
json.NewDecoder(r.Body).Decode(&req)
name, _ := h.authService.JoinCompanyByCode(user.ID, req.Code)
json.NewEncoder(w).Encode(map[string]string{"company": name})
}
func (h *Handler) GetInviteCodeHandler(w http.ResponseWriter, r *http.Request) {
tID, _ := strconv.ParseInt(r.Header.Get("X-Telegram-ID"), 10, 64)
user, _ := h.authService.GetUserByTgID(tID)
code, _ := h.authService.GetInviteCode(user.ID)
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