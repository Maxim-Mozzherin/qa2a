package scraper

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var (
	// Regex to extract bill numbers like "Чек 1 600 ₽", "1600 ₽", "Чек 700-1000 ₽"
	billRegex = regexp.MustCompile(`(?i)(?:чек|средний чек)?\s*([0-9\s\x{00A0}]+)\s*(?:₽|руб)`)
	digitsOnlyRegex = regexp.MustCompile(`\D+`)
)

// ExtractLegalInfo analyzes text from 2GIS for genuine legal entities (ООО, ИП, АО)
func ExtractLegalInfo(raw string) (legalName string, legalType string) {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return "", "NONE"
	}

	// Remove common prefixes/tails like ads disclaimer
	cleaned = strings.ReplaceAll(cleaned, `\"`, `"`)
	cleaned = strings.Trim(cleaned, " .,-")

	upper := strings.ToUpper(cleaned)

	// 1. Check for ООО
	if strings.Contains(upper, "ООО") || strings.Contains(upper, "ОБЩЕСТВО С ОГРАНИЧЕННОЙ") {
		legalType = "OOO"
		// If starts with "Общество с ограниченной ответственностью", normalize prefix
		reOOO := regexp.MustCompile(`(?i)Общество\s+с\s+ограниченной\s+ответственностью\s*`)
		if reOOO.MatchString(cleaned) {
			cleaned = reOOO.ReplaceAllString(cleaned, "ООО ")
		}
		// Clean quotes
		cleaned = cleanLegalName(cleaned)
		return cleaned, legalType
	}

	// 2. Check for explicit ИП
	if strings.Contains(upper, "ИП ") || strings.HasPrefix(upper, "ИП") || strings.Contains(upper, "ИНДИВИДУАЛЬНЫЙ ПРЕДПРИНИМАТЕЛЬ") {
		legalType = "IP"
		reIP := regexp.MustCompile(`(?i)Индивидуальный\s+предприниматель\s*`)
		if reIP.MatchString(cleaned) {
			cleaned = reIP.ReplaceAllString(cleaned, "ИП ")
		}
		if !strings.HasPrefix(strings.ToUpper(cleaned), "ИП") {
			cleaned = "ИП " + cleaned
		}
		cleaned = cleanLegalName(cleaned)
		return cleaned, legalType
	}

	// 3. Check for АО / ПАО / ЗАО
	if strings.Contains(upper, "ЗАО ") || strings.Contains(upper, "ПАО ") || strings.HasPrefix(upper, "АО ") || strings.Contains(upper, " АО ") || strings.Contains(upper, "АКЦИОНЕРНОЕ ОБЩЕСТВО") {
		legalType = "AO"
		cleaned = cleanLegalName(cleaned)
		return cleaned, legalType
	}

	// 4. Check if raw is a 3-word Russian personal name (Фамилия Имя Отчество), which is 2GIS's format for ИП
	words := strings.Fields(cleaned)
	if len(words) == 3 && isRussianPersonFullName(words) {
		legalType = "IP"
		cleaned = "ИП " + cleanLegalName(cleaned)
		return cleaned, legalType
	}

	// Not a legal entity! Do NOT pollute with restaurant trade names.
	return "", "NONE"
}

func isRussianPersonFullName(words []string) bool {
	for _, w := range words {
		// Strip punctuation
		wClean := strings.Trim(w, ".,«»\"'")
		runes := []rune(wClean)
		if len(runes) < 2 {
			return false
		}
		// Must start with uppercase
		if !unicode.IsUpper(runes[0]) {
			return false
		}
		// All characters must be Cyrillic
		for _, r := range runes {
			if !unicode.In(r, unicode.Cyrillic) {
				return false
			}
		}
	}
	return true
}

func cleanLegalName(s string) string {
	s = strings.ReplaceAll(s, "«", `"`)
	s = strings.ReplaceAll(s, "»", `"`)
	s = strings.Trim(s, " .,-")
	return s
}

