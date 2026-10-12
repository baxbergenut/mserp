package repository

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/fleetscope"
	"mserp/internal/weighmytruck"
	"os"
	"strings"
	"testing"
	"time"
)

type wmtFake struct {
	calls int
	fail  bool
}

func (p *wmtFake) Configured() bool { return true }
func (p *wmtFake) Change(_ context.Context, _ bool, _ weighmytruck.Driver) error {
	p.calls++
	if p.fail {
		return &weighmytruck.Error{Message: "uncertain", Uncertain: true}
	}
	return nil
}

func TestWMTSettlementDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_WMT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("local WMT test database required")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || !strings.Contains(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local test database required")
	}
	ctx := context.Background()
	admin, e := pgx.Connect(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close(ctx)
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("wmt_settlement_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			run := func(sql string, args ...any) {
				t.Helper()
				if _, e := admin.Exec(ctx, sql, args...); e != nil {
					t.Fatal(e)
				}
			}
			run(`CREATE SCHEMA ` + quoted + `;SET search_path TO ` + quoted + `,public`)
			defer admin.Exec(ctx, `DROP SCHEMA `+quoted+` CASCADE`)
			init, e := os.ReadFile("../../sql/init.sql")
			if e != nil {
				t.Fatal(e)
			}
			if mode == "migration" {
				before, _, ok := strings.Cut(string(init), "-- WeighMyTruck settlements (migration 074).")
				if !ok {
					t.Fatal("boundary")
				}
				run(before)
				m, e := os.ReadFile("../../sql/074_weighmytruck_settlements.sql")
				if e != nil {
					t.Fatal(e)
				}
				run(string(m))
				applyLaterTestMigrations(t, ctx, admin, "074")
			} else {
				run(string(init))
			}
			run(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app;GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app;GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA ` + quoted + ` TO mserp_app`)
			pc := cfg.Copy()
			pc.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			pc.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, pc)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			id := func(sql string, args ...any) string {
				t.Helper()
				var v string
				if e := pool.QueryRow(ctx, sql, args...).Scan(&v); e != nil {
					t.Fatal(e)
				}
				return v
			}
			driver := id(`INSERT INTO drivers(full_name,normalized_name,email,phone,pay_type,pay_rate) VALUES('Scale Driver','scale driver','scale@example.com','5551234567','cpm',0.75) RETURNING id`)
			owner := id(`INSERT INTO investors(full_name) VALUES('Scale Investor') RETURNING id`)
			truck := id(`INSERT INTO trucks(unit_number,owner_id) VALUES('074',$1) RETURNING id`, owner)
			week := ChargeCurrentWeek()
			date, _ := time.Parse(time.DateOnly, week)
			day := date.AddDate(0, 0, 2).Format(time.DateOnly)
			run(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,($3::date::timestamp AT TIME ZONE 'America/New_York'))`, truck, driver, week)
			run(`INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES($1,$2,$3::date,90)`, truck, owner, week)
			run(`INSERT INTO weighmytruck_imports(digest,row_count) VALUES('fixture',1)`)
			run(`INSERT INTO weighmytruck_memberships(driver_id,email,first_name,last_name,phone,driver_code,enrolled) VALUES($1,'scale@example.com','Scale','Driver','5551234567','007',true)`, driver)
			fake := &wmtFake{}
			repo := NewWeighMyTruckRepository(pool, fake)
			ticket := weighmytruck.Ticket{Number: "111", Site: "1", Date: day, Fee: "15.25", Tractor: "074", Raw: map[string]string{"Ticket Number": "111"}}
			if e := repo.StoreSettlements(ctx, day, []weighmytruck.Ticket{ticket}); e != nil {
				t.Fatal(e)
			}
			if e := repo.StoreSettlements(ctx, day, []weighmytruck.Ticket{ticket}); e != nil {
				t.Fatal(e)
			}
			list, e := repo.Transactions(ctx, week, date.AddDate(0, 0, 6).Format(time.DateOnly), "", Pagination{})
			if e != nil || list.Total != 1 || list.Items[0].DriverID != driver || list.Items[0].TruckID != truck || list.TotalAmount != "15.25" {
				t.Fatalf("import %+v %v", list, e)
			}
			pay := NewDriverPayRepository(pool)
			actor := id(`INSERT INTO app_users(username,password_hash) VALUES('scale-accountant','$2test') RETURNING id`)
			scales := func(report DriverPayWeek) []PayAutoCharge {
				out := []PayAutoCharge{}
				for _, d := range report.Drivers {
					for _, c := range d.AutoCharges {
						if strings.HasPrefix(c.Source, "weighmytruck:") {
							out = append(out, c)
						}
					}
				}
				return out
			}
			report, e := pay.Get(ctx, date)
			if e != nil || len(scales(report)) != 0 {
				t.Fatalf("company charge %v %v", scales(report), e)
			}
			if e = repo.SaveBilling(ctx, WMTBilling{DriverID: driver, WeekStart: week, Payer: "driver"}, "tester"); e != nil {
				t.Fatal(e)
			}
			report, e = pay.Get(ctx, date)
			if e != nil || len(scales(report)) != 1 || scales(report)[0].Amount != "-15.25" {
				t.Fatalf("CPM charge %+v %v", report, e)
			}
			if e = repo.SaveBilling(ctx, WMTBilling{DriverID: driver, WeekStart: week, Payer: "investor", Version: 1}, "tester"); e != nil {
				t.Fatal(e)
			}
			report, e = pay.Get(ctx, date)
			if e != nil || len(scales(report)) != 0 {
				t.Fatalf("double charge %v %v", scales(report), e)
			}
			investor, e := pay.InvestorPay(ctx, date)
			if e != nil || len(scales(investor)) != 1 || scales(investor)[0].Amount != "-15.25" {
				t.Fatalf("investor charge %+v %v", investor, e)
			}
			if e = repo.SaveBilling(ctx, WMTBilling{DriverID: driver, WeekStart: week, Payer: "company", Version: 1}, "tester"); e != ErrWMTConflict {
				t.Fatalf("stale billing %v", e)
			}
			// Unmatched source units never use the driver's current truck.
			ticket.Number = "112"
			ticket.Tractor = "UNKNOWN"
			ticket.DriverCode = "007"
			if e = repo.StoreSettlements(ctx, day, []weighmytruck.Ticket{ticket}); e != nil {
				t.Fatal(e)
			}
			list, e = repo.Transactions(ctx, week, date.AddDate(0, 0, 6).Format(time.DateOnly), "112", Pagination{})
			if e != nil || list.Items[0].TruckID != "" || list.Items[0].Warning == "" {
				t.Fatalf("unmatched %+v %v", list, e)
			}
			if e = repo.LinkTransaction(ctx, list.Items[0].ID, driver, truck, list.Items[0].Version, "tester"); e != nil {
				t.Fatal(e)
			}
			investor, e = pay.InvestorPay(ctx, date)
			if e != nil {
				t.Fatal(e)
			}
			final, e := pay.SettleInvestor(ctx, date, truck, investor.Revision, actor, "", false)
			if e != nil {
				t.Fatal(e)
			}
			if len(scales(final)) != 2 {
				t.Fatal("finalization lost scale tickets")
			}
			ticket.Fee = "20.00"
			if e = repo.StoreSettlements(ctx, day, []weighmytruck.Ticket{ticket}); e != nil {
				t.Fatal(e)
			}
			frozen, e := pay.InvestorPay(ctx, date)
			if e != nil || scales(frozen)[1].Amount != "-15.25" {
				t.Fatalf("frozen changed: %+v %v", scales(frozen), e)
			}
			list, e = repo.Transactions(ctx, week, date.AddDate(0, 0, 6).Format(time.DateOnly), "112", Pagination{})
			if e != nil || !strings.Contains(list.Items[0].Warning, "Finalized") {
				t.Fatalf("correction not flagged: %+v %v", list, e)
			}
			if e = repo.SaveBilling(ctx, WMTBilling{DriverID: driver, WeekStart: week, Payer: "driver", Version: 2}, actor); e == nil {
				t.Fatal("frozen payer changed")
			}
			if e = repo.LinkTransaction(ctx, list.Items[0].ID, driver, truck, list.Items[0].Version, actor); e == nil {
				t.Fatal("frozen link changed")
			}
			if _, e = pay.SettleInvestor(ctx, date, truck, frozen.Revision, actor, "Scale ticket correction", true); e != nil {
				t.Fatal(e)
			}
			updated, e := pay.InvestorPay(ctx, date)
			if e != nil || scales(updated)[1].Amount != "-20.00" {
				t.Fatalf("reopened correction: %+v %v", scales(updated), e)
			}
			// Automatic removal is one mutation, including repeated worker runs.
			run(`UPDATE drivers SET status='terminated',termination_date=current_date WHERE id=$1`, driver)
			if e = repo.ProcessAutomation(ctx); e != nil {
				t.Fatal(e)
			}
			if e = repo.ProcessAutomation(ctx); e != nil {
				t.Fatal(e)
			}
			if fake.calls != 1 {
				t.Fatalf("removal count %d", fake.calls)
			}
			run(`UPDATE drivers SET status='active',termination_date=NULL WHERE id=$1`, driver)
			tx, e := pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if e = queueWMTEnrollment(ctx, tx, driver, "tester"); e != nil {
				t.Fatal(e)
			}
			if e = tx.Commit(ctx); e != nil {
				t.Fatal(e)
			}
			if e = repo.ProcessAutomation(ctx); e != nil {
				t.Fatal(e)
			}
			if fake.calls != 2 {
				t.Fatal("queued onboarding not added")
			}
			fake.fail = true
			run(`UPDATE drivers SET status='terminated',termination_date=current_date WHERE id=$1`, driver)
			if e = repo.ProcessAutomation(ctx); e != nil {
				t.Fatal(e)
			}
			if e = repo.ProcessAutomation(ctx); e != nil {
				t.Fatal(e)
			}
			if fake.calls != 3 {
				t.Fatal("uncertain removal replayed")
			}
			// Onboarding commits the enrollment request with setup, and validation
			// failure leaves the hire pending without a partially created driver.
			fleet := NewFleetRepository(pool)
			event := fleetscope.Event{Version: 1, Type: "driver.hired", EventID: "07400000-0000-0000-0000-000000000011", CompanyID: "07400000-0000-0000-0000-000000000012", OccurredAt: time.Now(), Driver: fleetscope.Driver{ID: "07400000-0000-0000-0000-000000000013", FullName: "Onboard Scale", DriverType: "company", HireDate: week, Email: "onboard@example.test"}}
			hire, err := fleet.AcceptFleetScopeHire(ctx, event, "scale-onboard")
			if err != nil {
				t.Fatal(err)
			}
			input := DriverInput{FullName: "Onboard Scale", PayType: "cpm", PayRate: 0.65, Active: true, HireDate: &date}
			if _, err = fleet.CompleteDriverIntake(ctx, hire.IntakeID, actor, "", &input, false, true); err == nil {
				t.Fatal("invalid WMT onboarding succeeded")
			}
			if id(`SELECT count(*)::text FROM drivers WHERE full_name='Onboard Scale'`) != "0" {
				t.Fatal("failed onboarding created driver")
			}
			email, phone := "onboard@example.test", "5553211234"
			input.Email, input.Phone = &email, &phone
			created, err := fleet.CompleteDriverIntake(ctx, hire.IntakeID, actor, "", &input, false, true)
			if err != nil {
				t.Fatal(err)
			}
			if id(`SELECT state FROM weighmytruck_requests WHERE driver_id=$1`, created.ID) != "queued" {
				t.Fatal("onboarding queue missing")
			}
			fake.fail = false
			if err = repo.ProcessAutomation(ctx); err != nil {
				t.Fatal(err)
			}
			if id(`SELECT enrolled::text FROM weighmytruck_memberships WHERE driver_id=$1`, created.ID) != "true" {
				t.Fatal("onboarding did not enroll")
			}
		})
	}
}
