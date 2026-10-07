package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"
	"leads_monster/internal/config"
	"leads_monster/internal/db"
)

// ScraperStatus tracks the active or last scraping job
type ScraperStatus struct {
	IsRunning    bool      `json:"is_running"`
	City         string    `json:"city"`
	Query        string    `json:"query"`
	CurrentPage  int       `json:"current_page"`
	TotalPages   int       `json:"total_pages"`
	ItemsFound   int       `json:"items_found"`
	ItemsSaved   int       `json:"items_saved"`
	ItemsSkipped int       `json:"items_skipped"`
	LastError    string    `json:"last_error"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
}

var (
	GlobalScraperStatus = &ScraperStatus{}
	statusMu            sync.RWMutex
	activeCancel        context.CancelFunc
)

func GetStatus() ScraperStatus {
	statusMu.RLock()
	defer statusMu.RUnlock()
	return *GlobalScraperStatus
}

func updateStatus(fn func(s *ScraperStatus)) {
	statusMu.Lock()
	defer statusMu.Unlock()
	fn(GlobalScraperStatus)
}

// TwoGisAPIResponse models 2GIS Catalog API 3.0 response
type TwoGisAPIResponse struct {
	Meta struct {
		Code  int `json:"code"`
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	} `json:"meta"`
	Result struct {
		Total int         `json:"total"`
		Items []TwoGisItem `json:"items"`
	} `json:"result"`
}

type TwoGisItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AddressName string `json:"address_name"`
	CityAlias   string `json:"city_alias"`
	Org         struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"org"`
	Rubrics []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		ShortName string `json:"short_name"`
	} `json:"rubrics"`
	Reviews struct {
		GeneralRating float64 `json:"general_rating"`
		GeneralReviewCount int `json:"general_review_count"`
	} `json:"reviews"`
	Context struct {
		StopFactors []struct {
			Name string `json:"name"`
		} `json:"stop_factors"`
		Text string `json:"text"`
	} `json:"context"`
	ContactGroups []struct {
		Contacts []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
			Text  string `json:"text"`
		} `json:"contacts"`
	} `json:"contact_groups"`
	Ads struct {
		LegalText string `json:"legal_text"`
		Options   struct {
			Text string `json:"text"`
		} `json:"options"`
	} `json:"ads"`
}

type ScrapeOptions struct {
	City     string
	CityID   string
	Query    string
	APIKey   string
	MaxPages int
	MinBill  float64
}

// StartScraper launches the background crawler
func StartScraper(ctx context.Context, database *db.DB, cfg *config.Config, opts ScrapeOptions) error {
	statusMu.Lock()
	if GlobalScraperStatus.IsRunning {
		statusMu.Unlock()
		return fmt.Errorf("парсер уже запущен")
	}

	subCtx, cancel := context.WithCancel(ctx)
	activeCancel = cancel

	GlobalScraperStatus = &ScraperStatus{
		IsRunning:  true,
		City:       opts.City,
		Query:      opts.Query,
		StartedAt:  time.Now(),
		TotalPages: opts.MaxPages,
	}
	statusMu.Unlock()

	go runScraper(subCtx, database, cfg, opts)
	return nil
}

// StopScraper cancels active crawler
func StopScraper() {
	statusMu.Lock()
	defer statusMu.Unlock()
	if activeCancel != nil {
		activeCancel()
	}
	GlobalScraperStatus.IsRunning = false
	GlobalScraperStatus.FinishedAt = time.Now()
}