// ResolveRusprofileURL queries Rusprofile search API to retrieve exact direct dossier URL
func ResolveRusprofileURL(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return ""
	}

	// Strip "ИП " or "ООО " for broader matching if query is complex
	cleanQuery := strings.TrimPrefix(q, "ИП ")
	cleanQuery = strings.TrimPrefix(cleanQuery, "ООО ")
	cleanQuery = strings.ReplaceAll(cleanQuery, `"`, "")

	client := &http.Client{Timeout: 3 * time.Second}
	apiURL := "https://www.rusprofile.ru/ajax.php?action=search&query=" + url.QueryEscape(cleanQuery)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err == nil {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")

		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == 200 {
			defer resp.Body.Close()
			var data struct {
				UL []struct {
					Link string `json:"link"`
					OGRN string `json:"ogrn"`
				} `json:"ul"`
				IP []struct {
					Link   string `json:"link"`
					OGRNIP string `json:"ogrnip"`
				} `json:"ip"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
				if len(data.UL) > 0 && data.UL[0].Link != "" {
					return "https://www.rusprofile.ru" + data.UL[0].Link
				}
				if len(data.IP) > 0 && data.IP[0].Link != "" {
					return "https://www.rusprofile.ru" + data.IP[0].Link
				}
			}
		}
	}

	// Reliable fallback: Checko counterparty search which never 404s and directly resolves
	return "https://checko.ru/search?extra=1&query=" + url.QueryEscape(q)
}

// GenerateRusprofileURL creates verified search URL
func GenerateRusprofileURL(legalName string) string {
	if legalName == "" {
		return ""
	}
	// Default to Checko search URL for instant safe resolution
	return "https://checko.ru/search?extra=1&query=" + url.QueryEscape(legalName)
}

// ParseAverageBill extracts numerical value and raw string from text / context
func ParseAverageBill(text string) (raw string, val float64) {
	if text == "" {
		return "", 0.0
	}

	// Search for patterns like "Чек 1 600 ₽" or "Чек 800 ₽"
	matches := billRegex.FindStringSubmatch(text)
	if len(matches) > 1 {
		raw = strings.TrimSpace(matches[0])
		digits := strings.ReplaceAll(matches[1], " ", "")
		digits = strings.ReplaceAll(digits, "\u00a0", "") // non-breaking space
		if num, err := strconv.ParseFloat(digits, 64); err == nil {
			return raw, num
		}
	}

	// Secondary check: look for isolated numbers preceding ₽
	sub := regexp.MustCompile(`([0-9\s\x{00A0}]+)\s*₽`).FindStringSubmatch(text)
	if len(sub) > 1 {
		digits := strings.ReplaceAll(sub[1], " ", "")
		digits = strings.ReplaceAll(digits, "\u00a0", "")
		if num, err := strconv.ParseFloat(digits, 64); err == nil && num >= 100 {
			return strings.TrimSpace(sub[0]), num
		}
	}

	return "", 0.0
}

// CalculatePriority computes 'high', 'medium', or 'low' priority
func CalculatePriority(avgBill float64, reviewsCount int) string {
	if avgBill > 1200 && reviewsCount > 80 {
		return "high"
	}
	if avgBill >= 700 || reviewsCount > 30 {
		return "medium"
	}
	return "low"
}

// NormalizePhone standardizes phone numbers into Russian format
func NormalizePhone(phone string) string {
	digits := digitsOnlyRegex.ReplaceAllString(phone, "")
	if len(digits) == 11 && (digits[0] == '7' || digits[0] == '8') {
		return "+7 (" + digits[1:4] + ") " + digits[4:7] + "-" + digits[7:9] + "-" + digits[9:11]
	}
	if len(digits) == 10 {
		return "+7 (" + digits[0:3] + ") " + digits[3:6] + "-" + digits[6:8] + "-" + digits[8:10]
	}
	return phone
}

// CleanSocialURL cleans VK, TG, and website links
func CleanSocialURL(rawURL string) string {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return ""
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "https://" + u
	}
	return u
}
