package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"mserp/internal/fleetscope"
	"mserp/internal/weighmytruck"
)

type Config struct {
	WeighMyTruck                 weighmytruck.Options
	FleetScope                   fleetscope.Options
	BindAddress                  string
	Port                         string
	DatabaseURL                  string
	DataTruckAPIKey              string
	DataTruckCompanyName         string
	DataTruckNewLoadsInterval    time.Duration
	DataTruckOperationalInterval time.Duration
	GroqAPIKey                   string
	GroqModel                    string
	GeminiAPIKey                 string
	GeminiExpenseModel           string
	RelayEnvironment             string
	RelayAPIURL                  string
	RelayAPIKey                  string
	RelayFuelSyncStart           time.Time
	PrePassEnvironment           string
	PrePassAPIURL                string
	PrePassClientID              string
	PrePassClientSecret          string
	PrePassTollSyncStart         time.Time
	FrontendOrigin               string
	AuthCookieSecure             bool
	AuthSessionTTL               time.Duration
	ScheduledSyncsEnabled        bool
	ScheduledSyncsLocation       *time.Location
	ScheduledLoadsSyncTime       DailySyncTime
	ScheduledFuelSyncTime        DailySyncTime
	ScheduledTollsSyncTime       DailySyncTime
	FiveELDEnabled               bool
	FiveELDAPIURL                string
	FiveELDAPIKey                string
	FiveELDProviderToken         string
	FiveELDUSDOT                 string
	FiveELDSyncInterval          time.Duration
}

type DailySyncTime struct {
	Hour   int
	Minute int
}