func runScraper(ctx context.Context, database *db.DB, cfg *config.Config, opts ScrapeOptions) {
	defer func() {
		updateStatus(func(s *ScraperStatus) {
			s.IsRunning = false
			s.FinishedAt = time.Now()
		})
	}()

	apiKey := opts.APIKey
	if apiKey == "" {
		apiKey = cfg.TwoGisAPIKey
	}
	city := opts.City
	if city == "" {
		city = cfg.TwoGisDefaultCity
	}
	cityID := opts.CityID
	if cityID == "" {
		cityID = cfg.TwoGisDefaultCityID
	}
	query := opts.Query
	if query == "" {
		query = "ресторан, бар, гастробар, пиццерия, кафе"
	}
	maxPages := opts.MaxPages
	if maxPages <= 0 {
		maxPages = 20 // Default reasonable limit
	}

	client := &http.Client{
		Timeout: 20 * time.Second,
	}

	// Split comma-separated search terms if multiple specified
	queries := strings.Split(query, ",")
	for i := range queries {
		queries[i] = strings.TrimSpace(queries[i])
	}

	for _, singleQuery := range queries {
		if singleQuery == "" {
			continue
		}

		page := 1
		for page <= maxPages {
			select {
			case <-ctx.Done():
				log.Println("🛑 [Scraper] Парсинг прерван пользователем")
				return
			default:
			}

			updateStatus(func(s *ScraperStatus) {
				s.CurrentPage = page
				s.Query = singleQuery
			})

			log.Printf("🔍 [Scraper] Запрос страницы %d для '%s' (город: %s)...", page, singleQuery, city)

			var items []TwoGisItem
			var totalItems int
			var err error

			cityAlias := resolveCityAlias(city)

			// 1. If key is default blocked key or empty, use high-speed Direct Web SSR Scraper
			if apiKey == "" || apiKey == "rurbbn3446" {
				items, totalItems, err = fetch2GISWebPage(ctx, client, cityAlias, singleQuery, page)
				if err != nil {
					log.Printf("⚠️ [Scraper] Web парсер вернул ошибку: %v", err)
				}
			} else {
				// User provided custom commercial key, try official API first
				items, totalItems, err = fetch2GISPage(ctx, client, cfg, apiKey, cityID, singleQuery, page, 50)
				if err != nil && (strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "blocked")) {
					log.Printf("⚠️ [Scraper] API ключ вернул 403, переключение на Web парсер: %v", err)
					items, totalItems, err = fetch2GISWebPage(ctx, client, cityAlias, singleQuery, page)
				}
			}

			if err != nil {
				log.Printf("⚠️ [Scraper] Ошибка загрузки страницы %d: %v", page, err)
				updateStatus(func(s *ScraperStatus) {
					s.LastError = fmt.Sprintf("Ошибка стр. %d: %v", page, err)
				})
				break
			}

			if len(items) == 0 {
				log.Printf("ℹ️ [Scraper] Получен пустой список на странице %d, завершение для '%s'", page, singleQuery)
				break
			}

			updateStatus(func(s *ScraperStatus) {
				s.ItemsFound += len(items)
			})

			// Process and save leads
			savedCount := 0
			for _, item := range items {
				select {
				case <-ctx.Done():
					return
				default:
				}

				// Enrich firm details (legal name, phone numbers, vk, tg)
				enrichFirmDetails(ctx, client, cityAlias, item.ID, &item)

				saved, err := processAndSaveLead(database, item, city, opts.MinBill)
				if err != nil {
					log.Printf("⚠️ [Scraper] Ошибка сохранения заведения %s (%s): %v", item.Name, item.ID, err)
				} else if saved {
					savedCount++
				}
				// Small jitter between firm enrichment requests
				time.Sleep(100 * time.Millisecond)
			}

			updateStatus(func(s *ScraperStatus) {
				s.ItemsSaved += savedCount
			})

			// If we reached the end based on total
			if totalItems > 0 && page*12 >= totalItems {
				break
			}

			page++

			// Polite crawl jitter delay
			jitter := time.Duration(800+rand.Intn(400)) * time.Millisecond
			time.Sleep(jitter)
		}
	}

	log.Printf("🎉 [Scraper] Парсинг успешно завершен! Найдено: %d, Сохранено: %d",
		GetStatus().ItemsFound, GetStatus().ItemsSaved)
}

