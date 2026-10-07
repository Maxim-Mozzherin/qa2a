package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"qa2a/pkg/ratelimit"
)

// ============================================================================
// ЗАЯВКИ В БУХГАЛТЕРИЮ (SERVICE DESK)
// ============================================================================

// CreateAccountingTicketHandler создает новую заявку заведения в бухгалтерию.
func (h *Handler) CreateAccountingTicketHandler(w http.ResponseWriter, r *http.Request) {
	companyID := h.getCompanyID(r)
	userID := h.getUserID(r)

	if companyID == 0 || userID == 0 {
		respondError(w, http.StatusUnauthorized, "Недостаточно данных авторизации")
		return
	}

	clientIP := ratelimit.GetClientIP(r)
	rateKey := fmt.Sprintf("ticket_%d_%d_%s", companyID, userID, clientIP)
	if allowed, remaining := h.joinLimiter.Allow(rateKey); !allowed {
		respondError(w, http.StatusTooManyRequests, fmt.Sprintf("Слишком много заявок. Повторите попытку через %d сек.", int(remaining.Seconds())+1))
		return
	}

	hasAccess, err := h.checkAdminAccess(companyID, userID)
	if err != nil || !hasAccess {
		respondError(w, http.StatusForbidden, "Отправлять заявки могут только руководители заведения")
		return
	}

	// Лимит 50 МБ на запрос
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		respondError(w, http.StatusBadRequest, "Превышен лимит размера файлов (макс 50 МБ)")
		return
	}

	category := strings.TrimSpace(r.FormValue("category"))
	allowedCats := map[string]bool{
		"ttk":       true,
		"invoice":   true,
		"writeoff":  true,
		"inventory": true,
		"other":     true,
	}
	if !allowedCats[category] {
		respondError(w, http.StatusBadRequest, "Некорректная категория заявки")
		return
	}

	description := strings.TrimSpace(r.FormValue("description"))
	if description == "" {
		respondError(w, http.StatusBadRequest, "Пожалуйста, опишите суть вопроса")
		return
	}

	var savedPaths []string
	files := r.MultipartForm.File["media"] // Массив файлов
	
	ticketsDir := getUploadsTicketsDir()
	_ = os.MkdirAll(ticketsDir, 0750)

	allowedExts := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
		".mp4": true, ".mov": true,
	}

	for i, fileHeader := range files {
		ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
		if !allowedExts[ext] {
			respondError(w, http.StatusBadRequest, "Недопустимый формат файла. Разрешены только фото и видео.")
			return
		}

		file, err := fileHeader.Open()
		if err != nil { continue }

		// Проверяем сигнатуру содержимого (magic bytes) для исключения загрузки вредоносных скриптов под видом картинок
		headerBuf := make([]byte, 512)
		n, _ := io.ReadFull(file, headerBuf)
		if n == 0 {
			file.Close()
			continue
		}
		detectedMime := http.DetectContentType(headerBuf[:n])

		// Разрешенные MIME-типы медиа
		isAllowedMime := false
		switch detectedMime {
		case "image/jpeg", "image/png", "image/webp", "video/mp4":
			isAllowedMime = true
		default:
			// Для mov контейнеров и некоторых mp4 http.DetectContentType может выдать video/quicktime или application/octet-stream
			if (ext == ".mov" || ext == ".mp4") && (detectedMime == "video/quicktime" || detectedMime == "application/octet-stream") {
				isAllowedMime = true
			}
		}

		if !isAllowedMime {
			file.Close()
			respondError(w, http.StatusBadRequest, "Недопустимое содержимое файла. Обнаружен неподдерживаемый тип данных.")
			return
		}

		var reader io.Reader
		if seeker, ok := file.(io.ReadSeeker); ok {
			if _, err := seeker.Seek(0, io.SeekStart); err == nil {
				reader = file
			} else {
				reader = io.MultiReader(bytes.NewReader(headerBuf[:n]), file)
			}
		} else {
			reader = io.MultiReader(bytes.NewReader(headerBuf[:n]), file)
		}

		filename := fmt.Sprintf("%d_%d_%d%s", companyID, time.Now().UnixNano(), i, ext)
		outPath := filepath.Join(ticketsDir, filename)
		
		out, errCreate := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
		if errCreate == nil {
			// Лимит 25 МБ на один файл
			_, _ = io.Copy(out, io.LimitReader(reader, 25<<20))
			_ = out.Close()
			savedPaths = append(savedPaths, "/api/uploads/tickets/"+filename)
		}
		file.Close()
	}

	mediaJSON, _ := json.Marshal(savedPaths)
	if string(mediaJSON) == "null" { mediaJSON = []byte("[]") }

	err = h.inventoryService.GetRepo().CreateAccountingTicket(companyID, userID, category, "normal", description, string(mediaJSON))
	if err != nil {
		log.Printf("❌ [Tickets] Error creating ticket for company %d: %v", companyID, err)
		respondError(w, http.StatusInternalServerError, "Ошибка создания тикета в базе данных")
		return
	}

	respondJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

