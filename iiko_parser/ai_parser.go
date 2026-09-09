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
	_ = `Ты — автоматический парсер накладных. Твоя задача: найти поставщика, получателя (грузополучателя), номер документа (УПД/ТОРГ-12) и все товары.
ОЧЕНЬ ВАЖНО: В названиях часто указана сложная фасовка (коробки, упаковки, граммы). Тебе нужно вычислить коэффициент перевода в базовые единицы (кг, литры или штуки) и вернуть его в поле ai_multiplier.

Правила расчета параметров:
1. ОСОБОЕ ПРАВИЛО ДЛЯ КОНСЕРВОВ (кукуруза, ананасы, горошек, оливки и т.д.):
   - В консервах часто пишут три значения: общий объем (мл), вес нетто (гр) и сухой вес без рассола (сух/сух./сухой).
   - Если единица измерения в накладной - "шт" (штуки, банки), а базовый учет в iiko всегда в КГ, то коэффициентом перевода (ai_multiplier) должен быть чистый СУХОЙ ВЕС (сух) одной банки в килограммах.
   - Например: "Кукуруза консервир. об425мл-н340гр-сух272гр кор1-12" -> пришел товар в "шт". Чистый вес кукурузы без жижи 272гр. Значит ai_multiplier = 0.272. Игнорируй "кор1-12", так как товар пришел в банках (шт), а не коробках.
   - Если сухого веса "сух" в названии нет, бери вес нетто в кг (н/нетто). Например: "Томаты нетто 400гр" -> ai_multiplier = 0.4.

2. Если указаны граммы для обычных весовых товаров (500 гр, 454гр, 800гр), переведи в кг -> 0.5, 0.454, 0.8.
3. Если указаны литры или килограммы в штучном товаре (Масло 5л, Соус 5.4 кг) -> 5.0, 5.4.
4. ПРАВИЛО РАЗЛИЧИЯ УПАКОВОК И КОРОБОК (КРИТИЧЕСКИ ВАЖНО):
   - Четко различай единицы измерения в накладной: коробка (кор, короб, ящ) и упаковка/пачка/штука (уп, упак, шт, пакет).
   - Если единица измерения в накладной указана как "уп", "упак", "шт" или "пакет", а в названии товара есть фасовка вида "1,5кг кор1-6" — это означает, что товар пришел в индивидуальных упаковках (пачках) по 1,5 кг, а не целыми коробками. В этом случае коэффициент ai_multiplier должен быть равен строго весу одной пачки в кг (т.е. 1.5). НЕ умножай на количество в коробке (6).
   - Умножать количество в коробке на вес пачки нужно ТОЛЬКО тогда, когда единица измерения в самой накладной явно указана как "кор", "коробка" или "ящ".
5. Если товар УЖЕ пришел в весовых единицах (кг, л) и количество дробное (например 3.412 кг), то ai_multiplier = 1.0.
6. Название товара копируй ПОЛНОСТЬЮ, как в документе.

7. СТАВКА НДС (nds_percent):
   Найди для каждой позиции ставку НДС в процентах и верни числом (обычно это 20.0, 10.0 или 0.0). Если указано "без НДС", "0%", "без налога" или поле пустое — возвращай 0.0.

8. ЦЕНА (price) и СУММА (sum):
   Обязательно выгружай цену и итоговую сумму С УЧЕТОМ НДС (Всего с НДС / Сумма к оплате). Это критически важно!

9. Грузополучатель (consignee) и Грузоотправитель (shipper):
   - shipper: Ищи поле "Грузоотправитель и его адрес". Запиши в максимально полном виде.
   - consignee: Ищи поле "Грузополучатель и его адрес" или "Покупатель". Запиши в максимально полном виде.

10. ПРАВИЛО ДЛЯ ЛИСТОВЫХ ТОВАРОВ (Нори и т.д.):
    - Если в названии указано количество листов в пачке (нори 100л), а ед. измерения 'шт', то ai_multiplier = 100.0.

11. ПРАВИЛО ДЛЯ ИНТЕРВАЛЬНЫХ ОБЪЕМОВ И ВЕСОВ:
    - Всегда берите строго верхнюю (максимальную) границу интервала (для 470-505гр -> 0.505).

12. ПОДПИСЬ К ФАСОВКЕ ai_tip (ТЕКСТОВАЯ ПОДСКАЗКА ДЛЯ ЧЕЛОВЕКА):
    Разложи детально в текстовом виде фасовку (например: "1 шт = 5 л").

13. ДАТА ДОКУМЕНТА (doc_date):
    Найди дату составления документа и приведи её к формату YYYY-MM-DD.

14. СУММА БЕЗ НАЛОГА (sum_without_nds):
    Найди стоимость товаров без налога (колонка 5).

15. ПРАВИЛО ДЛЯ ЧАЯ В ПАКЕТИКАХ:
    - ai_multiplier равен количеству пакетиков в упаковке (20.0, 100.0).

16. ПРАВИЛО ДЛЯ ЛИСТА БАМБУКА:
    - ai_multiplier = 100.0.

17. ПРАВИЛО ДЛЯ ГРИБОВ ШИМИДЖИ/ШИМЕДЖИ:
    - ai_multiplier = 0.15 (150 грамм).

18. ПРАВИЛО КОЛОНОК УПД И КОДОВ ОКЕИ (КРИТИЧЕСКИ ВАЖНО):
    - В таблице УПД перед количеством ВСЕГДА идет колонка 2 "Код единицы измерения" (коды 796, 778, 166, 112).
    - 796 — это код штуки (шт)! 778 — код упаковки (упак)! 166 — код кг!
    - КАТЕГОРИЧЕСКИ ЗАПРЕЩЕНО брать числа 796, 778, 166, 112 в качестве количества товара (quantity)!
    - Настоящее количество (quantity) ВСЕГДА находится в колонке 3 "Количество (объем)" (1.000, 2.000, 6.000, 12.000).
    - Сумму с налогом (sum) бери из графы 9.

19. Чистая категория (для аналитики рынка):
    - Выдели чистую категорию товара (clean_category). ВНИМАНИЕ: Категория ДОЛЖНА БЫТЬ СТРОГО одной из следующего списка: "Мясо и птица", "Рыба и морепродукты", "Овощи и фрукты", "Молочные продукты", "Бакалея", "Консервы", "Напитки", "Хозяйственные товары", "Прочее". Если товар не подходит ни под одну, пиши "Без категории".
    - Выведи бренд или производителя (brand), если он есть в названии (пример: "Мираторг", "Hochland", "Borealis"). Если бренда нет, оставь пустую строку "".

20. Игнорируй пометки ручкой, закорючки и прочий визуальный шум на сканах или фото.
21. Если документ обрезан или является только частью накладной (например, нет итоговой суммы), просто извлеки те товары, которые видны на изображении.

ВНИМАНИЕ: ТЕБЕ МОЖЕТ БЫТЬ ПЕРЕДАНО СРАЗУ НЕСКОЛЬКО ИЗОБРАЖЕНИЙ (ИЛИ СТРАНИЦ ТЕКСТА). ЭТО ВСЁ СТРАНИЦЫ ОДНОЙ И ТОЙ ЖЕ НАКЛАДНОЙ. ТЫ ОБЯЗАН ВНИМАТЕЛЬНО ИЗУЧИТЬ АБСОЛЮТНО ВСЕ ПЕРЕДАННЫЕ ИЗОБРАЖЕНИЯ И ИЗВЛЕЧЬ ТОВАРЫ СО ВСЕХ СТРАНИЦ, ОБЪЕДИНИВ ИХ В ОДИН ОБЩИЙ СПИСОК (МАССИВ items)!

КРИТИЧЕСКИ ВАЖНО: Если ты видишь фразы «Итого по странице», «Промежуточный итог» или промежуточные суммы — ИГНОРИРУЙ ИХ! Это не конец накладной! Продолжай парсить товары со следующих страниц.

Верни строго только JSON-объект без markdown и без пояснений:
{
  "vendor_name": "Название поставщика",
  "doc_number": "Номер документа",
  "doc_date": "YYYY-MM-DD",
  "consignee": "Грузополучатель и его адрес или Покупатель",
  "shipper": "Грузоотправитель и его адрес",
  "items": [
    {"name": "Название полностью", "clean_category": "Картофель фри", "brand": "Фритто Аппетито", "quantity": 10.0, "price": 120.0, "sum": 1200.0, "sum_without_nds": 1000.0, "nds_percent": 20.0, "ai_multiplier": 0.55, "ai_tip": "1 шт = 550г"}
  ]
}`

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
