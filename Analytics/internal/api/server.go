package api

import (
	"database/sql"
	"strings"

	"analytics_service/internal/config"
	"analytics_service/internal/iiko"
	"analytics_service/internal/llm"
)

// Server инкапсулирует зависимости HTTP-сервера аналитики: подключения к БД, конфигурацию и клиенты внешних API
type Server struct {
	db         *sql.DB
	cfg        *config.Config
	iikoClient *iiko.Client
	llmClient  *llm.Client
}

// NewServer создает экземпляр API-сервера и инициирует фоновые задачи нормализации номенклатуры
func NewServer(db *sql.DB, cfg *config.Config) *Server {
	srv := &Server{
		db:         db,
		cfg:        cfg,
		iikoClient: iiko.NewClient(),
		llmClient:  llm.NewClient(cfg.AIBaseURL, cfg.AIApiKey, cfg.AIModel),
	}
	// Фоновая реклассификация всех позиций при старте сервера с новой таксономией
	go srv.ReclassifyAllItems()
	return srv
}

// cleanSupplierName очищает наименование контрагента от лишних кавычек и спецсимволов
func cleanSupplierName(name string) string {
	s := strings.TrimSpace(name)
	s = strings.Trim(s, `"'«»`)
	s = strings.TrimSpace(s)
	if s == "" {
		return "Не указан"
	}
	return s
}

// normalizeSupplierKey нормализует название поставщика для нечеткого поиска и объединения одинаковых юрлиц
func normalizeSupplierKey(name string) string {
	s := strings.ToLower(cleanSupplierName(name))
	s = strings.ReplaceAll(s, "ооо", "")
	s = strings.ReplaceAll(s, "ип", "")
	s = strings.ReplaceAll(s, "зао", "")
	s = strings.ReplaceAll(s, "пао", "")
	s = strings.ReplaceAll(s, `"`, "")
	s = strings.ReplaceAll(s, `«`, "")
	s = strings.ReplaceAll(s, `»`, "")
	s = strings.ReplaceAll(s, ".", " ")
	s = strings.ReplaceAll(s, ",", " ")
	return strings.TrimSpace(s)
}

// isSameSupplier определяет, относятся ли два названия к одному и тому же поставщику (например, ООО "РЕМО" и Ремо Пермь)
func isSameSupplier(s1, s2 string) bool {
	k1 := normalizeSupplierKey(s1)
	k2 := normalizeSupplierKey(s2)
	if k1 == "" || k2 == "" {
		return false
	}
	if k1 == k2 {
		return true
	}
	if len(k1) >= 4 && strings.Contains(k2, k1) {
		return true
	}
	if len(k2) >= 4 && strings.Contains(k1, k2) {
		return true
	}
	return false
}
