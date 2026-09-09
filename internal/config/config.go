package config

import (
	"log"
	"os"
	"strconv"
	"strings"
)

// Config encapsulates runtime configuration loaded from environment variables.
type Config struct {
	Port              string
	DatabaseURL       string
	WAPhoneNumber     string
	WAClientName      string
	DiscordWebhookURL string
	APIKey            string
	AntibanPreset         string
	BackupDir             string
	BackupRetentionDays   int
	BackupScheduledGroups []string
	BackupIntervalHours   int
	LogLevel              string
	CoolifyFQDN           string
}

// LoadConfig reads configuration from the OS environment with secure production defaults.
func LoadConfig() *Config {
	pgUser := getEnv("POSTGRES_USER", "postgres")
	pgPass := getEnv("POSTGRES_PASSWORD", "postgres_secure_pass")
	pgHost := getEnv("POSTGRES_HOST", "wa-postgres")
	pgPort := getEnv("POSTGRES_PORT", "5432")
	pgDB := getEnv("POSTGRES_DB", "whatsmeow")

	defaultDBURL := "postgres://" + pgUser + ":" + pgPass + "@" + pgHost + ":" + pgPort + "/" + pgDB + "?sslmode=disable"

	rawGroups := getEnv("BACKUP_SCHEDULED_GROUPS", "")
	var scheduledGroups []string
	if rawGroups != "" {
		for _, g := range strings.Split(rawGroups, ",") {
			trimmed := strings.TrimSpace(g)
			if trimmed != "" {
				scheduledGroups = append(scheduledGroups, trimmed)
			}
		}
	}

	cfg := &Config{
		Port:                  getEnv("PORT", "8080"),
		DatabaseURL:           getEnv("DATABASE_URL", defaultDBURL),
		WAPhoneNumber:         cleanPhone(getEnv("WA_PHONE_NUMBER", "")),
		WAClientName:          getEnv("WA_CLIENT_NAME", "Chrome (Linux)"),
		DiscordWebhookURL:     getEnv("DISCORD_WEBHOOK_URL", ""),
		APIKey:                getEnv("API_KEY", ""),
		AntibanPreset:         getEnv("ANTIBAN_PRESET", "moderate"),
		BackupDir:             getEnv("BACKUP_DIR", "/app/backups"),
		BackupRetentionDays:   getEnvInt("BACKUP_RETENTION_DAYS", 30),
		BackupScheduledGroups: scheduledGroups,
		BackupIntervalHours:   getEnvInt("BACKUP_INTERVAL_HOURS", 24),
		LogLevel:              getEnv("LOG_LEVEL", "INFO"),
		CoolifyFQDN:           getEnv("SERVICE_FQDN_WHATSAPP_BOT", getEnv("COOLIFY_FQDN", "")),
	}

	if cfg.WAPhoneNumber == "" {
		log.Println("[WARN] WA_PHONE_NUMBER is not set. You will need to trigger pairing via REST API: POST /api/v1/session/pair")
	}

	if cfg.APIKey == "" {
		log.Println("[WARN] API_KEY is not set! REST API endpoints are currently unprotected. Set API_KEY for production.")
	}

	return cfg
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && strings.TrimSpace(val) != "" {
		return strings.TrimSpace(val)
	}
	return defaultVal
}

func cleanPhone(phone string) string {
	phone = strings.TrimSpace(phone)
	phone = strings.TrimPrefix(phone, "+")
	phone = strings.ReplaceAll(phone, "-", "")
	phone = strings.ReplaceAll(phone, " ", "")
	return phone
}

func getEnvInt(key string, defaultVal int) int {
	valStr := getEnv(key, "")
	if valStr == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(valStr)
	if err != nil {
		return defaultVal
	}
	return n
}
