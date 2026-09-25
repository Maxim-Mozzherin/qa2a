package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// callLLM выполняет низкоуровневый HTTP-запрос к API нейросети с поддержкой fallback-моделей и повторных попыток.
func callLLM(contentParts []map[string]interface{}, modelsToTry ...string) (string, string, error) {
	payload := map[string]interface{}{
		"stream":      false,
		"max_tokens":  65536,
		"temperature": 0.1,
		"messages": []map[string]interface{}{
			{"role": "user", "content": contentParts},
		},
	}

	// Поддержка Chain-of-Thought (CoT) Thinking / Reasoning при необходимости
	thinkingBudgetStr := os.Getenv("AI_THINKING_BUDGET")
	if thinkingBudgetStr != "" {
		if budget, err := strconv.Atoi(thinkingBudgetStr); err == nil && budget > 0 {
			payload["thinking"] = map[string]interface{}{
				"type":          "enabled",
				"budget_tokens": budget,
			}
		}
	} else if effort := os.Getenv("AI_REASONING_EFFORT"); effort != "" {
		payload["reasoning_effort"] = effort
	}

	fallbackModels := strings.Split(aiModel, ",")
	if len(modelsToTry) > 0 && modelsToTry[0] != "" {
		fallbackModels = modelsToTry
	}
	var respBody []byte
	maxRetries := 5
	if len(fallbackModels) > maxRetries {
		maxRetries = len(fallbackModels)
	}
	var lastErr error
	var chosenModel string

	for attempt := 1; attempt <= maxRetries; attempt++ {
		modelToUse := strings.TrimSpace(fallbackModels[(attempt-1)%len(fallbackModels)])
		payload["model"] = modelToUse
		chosenModel = modelToUse

		jsonData, err := json.Marshal(payload)
		if err != nil {
			return "", "", fmt.Errorf("ошибка сериализации JSON для AI: %w", err)
		}

		req, err := http.NewRequest("POST", aiBaseUrl, bytes.NewBuffer(jsonData))
		if err != nil {
			return "", "", fmt.Errorf("ошибка формирования HTTP запроса к AI: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+aiApiKey)
		req.Header.Set("Content-Type", "application/json")

		// Ограничиваем время ожидания конкретной попытки:
		// Для Gemini жесткий тайм-аут 8s (чтобы не застревать в очередях OmniRoute при 429),
		// для Claude до 120s для полной и точной обработки объемных накладных.
		timeoutSec := 120
		if strings.Contains(strings.ToLower(modelToUse), "gemini") {
			timeoutSec = 8
		}
		ctxReq, cancelReq := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
		req = req.WithContext(ctxReq)

		resp, err := llmHTTPClient.Do(req)
		if err != nil {
			cancelReq()
			lastErr = fmt.Errorf("таймаут/сбой при обращении к модели %s (%s): %w", modelToUse, aiBaseUrl, err)
			log.Printf("⚠️ Модель %s не ответила за %ds или сбой (%v). Переход к следующей fallback-модели...", modelToUse, timeoutSec, err)
			continue
		}

		respBody, err = io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()
		cancelReq()

		if err != nil {
			lastErr = fmt.Errorf("ошибка чтения ответа AI: %w", err)
			continue
		}

		// Если провайдер/модель возвращает 400 Bad Request из-за неподдерживаемых параметров thinking,
		// автоматически удаляем thinking-параметры и повторяем запрос без падения
		if resp.StatusCode == http.StatusBadRequest && (payload["thinking"] != nil || payload["reasoning_effort"] != nil) {
			log.Printf("⚠️ Модель %s не поддерживает параметры thinking/reasoning_effort (HTTP 400). Повторяем без CoT...", modelToUse)
			delete(payload, "thinking")
			delete(payload, "reasoning_effort")
			attempt-- // Не сжигаем счетчик попыток
			continue
		}

		// Если OmniRoute временно сообщает chat_admission_busy, ожидаем освобождения очереди
		if resp.StatusCode == http.StatusServiceUnavailable && strings.Contains(string(respBody), "chat_admission_busy") {
			log.Printf("⚠️ OmniRoute admission queue busy (chat_admission_busy). Ожидание 1.5s перед повтором (попытка %d/%d)...", attempt, maxRetries)
			time.Sleep(1500 * time.Millisecond)
			continue
		}

		// Если модель исчерпала квоту (HTTP 429) или превышен лимит очереди (502 wedged / 503 budget / 504 timeout)
		// немедленно переключаемся на следующую fallback-модель без задержки
		if resp.StatusCode == http.StatusTooManyRequests ||
			resp.StatusCode == http.StatusBadGateway ||
			resp.StatusCode == http.StatusGatewayTimeout ||
			(resp.StatusCode == http.StatusServiceUnavailable && (strings.Contains(string(respBody), "queue") || strings.Contains(string(respBody), "budget") || strings.Contains(string(respBody), "wedged"))) ||
			strings.Contains(string(respBody), "rate-limit-watchdog-wedge-reset") {
			lastErr = fmt.Errorf("AI API ошибка/лимит модели %s (HTTP %d): %s", modelToUse, resp.StatusCode, string(respBody))
			log.Printf("⚠️ Модель %s недоступна или превысила квоту (HTTP %d). Мгновенный переход к следующей fallback-модели...", modelToUse, resp.StatusCode)
			continue
		}

		if resp.StatusCode >= http.StatusInternalServerError {
			lastErr = fmt.Errorf("AI API вернул ошибку (HTTP %d): %s", resp.StatusCode, string(respBody))
			log.Printf("⚠️ AI попытка %d/%d не удалась: модель=%s, HTTP %d — повтор через %ds", attempt, maxRetries, modelToUse, resp.StatusCode, attempt*2)
			time.Sleep(time.Duration(attempt*2) * time.Second)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return "", "", fmt.Errorf("AI API вернул ошибку (HTTP %d): %s", resp.StatusCode, string(respBody))
		}

		lastErr = nil
		log.Printf("✅ AI ответ получен: модель=%s, попытка=%d/%d", modelToUse, attempt, maxRetries)
		break
	}

	if lastErr != nil {
		return "", "", fmt.Errorf("не удалось получить ответ от нейросети после %d попыток. Последняя ошибка: %v", maxRetries, lastErr)
	}

	var apiResp struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", "", fmt.Errorf("ошибка разбора JSON ответа AI: %w\nСырые данные: %s", err, string(respBody))
	}

	if apiResp.Error.Message != "" {
		return "", "", fmt.Errorf("ошибка модели AI: %s", apiResp.Error.Message)
	}

	if len(apiResp.Choices) == 0 {
		return "", "", fmt.Errorf("AI вернул пустой список вариантов ответа")
	}

	content := strings.TrimSpace(apiResp.Choices[0].Message.Content)
	retModel := apiResp.Model
	if retModel == "" {
		retModel = chosenModel
	}

	return content, retModel, nil
}

// parseLLMContentToAiResponse очищает markdown-форматирование и парсит JSON в структуру AiResponse.
func parseLLMContentToAiResponse(content string, usedModel string) (*AiResponse, error) {
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
	} else if strings.HasPrefix(content, "```JSON") {
		content = strings.TrimPrefix(content, "```JSON")
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
	}
	if strings.HasSuffix(content, "```") {
		content = strings.TrimSuffix(content, "```")
	}
	content = strings.TrimSpace(content)

	startIdx := strings.Index(content, "{")
	endIdx := strings.LastIndex(content, "}")

	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		return nil, fmt.Errorf("в ответе AI не найден валидный JSON-объект:\n%s", content)
	}

	cleanJsonStr := content[startIdx : endIdx+1]

	var parsed AiResponse
	if err := json.Unmarshal([]byte(cleanJsonStr), &parsed); err != nil {
		return nil, fmt.Errorf("ошибка парсинга итогового JSON накладной: %w\nИзвлеченный фрагмент: %s", err, cleanJsonStr)
	}

	parsed.UsedModel = usedModel
	if parsed.UsedModel == "" {
		parsed.UsedModel = "unknown-model"
	}

	return &parsed, nil
}

