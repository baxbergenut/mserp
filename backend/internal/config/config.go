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
	BindAddress          string
	Port                 string
	DatabaseURL          string
	DataTruckAPIKey      string
	DataTruckCompanyName string
	GroqAPIKey           string
	GroqModel            string
	RelayEnvironment     string
	RelayAPIURL          string
	RelayAPIKey          string
	RelayFuelSyncStart   time.Time
	FrontendOrigin       string
	AuthCookieSecure     bool
	AuthSessionTTL       time.Duration
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

	cfg := Config{
		BindAddress:          envOrDefault("BIND_ADDRESS", "127.0.0.1"),
		Port:                 envOrDefault("PORT", "8080"),
		DatabaseURL:          strings.TrimSpace(os.Getenv("DATABASE_URL")),
		DataTruckAPIKey:      strings.TrimSpace(os.Getenv("DATATRUCK_API_KEY")),
		DataTruckCompanyName: strings.TrimSpace(os.Getenv("DATATRUCK_COMPANY_NAME")),
		GroqAPIKey:           strings.TrimSpace(os.Getenv("GROQ_API_KEY")),
		GroqModel:            envOrDefault("GROQ_MODEL", "qwen/qwen3.6-27b"),
		RelayEnvironment:     relayEnvironment,
		RelayAPIURL:          relayAPIURL,
		RelayAPIKey:          relayAPIKey,
		RelayFuelSyncStart:   relaySyncStart,
		FrontendOrigin:       frontendOrigin,
		AuthCookieSecure:     authCookieSecure,
		AuthSessionTTL:       authSessionTTL,
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
	if cfg.RelayEnvironment != "staging" && cfg.RelayEnvironment != "production" {
		return Config{}, errors.New("RELAY_ENVIRONMENT must be staging or production")
	}
	if cfg.RelayAPIKey == "" {
		return Config{}, errors.New("Relay API key is required for the selected environment")
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
