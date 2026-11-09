package repository

import (
	"context"
	"encoding/json"
	"errors"
	"mserp/internal/boardroutes"
	"mserp/internal/fiveeld"
	"strings"
	"time"
)

var ErrETARetryUnavailable = errors.New("automatic ETA cannot be retried for this selection")
var ErrETARetryCooldown = errors.New("ETA was just retried. Please wait a few seconds and try again.")

// RequestETARetry claims a retry while the immediate runner holds the automation lock.
func (r *DriverBoardRepository) RequestETARetry(ctx context.Context, id, key string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var active bool
	if err = tx.QueryRow(ctx, `SELECT active FROM drivers WHERE id=$1 FOR UPDATE`, id).Scan(&active); err != nil {
		return err
	}
	if !active {
		return ErrETARetryUnavailable
	}
	views, err := readBoardLoads(ctx, tx, []string{id}, r.loadStartDate(""), r.now())
	if err != nil {
		return err
	}
	a := views[id].Automation
	if a == nil || a.Context == "" || a.Context != key || a.RequiresConfirmation || !a.RoutingEnabled || !a.TelemetrySafe || a.Arrived || a.TravelMode != "auto" || a.TravelValue != "" {
		return ErrETARetryUnavailable
	}
	entry, err := boardEntry(ctx, tx, id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(entry.ETA) != "" && a.ManualETAValid {
		return ErrETARetryUnavailable
	}
	var travel BoardTravel
	if err = tx.QueryRow(ctx, `SELECT travel FROM driver_board_automation WHERE driver_id=$1`, id).Scan(&travel); err != nil {
		return err
	}
	now := r.now().UTC()
	var notBefore *time.Time
	if err = tx.QueryRow(ctx, `SELECT route_not_before FROM driver_board_automation WHERE driver_id=$1`, id).Scan(&notBefore); err != nil {
		return err
	}
	if notBefore != nil && now.Before(*notBefore) {
		return ErrETARetryBusy
	}
	// Manual clicks do not inherit the background worker's failed-route backoff.
	if !travel.RetryRequestedAt.IsZero() && now.Sub(travel.RetryRequestedAt) < 5*time.Second {
		return ErrETARetryCooldown
	}
	if travel.Context != key {
		travel = BoardTravel{Context: key, Enabled: true}
	}
	travel.RetryRequestedAt, travel.RetryCompletedAt = now, now
	body, err := json.Marshal(travel)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE driver_board_automation SET travel=$2,updated_at=now() WHERE driver_id=$1`, id, body); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

var ErrETARetryBusy = errors.New("ETA calculation is busy or the stop is still being edited; please try again shortly")
var ErrETARetryDisabled = errors.New("automatic ETA routing is unavailable")

// ConfigureETARetry is called once during startup, before serving requests.
func (r *DriverBoardRepository) ConfigureETARetry(routes *boardroutes.Client, eld *fiveeld.Client, usdot string) {
	r.retryRoutes, r.retryELD, r.retryUSDOT = routes, eld, usdot
}

// RetryETANow runs one driver's normal provider calculation before returning.
// Its claim is consumed before provider work, so cancellation/crashes never queue a retry.
func (r *DriverBoardRepository) RetryETANow(ctx context.Context, id, key string) (BoardLoads, error) {
	if r.retryRoutes == nil || !r.retryRoutes.Options.Enabled {
		return BoardLoads{}, ErrETARetryDisabled
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return BoardLoads{}, err
	}
	defer conn.Release()
	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(718504115)`).Scan(&locked); err != nil {
		return BoardLoads{}, err
	}
	if !locked {
		return BoardLoads{}, ErrETARetryBusy
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(cleanup, `SELECT pg_advisory_unlock(718504115)`)
	}()
	// Retire a legacy queued claim before handling this explicit immediate request.
	// Even if validation/backoff rejects it, it must not run later in the background.
	if _, err = conn.Exec(ctx, `UPDATE driver_board_automation SET travel=jsonb_set(travel,'{retryCompletedAt}',travel->'retryRequestedAt'),updated_at=now() WHERE driver_id=$1 AND travel->>'context'=$2 AND travel ? 'retryRequestedAt' AND travel->>'retryCompletedAt' IS DISTINCT FROM travel->>'retryRequestedAt'`, id, key); err != nil {
		return BoardLoads{}, err
	}
	if err = r.RequestETARetry(ctx, id, key); err != nil {
		return BoardLoads{}, err
	}
	if err = r.reconcileAutomation(ctx, conn, r.retryRoutes, r.retryELD, r.retryUSDOT, id, key); err != nil {
		return BoardLoads{}, err
	}
	view, err := r.Loads(ctx, id, "")
	if err != nil {
		return BoardLoads{}, err
	}
	if view.Automation == nil || view.Automation.Context != key {
		return BoardLoads{}, ErrETARetryUnavailable
	}
	// A concurrent edit may have imposed its settle fence after the claim.
	if view.Automation.TravelRetryPending {
		return BoardLoads{}, ErrETARetryBusy
	}
	return view, nil
}
