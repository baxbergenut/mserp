package repository

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"mserp/internal/boardroutes"
)

// RunReadyLocationListener responds to committed READY transitions without waiting
// for the minute automation pass. Notifications are only a wake-up hint: the
// transaction also records a durable pending request, which the minute worker
// recovers after a disconnected listener, process restart, or bounded failure.
func (r *DriverBoardRepository) RunReadyLocationListener(ctx context.Context, routes *boardroutes.Client, logger *slog.Logger) {
	if routes == nil || !routes.Options.Enabled || routes.Options.TomTomAPIKey == "" {
		return
	}
	for ctx.Err() == nil {
		err := r.listenReadyLocations(ctx, routes, logger)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Warn("ready location listener disconnected; minute worker retains pending requests")
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (r *DriverBoardRepository) listenReadyLocations(ctx context.Context, routes *boardroutes.Client, logger *slog.Logger) error {
	// A dedicated connection avoids retaining a scarce application pool slot
	// while waiting and never returns a LISTEN session to ordinary callers.
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, err := pgx.ConnectConfig(connectCtx, r.pool.Config().ConnConfig.Copy())
	cancel()
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = conn.Close(closeCtx)
	}()
	listenCtx, listenCancel := context.WithTimeout(ctx, 10*time.Second)
	_, err = conn.Exec(listenCtx, "LISTEN board_ready_location")
	listenCancel()
	if err != nil {
		return err
	}
	for {
		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		if notification.Channel != "board_ready_location" || notification.Payload == "" {
			continue
		}
		// One serialized runner prevents bursts of edits from spawning unbounded
		// goroutines. The repository checks current status and request identity
		// again, so obsolete/duplicate notifications do not make provider calls.
		runCtx, runCancel := context.WithTimeout(ctx, 40*time.Second)
		err = r.RefreshReadyLocation(runCtx, routes, notification.Payload)
		runCancel()
		if err != nil && ctx.Err() == nil {
			logger.Warn("ready location refresh deferred to minute worker")
		}
	}
}
