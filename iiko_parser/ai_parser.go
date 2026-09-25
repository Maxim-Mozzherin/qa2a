package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

// callLLM выполняет низкоуровневый HTTP-запрос к API нейросети с поддержкой fallback-моделей и повторных попыток.
func callLLM(contentParts []map[string]interface{}) (string, string, error) {
	payload := map[string]interface{}{
		"stream":      false,
		"max_tokens":  65536,
		"temperature": 0.1,
		"messages": []map[string]interface{}{
			{"role": "user", "content": contentParts},
		},
	}

	fallbackModels := strings.Split(aiModel, ",")
	var respBody []byte
	maxRetries := 3
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

		resp, err := llmHTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("сетевой сбой при обращении к AI (%s): %w", aiBaseUrl, err)
			time.Sleep(time.Duration(attempt*2) * time.Second)
			continue
		}

		respBody, err = io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()

		if err != nil {
			lastErr = fmt.Errorf("ошибка чтения ответа AI: %w", err)
			time.Sleep(time.Duration(attempt*2) * time.Second)
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
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

// parseMultiPageChunked выполняет постраничный параллельный парсинг многостраничных документов.
func parseMultiPageChunked(text string, imagesBase64 []string, customPrompt string) (*AiResponse, error) {
	mainPrompt := customPrompt
	if strings.TrimSpace(mainPrompt) == "" {
		mainPrompt = defaultParserPrompt
	}

	// Если страниц нет или только 1 страница — стандартный вызов
	if len(imagesBase64) <= 1 {
		var contentParts []map[string]interface{}
		contentParts = append(contentParts, map[string]interface{}{
			"type": "text",
			"text": mainPrompt + "\n\nТекст накладной (может быть пустым, если это скан):\n" + text,
		})
		for _, b64 := range imagesBase64 {
			contentParts = append(contentParts, map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]string{
					"url": "data:image/jpeg;base64," + b64,
				},
			})
		}

		rawContent, usedModel, err := callLLM(contentParts)
		if err != nil {
			return nil, err
		}
		return parseLLMContentToAiResponse(rawContent, usedModel)
	}

	// Многостраничный режим: обработка страниц параллельными горутинами
	numPages := len(imagesBase64)
	log.Printf("📄 Запуск многостраничного чанкинга: %d страниц(ы) документа", numPages)

	pageResponses := make([]*AiResponse, numPages)
	pageErrors := make([]error, numPages)

	// Ограничитель конкурентности (максимум 4 одновременных запроса к API, чтобы не ловить 429)
	maxConcurrent := 4
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

			promptToUse := mainPrompt
			// Для страниц 2..N используем специализированный лаконичный промпт
			if pageIdx > 0 {
				promptToUse = continuationPageParserPrompt
			}

			var parts []map[string]interface{}
			parts = append(parts, map[string]interface{}{
				"type": "text",
				"text": fmt.Sprintf("%s\n\n[Страница %d из %d]", promptToUse, pageIdx+1, numPages),
			})
			parts = append(parts, map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]string{
					"url": "data:image/jpeg;base64," + imagesBase64[pageIdx],
				},
			})

			raw, model, err := callLLM(parts)
			if err != nil {
				log.Printf("❌ Ошибка парсинга страницы %d: %v", pageIdx+1, err)
				pageErrors[pageIdx] = fmt.Errorf("страница %d: %w", pageIdx+1, err)
				return
			}

			parsed, err := parseLLMContentToAiResponse(raw, model)
			if err != nil {
				log.Printf("❌ Ошибка разбора JSON страницы %d: %v", pageIdx+1, err)
				pageErrors[pageIdx] = fmt.Errorf("страница %d: %w", pageIdx+1, err)
				return
			}

			log.Printf("✅ Страница %d/%d успешно распознана: %d позиций", pageIdx+1, numPages, len(parsed.Items))
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
	return merged, nil
}

// parseWithClaude — единая точка входа для парсинга документов.
func parseWithClaude(text string, imagesBase64 []string, customPrompt string) (*AiResponse, error) {
	return parseMultiPageChunked(text, imagesBase64, customPrompt)
}