// isSubtotalRow определяет, является ли строка служебным итогом по странице или строкой переноса.
func isSubtotalRow(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return true
	}
	subtotalKeywords := []string{
		"итого по странице",
		"всего по странице",
		"промежуточный итог",
		"всего перенесено",
		"перенос",
		"лист ",
	}
	for _, kw := range subtotalKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	if strings.Contains(lower, "страница") && strings.Contains(lower, "из") {
		return true
	}
	return false
}

// isDuplicateJunction проверяет, не является ли позиция дубликатом со стыка смежных страниц.
func isDuplicateJunction(prev AiItem, next AiItem) bool {
	pName := normalizeItemName(prev.Name)
	nName := normalizeItemName(next.Name)
	if pName != "" && nName != "" && pName == nName {
		if math.Abs(prev.Quantity-next.Quantity) < 0.001 && math.Abs(prev.Price-next.Price) < 0.02 {
			return true
		}
	}
	return false
}

// mergePageResponses объединяет постраничные ответы нейросети в единую непротиворечивую накладную.
func mergePageResponses(pages []*AiResponse) *AiResponse {
	if len(pages) == 0 {
		return &AiResponse{}
	}
	if len(pages) == 1 {
		res := pages[0]
		// Фильтруем служебные строки даже для одиночной страницы
		var filtered []AiItem
		for _, it := range res.Items {
			if !isSubtotalRow(it.Name) {
				filtered = append(filtered, it)
			}
		}
		for i := range filtered {
			filtered[i].Num = i + 1
		}
		res.Items = filtered
		return res
	}

	// 1. Берем реквизиты документа с первой страницы
	combined := &AiResponse{
		VendorName:   pages[0].VendorName,
		VendorINN:    pages[0].VendorINN,
		DocNumber:    pages[0].DocNumber,
		DocDate:      pages[0].DocDate,
		Consignee:    pages[0].Consignee,
		ConsigneeINN: pages[0].ConsigneeINN,
		Shipper:      pages[0].Shipper,
		UsedModel:    pages[0].UsedModel,
	}

	// 2. Ищем печатный итог на последней странице (или любой другой, где он найден)
	for i := len(pages) - 1; i >= 0; i-- {
		if pages[i] != nil && pages[i].DocPrintedTotalSum > 0 {
			combined.DocPrintedTotalSum = pages[i].DocPrintedTotalSum
			break
		}
	}

	// 3. Склеиваем товары со всех страниц в порядке 1..N с дедупликацией на стыках
	var allItems []AiItem
	for _, page := range pages {
		if page == nil {
			continue
		}
		for _, item := range page.Items {
			// Пропускаем служебные строки итогов страниц
			if isSubtotalRow(item.Name) {
				continue
			}
			// Проверяем дубликат на стыке
			if len(allItems) > 0 && isDuplicateJunction(allItems[len(allItems)-1], item) {
				log.Printf("ℹ️ Пропущен дубликат строки на стыке страниц: %s (qty=%.3f, price=%.2f)", item.Name, item.Quantity, item.Price)
				continue
			}
			allItems = append(allItems, item)
		}
	}

	// 4. Назначаем красивую сквозную нумерацию 1..N
	for i := range allItems {
		allItems[i].Num = i + 1
	}
	combined.Items = allItems

	return combined
}

