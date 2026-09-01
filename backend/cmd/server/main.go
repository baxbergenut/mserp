package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/cors"
	"github.com/joho/godotenv"

	"mserp/internal/assistant"
	"mserp/internal/config"
	"mserp/internal/datatruck"
	"mserp/internal/db"
	"mserp/internal/groq"
	"mserp/internal/httpapi"
	"mserp/internal/jobs"
	"mserp/internal/prepass"
	"mserp/internal/relay"
	"mserp/internal/repository"
	"mserp/internal/telegram"
)

func main() {
	_ = godotenv.Load(".env.relay.local", ".env.local", ".env", "/etc/mserp/mserp.env")

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if len(os.Args) > 1 {
		if len(os.Args) == 3 && os.Args[1] == "telegram-manager" {
			runTelegramManagerCommand(logger, os.Args[2])
			return
		}
		logger.Error("usage: mserp-api [telegram-manager <existing-username>]")
		os.Exit(2)
	}
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("connect database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	client := datatruck.NewClient(cfg.DataTruckAPIKey, cfg.DataTruckCompanyName)
	loadRepo := repository.NewLoadRepository(pool)
	fleetRepo := repository.NewFleetRepository(pool)
	tollRepo := repository.NewTollRepository(pool)
	fileRepo := repository.NewFileRepository(pool)
	fuelRepo := repository.NewFuelRepository(pool)
	dashboardRepo := repository.NewDashboardRepository(pool)
	authRepo := repository.NewAuthRepository(pool)
	assistantRepo := repository.NewAssistantRepository(pool)
	cabCardExtractor := groq.NewClient(cfg.GroqAPIKey, cfg.GroqModel)
	loadJob := jobs.NewSyncLoadsJob(client, loadRepo, logger)
	relayClient := relay.NewClient(cfg.RelayAPIURL, cfg.RelayAPIKey)
	fuelJob := jobs.NewSyncFuelJob(
		relayClient,
		fuelRepo,
		cfg.RelayEnvironment,
		cfg.RelayFuelSyncStart,
		logger,
	)
	prePassClient := prepass.NewClient(
		cfg.PrePassAPIURL,
		cfg.PrePassClientID,
		cfg.PrePassClientSecret,
	)
	tollJob := jobs.NewSyncTollsJob(
		prePassClient,
		tollRepo,
		cfg.PrePassEnvironment,
		cfg.PrePassTollSyncStart,
		logger,
	)
	var telegramService *assistant.Service
	telegramBotUsername := ""
	if cfg.TelegramEnabled {
		telegramClient := telegram.NewClient(cfg.TelegramBotToken)
		botUser, telegramErr := telegramClient.GetMe(ctx)
		if telegramErr != nil {
			logger.Error("validate Telegram bot", "error", telegramErr)
			os.Exit(1)
		}
		telegramBotUsername = botUser.Username
		if telegramErr := telegramClient.SetWebhook(ctx, cfg.TelegramWebhookURL, cfg.TelegramWebhookSecret); telegramErr != nil {
			logger.Error("register Telegram webhook", "error", telegramErr)
			os.Exit(1)
		}
		assistantModel := groq.NewClient(cfg.GroqAssistantAPIKey, cfg.GroqAssistantModel)
		toolExecutor := assistant.NewToolExecutor(assistantRepo, fleetRepo, loadRepo, fuelRepo,
			tollRepo, dashboardRepo, loadJob, fuelJob, tollJob)
		telegramService = assistant.NewService(assistantRepo, toolExecutor, assistantModel, telegramClient, logger)
		go telegramService.Run(ctx, 4)
		logger.Info("Telegram assistant enabled", "bot_username", telegramBotUsername)
	}
	go func() {
		if cleanupErr := assistantRepo.Cleanup(ctx); cleanupErr != nil {
			logger.Error("clean Telegram assistant data", "error", cleanupErr)
		}
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if cleanupErr := assistantRepo.Cleanup(ctx); cleanupErr != nil {
					logger.Error("clean Telegram assistant data", "error", cleanupErr)
				}
			}
		}
	}()
	router := httpapi.NewRouter(
		logger,
		loadJob,
		fuelJob,
		tollJob,
		pool,
		loadRepo,
		fleetRepo,
		tollRepo,
		fileRepo,
		fuelRepo,
		dashboardRepo,
		authRepo,
		cabCardExtractor,
		httpapi.AuthOptions{
			CookieSecure:   cfg.AuthCookieSecure,
			SessionTTL:     cfg.AuthSessionTTL,
			TelegramStatus: assistantRepo.ManagerStatus,
		},
		httpapi.TelegramOptions{
			Enabled: cfg.TelegramEnabled, WebhookSecret: cfg.TelegramWebhookSecret,
			BotUsername: telegramBotUsername, Service: telegramService, Repository: assistantRepo,
		},
	)
	handler := cors.Handler(cors.Options{
		AllowedOrigins:   []string{cfg.FrontendOrigin},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		AllowCredentials: true,
	})(router)

	server := &http.Server{
		Addr:        net.JoinHostPort(cfg.BindAddress, cfg.Port),
		Handler:     handler,
		ReadTimeout: 15 * time.Second,
		// DataTruck, Relay, and PrePass syncs are currently synchronous. Initial
		// backfills can cover months of data, while later runs skip completed
		// dates. Keep the connection open for the initial pass.
		WriteTimeout:      15 * time.Minute,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	schedulerDone := make(chan struct{})
	if cfg.ScheduledSyncsEnabled {
		go func() {
			defer close(schedulerDone)
			jobs.RunDailyScheduler(
				ctx,
				logger,
				cfg.ScheduledSyncsLocation,
				jobs.DailyJob{
					Name:   "loads",
					Hour:   cfg.ScheduledLoadsSyncTime.Hour,
					Minute: cfg.ScheduledLoadsSyncTime.Minute,
					Run: func(ctx context.Context) error {
						_, err := loadJob.Run(ctx)
						return err
					},
				},
				jobs.DailyJob{
					Name:   "fuel",
					Hour:   cfg.ScheduledFuelSyncTime.Hour,
					Minute: cfg.ScheduledFuelSyncTime.Minute,
					Run: func(ctx context.Context) error {
						_, err := fuelJob.Run(ctx)
						return err
					},
				},
				jobs.DailyJob{
					Name:   "tolls",
					Hour:   cfg.ScheduledTollsSyncTime.Hour,
					Minute: cfg.ScheduledTollsSyncTime.Minute,
					Run: func(ctx context.Context) error {
						_, err := tollJob.Run(ctx)
						return err
					},
				},
			)
		}()
	} else {
		close(schedulerDone)
		logger.Info("scheduled syncs disabled")
	}

	go func() {
		logger.Info("http server starting", "addr", server.Addr)
		if serveErr := server.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Error("http server failed", "error", serveErr)
			stop()
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
		logger.Error("shutdown server", "error", shutdownErr)
	}

	select {
	case <-schedulerDone:
	case <-shutdownCtx.Done():
		logger.Warn("scheduled syncs did not stop before shutdown timeout")
	}
}

func runTelegramManagerCommand(logger *slog.Logger, username string) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	username = strings.TrimSpace(username)
	if databaseURL == "" || username == "" {
		logger.Error("DATABASE_URL and an existing username are required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := db.NewPool(ctx, databaseURL)
	if err != nil {
		logger.Error("connect database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := repository.NewAssistantRepository(pool).BootstrapManager(ctx, username); err != nil {
		logger.Error("approve Telegram manager", "error", err)
		os.Exit(1)
	}
	logger.Info("Telegram manager approved", "username", username)
}
