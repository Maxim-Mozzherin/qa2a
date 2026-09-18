package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type AccountantTicketDTO struct {
	ID                int       `json:"id"`
	CompanyID         int       `json:"company_id"`
	CompanyName       string    `json:"company_name"`
	UserID            int       `json:"user_id"`
	UserName          string    `json:"user_name"`
	Category          string    `json:"category"`
	Priority          string    `json:"priority"`
	Description       string    `json:"description"`
	Status            string    `json:"status"`
	AccountantComment string    `json:"accountant_comment"`
	MediaPaths        string    `json:"media_paths"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func sendTelegramNotification(tgID int64, text string) {
	botToken := os.Getenv("BOT_TOKEN")
	if botToken == "" || tgID <= 0 { return }
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	payload := map[string]interface{}{"chat_id": tgID, "text": text, "parse_mode": "HTML"}
	bodyBytes, _ := json.Marshal(payload)
	
	client := &http.Client{Timeout: 10 * time.Second} // Fix: Add explicit 10s timeout
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(bodyBytes))
	if err != nil { 
	    log.Printf("[tg-push] Ошибка отправки TG %d: %v", tgID, err)
	    return 
	}
	defer resp.Body.Close()
}

func handleGetAccountingTickets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user := GetAuthUser(r)
	if user == nil {
		http.Error(w, "Неавторизованный доступ", http.StatusUnauthorized)
		return
	}

	companyIDStr := r.URL.Query().Get("company_id")
	var companyID int
	if companyIDStr != "" && companyIDStr != "0" {
		fmt.Sscanf(companyIDStr, "%d", &companyID)
		if !checkAccountantAccessUser(user, companyID) {
			http.Error(w, "Доступ к заявкам данного заведения запрещен", http.StatusForbidden)
			return
		}
	}

	query := `
		SELECT 
			t.id, t.company_id, c.name, t.user_id, COALESCE(u.full_name, u.username),
			t.category, t.priority, t.description, t.status, 
			COALESCE(t.accountant_comment, ''), 
			COALESCE(t.media_paths, '[]'),
			t.created_at, t.updated_at
		FROM accounting_tickets t 
		JOIN companies c ON t.company_id = c.id 
		JOIN users u ON t.user_id = u.id`

	var args []interface{}
	var conditions []string

	if companyID > 0 {
		conditions = append(conditions, fmt.Sprintf("t.company_id = $%d", len(args)+1))
		args = append(args, companyID)
	} else if user.Role != "superadmin" && user.Role != "global_accountant" {
		if user.AccountingFirmID != nil {
			conditions = append(conditions, fmt.Sprintf("c.accounting_firm_id = $%d", len(args)+1))
			args = append(args, *user.AccountingFirmID)
		} else {
			conditions = append(conditions, "1 = 0")
		}
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY CASE WHEN t.status = 'new' THEN 1 WHEN t.status = 'in_progress' THEN 2 ELSE 3 END, t.created_at DESC LIMIT 200"

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, "DB Error: "+err.Error(), 500)
		return
	}
	defer rows.Close()

	var tickets []AccountantTicketDTO
	for rows.Next() {
		var t AccountantTicketDTO
		if err := rows.Scan(
			&t.ID, &t.CompanyID, &t.CompanyName, &t.UserID, &t.UserName,
			&t.Category, &t.Priority, &t.Description, &t.Status,
			&t.AccountantComment, &t.MediaPaths, &t.CreatedAt, &t.UpdatedAt,
		); err == nil {
			tickets = append(tickets, t)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tickets)
}

func handleUpdateAccountingTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	var req struct {
		TicketID int    `json:"ticket_id"`
		Status   string `json:"status"`
		Comment  string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", 400)
		return
	}

	req.Status = strings.TrimSpace(req.Status)
	req.Comment = strings.TrimSpace(req.Comment)
	if req.TicketID <= 0 || req.Status == "" {
		http.Error(w, "Bad request", 400)
		return
	}

	var tgID int64
	var companyID int
	var companyName, category string
	err := db.QueryRow(`
		SELECT u.tg_id, c.id, c.name, t.category 
		FROM accounting_tickets t 
		JOIN users u ON t.user_id = u.id 
		JOIN companies c ON t.company_id = c.id 
		WHERE t.id = $1`, req.TicketID).Scan(&tgID, &companyID, &companyName, &category)
	if err != nil {
		http.Error(w, "Заявка не найдена", 404)
		return
	}

	user := GetAuthUser(r)
	if !checkAccountantAccessUser(user, companyID) {
		http.Error(w, "Доступ к обновлению заявки данного заведения запрещен", http.StatusForbidden)
		return
	}

	_, err = db.Exec(`UPDATE accounting_tickets SET status = $1, accountant_comment = $2, updated_at = NOW() WHERE id = $3`, req.Status, req.Comment, req.TicketID)
	if err != nil {
		http.Error(w, "DB update error", 500)
		return
	}

	statusRus := map[string]string{"new": "🟡 На рассмотрении", "in_progress": "🔵 В работе у бухгалтера", "resolved": "🟢 Выполнена", "rejected": "🔴 Отклонена"}[req.Status]
	catRus := map[string]string{"ttk": "Техкарты / Меню", "invoice": "Накладная / Поставщик", "writeoff": "Списание / Склад", "inventory": "Инвентаризация", "other": "Общий вопрос"}[category]
	if statusRus == "" { statusRus = req.Status }
	if catRus == "" { catRus = category }

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("🔔 <b>Обновление по заявке #%05d</b>\n🏢 Заведение: <b>%s</b>\n📁 Категория: <b>%s</b>\n📌 Статус: <b>%s</b>\n", 
		req.TicketID, html.EscapeString(companyName), catRus, statusRus))
	if req.Comment != "" { 
		msg.WriteString(fmt.Sprintf("\n💬 <b>Ответ бухгалтера:</b>\n<i>%s</i>\n", html.EscapeString(req.Comment))) 
	}

	go sendTelegramNotification(tgID, msg.String())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
