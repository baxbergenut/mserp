package repository

import (
	"context"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

func TestPayrollPublicationTruckInputsDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `INSERT INTO investor_pay_weeks(truck_id,owner_id,week_start,edits)
 VALUES($1,'18200000-0000-0000-0000-000000000003','2026-10-05','{"notes":"post-write"}');`, projectionTruck); err != nil {
			t.Fatal(err)
		}
		read := payrollPublicationReadContext(ctx, tx)
		for _, date := range []string{"2026-10-05", "2026-10-12"} {
			for _, costsOnly := range []bool{false, true} {
				for _, trucks := range [][]string{nil, {}, {projectionTruck}} {
					selected := context.WithValue(read, payrollCostsOnlyKey{}, costsOnly)
					selected = context.WithValue(selected, payrollSelectionKey{}, &payrollSelection{trucks: trucks})
					want, err := readTruckPayInputsUncached(selected, tx, date)
					if err != nil {
						t.Fatal(err)
					}
					got, err := readTruckPayInputs(selected, tx, date)
					if err != nil || !reflect.DeepEqual(got, want) {
						t.Fatal("publication routing inputs differ", date, costsOnly, trucks, err)
					}
					got.metadata["caller"] = truckPayMetadata{unit: "changed"}
					for _, edit := range got.edits {
						if len(edit.edits) > 0 {
							edit.edits[0] = 'x'
						}
					}
					for _, drivers := range got.assigned {
						drivers["caller"] = true
					}
					measured, metrics := db.WithQueryMetrics(selected)
					again, err := readTruckPayInputs(measured, tx, date)
					if err != nil || !reflect.DeepEqual(again, want) || metrics.Queries.Load() != 0 {
						t.Fatal("shared inputs mutated or reloaded", date, err, metrics.Queries.Load())
					}
					authority, metrics := db.WithQueryMetrics(context.WithValue(selected, payrollAuthoritativeKey{}, true))
					if _, err := readTruckPayInputs(authority, tx, date); err != nil || metrics.Queries.Load() == 0 {
						t.Fatal("authoritative path reused derived inputs", err, metrics.Queries.Load())
					}
				}
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE investor_pay_weeks SET edits='{"notes":"new phase"}' WHERE truck_id=$1`, projectionTruck); err != nil {
			t.Fatal(err)
		}
		// A canonical write must start a new phase even in the same transaction.
		fresh := payrollPublicationReadContext(read, tx)
		got, err := readTruckPayInputs(fresh, tx, "2026-10-05")
		want, wantErr := readTruckPayInputsUncached(ctx, tx, "2026-10-05")
		if err != nil || wantErr != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("post-write phase retained stale inputs", err, wantErr)
		}
	})
}
