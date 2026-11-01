package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

func TestPayrollScaleBatchDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		if _, err := pool.Exec(ctx, `INSERT INTO weighmytruck_transactions(id,site_number,ticket_number,transaction_date,amount,driver_id,truck_id,match_warning,raw_payload)
 SELECT md5('scale-batch-'||n)::uuid,'test',n::text,day,15.50,'18200000-0000-0000-0000-000000000001',
 CASE WHEN n=4 THEN NULL ELSE '18200000-0000-0000-0000-000000000002'::uuid END,
 CASE WHEN n=3 THEN 'Source conflicts' ELSE '' END,'{}'
 FROM (VALUES(1,'2026-10-04'::date),(2,'2026-10-05'::date),(3,'2026-10-06'::date),(4,'2026-10-07'::date)) x(n,day)`); err != nil {
			t.Fatal(err)
		}
		weeks := []string{"2026-09-28", "2026-10-05", "2026-10-12"}
		for phase := 0; phase < 3; phase++ {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			read := payrollPublicationReadContext(ctx, tx)
			if err := preparePayrollScaleWeeks(read, tx, weeks); err != nil {
				t.Fatal(err)
			}
			for _, week := range weeks {
				measured, metrics := db.WithQueryMetrics(read)
				drivers, trucks, err := wmtPayrollCharges(measured, tx, week)
				if err != nil || metrics.Queries.Load() != 0 {
					t.Fatal("batch scale input unavailable", phase, week, err)
				}
				wantDrivers, wantTrucks, err := wmtPayrollChargesUncached(ctx, tx, week)
				want, _ := json.Marshal(payrollScaleWeek{wantDrivers, wantTrucks})
				got, _ := json.Marshal(payrollScaleWeek{drivers, trucks})
				if err != nil || string(want) != string(got) {
					t.Fatal("dated/frozen scale routing differs", phase, week, err, string(want), string(got))
				}
				if week != "2026-10-12" {
					count := len(drivers[projectionDriver]) + len(trucks[projectionTruck])
					if (phase == 1 && week == "2026-09-28" && count != 0) || ((phase != 1 || week != "2026-09-28") && count != 1) {
						t.Fatal("expected routing fixture not exercised", phase, week, count)
					}
				}
				for _, rows := range []map[string][]PayAutoCharge{drivers, trucks} {
					for id := range rows {
						rows[id][0].Amount = "999.00"
					}
				}
				drivers, trucks, err = wmtPayrollCharges(read, tx, week)
				got, _ = json.Marshal(payrollScaleWeek{drivers, trucks})
				if err != nil || string(want) != string(got) {
					t.Fatal("caller mutated retained scale inputs")
				}
			}
			_ = tx.Rollback(ctx)
			if phase == 0 {
				_, err = pool.Exec(ctx, `INSERT INTO payroll_settlements(driver_id,week_start,report)
 VALUES('18200000-0000-0000-0000-000000000001','2026-09-28',jsonb_build_object('autoCharges',jsonb_build_array(jsonb_build_object('source','weighmytruck:'||md5('scale-batch-1')::uuid::text))))`)
			} else if phase == 1 {
				_, err = pool.Exec(ctx, `UPDATE investors SET driver_id='18200000-0000-0000-0000-000000000001' WHERE id='18200000-0000-0000-0000-000000000003'`)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	})
}
