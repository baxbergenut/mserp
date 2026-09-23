package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/cors"
	"github.com/joho/godotenv"

	"mserp/internal/config"
	"mserp/internal/datatruck"
	"mserp/internal/db"
	"mserp/internal/gemini"
	"mserp/internal/groq"
	"mserp/internal/httpapi"
	"mserp/internal/jobs"
	"mserp/internal/prepass"
	"mserp/internal/relay"
	"mserp/internal/repository"
	"mserp/internal/telegram"
	"mserp/internal/telegramexpense"
)

func main() {
	syncLoadsOnly := flag.Bool("sync-loads-once", false, "run one load sync and exit without starting the HTTP server or scheduled jobs")
	flag.Parse()
	_ = godotenv.Load(".env.relay.local", ".env.local", ".env", "/etc/mserp/mserp.env")

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
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
	loadJob := jobs.NewSyncLoadsJob(client, loadRepo, logger)
	if *syncLoadsOnly {
		// Operator recovery runs can outlast the HTTP request timeout after
		// a long outage. Use the same job and configuration as the scheduler.
		syncCtx, cancel := context.WithTimeout(ctx, 45*time.Minute)
		defer cancel()
		if _, err := loadJob.Run(syncCtx); err != nil {
			logger.Error("one-time load sync failed", "error", err)
			os.Exit(1)
		}
		return
	}
	fleetRepo := repository.NewFleetRepository(pool)
	tollRepo := repository.NewTollRepository(pool)
	fileRepo := repository.NewFileRepository(pool)
	fuelRepo := repository.NewFuelRepository(pool)
	dashboardRepo := repository.NewDashboardRepository(pool)
	expenseRepo := repository.NewExpenseRepository(pool)
	authRepo := repository.NewAuthRepository(pool)
	cabCardExtractor := groq.NewClient(cfg.GroqAPIKey, cfg.GroqModel)
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
	var telegramExpenseService *telegramexpense.Service
	if cfg.TelegramExpensesEnabled {
		telegramClient := telegram.NewClient(cfg.TelegramBotToken)
		bot, telegramErr := telegramClient.GetMe(ctx)
		if telegramErr != nil {
			logger.Error("validate Telegram expense bot", "error", telegramErr)
			os.Exit(1)
		}
		if telegramErr := telegramClient.SetWebhook(ctx, cfg.TelegramWebhookURL, cfg.TelegramWebhookSecret); telegramErr != nil {
			logger.Error("register Telegram expense webhook", "error", telegramErr)
			os.Exit(1)
		}
		geminiClient := gemini.NewClient(cfg.GeminiAPIKey, cfg.GeminiExpenseModel)
		telegramExpenseService = telegramexpense.NewService(
			expenseRepo,
			telegramClient,
			geminiClient,
			cfg.TelegramAllowedChatIDs,
			cfg.ScheduledSyncsLocation,
			logger,
		)
		telegramExpenseService.Run(ctx, 2)
		logger.Info("Telegram expense ingestion enabled", "bot_username", bot.Username)
	}
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
		expenseRepo,
		authRepo,
		cabCardExtractor,
		httpapi.AuthOptions{
			CookieSecure: cfg.AuthCookieSecure,
			SessionTTL:   cfg.AuthSessionTTL,
		},
		httpapi.TelegramExpenseOptions{
			Enabled:       cfg.TelegramExpensesEnabled,
			WebhookSecret: cfg.TelegramWebhookSecret,
			Service:       telegramExpenseService,
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