func fetch2GISPage(ctx context.Context, client *http.Client, cfg *config.Config, apiKey, cityID, query string, page, pageSize int) ([]TwoGisItem, int, error) {
	apiURL := cfg.TwoGisAPIURL
	if apiURL == "" {
		apiURL = "https://catalog.api.2gis.com/3.0/items"
	}

	u, err := url.Parse(apiURL)
	if err != nil {
		return nil, 0, err
	}

	q := u.Query()
	q.Set("q", query)
	q.Set("key", apiKey)
	if cityID != "" {
		q.Set("city_id", cityID)
	}
	q.Set("page", fmt.Sprintf("%d", page))
	q.Set("page_size", fmt.Sprintf("%d", pageSize))
	q.Set("fields", "items.contact_groups,items.org,items.rubrics,items.schedule,items.flags,items.point,items.reviews,items.context,items.ads")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}

	var parsed TwoGisAPIResponse
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		return nil, 0, fmt.Errorf("JSON parse error: %w", err)
	}

	if parsed.Meta.Code != 200 && parsed.Meta.Code != 0 {
		return nil, 0, fmt.Errorf("2GIS API error %d: %s (%s)", parsed.Meta.Code, parsed.Meta.Error.Message, parsed.Meta.Error.Type)
	}

	return parsed.Result.Items, parsed.Result.Total, nil
}

func processAndSaveLead(database *db.DB, item TwoGisItem, city string, minBill float64) (bool, error) {
	if item.ID == "" || item.Name == "" {
		return false, nil
	}

	// 1. Legal Entity Extraction
	legalRaw := item.Ads.LegalText
	if legalRaw == "" {
		legalRaw = item.Org.Name
	}
	legalName, legalType := ExtractLegalInfo(legalRaw)

	// 2. Rusprofile / Counterparty URL
	var rusprofileURL string
	if legalName != "" && legalType != "NONE" {
		rusprofileURL = ResolveRusprofileURL(legalName)
	}

	// 3. Average Bill Parsing
	billRaw, billVal := ParseAverageBill(item.Context.Text)
	if billRaw == "" {
		for _, sf := range item.Context.StopFactors {
			if r, v := ParseAverageBill(sf.Name); r != "" {
				billRaw, billVal = r, v
				break
			}
		}
	}

	// Filter by min bill if specified (> 0)
	if minBill > 0 && billVal > 0 && billVal < minBill {
		return false, nil
	}

	// 4. Rubrics
	var rubrics []string
	for _, r := range item.Rubrics {
		name := strings.TrimSpace(r.Name)
		if name != "" {
			rubrics = append(rubrics, name)
		}
	}

	// 5. Contacts (phones, website, vk, tg)
	var phones []string
	var website, vkURL, tgURL string

	for _, cg := range item.ContactGroups {
		for _, c := range cg.Contacts {
			val := strings.TrimSpace(c.Value)
			switch c.Type {
			case "phone":
				phones = append(phones, NormalizePhone(val))
			case "website":
				if website == "" {
					website = CleanSocialURL(val)
				}
			case "vkontakte":
				if vkURL == "" {
					vkURL = CleanSocialURL(val)
				}
			case "telegram":
				if tgURL == "" {
					tgURL = CleanSocialURL(val)
				}
			}
			// Fallback check in value URL
			if strings.Contains(val, "vk.com") && vkURL == "" {
				vkURL = CleanSocialURL(val)
			} else if strings.Contains(val, "t.me") && tgURL == "" {
				tgURL = CleanSocialURL(val)
			}
		}
	}

	// 6. Rating & Reviews
	rating := item.Reviews.GeneralRating
	reviewsCount := item.Reviews.GeneralReviewCount

	// 7. 2GIS URL
	cityAlias := item.CityAlias
	if cityAlias == "" {
		cityAlias = resolveCityAlias(city)
	}
	twoGisURL := fmt.Sprintf("https://2gis.ru/%s/firm/%s", cityAlias, item.ID)

	// 8. Priority Calculation
	priority := CalculatePriority(billVal, reviewsCount)

	address := strings.TrimSpace(item.AddressName)
	if address == "" {
		address = city
	}

	// UPSERT into leads_restaurants
	query := `
		INSERT INTO leads_restaurants (
			two_gis_id, name, legal_name, legal_type, address, city, rubrics,
			avg_bill_raw, avg_bill_val, phones, website, vk_url, tg_url,
			rating, reviews_count, two_gis_url, rusprofile_url, priority, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18, NOW()
		)
		ON CONFLICT (two_gis_id) DO UPDATE SET
			name = EXCLUDED.name,
			legal_name = EXCLUDED.legal_name,
			legal_type = EXCLUDED.legal_type,
			address = EXCLUDED.address,
			city = EXCLUDED.city,
			rubrics = EXCLUDED.rubrics,
			avg_bill_raw = EXCLUDED.avg_bill_raw,
			avg_bill_val = EXCLUDED.avg_bill_val,
			phones = EXCLUDED.phones,
			website = EXCLUDED.website,
			vk_url = EXCLUDED.vk_url,
			tg_url = EXCLUDED.tg_url,
			rating = EXCLUDED.rating,
			reviews_count = EXCLUDED.reviews_count,
			two_gis_url = EXCLUDED.two_gis_url,
			rusprofile_url = EXCLUDED.rusprofile_url,
			priority = EXCLUDED.priority,
			updated_at = NOW();
	`

	_, err := database.Exec(query,
		item.ID, item.Name, legalName, legalType, address, city, pq.Array(rubrics),
		billRaw, billVal, pq.Array(phones), website, vkURL, tgURL,
		rating, reviewsCount, twoGisURL, rusprofileURL, priority,
	)

	return err == nil, err
}

