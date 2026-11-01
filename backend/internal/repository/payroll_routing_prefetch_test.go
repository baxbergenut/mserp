package repository

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

func TestPayrollRoutingPrefetchDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		weeks := []time.Time{}
		for _, date := range []string{"2026-09-28", "2026-10-05", "2026-10-12"} {
			week, _ := chargeWeek(date)
			weeks = append(weeks, week)
		}
		for phase := 0; phase < 3; phase++ {
			if phase == 2 {
				if _, err := pool.Exec(ctx, `INSERT INTO tolls(truck_id,posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint)
 VALUES($1,'2026-10-06','2026-10-06','','','','','PROJECTION','','','2026-10-06','12:00','',40,repeat('b',64))`, projectionTruck); err != nil {
					t.Fatal(err)
				}
			}
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			snapshot, err := payrollCacheSnapshot(ctx, tx)
			if err != nil {
				t.Fatal(err)
			}
			read := payrollRoutingFactsContext(context.WithValue(ctx, payrollNormalizedFactsKey{}, true), snapshot)
			if phase > 0 {
				if err = prefetchPayrollRoutingFacts(read, tx, weeks); err != nil {
					t.Fatal(err)
				}
			}
			for _, week := range weeks {
				date := week.Format(time.DateOnly)
				want, err := readTruckCostAllocationsUncached(ctx, tx, date)
				if err != nil {
					t.Fatal(err)
				}
				measured, metrics := db.WithQueryMetrics(read)
				got, err := readTruckCostAllocations(measured, tx, date)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatal("prefetched cost facts differ", phase, date, got, want, err)
				}
				if phase == 1 && metrics.Queries.Load() != 0 {
					t.Fatal("complete prefetched fact was queried again", metrics.Queries.Load())
				}
				if phase == 2 && metrics.Queries.Load() == 0 {
					t.Fatal("changed source used old revision cohort")
				}
				// Returned maps are owned by each consumer, including when there
				// are no records in this week.
				got["caller-only"] = &truckCostAllocation{fuel: 999}
				again, err := readTruckCostAllocations(read, tx, date)
				if err != nil || !reflect.DeepEqual(again, want) {
					t.Fatal("prefetch shared mutable source input", date, err)
				}
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
	})
}
