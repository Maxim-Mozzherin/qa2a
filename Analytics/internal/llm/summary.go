package llm

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// AuditorPayload is the strict, pre-aggregated financial input passed to the LLM
type AuditorPayload struct {
	RestaurantName            string   `json:"restaurant_name"`
	City                      string   `json:"city"`
	AnalysisPeriodDays        int      `json:"analysis_period_days"`
	MonthlyPurchasesRub       float64  `json:"monthly_purchases_rub"`
	TotalMonthlyOverpayRub      float64  `json:"total_monthly_overpay_rub"`
	TotalMonthlyOverpayVsMinRub float64  `json:"total_monthly_overpay_vs_min_rub,omitempty"`
	OverpayBudgetPercent       float64  `json:"overpay_budget_percent"`
	SupplierHHI               float64  `json:"supplier_hhi"`
	SupplierHHIStatus         string   `json:"supplier_hhi_status"`
	TopOverpaidCategories     []string `json:"top_overpaid_categories"`
	OffContractSpendMonthlyRub float64  `json:"off_contract_spend_monthly_rub"`
	OffContractSpendSharePct   float64  `json:"off_contract_spend_share_pct"`
}

type Client struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

func NewClient(baseURL, apiKey, model string) *Client {
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"
	}
	if model == "" {
		model = "gemini-2.5-flash"
	}
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
		HTTPClient: &http.Client{
			Timeout: 25 * time.Second,
		},
	}
}

const systemPrompt = `You are a Principal Financial Auditor in commercial hospitality (HoReCa).
Your task is to write a strictly concise, diplomatic, and factual executive summary (10 to 12 lines) based solely on the provided JSON data.
STRICT CONSTRAINTS:
1. Do NOT invent numbers or percentages not present in the payload.
2. STRICTLY FORBIDDEN: Do NOT mention or name any specific supplier or vendor company (NEVER mention company names or legal entities). Always use generalized corporate terms like "ключевые контрагенты", "пул основных поставщиков", "профильные дистрибьюторы".
3. Do NOT use aggressive or informal words ("претензия", "обман", "налог на лень", "штраф", "откат").
4. Use corporate, constructive, and diplomatic language ("актуализировать", "рассмотреть возможность пересмотра", "оптимизировать").
5. Strictly follow the 3-section layout below.

OUTPUT FORMAT:
1. РЕЗЮМЕ АУДИТА:
[2-3 lines summarizing total loss, budget percentage, top loss categories, and HHI monopolization status]

2. ФАКТОРЫ ФОРМИРОВАНИЯ РАЗРЫВА:
- Отклонение контрактных цен от среднерыночных ориентиров по регулярным сырьевым позициям.
- Объем оперативных внеконтрактных (розничных) закупок составляет [X] ₽/мес ([Y]% бюджета), что формирует дополнительную издержку за счет розничной наценки.
- Фиксация ценового спреда внутри пула регулярных поставщиков по отношению к минимальным рыночным значениям когорты.

3. РЕКОМЕНДАЦИИ ПО ОПТИМИЗАЦИИ:
- Провести плановую актуализацию коммерческих условий и объемных спецификаций с ключевыми поставщиками.
- Оптимизировать минимальные складские остатки по критическим позициям для минимизации розничных закупок.
- Рассмотреть целевую диверсификацию закупок в категориях [Top Categories] для выравнивания закупочных цен с медианой рынка.`

