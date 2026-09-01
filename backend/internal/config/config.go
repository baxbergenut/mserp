package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	BindAddress            string
	Port                   string
	DatabaseURL            string
	DataTruckAPIKey        string
	DataTruckCompanyName   string
	GroqAPIKey             string
	GroqModel              string
	GroqAssistantAPIKey    string
	GroqAssistantModel     string
	TelegramEnabled        bool
	TelegramBotToken       string
	TelegramWebhookSecret  string
	TelegramWebhookURL     string
	RelayEnvironment       string
	RelayAPIURL            string
	RelayAPIKey            string
	RelayFuelSyncStart     time.Time
	PrePassEnvironment     string
	PrePassAPIURL          string
	PrePassClientID        string
	PrePassClientSecret    string
	PrePassTollSyncStart   time.Time
	FrontendOrigin         string
	AuthCookieSecure       bool
	AuthSessionTTL         time.Duration
	ScheduledSyncsEnabled  bool
	ScheduledSyncsLocation *time.Location
	ScheduledLoadsSyncTime DailySyncTime
	ScheduledFuelSyncTime  DailySyncTime
	ScheduledTollsSyncTime DailySyncTime
}

type DailySyncTime struct {
	Hour   int
	Minute int
}

func Load() (Config, error) {
	relayEnvironment := strings.ToLower(envOrDefault("RELAY_ENVIRONMENT", "production"))
	relayAPIURL := strings.TrimSpace(os.Getenv("RELAY_API_URL"))
	relayAPIKey := strings.TrimSpace(os.Getenv("RELAY_API_KEY"))
	if relayEnvironment == "staging" {
		if relayAPIURL == "" {
			relayAPIURL = "https://staging.relaypayments.com/api"
		}
		if relayAPIKey == "" {
			relayAPIKey = strings.TrimSpace(os.Getenv("RELAY_STAGING_API_KEY"))
		}
	} else if relayEnvironment == "production" {
		if relayAPIURL == "" {
			relayAPIURL = "https://app.relaypayments.com/api"
		}
		if relayAPIKey == "" {
			relayAPIKey = strings.TrimSpace(os.Getenv("RELAY_PRODUCTION_API_KEY"))
		}
	}

	relaySyncStart := utcDate(time.Now().UTC().AddDate(0, 0, -30))
	if value := strings.TrimSpace(os.Getenv("RELAY_FUEL_SYNC_START_DATE")); value != "" {
		parsed, err := time.Parse(time.DateOnly, value)
		if err != nil {
			return Config{}, fmt.Errorf("RELAY_FUEL_SYNC_START_DATE must use YYYY-MM-DD: %w", err)
		}
		relaySyncStart = parsed
	}

	prePassEnvironment := strings.ToLower(envOrDefault("PREPASS_ENVIRONMENT", "production"))
	prePassAPIURL := strings.TrimSpace(os.Getenv("PREPASS_API_URL"))
	prePassClientID := strings.TrimSpace(os.Getenv("PREPASS_CLIENT_ID"))
	prePassClientSecret := strings.TrimSpace(os.Getenv("PREPASS_CLIENT_SECRET"))
	if prePassEnvironment == "nonproduction" {
		if prePassAPIURL == "" {
			prePassAPIURL = "https://api-npr.prepass.com"
		}
		if prePassClientID == "" {
			prePassClientID = strings.TrimSpace(os.Getenv("PREPASS_NONPRODUCTION_CLIENT_ID"))
		}
		if prePassClientSecret == "" {
			prePassClientSecret = strings.TrimSpace(os.Getenv("PREPASS_NONPRODUCTION_CLIENT_SECRET"))
		}
	} else if prePassEnvironment == "production" {
		if prePassAPIURL == "" {
			prePassAPIURL = "https://api.prepass.com"
		}
		if prePassClientID == "" {
			prePassClientID = strings.TrimSpace(os.Getenv("PREPASS_PRODUCTION_CLIENT_ID"))
		}
		if prePassClientSecret == "" {
			prePassClientSecret = strings.TrimSpace(os.Getenv("PREPASS_PRODUCTION_CLIENT_SECRET"))
		}
	}
	nowUTC := time.Now().UTC()
	prePassSyncStart := time.Date(nowUTC.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
	if value := strings.TrimSpace(os.Getenv("PREPASS_TOLL_SYNC_START_DATE")); value != "" {
		parsed, err := time.Parse(time.DateOnly, value)
		if err != nil {
			return Config{}, fmt.Errorf("PREPASS_TOLL_SYNC_START_DATE must use YYYY-MM-DD: %w", err)
		}
		prePassSyncStart = parsed
	}

	frontendOrigin := strings.TrimRight(envOrDefault("FRONTEND_ORIGIN", "http://localhost:3000"), "/")
	parsedOrigin, err := url.Parse(frontendOrigin)
	if err != nil || (parsedOrigin.Scheme != "http" && parsedOrigin.Scheme != "https") ||
		parsedOrigin.Host == "" || parsedOrigin.Path != "" || parsedOrigin.RawQuery != "" ||
		parsedOrigin.Fragment != "" || parsedOrigin.User != nil {
		return Config{}, errors.New("FRONTEND_ORIGIN must be an origin such as https://erp.example.com")
	}
	authCookieSecure := parsedOrigin.Scheme == "https"
	if value := strings.TrimSpace(os.Getenv("AUTH_COOKIE_SECURE")); value != "" {
		authCookieSecure, err = strconv.ParseBool(value)
		if err != nil {
			return Config{}, errors.New("AUTH_COOKIE_SECURE must be true or false")
		}
	}
	if !authCookieSecure && parsedOrigin.Hostname() != "localhost" &&
		parsedOrigin.Hostname() != "127.0.0.1" && parsedOrigin.Hostname() != "::1" {
		return Config{}, errors.New("AUTH_COOKIE_SECURE may only be false for local development")
	}
	authSessionTTL, err := time.ParseDuration(envOrDefault("AUTH_SESSION_TTL", "12h"))
	if err != nil || authSessionTTL < 15*time.Minute || authSessionTTL > 7*24*time.Hour {
		return Config{}, errors.New("AUTH_SESSION_TTL must be a duration between 15m and 168h")
	}
	telegramEnabled, err := strconv.ParseBool(envOrDefault("TELEGRAM_ENABLED", "false"))
	if err != nil {
		return Config{}, errors.New("TELEGRAM_ENABLED must be true or false")
	}
	telegramWebhookURL := strings.TrimSpace(os.Getenv("TELEGRAM_WEBHOOK_URL"))
	if telegramEnabled {
		parsedWebhook, parseErr := url.Parse(telegramWebhookURL)
		if parseErr != nil || parsedWebhook.Scheme != "https" || parsedWebhook.Host == "" ||
			parsedWebhook.RawQuery != "" || parsedWebhook.Fragment != "" || parsedWebhook.User != nil {
			return Config{}, errors.New("TELEGRAM_WEBHOOK_URL must be an HTTPS URL without a query or fragment")
		}
	}
	scheduledSyncsEnabled, err := strconv.ParseBool(envOrDefault("SCHEDULED_SYNCS_ENABLED", "true"))
	if err != nil {
		return Config{}, errors.New("SCHEDULED_SYNCS_ENABLED must be true or false")
	}
	scheduledSyncsLocation, err := time.LoadLocation(envOrDefault("SCHEDULED_SYNCS_TIMEZONE", "America/New_York"))
	if err != nil {
		return Config{}, fmt.Errorf("load SCHEDULED_SYNCS_TIMEZONE: %w", err)
	}
	scheduledLoadsSyncTime, err := parseDailySyncTime("SCHEDULED_LOADS_SYNC_TIME", "06:00")
	if err != nil {
		return Config{}, err
	}
	scheduledFuelSyncTime, err := parseDailySyncTime("SCHEDULED_FUEL_SYNC_TIME", "06:30")
	if err != nil {
		return Config{}, err
	}
	scheduledTollsSyncTime, err := parseDailySyncTime("SCHEDULED_TOLLS_SYNC_TIME", "07:00")
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		BindAddress:            envOrDefault("BIND_ADDRESS", "127.0.0.1"),
		Port:                   envOrDefault("PORT", "8080"),
		DatabaseURL:            strings.TrimSpace(os.Getenv("DATABASE_URL")),
		DataTruckAPIKey:        strings.TrimSpace(os.Getenv("DATATRUCK_API_KEY")),
		DataTruckCompanyName:   strings.TrimSpace(os.Getenv("DATATRUCK_COMPANY_NAME")),
		GroqAPIKey:             strings.TrimSpace(os.Getenv("GROQ_API_KEY")),
		GroqModel:              envOrDefault("GROQ_MODEL", "qwen/qwen3.6-27b"),
		GroqAssistantAPIKey:    strings.TrimSpace(os.Getenv("GROQ_ASSISTANT_API_KEY")),
		GroqAssistantModel:     envOrDefault("GROQ_ASSISTANT_MODEL", "openai/gpt-oss-120b"),
		TelegramEnabled:        telegramEnabled,
		TelegramBotToken:       strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		TelegramWebhookSecret:  strings.TrimSpace(os.Getenv("TELEGRAM_WEBHOOK_SECRET")),
		TelegramWebhookURL:     telegramWebhookURL,
		RelayEnvironment:       relayEnvironment,
		RelayAPIURL:            relayAPIURL,
		RelayAPIKey:            relayAPIKey,
		RelayFuelSyncStart:     relaySyncStart,
		PrePassEnvironment:     prePassEnvironment,
		PrePassAPIURL:          prePassAPIURL,
		PrePassClientID:        prePassClientID,
		PrePassClientSecret:    prePassClientSecret,
		PrePassTollSyncStart:   prePassSyncStart,
		FrontendOrigin:         frontendOrigin,
		AuthCookieSecure:       authCookieSecure,
		AuthSessionTTL:         authSessionTTL,
		ScheduledSyncsEnabled:  scheduledSyncsEnabled,
		ScheduledSyncsLocation: scheduledSyncsLocation,
		ScheduledLoadsSyncTime: scheduledLoadsSyncTime,
		ScheduledFuelSyncTime:  scheduledFuelSyncTime,
		ScheduledTollsSyncTime: scheduledTollsSyncTime,
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.DataTruckAPIKey == "" {
		return Config{}, errors.New("DATATRUCK_API_KEY is required")
	}
	if cfg.DataTruckCompanyName == "" {
		return Config{}, errors.New("DATATRUCK_COMPANY_NAME is required")
	}
	if cfg.GroqAssistantAPIKey == "" {
		cfg.GroqAssistantAPIKey = cfg.GroqAPIKey
	}
	if cfg.TelegramEnabled {
		if cfg.TelegramBotToken == "" {
			return Config{}, errors.New("TELEGRAM_BOT_TOKEN is required when Telegram is enabled")
		}
		if strings.Contains(cfg.TelegramWebhookURL, cfg.TelegramBotToken) {
			return Config{}, errors.New("TELEGRAM_WEBHOOK_URL must not contain the bot token")
		}
		if cfg.TelegramWebhookSecret == "" || len(cfg.TelegramWebhookSecret) > 256 ||
			strings.ContainsFunc(cfg.TelegramWebhookSecret, func(r rune) bool {
				return !(r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z')
			}) {
			return Config{}, errors.New("TELEGRAM_WEBHOOK_SECRET must contain 1-256 letters, digits, underscores, or hyphens")
		}
		if cfg.GroqAssistantAPIKey == "" {
			return Config{}, errors.New("GROQ_ASSISTANT_API_KEY or GROQ_API_KEY is required when Telegram is enabled")
		}
	}
	if cfg.RelayEnvironment != "staging" && cfg.RelayEnvironment != "production" {
		return Config{}, errors.New("RELAY_ENVIRONMENT must be staging or production")
	}
	if cfg.RelayAPIKey == "" {
		return Config{}, errors.New("Relay API key is required for the selected environment")
	}
	if cfg.PrePassEnvironment != "nonproduction" && cfg.PrePassEnvironment != "production" {
		return Config{}, errors.New("PREPASS_ENVIRONMENT must be nonproduction or production")
	}
	if cfg.PrePassClientID == "" || cfg.PrePassClientSecret == "" {
		return Config{}, errors.New("PrePass client ID and secret are required for the selected environment")
	}

	return cfg, nil
}

func utcDate(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func parseDailySyncTime(key, fallback string) (DailySyncTime, error) {
	value := envOrDefault(key, fallback)
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return DailySyncTime{}, fmt.Errorf("%s must use 24-hour HH:MM format: %w", key, err)
	}
	return DailySyncTime{Hour: parsed.Hour(), Minute: parsed.Minute()}, nil
}
