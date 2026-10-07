package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port               string
	DBHost             string
	DBPort             string
	DBUser             string
	DBPass             string
	DBName             string
	TwoGisAPIKey       string
	TwoGisDefaultCity   string
	TwoGisDefaultCityID string
	TwoGisAPIURL       string
}

func LoadConfig() (*Config, error) {
	// Attempt to load .env from current directory or parent directory
	_ = godotenv.Load()
	_ = godotenv.Load("../../.env")
	_ = godotenv.Load("Z:/Parser/.env")
	_ = godotenv.Load("Z:/.env")

	cfg := &Config{
		Port:               getEnv("PORT", "8095"),
		DBHost:             getEnv("DB_HOST", "localhost"),
		DBPort:             getEnv("DB_PORT", "5433"),
		DBUser:             getEnv("DB_USER", "admin"),
		DBPass:             getEnv("DB_PASS", "!123Maxim.!"),
		DBName:             getEnv("DB_NAME", "qa2a"),
		TwoGisAPIKey:       getEnv("TWOGIS_API_KEY", "rurbbn3446"),
		TwoGisDefaultCity:   getEnv("TWOGIS_DEFAULT_CITY", "Пермь"),
		TwoGisDefaultCityID: getEnv("TWOGIS_DEFAULT_CITY_ID", "449942122394391"),
		TwoGisAPIURL:       getEnv("TWOGIS_API_URL", "https://catalog.api.2gis.com/3.0/items"),
	}

	return cfg, nil
}

func (c *Config) DatabaseDSN() string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable connect_timeout=10",
		c.DBHost, c.DBPort, c.DBUser, c.DBPass, c.DBName)
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