// GenerateSummary queries the LLM API with deterministic temperature (0.1)
func (c *Client) GenerateSummary(ctx context.Context, payload AuditorPayload) (string, error) {
	if c.APIKey == "" {
		log.Printf("⚠️ [AI Auditor] API key missing, generating deterministic fallback narrative")
		return generateDeterministicFallback(payload), nil
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	reqBody := map[string]interface{}{
		"model":       c.Model,
		"temperature": 0.1,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": fmt.Sprintf("Данные аудита для формирования заключения:\n%s", string(payloadBytes))},
		},
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	url := c.BaseURL
	// If query params are needed (e.g. for Google Gemini direct API key in query if not using Bearer)
	if strings.Contains(url, "generativelanguage.googleapis.com") && !strings.Contains(url, "key=") {
		if strings.Contains(url, "?") {
			url += "&key=" + c.APIKey
		} else {
			url += "?key=" + c.APIKey
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBytes))
	if err != nil {
		return "", err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		log.Printf("⚠️ [AI Auditor] HTTP request failed: %v, using deterministic fallback", err)
		return generateDeterministicFallback(payload), nil
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return generateDeterministicFallback(payload), nil
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("⚠️ [AI Auditor] LLM API returned status %d: %s. Using deterministic fallback.", resp.StatusCode, string(bodyBytes))
		return generateDeterministicFallback(payload), nil
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(bodyBytes, &chatResp); err != nil || len(chatResp.Choices) == 0 {
		log.Printf("⚠️ [AI Auditor] Failed to decode LLM response: %v, using fallback", err)
		return generateDeterministicFallback(payload), nil
	}

	summary := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	if summary == "" {
		return generateDeterministicFallback(payload), nil
	}

	return summary, nil
}

// GetOrGenerateSummary retrieves a cached summary within 24h or generates a fresh one
func GetOrGenerateSummary(ctx context.Context, db *sql.DB, client *Client, payload AuditorPayload, restID, periodDays int) (string, error) {
	// 1. Check cache
	var cachedSummary string
	var createdAt time.Time
	query := `SELECT summary_text, created_at FROM analytics_audit_summaries WHERE restaurant_id = $1 AND period_days = $2`
	err := db.QueryRowContext(ctx, query, restID, periodDays).Scan(&cachedSummary, &createdAt)
	if err == nil && cachedSummary != "" && time.Since(createdAt) < 24*time.Hour {
		return cachedSummary, nil
	}

	// 2. Generate new summary
	summary, err := client.GenerateSummary(ctx, payload)
	if err != nil {
		summary = generateDeterministicFallback(payload)
	}

	// 3. Persist to cache
	upsertQuery := `
		INSERT INTO analytics_audit_summaries (restaurant_id, period_days, summary_text, created_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (restaurant_id, period_days)
		DO UPDATE SET summary_text = EXCLUDED.summary_text, created_at = NOW()`
	_, _ = db.ExecContext(ctx, upsertQuery, restID, periodDays, summary)

	return summary, nil
}

// generateDeterministicFallback builds a 100% accurate, zero-hallucination narrative strictly from the payload
func generateDeterministicFallback(p AuditorPayload) string {
	topCatsStr := strings.Join(p.TopOverpaidCategories, ", ")
	if topCatsStr == "" {
		topCatsStr = "основным товарным группам"
	}

	minExtra := ""
	if p.TotalMonthlyOverpayVsMinRub > p.TotalMonthlyOverpayRub {
		minExtra = fmt.Sprintf(", с потенциалом оптимизации до %.0f ₽ к минимуму когорты", p.TotalMonthlyOverpayVsMinRub)
	}

	return fmt.Sprintf(`1. РЕЗЮМЕ АУДИТА:
По результатам финансового анализа закупок заведения «%s» (%s) за %d дней выявлен расчетный резерв оптимизации в размере %.0f ₽ в месяц к средней рынка (%.1f%% от общего бюджета закупок%s). Основная концентрация ценового разрыва локализована в категориях: %s. Индекс концентрации HHI составляет %.0f (%s).

2. ФАКТОРЫ ФОРМИРОВАНИЯ РАЗРЫВА:
- Отклонение контрактных цен от среднерыночных ориентиров по регулярным сырьевым позициям.
- Объем оперативных внеконтрактных (розничных) закупок составляет %.0f ₽/мес (%.1f%% бюджета), что формирует дополнительную издержку за счет розничной наценки.
- Фиксация ценового спреда внутри пула регулярных поставщиков по отношению к минимальным рыночным значениям когорты.

3. РЕКОМЕНДАЦИИ ПО ОПТИМИЗАЦИИ:
- Провести плановую актуализацию коммерческих условий и объемных спецификаций с ключевыми поставщиками.
- Оптимизировать минимальные складские остатки по критическим позициям для минимизации розничных закупок.
- Рассмотреть целевую диверсификацию закупок в категориях %s для выравнивания закупочных цен с медианой рынка.`,
		p.RestaurantName, p.City, p.AnalysisPeriodDays, p.TotalMonthlyOverpayRub, p.OverpayBudgetPercent, minExtra, topCatsStr, p.SupplierHHI, p.SupplierHHIStatus,
		p.OffContractSpendMonthlyRub, p.OffContractSpendSharePct,
		topCatsStr,
	)
}
