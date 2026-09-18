package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
		"model":            aiModel,
		"stream":           false,
		"max_tokens":       65536,
		"temperature":      0.1,
		"reasoning_effort": "none",
		"thinking_config":  map[string]int{"thinking_budget": 0},
		"messages": []map[string]interface{}{
			{"role": "user", "content": contentParts},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации JSON для AI: %w", err)
	}

	var respBody []byte
	maxRetries := 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
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
			time.Sleep(time.Duration(attempt*2) * time.Second)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("AI API вернул ошибку (HTTP %d): %s", resp.StatusCode, string(respBody))
		}

		lastErr = nil
		break
	}

	if lastErr != nil {
		return nil, fmt.Errorf("не удалось получить ответ от нейросети после %d попыток. Последняя ошибка: %v", maxRetries, lastErr)
	}

	var apiResp struct {
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

	var aiResp AiResponse
	if err := json.Unmarshal([]byte(cleanJsonStr), &aiResp); err != nil {
		return nil, fmt.Errorf("ошибка парсинга итогового JSON накладной: %w\nИзвлеченный фрагмент: %s", err, cleanJsonStr)
	}

	return &aiResp, nil
}