func Load() (Config, error) {
	newLoadsInterval, err := parseDataTruckInterval("DATATRUCK_NEW_LOADS_INTERVAL", "1m")
	if err != nil {
		return Config{}, err
	}
	operationalInterval, err := parseDataTruckInterval("DATATRUCK_OPERATIONAL_SYNC_INTERVAL", "5m")
	if err != nil {
		return Config{}, err
	}
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
	fiveELDAPIKey := strings.TrimSpace(os.Getenv("FIVE_ELD_API_KEY"))
	fiveELDProviderToken := strings.TrimSpace(os.Getenv("FIVE_ELD_PROVIDER_TOKEN"))
	fiveELDUSDOT := strings.TrimSpace(os.Getenv("FIVE_ELD_USDOT"))
	fiveELDEnabled := fiveELDAPIKey != "" || fiveELDProviderToken != "" || fiveELDUSDOT != ""
	if fiveELDEnabled && (fiveELDAPIKey == "" || fiveELDProviderToken == "" || fiveELDUSDOT == "") {
		return Config{}, errors.New("FIVE_ELD_API_KEY, FIVE_ELD_PROVIDER_TOKEN and FIVE_ELD_USDOT must all be set")
	}
	fiveELDAPIURL := strings.TrimRight(envOrDefault("FIVE_ELD_API_URL", "https://read.fiveeld.com"), "/")
	if fiveELDEnabled {
		parsed, parseErr := url.Parse(fiveELDAPIURL)
		if parseErr != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.User != nil {
			return Config{}, errors.New("FIVE_ELD_API_URL must be an HTTPS origin")
		}
	}
	fiveELDSyncInterval, err := parseDurationRange("FIVE_ELD_SYNC_INTERVAL", "5m", time.Minute, time.Hour)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		WeighMyTruck: weighmytruck.Options{
			ClientID:     strings.TrimSpace(os.Getenv("WEIGHMYTRUCK_CLIENT_ID")),
			ClientSecret: strings.TrimSpace(os.Getenv("WEIGHMYTRUCK_CLIENT_SECRET")),
			TokenURL:     envOrDefault("WEIGHMYTRUCK_TOKEN_URL", "https://login.microsoftonline.com/0c42b1f7-92b4-4df0-915d-4bebceb95491/oauth2/v2.0/token"),
			Scope:        envOrDefault("WEIGHMYTRUCK_SCOPE", "api://6b61f2df-5ec0-4dea-bfb8-b9589ec9942d/.default"),
			APIURL:       envOrDefault("WEIGHMYTRUCK_API_URL", "https://app.weighmytruck.com/fleetsettlement"),
			CompanyName:  envOrDefault("WEIGHMYTRUCK_COMPANY_NAME", "MS Express Inc."),
		},
		FleetScope:                   fleetscope.Options{CompanyID: strings.TrimSpace(os.Getenv("FLEETSCOPE_COMPANY_ID")), Secret: strings.TrimSpace(os.Getenv("FLEETSCOPE_WEBHOOK_SECRET"))},
		BindAddress:                  envOrDefault("BIND_ADDRESS", "127.0.0.1"),
		Port:                         envOrDefault("PORT", "8080"),
		DatabaseURL:                  strings.TrimSpace(os.Getenv("DATABASE_URL")),
		DataTruckAPIKey:              strings.TrimSpace(os.Getenv("DATATRUCK_API_KEY")),
		DataTruckCompanyName:         strings.TrimSpace(os.Getenv("DATATRUCK_COMPANY_NAME")),
		GroqAPIKey:                   strings.TrimSpace(os.Getenv("GROQ_API_KEY")),
		GroqModel:                    envOrDefault("GROQ_MODEL", "qwen/qwen3.6-27b"),
		GeminiAPIKey:                 strings.TrimSpace(os.Getenv("GEMINI_API_KEY")),
		GeminiExpenseModel:           envOrDefault("GEMINI_EXPENSE_MODEL", "gemini-3.5-flash-lite"),
		RelayEnvironment:             relayEnvironment,
		RelayAPIURL:                  relayAPIURL,
		RelayAPIKey:                  relayAPIKey,
		RelayFuelSyncStart:           relaySyncStart,
		PrePassEnvironment:           prePassEnvironment,
		PrePassAPIURL:                prePassAPIURL,
		PrePassClientID:              prePassClientID,
		PrePassClientSecret:          prePassClientSecret,
		PrePassTollSyncStart:         prePassSyncStart,
		FrontendOrigin:               frontendOrigin,
		AuthCookieSecure:             authCookieSecure,
		AuthSessionTTL:               authSessionTTL,
		ScheduledSyncsEnabled:        scheduledSyncsEnabled,
		ScheduledSyncsLocation:       scheduledSyncsLocation,
		ScheduledLoadsSyncTime:       scheduledLoadsSyncTime,
		DataTruckNewLoadsInterval:    newLoadsInterval,
		DataTruckOperationalInterval: operationalInterval,
		ScheduledFuelSyncTime:        scheduledFuelSyncTime,
		ScheduledTollsSyncTime:       scheduledTollsSyncTime,
		FiveELDEnabled:               fiveELDEnabled,
		FiveELDAPIURL:                fiveELDAPIURL,
		FiveELDAPIKey:                fiveELDAPIKey,
		FiveELDProviderToken:         fiveELDProviderToken,
		FiveELDUSDOT:                 fiveELDUSDOT,
		FiveELDSyncInterval:          fiveELDSyncInterval,
	}

	if err := cfg.WeighMyTruck.Validate(); err != nil {
		return Config{}, err
	}
	if err := cfg.FleetScope.Validate(); err != nil {
		return Config{}, err
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
	if cfg.PrePassEnvironment != "nonproduction" && cfg.PrePassEnvironment != "production" {
		return Config{}, errors.New("PREPASS_ENVIRONMENT must be nonproduction or production")
	}
	if cfg.PrePassClientID == "" || cfg.PrePassClientSecret == "" {
		return Config{}, errors.New("PrePass client ID and secret are required for the selected environment")
	}

	return cfg, nil
}

func parseDataTruckInterval(name, fallback string) (time.Duration, error) {
	return parseDurationRange(name, fallback, time.Minute, 24*time.Hour)
}

func parseDurationRange(key, fallback string, minimum, maximum time.Duration) (time.Duration, error) {
	value, err := time.ParseDuration(envOrDefault(key, fallback))
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be a duration between %s and %s", key, minimum, maximum)
	}
	return value, nil
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
