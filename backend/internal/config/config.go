package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
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

	cfg := Config{
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
