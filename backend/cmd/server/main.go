package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/cors"
	"github.com/joho/godotenv"

	"mserp/internal/boardroutes"
	"mserp/internal/config"
	"mserp/internal/datatruck"
	"mserp/internal/db"
	"mserp/internal/fiveeld"
	"mserp/internal/groq"
	"mserp/internal/httpapi"
	"mserp/internal/jobs"
	"mserp/internal/openai"
	"mserp/internal/prepass"
	"mserp/internal/relay"
	"mserp/internal/repository"
	"mserp/internal/samsara"
	"mserp/internal/weighmytruck"
)

func main() {
	statusPreview := flag.Bool("fleet-status-preview", false, "print the read-only status activation snapshot and exit")
	statusActivate := flag.String("fleet-status-activate", "", "activate the explicitly reviewed status snapshot SHA256 and exit")
	wmtImport := flag.String("import-weighmytruck-roster", "", "initialize WeighMyTruck tracker from website CSV and exit without external calls")
	syncWMTOnly := flag.Bool("sync-weighmytruck-once", false, "sync scale transactions and exit")
	syncLoadsOnly := flag.Bool("sync-loads-once", false, "run one load sync and exit without starting the HTTP server or scheduled jobs")
	backfillTollLocations := flag.Bool("backfill-toll-locations", false, "refresh current-year PrePass location metadata and exit")
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
	if *statusPreview || *statusActivate != "" {
		repo := repository.NewFleetRepository(pool)
		if *statusActivate != "" {
			if err = repo.ActivateStatuses(ctx, *statusActivate); err != nil {
				logger.Error("activate fleet statuses", "error", err)
				os.Exit(1)
			}
			logger.Info("fleet statuses activated")
			return
		}
		v, e := repo.PreviewStatuses(ctx)
		if e != nil {
			logger.Error("preview fleet statuses", "error", e)
			os.Exit(1)
		}
		if e = json.NewEncoder(os.Stdout).Encode(v); e != nil {
			os.Exit(1)
		}
		return
	}
	if *wmtImport != "" {
		file, err := os.Open(*wmtImport)
		if err != nil {
			logger.Error("open WeighMyTruck roster", "error", err)
			os.Exit(1)
		}
		defer file.Close()
		result, err := repository.NewWeighMyTruckRepository(pool, nil).ImportWeighMyTruck(ctx, file)
		if err != nil {
			logger.Error("import WeighMyTruck roster", "error", err)
			os.Exit(1)
		}
		logger.Info("WeighMyTruck roster imported", "total", result.Total, "linked", result.Linked, "unlinked", result.Unlinked, "alreadyImported", result.AlreadyImported)
		return
	}

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
	wmtClient := weighmytruck.NewClient(cfg.WeighMyTruck)
	wmtRepo := repository.NewWeighMyTruckRepository(pool, wmtClient)
	if *syncWMTOnly {
		n, e := wmtRepo.SyncSettlements(ctx)
		if e != nil {
			logger.Error("scale sync failed", "error", e)
			os.Exit(1)
		}
		logger.Info("scale sync complete", "tickets", n)
		return
	}
	fleetRepo := repository.NewFleetRepository(pool)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			if err := fleetRepo.ReconcileStatusProgress(ctx); err != nil && ctx.Err() == nil {
				logger.Error("reconcile status queue", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	dataTruckRepo := repository.NewDataTruckRepository(pool, client, cfg.DataTruckMCID)
	fleetRepo.ConfigureDataTruck(dataTruckRepo)
	tollRepo := repository.NewTollRepository(pool)
	fileRepo := repository.NewFileRepository(pool)
	fuelRepo := repository.NewFuelRepository(pool)
	dashboardRepo := repository.NewDashboardRepository(pool)
	expenseRepo := repository.NewExpenseRepository(pool)
	grossBoardRepo := repository.NewGrossBoardRepository(pool)
	authRepo := repository.NewAuthRepository(pool)
	customTaskRepo := repository.NewCustomTaskRepository(pool)
	cabCardExtractor := groq.NewClient(cfg.GroqAPIKey, cfg.GroqModel)
	relayClient := relay.NewClient(cfg.RelayAPIURL, cfg.RelayAPIKey)
	fuelRepo.ConfigureRelayDrivers(relayClient, cfg.RelayEnvironment)
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
	tollRepo.ConfigurePrePassFleet(prePassClient, cfg.PrePassEnvironment)
	if *backfillTollLocations {
		backfillCtx, cancel := context.WithTimeout(ctx, 45*time.Minute)
		defer cancel()
		if err := jobs.BackfillTollLocations(backfillCtx, prePassClient, tollRepo, cfg.PrePassEnvironment, time.Now().UTC(), logger); err != nil {
			logger.Error("toll location backfill failed", "error", err)
			os.Exit(1)
		}
		return
	}
	tollJob := jobs.NewSyncTollsJob(
		prePassClient,
		tollRepo,
		cfg.PrePassEnvironment,
		cfg.PrePassTollSyncStart,
		logger,
	)
	var fiveELDJob *jobs.SyncFiveELDJob
	var fiveELDTelemetry *fiveeld.Client
	if cfg.FiveELDEnabled {
		fiveELDTelemetry = fiveeld.NewClient(cfg.FiveELDAPIURL, cfg.FiveELDAPIKey, cfg.FiveELDProviderToken)
		fiveELDJob = jobs.NewSyncFiveELDJob(
			fiveELDTelemetry,
			repository.NewFiveELDRepository(pool), cfg.FiveELDUSDOT, logger,
		)
	}
	var samsaraJob *jobs.SyncSamsaraJob
	var locationRefresher jobs.LocationRefresher
	if fiveELDJob != nil {
		locationRefresher = fiveELDJob
	}
	if cfg.Samsara.Token != "" {
		samsaraJob = jobs.NewSyncSamsaraJob(samsara.NewClient(cfg.Samsara.URL, cfg.Samsara.Token, cfg.Samsara.Version), repository.NewSamsaraRepository(pool))
		combined := &jobs.TelemetryRefreshJob{Samsara: samsaraJob, Logger: logger}
		if fiveELDJob != nil {
			combined.FiveELD = fiveELDJob
		}
		locationRefresher = combined
	}
	samsaraDone := make(chan struct{})
	go func() {
		defer close(samsaraDone)
		if samsaraJob == nil {
			return
		}
		jobs.RunIntervalJob(ctx, logger, jobs.IntervalJob{Name: "samsara", Interval: cfg.Samsara.Interval, Run: func(runCtx context.Context) error {
			bounded, cancel := context.WithTimeout(runCtx, 45*time.Second)
			defer cancel()
			_, err := samsaraJob.Run(bounded)
			return err
		}})
	}()
	expenseExtractor := openai.NewClient(cfg.OpenAIAPIKey, cfg.OpenAIExpenseModel)
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
		grossBoardRepo,
		authRepo,
		customTaskRepo,
		cabCardExtractor,
		expenseExtractor,
		locationRefresher,
		wmtClient,
		httpapi.AuthOptions{
			CookieSecure: cfg.AuthCookieSecure,
			SessionTTL:   cfg.AuthSessionTTL,
		},
		cfg.FleetScope,
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

	if draining, ok := router.(interface{ Shutdown() }); ok {
		server.RegisterOnShutdown(draining.Shutdown)
	}

	serviceAutomationDone := make(chan struct{})
	dataTruckAutomationDone := make(chan struct{})
	prepassAssignmentsDone := make(chan struct{})
	go func() {
		defer close(prepassAssignmentsDone)
		jobs.RunIntervalJob(ctx, logger, jobs.IntervalJob{Name: "prepass-assignments", Interval: 15 * time.Second, Run: tollRepo.ReconcilePrePassAssignments})
	}()
	go func() {
		defer close(serviceAutomationDone)
		jobs.RunIntervalJob(ctx, logger, jobs.IntervalJob{Name: "driver-service-setup", Interval: 5 * time.Second, Run: repository.NewDriverServiceRepository(pool, fuelRepo, wmtRepo, dataTruckRepo).Process})
	}()
	go func() {
		defer close(dataTruckAutomationDone)
		jobs.RunIntervalJob(ctx, logger, jobs.IntervalJob{Name: "datatruck-truck-setup", Interval: 5 * time.Second, Run: dataTruckRepo.ProcessTrucks})
	}()
	wmtAutomationDone := make(chan struct{})
	relayAutomationDone := make(chan struct{})
	go func() {
		defer close(relayAutomationDone)
		if cfg.RelayAPIKey != "" {
			jobs.RunIntervalJob(ctx, logger, jobs.IntervalJob{Name: "relay-offboarding", Interval: time.Minute, Run: fuelRepo.ProcessRelayOffboarding})
		}
	}()
	go func() {
		defer close(wmtAutomationDone)
		jobs.RunIntervalJob(ctx, logger, jobs.IntervalJob{Name: "weighmytruck-membership", Interval: time.Minute, Run: wmtRepo.ProcessAutomation})
	}()
	escrowTasksDone := make(chan struct{})
	go func() {
		defer close(escrowTasksDone)
		jobs.RunIntervalJob(ctx, logger, jobs.IntervalJob{Name: "escrow-release-tasks", Interval: time.Minute, Run: func(runCtx context.Context) error {
			bounded, cancel := context.WithTimeout(runCtx, 30*time.Second)
			defer cancel()
			return customTaskRepo.GenerateEscrowTasks(bounded)
		}})
	}()
	maintenanceTasksDone := make(chan struct{})
	go func() {
		defer close(maintenanceTasksDone)
		repo := repository.NewMaintenanceRepository(pool)
		jobs.RunIntervalJob(ctx, logger, jobs.IntervalJob{Name: "maintenance-tasks", Interval: time.Minute, Run: func(runCtx context.Context) error {
			bounded, cancel := context.WithTimeout(runCtx, 50*time.Second)
			defer cancel()
			return jobs.RefreshMaintenanceTasks(bounded, repo, fiveELDJob)
		}})
	}()
	schedulerDone := make(chan struct{})
	boardAutomationDone := make(chan struct{})
	go func() {
		defer close(boardAutomationDone)
		repo := repository.NewDriverBoardRepository(pool, cfg.FiveELDEnabled || cfg.Samsara.Token != "")
		routes := boardroutes.NewClient(cfg.BoardRoutes)
		jobs.RunIntervalJob(ctx, logger, jobs.IntervalJob{Name: "board-automation", Interval: time.Minute, Run: func(runCtx context.Context) error {
			bounded, cancel := context.WithTimeout(runCtx, 40*time.Second)
			defer cancel()
			return repo.ReconcileAutomation(bounded, routes, fiveELDTelemetry, cfg.FiveELDUSDOT)
		}})
	}()
	fleetSyncDone := make(chan struct{})
	go func() {
		defer close(fleetSyncDone)
		repo := repository.NewFleetSyncRepository(pool, cfg.FleetScope.Sync)
		jobs.RunIntervalJob(ctx, logger, jobs.IntervalJob{Name: "fleetscope-fleet-sync", Interval: 5 * time.Second, Run: repo.Process})
	}()
	if cfg.ScheduledSyncsEnabled {
		go func() {
			defer close(schedulerDone)
			var workers sync.WaitGroup
			workers.Add(1)
			go func() {
				defer workers.Done()
				jobs.RunDailyScheduler(ctx, logger, cfg.ScheduledSyncsLocation,
					jobs.DailyJob{Name: "prepass-fleet", Hour: cfg.ScheduledFuelSyncTime.Hour, Minute: cfg.ScheduledFuelSyncTime.Minute, Run: func(ctx context.Context) error {
						if cfg.PrePassClientID == "" || cfg.PrePassClientSecret == "" {
							return nil
						}
						bounded, cancel := context.WithTimeout(ctx, 10*time.Minute)
						defer cancel()
						return tollRepo.RefreshPrePassFleet(bounded)
					}},
					jobs.DailyJob{Name: "weighmytruck", Hour: cfg.ScheduledFuelSyncTime.Hour, Minute: cfg.ScheduledFuelSyncTime.Minute, Run: func(ctx context.Context) error {
						if !wmtClient.Configured() {
							return nil
						}
						_, e := wmtRepo.SyncSettlements(ctx)
						return e
					}},
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
			for _, loadSync := range []jobs.IntervalJob{
				{Name: "new-loads", Interval: cfg.DataTruckNewLoadsInterval, Run: func(runCtx context.Context) error {
					_, err := loadJob.RunNew(runCtx)
					return err
				}},
				{Name: "operational-loads", Interval: cfg.DataTruckOperationalInterval, Run: func(runCtx context.Context) error {
					_, err := loadJob.RunOperational(runCtx)
					return err
				}},
			} {
				workers.Add(1)
				go func(job jobs.IntervalJob) {
					defer workers.Done()
					jobs.RunIntervalJob(ctx, logger, job)
				}(loadSync)
			}
			if fiveELDJob != nil {
				workers.Add(1)
				go func() {
					defer workers.Done()
					jobs.RunIntervalJob(ctx, logger, jobs.IntervalJob{Name: "five-eld", Interval: cfg.FiveELDSyncInterval, Run: func(runCtx context.Context) error {
						_, runErr := fiveELDJob.Run(runCtx)
						return runErr
					}})
				}()
			}
			workers.Wait()
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
	case <-relayAutomationDone:
	case <-shutdownCtx.Done():
	}
	select {
	case <-serviceAutomationDone:
	case <-shutdownCtx.Done():
	}
	select {
	case <-dataTruckAutomationDone:
	case <-shutdownCtx.Done():
	}
	select {
	case <-prepassAssignmentsDone:
	case <-shutdownCtx.Done():
	}
	select {
	case <-wmtAutomationDone:
	case <-shutdownCtx.Done():
	}
	select {
	case <-escrowTasksDone:
	case <-shutdownCtx.Done():
	}
	select {
	case <-maintenanceTasksDone:
	case <-shutdownCtx.Done():
	}
	select {
	case <-samsaraDone:
	case <-shutdownCtx.Done():
	}
	select {
	case <-fleetSyncDone:
	case <-shutdownCtx.Done():
	}
	select {
	case <-schedulerDone:
	case <-shutdownCtx.Done():
		logger.Warn("scheduled syncs did not stop before shutdown timeout")
	}
	select {
	case <-boardAutomationDone:
	case <-shutdownCtx.Done():
	}
}