// GetAccountingTicketsHandler возвращает историю обращений заведения в бухгалтерию.
func (h *Handler) GetAccountingTicketsHandler(w http.ResponseWriter, r *http.Request) {
	companyID := h.getCompanyID(r)
	userID := h.getUserID(r)

	if companyID == 0 || userID == 0 {
		respondError(w, http.StatusUnauthorized, "Неавторизованный запрос")
		return
	}

	hasAccess, err := h.checkAdminAccess(companyID, userID)
	if err != nil || !hasAccess {
		respondError(w, http.StatusForbidden, "Доступ разрешен только руководству заведения")
		return
	}

	tickets, err := h.inventoryService.GetRepo().GetCompanyTickets(companyID)
	if err != nil {
		log.Printf("❌ [Tickets] Error fetching company tickets for company %d: %v", companyID, err)
		respondError(w, http.StatusInternalServerError, "Ошибка чтения списка заявок")
		return
	}

	respondJSON(w, http.StatusOK, tickets)
}

// ServeTicketMediaHandler отдает медиафайлы заявок только авторизованным пользователям компании.
func (h *Handler) ServeTicketMediaHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
		respondError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	userID := h.getUserID(r)
	if userID == 0 {
		respondError(w, http.StatusUnauthorized, "Неавторизованный доступ")
		return
	}

	vars := mux.Vars(r)
	rawFilename := vars["filename"]
	filename := filepath.Base(rawFilename)
	if filename == "" || filename != rawFilename || filename == "." || filename == ".." ||
		strings.Contains(rawFilename, "/") || strings.Contains(rawFilename, "\\") {
		respondError(w, http.StatusBadRequest, "Недопустимое имя файла")
		return
	}

	ext := strings.ToLower(filepath.Ext(filename))
	allowedExts := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
		".mp4": true, ".mov": true,
	}
	if !allowedExts[ext] {
		respondError(w, http.StatusForbidden, "Недопустимый формат файла")
		return
	}

	// Извлекаем companyID из имени файла: <companyID>_<timestamp>_<idx><ext>
	parts := strings.SplitN(filename, "_", 2)
	if len(parts) < 2 {
		respondError(w, http.StatusBadRequest, "Некорректный формат имени файла")
		return
	}
	var companyID int
	if _, err := fmt.Sscanf(parts[0], "%d", &companyID); err != nil || companyID <= 0 {
		respondError(w, http.StatusBadRequest, "Некорректный ID компании")
		return
	}

	// Проверяем членство пользователя в компании или роль superadmin
	var hasMembership bool
	err := h.inventoryService.GetRepo().GetDb().QueryRow(
		"SELECT EXISTS(SELECT 1 FROM memberships WHERE company_id = $1 AND user_id = $2)",
		companyID, userID,
	).Scan(&hasMembership)

	if err != nil || !hasMembership {
		var isPlatformAdmin bool
		// Check if user matches configured AdminTgID
		errAdmin := h.inventoryService.GetRepo().GetDb().QueryRow(
			"SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND tg_id = $2)", userID, h.adminTgID,
		).Scan(&isPlatformAdmin)
		if errAdmin != nil || !isPlatformAdmin {
			respondError(w, http.StatusForbidden, "Доступ к файлу запрещен")
			return
		}
	}

	fullPath := filepath.Join(getUploadsTicketsDir(), filename)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		respondError(w, http.StatusNotFound, "Файл не найден")
		return
	}

	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, fullPath)
}

func getUploadsTicketsDir() string {
	uploadsDir := os.Getenv("UPLOADS_DIR")
	if uploadsDir == "" {
		if _, err := os.Stat("/app/uploads"); err == nil {
			uploadsDir = "/app/uploads"
		} else if _, err := os.Stat("/opt/qa2a-reboot/uploads"); err == nil {
			uploadsDir = "/opt/qa2a-reboot/uploads"
		} else {
			uploadsDir = "uploads"
		}
	}
	return filepath.Join(uploadsDir, "tickets")
}

