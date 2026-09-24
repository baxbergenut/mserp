package repository

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"mserp/internal/prepass"
)

const updateTollLocationSQL = `UPDATE tolls SET
	toll_agency_state = COALESCE($3, toll_agency_state),
	toll_agency_name = COALESCE($4, toll_agency_name),
	entry_plaza_name = COALESCE($5, entry_plaza_name),
	exit_plaza_name = COALESCE($6, exit_plaza_name),
	location_synced_at = $7
	WHERE prepass_environment = $1 AND prepass_toll_id = $2`

func tollLocationArgs(environment string, value prepass.Transaction, syncedAt time.Time) []any {
	return []any{environment, value.TollID, optionalString(normalizeTollState(value.TollAgencyState)),
		optionalString(value.TollAgencyName), optionalString(value.EntryPlazaName), optionalString(value.ExitPlazaName), syncedAt}
}

// Refresh location metadata only; leave charges, assignments and sync-day records intact.
func (r *TollRepository) UpdateTollLocations(ctx context.Context, environment string, values []prepass.Transaction, syncedAt time.Time) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	batch := &pgx.Batch{}
	for _, value := range values {
		batch.Queue(updateTollLocationSQL, tollLocationArgs(environment, value, syncedAt)...)
	}
	results := tx.SendBatch(ctx, batch)
	var updated int64
	for range values {
		command, err := results.Exec()
		if err != nil {
			results.Close()
			return 0, err
		}
		updated += command.RowsAffected()
	}
	if err := results.Close(); err != nil {
		return 0, err
	}
	return updated, tx.Commit(ctx)
}

func normalizeTollState(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if code, ok := tollStateNames[value]; ok {
		return code
	}
	// Keep unrecognized source values visible but do not plot them as a US state.
	return value
}
