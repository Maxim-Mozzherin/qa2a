package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config инкапсулирует все параметры окружения, необходимые для работы приложения.
type Config struct {
	// Сетевые настройки сервиса
	Port string

	// Telegram Bot API
	BotToken string

	// Ключ для внешних интеграций (межсервисный обмен с iiko_parser)
	ExternalApiKey string

	// Симметричный ключ AES-256 (32 байта) для шифрования паролей iiko RMS в БД
	EncryptionKey string

	// Параметры подключения к PostgreSQL
	DBHost    string
	DBPort    string
	DBName    string
	DBUser    string
	DBPass    string
	DBSSLMode string

	// Параметры пула соединений базы данных
	DBMaxOpenConns    int
	DBMaxIdleConns    int
	DBConnMaxLifetime time.Duration
	DBConnMaxIdleTime time.Duration
}

// Load выполняет считывание конфигурации из файла .env и переменных окружения ОС.
func Load() (*Config, error) {
	// Пытаемся загрузить .env из текущей рабочей директории или абсолютного пути
	_ = godotenv.Load()
	_ = godotenv.Load("/opt/qa2a-reboot/.env")

	cfg := &Config{
		Port:           getEnv("PORT", "8082"),
		BotToken:       os.Getenv("BOT_TOKEN"),
		ExternalApiKey: getEnv("EXTERNAL_API_KEY", "moztech-secret-token-8099"),
		EncryptionKey:  getEnv("ENCRYPTION_KEY", "qa2a-reboot-default-aes-secret-key-32b"),

		DBHost:    getEnv("DB_HOST", "localhost"),
		DBPort:    getEnv("DB_PORT", "5433"), // Дефолтный порт PostgreSQL для QA2A
		DBName:    getEnv("DB_NAME", "qa2a"),
		DBUser:    getEnv("DB_USER", "admin"),
		DBPass:    os.Getenv("DB_PASS"),
		DBSSLMode: getEnv("DB_SSLMODE", "disable"),

		DBMaxOpenConns:    getEnvAsInt("DB_MAX_OPEN_CONNS", 25),
		DBMaxIdleConns:    getEnvAsInt("DB_MAX_IDLE_CONNS", 10),
		DBConnMaxLifetime: getEnvAsDuration("DB_CONN_MAX_LIFETIME", 15*time.Minute),
		DBConnMaxIdleTime: getEnvAsDuration("DB_CONN_MAX_IDLE_TIME", 5*time.Minute),
	}

	return cfg, nil
}

// DSN формирует безопасную строку подключения для драйвера PostgreSQL (lib/pq).
func (c *Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s connect_timeout=10",
		c.DBHost,
		c.DBPort,
		c.DBUser,
		c.DBPass,
		c.DBName,
		c.DBSSLMode,
	)
}

// URLDSN формирует URL-совместимую строку подключения (postgres://...).
func (c *Config) URLDSN() string {
	userInfo := url.UserPassword(c.DBUser, c.DBPass)
	return fmt.Sprintf(
		"postgres://%s@%s:%s/%s?sslmode=%s",
		userInfo.String(),
		c.DBHost,
		c.DBPort,
		c.DBName,
		c.DBSSLMode,
	)
}

// ============================================================================
// ВСПОМОГАТЕЛЬНЫЕ МЕТОДЫ ЧТЕНИЯ ОКРУЖЕНИЯ
// ============================================================================

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvAsInt(key string, defaultVal int) int {
	valStr := os.Getenv(key)
	if valStr == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(valStr)
	if err != nil {
		return defaultVal
	}
	return val
}

func getEnvAsDuration(key string, defaultVal time.Duration) time.Duration {
	valStr := os.Getenv(key)
	if valStr == "" {
		return defaultVal
	}
	val, err := time.ParseDuration(valStr)
	if err != nil {
		return defaultVal
	}
	return val
}

