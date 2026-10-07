package config

import (
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port          string
	DBHost        string
	DBPort        string
	DBUser        string
	DBPass        string
	DBName        string
	EncryptionKey string
	AIBaseURL     string
	AIApiKey      string
	AIModel       string
	BotToken      string
	FontPath      string
}

func Load() *Config {
	_ = godotenv.Load(".env")
	_ = godotenv.Load("/opt/Analytics/.env")
	_ = godotenv.Load("/opt/iiko_parser/.env")
	_ = godotenv.Load("/opt/qa2a-reboot/.env")

	apiKey := os.Getenv("AI_API_KEY")
	if googleKeys := os.Getenv("GOOGLE_API_KEYS"); googleKeys != "" {
		keys := strings.Split(googleKeys, ",")
		if len(keys) > 0 && strings.TrimSpace(keys[0]) != "" {
			apiKey = strings.TrimSpace(keys[0])
		}
	} else if openrouter := os.Getenv("OPENROUTER_API_KEY"); openrouter != "" && apiKey == "" {
		apiKey = openrouter
	}

	aiBaseURL := getEnv("AI_BASE_URL", "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions")
	aiModel := getEnv("AI_MODEL", "gemini-2.5-flash")
	botToken := getEnv("BOT_TOKEN", "8364435346:AAHoKylC6rhKsvWqP6Qp-IoAIqQBPOqfZSA")
	fontPath := getEnv("FONT_PATH", "./fonts/DejaVuSans.ttf")

	return &Config{
		Port:          getEnv("PORT", "8098"),
		DBHost:        getEnv("DB_HOST", "localhost"),
		DBPort:        getEnv("DB_PORT", "5433"),
		DBUser:        getEnv("DB_USER", "admin"),
		DBPass:        getEnv("DB_PASS", "!123Maxim.!"),
		DBName:        getEnv("DB_NAME", "qa2a"),
		EncryptionKey: getEnv("ENCRYPTION_KEY", "D7F8A3B2E1C4F5A67890BCDEF1234567"),
		AIBaseURL:     aiBaseURL,
		AIApiKey:      apiKey,
		AIModel:       aiModel,
		BotToken:      botToken,
		FontPath:      fontPath,
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
