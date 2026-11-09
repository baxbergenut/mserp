package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrETARetryUnavailable = errors.New("automatic ETA cannot be retried for this selection")
var ErrETARetryCooldown = errors.New("please wait five minutes before retrying automatic ETA again")

// RequestETARetry queues provider work; HTTP handlers never call paid providers.
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
	views, err := readBoardLoads(ctx, tx, []string{id}, r.loadStartDate(""))
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
	if travel.Context == key && travel.retryPending() {
		return tx.Commit(ctx)
	}
	if !travel.RetryRequestedAt.IsZero() && now.Sub(travel.RetryRequestedAt) < 5*time.Minute {
		return ErrETARetryCooldown
	}
	if travel.Context != key {
		travel = BoardTravel{Context: key, Enabled: true}
	}
	travel.RetryRequestedAt = now
	body, err := json.Marshal(travel)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE driver_board_automation SET travel=$2,updated_at=now() WHERE driver_id=$1`, id, body); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