// IngestRawLead allows manual or mock insertion of lead items
func IngestRawLead(database *db.DB, item TwoGisItem, city string, minBill float64) (bool, error) {
	return processAndSaveLead(database, item, city, minBill)
}

func resolveCityAlias(city string) string {
	lower := strings.ToLower(strings.TrimSpace(city))
	switch {
	case strings.Contains(lower, "перм"):
		return "perm"
	case strings.Contains(lower, "москв"):
		return "moscow"
	case strings.Contains(lower, "петербург") || strings.Contains(lower, "спб"):
		return "spb"
	case strings.Contains(lower, "екатеринбург"):
		return "ekaterinburg"
	case strings.Contains(lower, "казан"):
		return "kazan"
	case strings.Contains(lower, "новосибирск"):
		return "novosibirsk"
	case strings.Contains(lower, "сочи"):
		return "sochi"
	case strings.Contains(lower, "уф"):
		return "ufa"
	case strings.Contains(lower, "самара"):
		return "samara"
	case strings.Contains(lower, "нижний"):
		return "nizhny_novgorod"
	case strings.Contains(lower, "челябинск"):
		return "chelyabinsk"
	case strings.Contains(lower, "краснодар"):
		return "krasnodar"
	case strings.Contains(lower, "красноярск"):
		return "krasnoyarsk"
	case strings.Contains(lower, "ростов"):
		return "rostov_na_donu"
	case strings.Contains(lower, "воронеж"):
		return "voronezh"
	case strings.Contains(lower, "волгоград"):
		return "volgograd"
	case strings.Contains(lower, "омск"):
		return "omsk"
	case strings.Contains(lower, "тюмен"):
		return "tyumen"
	default:
		if lower != "" && !strings.Contains(lower, " ") {
			return lower
		}
		return "perm"
	}
}

