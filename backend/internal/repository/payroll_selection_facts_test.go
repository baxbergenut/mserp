package repository

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollFuelSelectionFactsDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		const driver = "18200000-0000-0000-0000-000000009001"
		exec := func(query string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, query, args...); err != nil {
				t.Fatal(err)
			}
		}
		exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date) VALUES($1,'Fuel only','fuel only','cpm',0.65,'2026-01-01')`, driver)
		prepare := func() {
			t.Helper()
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			snapshot, err := payrollCacheSnapshot(ctx, tx)
			if err == nil {
				err = preparePayrollFuelSelectionFacts(ctx, tx, snapshot)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
		check := func(week string, connected bool) {
			t.Helper()
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			read := payrollWeekContext(ctx, week)
			raw, err := selectPayrollContext(read, tx, "driver", driver)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := selectPayrollContext(context.WithValue(read, payrollSelectionFactsKey{}, true), tx, "driver", driver)
			if err != nil {
				t.Fatal(err)
			}
			want := raw.Value(payrollSelectionKey{}).(*payrollSelection)
			got := prepared.Value(payrollSelectionKey{}).(*payrollSelection)
			if !slices.Equal(want.drivers, got.drivers) || !slices.Equal(want.trucks, got.trucks) || !slices.Equal(want.owners, got.owners) || slices.Contains(got.trucks, projectionTruck) != connected {
				t.Fatal("fuel selection differs", week, connected, want, got)
			}
			// Distinct periods remain separate even when fetched in one statement.
			// Include the empty all-history scope to exercise the conservative path.
			weeks := []string{week, "2026-11-02"}
			batch, err := payrollSelectionBatchContext(ctx, tx, weeks)
			if err != nil {
				t.Fatal(err)
			}
			batched, err := selectPayrollContext(payrollWeekContext(batch, week), tx, "driver", driver)
			if err != nil {
				t.Fatal(err)
			}
			group := batched.Value(payrollSelectionKey{}).(*payrollSelection)
			if !slices.Equal(want.drivers, group.drivers) || !slices.Equal(want.trucks, group.trucks) || !slices.Equal(want.owners, group.owners) {
				t.Fatal("batched period graph differs", week, want, group)
			}
		}
		prepare() // Complete empty scope must not conceal later matching inserts.
		check("", false)
		check("2026-10-05", false)
		exec(`INSERT INTO fuel_transactions(relay_environment,relay_transaction_id,driver_id,relay_driver_id,purchased_at,total_amount_paid,total_retail_price,total_amount_saved,is_direct_bill,currency_code,merchant_id,merchant_name,merchant_number,location_id,location_name,merchant_location_id,address,city,state,postal_code,latitude,longitude,timezone,prompts,raw_payload)
 VALUES('production','selection-fact',$1,'identity','2026-10-06 12:00+00',100,100,0,false,'USD','','Pilot','','','','','','City','OH','',0,0,'US/Eastern','[{"label":"Truck #","value":"PROJECTION"}]','{}')`, driver)
		for _, cached := range []bool{false, true} {
			if cached {
				prepare()
			}
			check("", true)
			check("2026-09-28", false)
			check("2026-10-05", true)
			check("2026-10-12", true) // Previous-week cost routing remains included.
			check("2026-10-19", false)
		}
		exec(`UPDATE fuel_transactions SET purchased_at='2026-10-13 12:00+00' WHERE relay_transaction_id='selection-fact'`)
		check("2026-10-05", false) // Exact source revisions reject old-week facts.
		check("2026-10-19", true)
		prepare()
		check("2026-10-05", false)
		check("2026-10-19", true)
		exec(`DELETE FROM fuel_transactions WHERE relay_transaction_id='selection-fact'`)
		check("", false)
		prepare()
		check("", false)
		check("2026-10-19", false)
	})
}
