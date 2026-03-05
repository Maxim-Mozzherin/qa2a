package config
import (
    "os"
    "github.com/joho/godotenv"
)
type Config struct {
    BotToken string
    Port     string
    DBHost   string
    DBPort   string
    DBName   string
    DBUser   string
    DBPass   string
}
func Load() (*Config, error) {
    _ = godotenv.Load() // Загружаем .env
    
    cfg := &Config{
        BotToken: os.Getenv("BOT_TOKEN"),
        DBHost:   os.Getenv("DB_HOST"),
        DBPort:   os.Getenv("DB_PORT"),
        DBName:   os.Getenv("DB_NAME"),
        DBUser:   os.Getenv("DB_USER"),
        DBPass:   os.Getenv("DB_PASS"),
        Port:     os.Getenv("PORT"),
    }
    
    // Дефолтные значения, если .env пуст
    if cfg.Port == "" { cfg.Port = "8080" }
    if cfg.DBHost == "" { cfg.DBHost = "localhost" }
    if cfg.DBPort == "" { cfg.DBPort = "5432" }
    
    return cfg, nil
}
func (c *Config) DSN() string {
    return "host=" + c.DBHost + " port=" + c.DBPort + " user=" + c.DBUser + " password=" + c.DBPass + " dbname=" + c.DBName + " sslmode=disable"
}
