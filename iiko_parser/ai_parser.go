package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

func parseWithClaude(text string, imagesBase64 []string, customPrompt string) (*AiResponse, error) {
	prompt := customPrompt
	if strings.TrimSpace(prompt) == "" {
		prompt = defaultParserPrompt
	}
	

	var contentParts []map[string]interface{}
	contentParts = append(contentParts, map[string]interface{}{
		"type": "text",
		"text": prompt + "\n\nТекст накладной (может быть пустым, если это скан):\n" + text,
	})
	for _, b64 := range imagesBase64 {
		contentParts = append(contentParts, map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]string{
				"url": "data:image/jpeg;base64," + b64,
			},
		})
	}

	payload := map[string]interface{}{
		"stream":           false,
		"max_tokens":       65536,
		"temperature":      0.1,
		"reasoning_effort": "none",
		"thinking_config":  map[string]int{"thinking_budget": 0},
		"messages": []map[string]interface{}{
			{"role": "user", "content": contentParts},
		},
	}

	fallbackModels := strings.Split(aiModel, ",")

	var respBody []byte
	maxRetries := 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		modelToUse := strings.TrimSpace(fallbackModels[(attempt-1)%len(fallbackModels)])
		payload["model"] = modelToUse

		jsonData, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("ошибка сериализации JSON для AI: %w", err)
		}

		req, err := http.NewRequest("POST", aiBaseUrl, bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("ошибка формирования HTTP запроса к AI: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+aiApiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := llmHTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("сетевой сбой при обращении к AI (%s): %w", aiBaseUrl, err)
			time.Sleep(time.Duration(attempt*2) * time.Second)
			continue
		}

		respBody, err = io.ReadAll(resp.Body)
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
			return nil, fmt.Errorf("AI API вернул ошибку (HTTP %d): %s", resp.StatusCode, string(respBody))
		}

		lastErr = nil
		log.Printf("✅ AI ответ получен: модель=%s, попытка=%d/%d", modelToUse, attempt, maxRetries)
		break
	}

	if lastErr != nil {
		return nil, fmt.Errorf("не удалось получить ответ от нейросети после %d попыток. Последняя ошибка: %v", maxRetries, lastErr)
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
		return nil, fmt.Errorf("ошибка разбора JSON ответа AI: %w\nСырые данные: %s", err, string(respBody))
	}

	if apiResp.Error.Message != "" {
		return nil, fmt.Errorf("ошибка модели AI: %s", apiResp.Error.Message)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("AI вернул пустой список вариантов ответа")
	}

	content := strings.TrimSpace(apiResp.Choices[0].Message.Content)

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

	var parsedAiResp AiResponse
	if err := json.Unmarshal([]byte(cleanJsonStr), &parsedAiResp); err != nil {
		return nil, fmt.Errorf("ошибка парсинга итогового JSON накладной: %w\nИзвлеченный фрагмент: %s", err, cleanJsonStr)
	}
	
	parsedAiResp.UsedModel = apiResp.Model
	if parsedAiResp.UsedModel == "" {
		parsedAiResp.UsedModel = "unknown-model (gateway stripped metadata)"
	}

	return &parsedAiResp, nil
}