// callDirectGoogleGemini выполняет прямой HTTP-запрос к официальному Google Gemini API (v1beta).
func callDirectGoogleGemini(prompt string, imagesBase64 []string, modelName string) (string, string, error) {
	if googleApiKey == "" {
		return "", "", fmt.Errorf("GOOGLE_API_KEY не задан")
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", modelName, googleApiKey)

	type inlineData struct {
		MimeType string `json:"mime_type"`
		Data     string `json:"data"`
	}
	type part struct {
		Text       string      `json:"text,omitempty"`
		InlineData *inlineData `json:"inline_data,omitempty"`
	}
	type contentObj struct {
		Role  string `json:"role,omitempty"`
		Parts []part `json:"parts"`
	}
	type genConfig struct {
		Temperature      float64 `json:"temperature"`
		MaxOutputTokens  int     `json:"maxOutputTokens"`
		ResponseMimeType string  `json:"responseMimeType,omitempty"`
	}
	type reqPayload struct {
		Contents         []contentObj `json:"contents"`
		GenerationConfig genConfig    `json:"generationConfig"`
	}

	var parts []part
	if prompt != "" {
		parts = append(parts, part{Text: prompt})
	}
	for _, b64 := range imagesBase64 {
		parts = append(parts, part{
			InlineData: &inlineData{
				MimeType: "image/jpeg",
				Data:     b64,
			},
		})
	}

	bodyObj := reqPayload{
		Contents: []contentObj{
			{Role: "user", Parts: parts},
		},
		GenerationConfig: genConfig{
			Temperature:      0.1,
			MaxOutputTokens:  65536,
			ResponseMimeType: "application/json",
		},
	}

	jsonData, err := json.Marshal(bodyObj)
	if err != nil {
		return "", "", fmt.Errorf("ошибка сериализации JSON для Google Gemini API: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", "", fmt.Errorf("ошибка формирования HTTP-запроса: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("сетевой сбой обращения к Google Gemini API: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return "", "", fmt.Errorf("ошибка чтения ответа Google Gemini API: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("Google Gemini API HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	type geminiResponse struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}

	var parsedResp geminiResponse
	if err := json.Unmarshal(respBody, &parsedResp); err != nil {
		return "", "", fmt.Errorf("ошибка декодирования ответа Google Gemini: %w", err)
	}

	if parsedResp.Error != nil {
		return "", "", fmt.Errorf("ошибка Google Gemini API: [%d] %s", parsedResp.Error.Code, parsedResp.Error.Message)
	}

	if len(parsedResp.Candidates) == 0 || len(parsedResp.Candidates[0].Content.Parts) == 0 {
		return "", "", fmt.Errorf("Google Gemini API вернул пустой список кандидатов")
	}

	var sb strings.Builder
	for _, p := range parsedResp.Candidates[0].Content.Parts {
		sb.WriteString(p.Text)
	}

	return strings.TrimSpace(sb.String()), fmt.Sprintf("%s (Google API)", modelName), nil
}

// callOmniRouteWithParts упаковывает текстовый промпт и изображения в формат OpenAI API для OmniRoute.
func callOmniRouteWithParts(prompt string, imagesBase64 []string, models ...string) (string, string, error) {
	var contentParts []map[string]interface{}
	if prompt != "" {
		contentParts = append(contentParts, map[string]interface{}{
			"type": "text",
			"text": prompt,
		})
	}
	for _, b64 := range imagesBase64 {
		contentParts = append(contentParts, map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]string{
				"url": "data:image/jpeg;base64," + b64,
			},
		})
	}
	return callLLM(contentParts, models...)
}

// dispatchLLMCall маршрутизирует запрос к выбранной пользователем модели.
// При выборе Google API (gemini-3.8-flash) сначала совершается прямой запрос с ключом Google.
// В случае сетевого сбоя или отказа доступа (например 403 / блокировка IP) автоматически и прозрачно
// выполняется переключение на OmniRoute без потери данных и без падения задачи бухгалтера.
func dispatchLLMCall(prompt string, imagesBase64 []string, requestedModel string) (string, string, error) {
	req := strings.TrimSpace(requestedModel)

	// 1. Прямой запрос к Google API для Gemini 3.8 Flash
	if req == "gemini-3.8-flash" || req == "gemini-3.8-flash-direct" {
		log.Printf("🌐 Запрос к прямому Google Gemini API (модель: gemini-3.8-flash)...")
		content, usedModel, err := callDirectGoogleGemini(prompt, imagesBase64, "gemini-3.8-flash")
		if err == nil {
			log.Printf("✅ Google Gemini API (direct) успешно распознал данные")
			return content, usedModel, nil
		}

		log.Printf("⚠️ Прямой запрос к Google API не удался (%v). Выполняем авто-fallback на Claude Sonnet 4.5...", err)
		omniModels := []string{"kr/claude-sonnet-4.5", "no-think/kr/claude-sonnet-4.5", "kr/claude-haiku-4.5"}
		return callOmniRouteWithParts(prompt, imagesBase64, omniModels...)
	}

	// 2. Обработка других моделей через OmniRoute
	var omniModels []string
	switch req {
	case "gemini-3.5-flash", "gemini/gemini-3.5-flash":
		omniModels = []string{"gemini/gemini-3.5-flash", "kr/claude-sonnet-4.5", "no-think/kr/claude-sonnet-4.5", "kr/claude-haiku-4.5"}
	case "gemini-3-flash", "gemini/gemini-3-flash-preview":
		omniModels = []string{"gemini/gemini-3-flash-preview", "kr/claude-sonnet-4.5", "no-think/kr/claude-sonnet-4.5", "kr/claude-haiku-4.5"}
	case "claude-sonnet-4.5", "kr/claude-sonnet-4.5":
		omniModels = []string{"kr/claude-sonnet-4.5", "no-think/kr/claude-sonnet-4.5", "kr/claude-haiku-4.5"}
	default:
		if req != "" {
			omniModels = []string{req, "kr/claude-sonnet-4.5", "no-think/kr/claude-sonnet-4.5", "kr/claude-haiku-4.5"}
		} else {
			omniModels = []string{"kr/claude-sonnet-4.5", "no-think/kr/claude-sonnet-4.5", "kr/claude-haiku-4.5"}
		}
	}

	return callOmniRouteWithParts(prompt, imagesBase64, omniModels...)
}

// parseMultiPageChunked выполняет постраничный параллельный парсинг многостраничных документов.
func parseMultiPageChunked(text string, imagesBase64 []string, customPrompt string, requestedModel string) (*AiResponse, error) {
	mainPrompt := customPrompt
	if strings.TrimSpace(mainPrompt) == "" {
		mainPrompt = defaultParserPrompt
	}

	// Если страниц нет или только 1 страница — стандартный вызов
	if len(imagesBase64) <= 1 {
		fullPrompt := mainPrompt + "\n\nТекст накладной (может быть пустым, если это скан):\n" + text
		rawContent, usedModel, err := dispatchLLMCall(fullPrompt, imagesBase64, requestedModel)
		if err != nil {
			return nil, err
		}
		singleResp, err := parseLLMContentToAiResponse(rawContent, usedModel)
		if err != nil {
			return nil, err
		}
		singleResp = mergePageResponses([]*AiResponse{singleResp})
		return applyAutoReflection(singleResp, imagesBase64, text, requestedModel), nil
	}

	// Многостраничный режим: обработка страниц параллельными горутинами
	numPages := len(imagesBase64)
	log.Printf("📄 Запуск многостраничного чанкинга: %d страниц(ы) документа (модель: %s)", numPages, requestedModel)

	pageResponses := make([]*AiResponse, numPages)
	pageErrors := make([]error, numPages)

	// Ограничитель конкурентности (по умолчанию 2 для ускоренной параллельной обработки страниц)
	maxConcurrent := 2
	if mcStr := os.Getenv("AI_MAX_CONCURRENT"); mcStr != "" {
		if mc, err := strconv.Atoi(mcStr); err == nil && mc > 0 {
			maxConcurrent = mc
		}
	}
	if numPages < maxConcurrent {
		maxConcurrent = numPages
	}
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for i := 0; i < numPages; i++ {
		wg.Add(1)
		go func(pageIdx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Небольшой сдвиг запуска между страницами во избежание резких скачков
			if pageIdx > 0 {
				time.Sleep(time.Duration(pageIdx*300) * time.Millisecond)
			}

			var promptToUse string
			if pageIdx == 0 {
				promptToUse = chunkPage1Prompt
			} else {
				promptToUse = continuationPageParserPrompt
			}
			fullPrompt := fmt.Sprintf("%s\n\nВНИМАНИЕ: Обработай СТРОГО страницу %d из %d и верни только JSON-объект.", promptToUse, pageIdx+1, numPages)

			raw, model, err := dispatchLLMCall(fullPrompt, []string{imagesBase64[pageIdx]}, requestedModel)
			if err != nil {
				log.Printf("❌ Ошибка парсинга страницы %d: %v", pageIdx+1, err)
				pageErrors[pageIdx] = fmt.Errorf("страница %d: %w", pageIdx+1, err)
				return
			}

			parsed, err := parseLLMContentToAiResponse(raw, model)
			if err != nil {
				log.Printf("⚠️ Ошибка разбора JSON страницы %d (%v). Повторная строгая попытка через Claude...", pageIdx+1, err)
				retryPrompt := fmt.Sprintf("%s\n\nОШИБКА: Твой ответ обязан содержать СТРОГО валидный JSON-объект {...} с товарами страницы %d! Без вступительного текста, без markdown, не жди другие страницы!", promptToUse, pageIdx+1)
				retryRaw, retryModel, retryErr := dispatchLLMCall(retryPrompt, []string{imagesBase64[pageIdx]}, "kr/claude-sonnet-4.5")
				if retryErr == nil {
					if retryParsed, retryJsonErr := parseLLMContentToAiResponse(retryRaw, retryModel); retryJsonErr == nil {
						parsed = retryParsed
						model = retryModel
						err = nil
					}
				}
			}

			if err != nil {
				log.Printf("❌ Ошибка парсинга страницы %d: %v", pageIdx+1, err)
				pageErrors[pageIdx] = fmt.Errorf("страница %d: %w", pageIdx+1, err)
				return
			}

			log.Printf("✅ Страница %d/%d успешно распознана: %d позиций (модель: %s)", pageIdx+1, numPages, len(parsed.Items), model)
			pageResponses[pageIdx] = parsed
		}(i)
	}

	wg.Wait()

	// Проверяем ошибки: первая страница обязана быть успешной (там шапка)
	if pageErrors[0] != nil {
		return nil, fmt.Errorf("не удалось распознать титульную страницу (1) накладной: %w", pageErrors[0])
	}

	// Для остальных страниц: если какая-то страница сбойнула, логируем, но объединяем то, что распозналось
	for i := 1; i < numPages; i++ {
		if pageErrors[i] != nil {
			log.Printf("⚠️ Страница %d не распознана из-за ошибки: %v", i+1, pageErrors[i])
		}
	}

	merged := mergePageResponses(pageResponses)
	log.Printf("🎉 Чанкинг завершен: суммарно объединено %d позиций со всех %d страниц", len(merged.Items), numPages)
	merged = applyAutoReflection(merged, imagesBase64, text, requestedModel)
	return merged, nil
}

// calculateTotalSum вычисляет арифметическую сумму всех распознанных позиций с округлением до копеек.
func calculateTotalSum(items []AiItem) float64 {
	var total float64
	for _, it := range items {
		total += it.Sum
	}
	return math.Round(total*100) / 100
}

// applyAutoReflection выполняет проверку сходимости сумм:
// Сравнивает арифметическую сумму строк (calculatedTotal) с печатным итогом документа (doc_printed_total_sum).
// Если расхождение превышает 1.0 рубль, делает автоматический прицельный дозапрос к AI (Reflection),
// передавая точную дельту и прося перепроверить строки на предмет пропусков или искажений сумм.
func applyAutoReflection(parsed *AiResponse, imagesBase64 []string, text string, requestedModel string) *AiResponse {
	if parsed == nil || len(parsed.Items) == 0 {
		return parsed
	}
	if parsed.DocPrintedTotalSum <= 0.01 {
		return parsed
	}

	calcTotal := calculateTotalSum(parsed.Items)
	delta := math.Abs(calcTotal - parsed.DocPrintedTotalSum)
	if delta <= 1.0 {
		log.Printf("⚖️ Auto-Reflection: суммы сходятся (расчетная=%.2f, печатная=%.2f, дельта=%.2f <= 1.00)", calcTotal, parsed.DocPrintedTotalSum, delta)
		return parsed
	}

	log.Printf("⚠️ Auto-Reflection: обнаружено расхождение сумм (расчетная=%.2f, печатная=%.2f, дельта=%.2f > 1.00). Запуск самоисправления нейросетью...", calcTotal, parsed.DocPrintedTotalSum, delta)

	reflectionPrompt := fmt.Sprintf(`ВНИМАНИЕ! В РАСПОЗНАННОЙ НАКЛАДНОЙ ОБНАРУЖЕНО РАСХОЖДЕНИЕ СУММ:
Печатная итоговая сумма документа (Всего к оплате): %.2f
Сумма распознанных позиций: %.2f
Расхождение (дельта): %.2f

Твоя задача — внимательно перепроверить изображение(я) документа и выполнить самоисправление (Self-Correction):
1. Найди пропущенные строки товаров, которые не были извлечены (особенно если сумма меньше печатного итога).
2. Проверь каждую строку на предмет неверно распознанного количества, цены или ставки НДС.
3. Убедись, что промежуточные итоги («Итого по странице», «Всего перенесено») не попали в список товаров.
4. Верни ПОЛНЫЙ исправленный список товаров накладной, сумма которых должна строго сходиться с печатным итогом %.2f.

Верни строго только JSON-объект:
{
  "doc_printed_total_sum": %.2f,
  "items": [
    {
      "original_prefix": "Слово",
      "num": 1,
      "name": "Название товара",
      "clean_category": "Бакалея",
      "brand": "",
      "quantity": 10.0,
      "unit": "упак",
      "base_unit": "кг",
      "price": 120.0,
      "sum": 1200.0,
      "sum_without_nds": 1000.0,
      "nds_percent": 20.0,
      "ai_multiplier": 0.5,
      "ai_tip": "1 шт = 500г"
    }
  ]
}`, parsed.DocPrintedTotalSum, calcTotal, delta, parsed.DocPrintedTotalSum, parsed.DocPrintedTotalSum)

	// Ограничиваем количество картинок для рефлексии (до 5 ключевых страниц), чтобы не раздувать payload
	reflectImages := imagesBase64
	if len(reflectImages) > 5 {
		reflectImages = reflectImages[:5]
	}

	raw, model, err := dispatchLLMCall(reflectionPrompt, reflectImages, requestedModel)
	if err != nil {
		log.Printf("⚠️ Auto-Reflection: сетевая ошибка при самоисправлении: %v (сохраняем исходный результат)", err)
		return parsed
	}

	reflected, err := parseLLMContentToAiResponse(raw, model)
	if err != nil || reflected == nil || len(reflected.Items) == 0 {
		log.Printf("⚠️ Auto-Reflection: не удалось разобрать исправленный JSON: %v (сохраняем исходный результат)", err)
		return parsed
	}

	// Фильтруем возможные служебные строки из ответа рефлексии
	var cleanItems []AiItem
	for _, it := range reflected.Items {
		if !isSubtotalRow(it.Name) {
			cleanItems = append(cleanItems, it)
		}
	}
	for i := range cleanItems {
		cleanItems[i].Num = i + 1
	}

	newCalcTotal := calculateTotalSum(cleanItems)
	newDelta := math.Abs(newCalcTotal - parsed.DocPrintedTotalSum)

	log.Printf("⚖️ Auto-Reflection: исходная дельта=%.2f, новая дельта=%.2f", delta, newDelta)
	if newDelta < delta {
		log.Printf("✅ Auto-Reflection успешно улучшила результат (дельта снижена с %.2f до %.2f)! Применен исправленный список из %d позиций", delta, newDelta, len(cleanItems))
		parsed.Items = cleanItems
		if reflected.DocPrintedTotalSum > 0 {
			parsed.DocPrintedTotalSum = reflected.DocPrintedTotalSum
		}
	} else {
		log.Printf("ℹ️ Auto-Reflection не улучшила расхождение (%.2f >= %.2f). Сохраняем исходный результат.", newDelta, delta)
	}

	return parsed
}

// parseWithClaude — единая точка входа для парсинга документов.
func parseWithClaude(text string, imagesBase64 []string, customPrompt string, optionalModel ...string) (*AiResponse, error) {
	reqModel := ""
	if len(optionalModel) > 0 {
		reqModel = optionalModel[0]
	}
	return parseMultiPageChunked(text, imagesBase64, customPrompt, reqModel)
}
