package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

// checkAdminAccess проверяет, наделен ли пользователь административными полномочиями в заведении (Owner, Admin, Manager).
func (h *Handler) checkAdminAccess(companyID, userID int) (bool, error) {
	if companyID == 0 || userID == 0 {
		return false, nil
	}

	membership, err := h.inventoryService.GetRepo().GetMembership(companyID, userID)
	if err != nil {
		return false, err
	}

	role := strings.ToLower(strings.TrimSpace(membership.Role))
	return role == "owner" || role == "manager" || role == "admin", nil
}

// GetSuppliersHandler обрабатывает эндпоинт GET /api/suppliers.
// Возвращает всех известных заведению поставщиков с их Telegram-контактами (доступно руководству).
func (h *Handler) GetSuppliersHandler(w http.ResponseWriter, r *http.Request) {
	companyID := h.getCompanyID(r)
	userID := h.getUserID(r)

	if companyID == 0 || userID == 0 {
		respondError(w, http.StatusUnauthorized, "Недостаточно данных авторизации для доступа к справочнику поставщиков")
		return
	}

	hasAccess, err := h.checkAdminAccess(companyID, userID)
	if err != nil || !hasAccess {
		respondError(w, http.StatusForbidden, "Доступ запрещен: просматривать поставщиков могут только руководители заведения")
		return
	}

	list, err := h.inventoryService.GetAllSuppliersWithContacts(companyID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка получения списка поставщиков: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, list)
}

// SaveSupplierContactHandler обрабатывает эндпоинт POST /api/suppliers/contacts.
// Сохраняет Telegram-юзернейм представителя поставщика для отправки заказов в один клик.
func (h *Handler) SaveSupplierContactHandler(w http.ResponseWriter, r *http.Request) {
	companyID := h.getCompanyID(r)
	userID := h.getUserID(r)

	if companyID == 0 || userID == 0 {
		respondError(w, http.StatusUnauthorized, "Неавторизованный запрос")
		return
	}

	hasAccess, err := h.checkAdminAccess(companyID, userID)
	if err != nil || !hasAccess {
		respondError(w, http.StatusForbidden, "Доступ запрещен: сохранять контакты поставщиков могут только Администраторы")
		return
	}

	var req struct {
		SupplierUUID string `json:"supplier_uuid"`
		TgUsername   string `json:"tg_username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат JSON-запроса")
		return
	}

	req.SupplierUUID = strings.TrimSpace(req.SupplierUUID)
	req.TgUsername = strings.TrimSpace(req.TgUsername)

	if req.SupplierUUID == "" {
		respondError(w, http.StatusBadRequest, "Не указан UUID поставщика (supplier_uuid)")
		return
	}

	err = h.inventoryService.SaveSupplierContact(companyID, req.SupplierUUID, req.TgUsername)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Не удалось сохранить контакт: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "Контакт поставщика успешно привязан",
	})
}

// GetPositionSuppliersHandler обрабатывает эндпоинт GET /api/positions/{uuid}/suppliers.
// Подбирает рекомендованных поставщиков под выбранный товар при составлении заявки сотрудником.
func (h *Handler) GetPositionSuppliersHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	productUUID := strings.TrimSpace(vars["uuid"])
	companyID := h.getCompanyID(r)

	if companyID == 0 {
		respondError(w, http.StatusBadRequest, "Отсутствует идентификатор заведения (X-Company-ID)")
		return
	}
	if productUUID == "" {
		respondError(w, http.StatusBadRequest, "Не указан идентификатор товара (UUID)")
		return
	}

	res, err := h.inventoryService.GetPositionSuppliers(companyID, productUUID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка подбора поставщиков: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, res)
}

// GetProcurementSuppliersHandler обрабатывает эндпоинт GET /api/procurements/{id:[0-9]+}/suppliers.
// Формирует и группирует позиции утвержденной заявки по поставщикам для отправки текста в Telegram.
func (h *Handler) GetProcurementSuppliersHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	reqID, _ := strconv.Atoi(vars["id"])
	companyID := h.getCompanyID(r)
	userID := h.getUserID(r)

	if companyID == 0 || userID == 0 {
		respondError(w, http.StatusUnauthorized, "Недостаточно данных авторизации")
		return
	}

	hasAccess, err := h.checkAdminAccess(companyID, userID)
	if err != nil || !hasAccess {
		respondError(w, http.StatusForbidden, "Доступ запрещен: формировать и отправлять заказы контрагентам может только руководство")
		return
	}

	if reqID <= 0 {
		respondError(w, http.StatusBadRequest, "Не передан корректный ID заявки на закупку")
		return
	}

	list, err := h.inventoryService.GetProcurementItemsWithSuppliers(companyID, reqID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка формирования строк заявки: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, list)
}

