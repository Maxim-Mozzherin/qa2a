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

// callLLM выполняет низкоуровневый HTTP-запрос к API нейросети с поддержкой fallback-моделей Gemini и быстрого failover.
func callLLM(contentParts []map[string]interface{}) (string, string, error) {
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

	rawModels := strings.Split(aiModel, ",")
	var fallbackModels []string
	for _, m := range rawModels {
		trimmed := strings.TrimSpace(m)
		if trimmed != "" {
			fallbackModels = append(fallbackModels, trimmed)
		}
	}
	if len(fallbackModels) == 0 {
		fallbackModels = []string{
			"gemini/gemini-3.5-flash",
			"gemini/gemini-3-flash-preview",
			"gemini/gemini-3.1-flash-lite-preview",
		}
	}

	var respBody []byte
	maxRetries := len(fallbackModels)
	var lastErr error
	var chosenModel string

	for attempt := 1; attempt <= maxRetries; attempt++ {
		modelToUse := fallbackModels[attempt-1]
		payload["model"] = modelToUse
		chosenModel = modelToUse

		jsonData, err := json.Marshal(payload)
		if err != nil {
			return "", "", fmt.Errorf("ошибка сериализации JSON для AI: %w", err)
		}

		// Fail-fast таймаут: 12 секунд на каждую модель Gemini, чтобы не висеть при сбоях провайдера
		attemptTimeout := 12 * time.Second
		ctx, cancel := context.WithTimeout(context.Background(), attemptTimeout)

		req, err := http.NewRequestWithContext(ctx, "POST", aiBaseUrl, bytes.NewBuffer(jsonData))
		if err != nil {
			cancel()
			return "", "", fmt.Errorf("ошибка формирования HTTP запроса к AI: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+aiApiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := llmHTTPClient.Do(req)
		if err != nil {
			cancel()
			lastErr = fmt.Errorf("модель %s не ответила за %v или сбой сети: %w", modelToUse, attemptTimeout, err)
			log.Printf("⚠️ Модель %s не ответила за %v (%v). Переход к следующей модели Gemini...", modelToUse, attemptTimeout, err)
			continue
		}

		respBody, err = io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()
		cancel()

		if err != nil {
			lastErr = fmt.Errorf("ошибка чтения ответа AI (%s): %w", modelToUse, err)
			log.Printf("⚠️ Ошибка чтения ответа модели %s: %v. Переход к следующей модели Gemini...", modelToUse, err)
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

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError || resp.StatusCode == http.StatusNotFound {
			lastErr = fmt.Errorf("модель %s вернула HTTP %d: %s", modelToUse, resp.StatusCode, string(respBody))
			log.Printf("⚠️ Модель %s вернула HTTP %d — немедленный переход к следующей модели Gemini...", modelToUse, resp.StatusCode)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("модель %s вернула ошибку (HTTP %d): %s", modelToUse, resp.StatusCode, string(respBody))
			log.Printf("⚠️ Модель %s вернула ошибку (HTTP %d): %s. Переход к следующей модели Gemini...", modelToUse, resp.StatusCode, string(respBody))
			continue
		}

		lastErr = nil
		log.Printf("✅ AI ответ получен: модель=%s, попытка=%d/%d", modelToUse, attempt, maxRetries)
		break
	}

	if lastErr != nil {
		return "", "", fmt.Errorf("не удалось получить ответ от моделей Gemini (%s) после %d попыток. Последняя ошибка: %v", strings.Join(fallbackModels, " -> "), maxRetries, lastErr)
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
		singleResp, err := parseLLMContentToAiResponse(rawContent, usedModel)
		if err != nil {
			return nil, err
		}
		singleResp = mergePageResponses([]*AiResponse{singleResp})
		return applyAutoReflection(singleResp, imagesBase64, text), nil
	}

	// Многостраничный режим: обработка страниц параллельными горутинами
	numPages := len(imagesBase64)
	log.Printf("📄 Запуск многостраничного чанкинга: %d страниц(ы) документа", numPages)

	pageResponses := make([]*AiResponse, numPages)
	pageErrors := make([]error, numPages)

	// Ограничитель конкурентности (по умолчанию 2 для быстрой параллельной обработки страниц)
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

			// Сдвиг между страницами, чтобы запросы не создавали резких пиков
			if pageIdx > 0 {
				time.Sleep(time.Duration(pageIdx*300) * time.Millisecond)
			}

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
	merged = applyAutoReflection(merged, imagesBase64, text)
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
func applyAutoReflection(parsed *AiResponse, imagesBase64 []string, text string) *AiResponse {
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

	var parts []map[string]interface{}
	parts = append(parts, map[string]interface{}{
		"type": "text",
		"text": reflectionPrompt,
	})

	// Ограничиваем количество картинок для рефлексии (до 5 ключевых страниц), чтобы не раздувать payload
	reflectImages := imagesBase64
	if len(reflectImages) > 5 {
		reflectImages = reflectImages[:5]
	}
	for _, b64 := range reflectImages {
		parts = append(parts, map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]string{
				"url": "data:image/jpeg;base64," + b64,
			},
		})
	}

	raw, model, err := callLLM(parts)
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
func parseWithClaude(text string, imagesBase64 []string, customPrompt string) (*AiResponse, error) {
	return parseMultiPageChunked(text, imagesBase64, customPrompt)
}
