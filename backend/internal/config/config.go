package config

import (
	"errors"
	"os"
	"strings"
)

type Config struct {
	Port                 string
	DatabaseURL          string
	DataTruckAPIKey      string
	DataTruckCompanyName string
}

func Load() (Config, error) {
	cfg := Config{
		Port:                 envOrDefault("PORT", "8080"),
		DatabaseURL:          strings.TrimSpace(os.Getenv("DATABASE_URL")),
		DataTruckAPIKey:      strings.TrimSpace(os.Getenv("DATATRUCK_API_KEY")),
		DataTruckCompanyName: strings.TrimSpace(os.Getenv("DATATRUCK_COMPANY_NAME")),
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

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
