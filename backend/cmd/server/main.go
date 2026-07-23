package main

import (
	"context"
	"errors"
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
	"mserp/internal/groq"
	"mserp/internal/httpapi"
	"mserp/internal/jobs"
	"mserp/internal/relay"
	"mserp/internal/repository"
)

func main() {
	_ = godotenv.Load(".env.relay.local", ".env.local", ".env")

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
	fleetRepo := repository.NewFleetRepository(pool)
	tollRepo := repository.NewTollRepository(pool)
	fileRepo := repository.NewFileRepository(pool)
	fuelRepo := repository.NewFuelRepository(pool)
	dashboardRepo := repository.NewDashboardRepository(pool)
	authRepo := repository.NewAuthRepository(pool)
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
	router := httpapi.NewRouter(
		logger,
		loadJob,
		fuelJob,
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
			CookieSecure: cfg.AuthCookieSecure,
			SessionTTL:   cfg.AuthSessionTTL,
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
		// DataTruck and Relay syncs are currently synchronous. A first Relay
		// backfill can cover months of daily requests, while later runs skip
		// completed dates. Keep the connection open for the initial pass.
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
