package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Fill only an explicitly pending blank, once. Source updates never replace a
// saved amount. The caller holds the exclusive source/financial gate, so this
// canonical write, its audit, and payroll invalidation share the import commit.
func fillPendingDriverGross(ctx context.Context, tx pgx.Tx, ids []int) error {
	var supported bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid=to_regclass('gross_board_entries') AND attname='driver_gross_pending' AND NOT attisdropped)`).Scan(&supported); err != nil {
		return err
	}
	if !supported {
		return nil
	}
	for _, table := range []string{"gross_board_entries", "gross_board_extra_entries"} {
		slot, deleted := "0", "false"
		if table == "gross_board_extra_entries" {
			slot, deleted = "slot", "deleted"
		}
		rows, err := tx.Query(ctx, `SELECT e.driver_id::text,e.service_date::text,e.slot,e.version,trip_source.driver_gross::text
 FROM (SELECT g.*,`+slot+` source_slot,`+deleted+` source_deleted FROM `+table+` g
 WHERE g.driver_gross_pending AND g.driver_rate IS NULL) g
 CROSS JOIN LATERAL (SELECT g.driver_id,g.service_date,g.source_slot slot,g.version,g.load_number,g.load_record_id,g.load_trip_id,g.source_deleted deleted,g.day_status) e
 `+grossBoardResolvedLoad+loadTripSourceSQL+`
 WHERE l.id=ANY($1::int[]) AND trip_source.driver_gross IS NOT NULL AND NOT trip_source.review
 AND NOT EXISTS(SELECT 1 FROM gross_board_transfers t WHERE t.driver_id=e.driver_id AND t.source_date=e.service_date AND t.source_slot=e.slot AND t.active)
 ORDER BY e.driver_id,e.service_date,e.slot`, ids)
		if err != nil {
			return err
		}
		type pending struct {
			input PaySourceAcceptance
			gross string
		}
		candidates := []pending{}
		for rows.Next() {
			var p pending
			if err = rows.Scan(&p.input.DriverID, &p.input.Date, &p.input.Slot, &p.input.Version, &p.gross); err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, p)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, p := range candidates {
			nested, err := tx.Begin(ctx)
			if err != nil {
				return err
			}
			err = assertPaySourceOpen(ctx, nested, p.input)
			if err == nil {
				err = setBoardActor(ctx, nested, "", "datatruck_driver_gross")
			}
			if err == nil {
				_, err = nested.Exec(ctx, `UPDATE `+table+` SET driver_rate=$5::numeric,version=version+1,updated_at=now()
 WHERE driver_id=$1 AND service_date=$2::date AND `+slot+`=$3 AND version=$4 AND driver_rate IS NULL`, p.input.DriverID, p.input.Date, p.input.Slot, p.input.Version, p.gross)
			}
			if err != nil {
				_ = nested.Rollback(ctx)
				var validation *ChargeValidationError
				var database *pgconn.PgError
				if errors.As(err, &validation) || (errors.As(err, &database) && (strings.Contains(strings.ToLower(database.Message), "finaliz") || strings.Contains(strings.ToLower(database.Message), "reopen"))) {
					continue
				}
				return err
			}
			if err = nested.Commit(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}
