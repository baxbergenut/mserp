package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

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
				if count := len(drivers[projectionDriver]) + len(trucks[projectionTruck]); count != 2-min(phase, 1) {
					t.Fatal("expected all unpaid tickets across every week", phase, week, count)
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
 VALUES('18200000-0000-0000-0000-000000000001','2026-10-12',jsonb_build_object('autoCharges',jsonb_build_array(jsonb_build_object('source','weighmytruck:'||md5('scale-batch-1')::uuid::text))))`)
			} else if phase == 1 {
				_, err = pool.Exec(ctx, `UPDATE investors SET driver_id='18200000-0000-0000-0000-000000000001' WHERE id='18200000-0000-0000-0000-000000000003'`)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestPayrollScaleCollectionAcrossWeeksDatabase(t *testing.T) {
	for _, kind := range []string{"driver", "investor"} {
		t.Run(kind, func(t *testing.T) {
			withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
				seedPayrollProjectionTest(t, ctx, pool)
				const actor = "18200000-0000-0000-0000-000000007400"
				const ticket = "18200000-0000-0000-0000-000000007401"
				if _, err := pool.Exec(ctx, `INSERT INTO app_users(id,username,password_hash) VALUES
 ('18200000-0000-0000-0000-000000007400','Scale payroll test',crypt('test-only',gen_salt('bf')));
 INSERT INTO weighmytruck_transactions(id,site_number,ticket_number,transaction_date,amount,driver_id,truck_id,raw_payload)
 VALUES('18200000-0000-0000-0000-000000007401','test','unpaid-scale','2026-10-21',15.25,
 '18200000-0000-0000-0000-000000000001','18200000-0000-0000-0000-000000000002','{}')`); err != nil {
					t.Fatal(err)
				}
				statement := projectionTruck
				if kind == "driver" {
					statement = projectionDriver
					if _, err := pool.Exec(ctx, `UPDATE investors SET driver_id=$1 WHERE full_name='Projection Investor'`, projectionDriver); err != nil {
						t.Fatal(err)
					}
				}
				repo := NewDriverPayRepository(pool)
				repo.ConfigurePreparedReads(true)
				repo.ConfigurePreparedCommands(true)
				repo.ConfigurePreparedFinalization(true)
				repo.resourceConcurrency = true
				if err := repo.EnablePayrollShadow(ctx); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources'`); err != nil {
					t.Fatal(err)
				}
				earlier, _ := chargeWeek("2026-10-12")
				later, _ := chargeWeek("2026-10-26")
				for _, week := range []time.Time{earlier, later} {
					if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
						t.Fatal(err)
					}
					if worked, err := repo.PrepareNextPayrollWeek(ctx); err != nil || !worked {
						t.Fatal("prepare scale fixture", worked, err)
					}
				}
				read := func(week time.Time, want bool) DriverPayWeek {
					t.Helper()
					page, err := repo.PreparedPayroll(ctx, kind, week, PayPageQuery{ID: statement}, true)
					if err != nil || page.Preparation.State != "ready" {
						t.Fatal("read prepared scale fixture", page.Preparation, err)
					}
					found := false
					for _, d := range page.Drivers {
						for _, row := range d.AutoCharges {
							found = found || row.Source == "weighmytruck:"+ticket && row.Amount == "-15.25"
						}
					}
					if found != want {
						t.Fatal("unexpected scale charge", week, page.Drivers)
					}
					return page
				}
				settle := func(page DriverPayWeek, week time.Time, request string, reopen bool) error {
					t.Helper()
					d := page.Drivers[0]
					command := PayrollSettlementCommand{RequestID: request, ExpectedStatementRevision: d.StatementRevision, ExpenseRevision: d.ExpenseRevision}
					settle := repo.SettleInvestor
					if kind == "driver" {
						settle = repo.Settle
					}
					_, err := settle(WithPayrollReceipt(ctx), week, statement, page.Revision, actor, "", reopen, false, command)
					return err
				}
				first, stale := read(earlier, true), read(later, true)
				if err := settle(first, earlier, "18200000-0000-0000-0000-000000007402", false); err != nil {
					t.Fatal("finalize an earlier week with a later ticket", err)
				}
				frozen := read(earlier, true)
				read(later, false)
				if err := settle(stale, later, "18200000-0000-0000-0000-000000007403", false); !errors.Is(err, ErrDriverPayConflict) {
					t.Fatal("stale statement could collect the same ticket", err)
				}
				if err := settle(frozen, earlier, "18200000-0000-0000-0000-000000007404", true); err != nil {
					t.Fatal("reopen collecting statement", err)
				}
				read(earlier, true)
				read(later, true)
			})
		})
	}
}
