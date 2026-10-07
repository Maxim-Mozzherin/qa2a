package telegram

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// BotClient инкапсулирует взаимодействие с Telegram Bot API
type BotClient struct {
	token      string
	db         *sql.DB
	httpClient *http.Client
}

// NewBotClient создает новый клиент для отправки отчетов
func NewBotClient(token string, db *sql.DB) *BotClient {
	return &BotClient{
		token: token,
		db:    db,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// ResolveRecipients разбирает список логинов/ID (через запятую или пробел)
// и сопоставляет их с базой пользователей QA2A для получения числовых chat_id
func (b *BotClient) ResolveRecipients(recipientsStr string) ([]int64, []string) {
	var chatIDs []int64
	var unresolved []string
	seen := make(map[int64]bool)

	tokens := strings.FieldsFunc(recipientsStr, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	})

	for _, raw := range tokens {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}

		// 1. Проверяем, если передан чистый numeric chat_id
		if id, err := strconv.ParseInt(item, 10, 64); err == nil && id > 0 {
			if !seen[id] {
				seen[id] = true
				chatIDs = append(chatIDs, id)
			}
			continue
		}

		// 2. Иначе это @username: убираем '@' и ищем в таблице users
		cleanUser := strings.ToLower(strings.TrimPrefix(item, "@"))
		if cleanUser == "" {
			continue
		}

		var tgID int64
		err := b.db.QueryRow(`
			SELECT tg_id 
			FROM users 
			WHERE LOWER(TRIM(LEADING '@' FROM username)) = $1 
			  AND tg_id > 0 
			ORDER BY id DESC 
			LIMIT 1`, cleanUser).Scan(&tgID)

		if err == nil && tgID > 0 {
			if !seen[tgID] {
				seen[tgID] = true
				chatIDs = append(chatIDs, tgID)
			}
		} else {
			unresolved = append(unresolved, "@"+cleanUser)
		}
	}

	return chatIDs, unresolved
}

// SendMessage отправляет форматированное текстовое сообщение в Telegram
func (b *BotClient) SendMessage(chatID int64, htmlText string) error {
	if b.token == "" || chatID <= 0 {
		return fmt.Errorf("некорректный токен или chat_id (%d)", chatID)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", b.token)
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       htmlText,
		"parse_mode": "HTML",
		"disable_web_page_preview": true,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("ошибка маршалинга JSON: %w", err)
	}

	resp, err := b.httpClient.Post(url, "application/json", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return fmt.Errorf("сетевая ошибка отправки в TG: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Telegram API вернул HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// SendDocument отправляет PDF-документ файлом в чат Telegram
func (b *BotClient) SendDocument(chatID int64, filename string, pdfData []byte, caption string) error {
	if b.token == "" || chatID <= 0 {
		return fmt.Errorf("некорректный токен или chat_id (%d)", chatID)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendDocument", b.token)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// chat_id
	if err := writer.WriteField("chat_id", strconv.FormatInt(chatID, 10)); err != nil {
		return err
	}

	// caption
	if caption != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			return err
		}
		if err := writer.WriteField("parse_mode", "HTML"); err != nil {
			return err
		}
	}

	// file attachment
	part, err := writer.CreateFormFile("document", filename)
	if err != nil {
		return fmt.Errorf("ошибка создания form file: %w", err)
	}
	if _, err := part.Write(pdfData); err != nil {
		return fmt.Errorf("ошибка записи pdf данных: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("ошибка закрытия multipart writer: %w", err)
	}

	req, err := http.NewRequest("POST", url, &buf)
	if err != nil {
		return fmt.Errorf("ошибка создания запроса: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("сетевая ошибка при отправке документа: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Telegram API (sendDocument) вернул HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	log.Printf("📎 [Telegram] Документ %s успешно отправлен пользователю %d", filename, chatID)
	return nil
}