func fetch2GISWebPage(ctx context.Context, client *http.Client, cityAlias, query string, page int) ([]TwoGisItem, int, error) {
	var searchURL string
	if page <= 1 {
		searchURL = fmt.Sprintf("https://2gis.ru/%s/search/%s", cityAlias, url.PathEscape(query))
	} else {
		searchURL = fmt.Sprintf("https://2gis.ru/%s/search/%s/page/%d", cityAlias, url.PathEscape(query), page)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Cookie", "dg5_museum_accept=true")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	bodyStr := string(body)

	prefix := "var initialState = JSON.parse('"
	startIdx := strings.Index(bodyStr, prefix)
	if startIdx == -1 {
		return nil, 0, fmt.Errorf("initialState not found in response HTML")
	}
	startIdx += len(prefix)
	endIdx := strings.Index(bodyStr[startIdx:], "');")
	if endIdx == -1 {
		return nil, 0, fmt.Errorf("end of initialState not found")
	}

	rawJSON := bodyStr[startIdx : startIdx+endIdx]
	rawJSON = strings.ReplaceAll(rawJSON, `\'`, `'`)
	rawJSON = strings.ReplaceAll(rawJSON, `\\`, `\`)

	var state map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &state); err != nil {
		return nil, 0, fmt.Errorf("json unmarshal error: %w", err)
	}

	data, _ := state["data"].(map[string]interface{})
	if data == nil {
		return nil, 0, fmt.Errorf("data field missing in state")
	}

	totalItems := 0
	if search, ok := data["search"].(map[string]interface{}); ok {
		if sp, ok := search["profile"].(map[string]interface{}); ok {
			for _, val := range sp {
				if vMap, ok := val.(map[string]interface{}); ok {
					if dMap, ok := vMap["data"].(map[string]interface{}); ok {
						if t, ok := dMap["total"].(float64); ok {
							totalItems = int(t)
							break
						}
					}
				}
			}
		}
	}

	entity, _ := data["entity"].(map[string]interface{})
	if entity == nil {
		return nil, totalItems, nil
	}
	profiles, _ := entity["profile"].(map[string]interface{})

	var items []TwoGisItem
	for id, val := range profiles {
		pMap, _ := val.(map[string]interface{})
		d, _ := pMap["data"].(map[string]interface{})
		if d == nil {
			continue
		}

		item := TwoGisItem{
			ID:        id,
			CityAlias: cityAlias,
		}
		item.Name, _ = d["name"].(string)
		item.AddressName, _ = d["address_name"].(string)

		if rubricsList, ok := d["rubrics"].([]interface{}); ok {
			for _, r := range rubricsList {
				if rMap, ok := r.(map[string]interface{}); ok {
					if rName, ok := rMap["name"].(string); ok && rName != "" {
						item.Rubrics = append(item.Rubrics, struct {
							ID        string `json:"id"`
							Name      string `json:"name"`
							ShortName string `json:"short_name"`
						}{Name: rName})
					}
				}
			}
		}

		if revs, ok := d["reviews"].(map[string]interface{}); ok {
			if r, ok := revs["general_rating"].(float64); ok {
				item.Reviews.GeneralRating = r
			}
			if c, ok := revs["general_review_count"].(float64); ok {
				item.Reviews.GeneralReviewCount = int(c)
			}
		}

		// Average bill from attribute_groups
		if attrs, ok := d["attribute_groups"].([]interface{}); ok {
			for _, ag := range attrs {
				agMap, _ := ag.(map[string]interface{})
				if attrList, ok := agMap["attributes"].([]interface{}); ok {
					for _, a := range attrList {
						aMap, _ := a.(map[string]interface{})
						aName, _ := aMap["name"].(string)
						if strings.Contains(aName, "Чек ") || strings.Contains(aName, "₽") {
							item.Context.Text = aName
						}
					}
				}
			}
		}

		// Legal name from ads
		if ads, ok := d["ads"].(map[string]interface{}); ok {
			if opt, ok := ads["options"].(map[string]interface{}); ok {
				if ln, ok := opt["legal_name"].(string); ok {
					item.Ads.LegalText = ln
				}
			}
		}
		if org, ok := d["org"].(map[string]interface{}); ok {
			orgName, _ := org["name"].(string)
			upperOrg := strings.ToUpper(orgName)
			if strings.Contains(upperOrg, "ООО") || strings.Contains(upperOrg, "ИП") {
				item.Org.Name = orgName
			}
		}

		items = append(items, item)
	}

	return items, totalItems, nil
}

func enrichFirmDetails(ctx context.Context, client *http.Client, cityAlias, firmID string, item *TwoGisItem) {
	firmURL := fmt.Sprintf("https://2gis.ru/%s/firm/%s", cityAlias, firmID)
	req, err := http.NewRequestWithContext(ctx, "GET", firmURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Cookie", "dg5_museum_accept=true")

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	bodyStr := string(body)

	prefix := "var initialState = JSON.parse('"
	startIdx := strings.Index(bodyStr, prefix)
	if startIdx == -1 {
		return
	}
	startIdx += len(prefix)
	endIdx := strings.Index(bodyStr[startIdx:], "');")
	if endIdx == -1 {
		return
	}

	rawJSON := bodyStr[startIdx : startIdx+endIdx]
	rawJSON = strings.ReplaceAll(rawJSON, `\'`, `'`)
	rawJSON = strings.ReplaceAll(rawJSON, `\\`, `\`)

	// Extract legal_name STRICTLY for this firmID (never touch data.gta third-party banners)
	var state map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &state); err == nil {
		if data, ok := state["data"].(map[string]interface{}); ok {
			// 1. Check searchContext matching this firmID
			if sc, ok := data["searchContext"].(map[string]interface{}); ok {
				for k, v := range sc {
					if strings.Contains(k, firmID) {
						if vMap, ok := v.(map[string]interface{}); ok {
							if ads, ok := vMap["ads"].(map[string]interface{}); ok {
								if opt, ok := ads["options"].(map[string]interface{}); ok {
									if ln, ok := opt["legal_name"].(string); ok && ln != "" {
										item.Ads.LegalText = ln
										break
									}
								}
								if ln, ok := ads["legal_name"].(string); ok && ln != "" {
									item.Ads.LegalText = ln
									break
								}
							}
						}
					}
				}
			}

			// 2. Check entity.profile[firmID].data.ads
			if item.Ads.LegalText == "" {
				if entity, ok := data["entity"].(map[string]interface{}); ok {
					if profs, ok := entity["profile"].(map[string]interface{}); ok {
						if pVal, ok := profs[firmID].(map[string]interface{}); ok {
							if pData, ok := pVal["data"].(map[string]interface{}); ok {
								if ads, ok := pData["ads"].(map[string]interface{}); ok {
									if opt, ok := ads["options"].(map[string]interface{}); ok {
										if ln, ok := opt["legal_name"].(string); ok && ln != "" {
											item.Ads.LegalText = ln
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// Extract phones
	rePhone := regexp.MustCompile(`"type"\s*:\s*"phone"\s*,\s*"value"\s*:\s*"([^"]+)"`)
	phones := rePhone.FindAllStringSubmatch(rawJSON, -1)
	if len(phones) > 0 {
		var contacts []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
			Text  string `json:"text"`
		}
		for _, p := range phones {
			contacts = append(contacts, struct {
				Type  string `json:"type"`
				Value string `json:"value"`
				Text  string `json:"text"`
			}{Type: "phone", Value: p[1]})
		}

		// Also check VK & TG
		reVK := regexp.MustCompile(`https?://vk\.com/[a-zA-Z0-9_\.]+`)
		if m := reVK.FindString(rawJSON); m != "" {
			contacts = append(contacts, struct {
				Type  string `json:"type"`
				Value string `json:"value"`
				Text  string `json:"text"`
			}{Type: "vkontakte", Value: m})
		}
		reTG := regexp.MustCompile(`https?://t\.me/[a-zA-Z0-9_\.]+`)
		if m := reTG.FindString(rawJSON); m != "" {
			contacts = append(contacts, struct {
				Type  string `json:"type"`
				Value string `json:"value"`
				Text  string `json:"text"`
			}{Type: "telegram", Value: m})
		}

		item.ContactGroups = append(item.ContactGroups, struct {
			Contacts []struct {
				Type  string `json:"type"`
				Value string `json:"value"`
				Text  string `json:"text"`
			} `json:"contacts"`
		}{Contacts: contacts})
	}

	// Extract average check if not already present
	if item.Context.Text == "" {
		reBill := regexp.MustCompile(`(Чек\s+[0-9\s]+₽|Ланч\s+от\s+[0-9\s]+₽)`)
		if m := reBill.FindString(rawJSON); m != "" {
			item.Context.Text = m
		}
	}
}
