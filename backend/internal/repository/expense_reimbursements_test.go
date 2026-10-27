package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExpenseReimbursementsDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set a disposable _test database")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.Contains(u.Path, "_test") {
		t.Fatal("only disposable _test databases allowed")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/157_expense_reimbursement_payments.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("reimburse_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, e := admin.Exec(ctx, sql, args...); e != nil {
					t.Fatal(e)
				}
			}
			id := func(sql string, args ...any) string {
				t.Helper()
				var s string
				if e := admin.QueryRow(ctx, sql, args...).Scan(&s); e != nil {
					t.Fatal(e)
				}
				return s
			}
			exec("CREATE SCHEMA " + quoted + "; SET search_path TO " + quoted + ",public")
			defer func() { _, _ = admin.Exec(ctx, "SET search_path TO public; DROP SCHEMA "+quoted+" CASCADE") }()
			initial := strings.ReplaceAll(string(source), "\r\n", "\n")
			body := strings.ReplaceAll(string(migration), "\r\n", "\n")
			if mode == "migration" {
				if strings.Count(initial, body) != 1 {
					t.Fatal("missing reimbursement migration")
				}
				initial = strings.Replace(initial, body, "", 1)
			}
			exec(initial)
			var legacyExpense, legacyDriver string
			if mode == "migration" {
				legacyDriver = id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Legacy credit','legacy credit','cpm',0.75) RETURNING id`)
				legacyExpense = id(`INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,payment_type,driver_id,covered_by) SELECT id,name,'Legacy reimbursement','2026-09-30',12.25,'Reimburse',$1,'Company' FROM expense_settings WHERE kind='category' AND name='Other' RETURNING id`, legacyDriver)
				old := DriverPayDriver{ID: legacyDriver, FullName: "Legacy credit", PayType: "cpm", PayRate: "0.75", Loads: []DriverPayLoad{}, Edits: DriverPayEdits{DriverID: legacyDriver, WeekStart: "2026-09-28"}, AutoCharges: []PayAutoCharge{{Name: "Legacy reimbursement", Amount: "12.25", Source: "expense-reimburse:" + legacyExpense}}}
				b, _ := json.Marshal(old)
				exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-09-28',$2::jsonb)`, legacyDriver, string(b))
				exec(string(migration))
				if id(`SELECT amount::text FROM expense_reimbursement_payments WHERE expense_id=$1`, legacyExpense) != "12.25" {
					t.Fatal("old frozen credit not reserved")
				}
			}
			exec("GRANT USAGE ON SCHEMA " + quoted + " TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + quoted + " TO mserp_app")
			cfg, e := pgxpool.ParseConfig(dsn)
			if e != nil {
				t.Fatal(e)
			}
			cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			cfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			pay := NewDriverPayRepository(pool)
			expenses := NewExpenseRepository(pool)
			week, _ := time.Parse(time.DateOnly, "2026-09-28")
			read := func(driver string, w time.Time) DriverPayDriver {
				t.Helper()
				r, e := pay.GetDriverWeek(ctx, w, driver)
				if e != nil {
					t.Fatal(e)
				}
				for _, d := range r.Drivers {
					if d.ID == driver {
						return d
					}
				}
				return DriverPayDriver{ID: driver, Edits: DriverPayEdits{DriverID: driver, WeekStart: w.Format(time.DateOnly)}}
			}
			row := func(d DriverPayDriver, expense string) *ExpenseDeduction {
				for i := range d.Edits.ExpenseDeductions {
					if d.Edits.ExpenseDeductions[i].ExpenseID == expense {
						return &d.Edits.ExpenseDeductions[i]
					}
				}
				return nil
			}
			setting := id(`SELECT id::text FROM expense_settings WHERE kind='payment_method' AND name='Reimburse' AND active`)
			_, e = expenses.SaveExpenseSetting(ctx, setting, ExpenseSetting{Kind: "payment_method", Name: "Refund", Active: true, Version: 1})
			if !errors.Is(e, ErrExpenseSettingPermanent) {
				t.Fatal("permanent method editable", e)
			}
			for _, q := range []string{`UPDATE expense_settings SET name='Refund' WHERE id=$1`, `UPDATE expense_settings SET active=false WHERE id=$1`, `DELETE FROM expense_settings WHERE id=$1`} {
				if _, e = pool.Exec(ctx, q, setting); e == nil {
					t.Fatal("permanent method changed")
				}
			}
			for _, classification := range []string{"company", "investor_driver", "owner_operator", "unassigned"} {
				for _, tariff := range []string{"cpm", "gross_percentage"} {
					name := classification + " " + tariff
					driver := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,hire_date,roster_start_week) VALUES($1,$1,$2,0.75,'2026-09-01','2026-09-07') RETURNING id`, name, tariff)
					var truck, owner any
					responsibility := "Company"
					if classification != "unassigned" {
						owner = "00000000-0000-0000-0000-000000000001"
						if classification != "company" {
							owner = id(`INSERT INTO investors(full_name,driver_id) VALUES($1,$2) RETURNING id`, name, driver)
							if classification == "investor_driver" {
								exec(`UPDATE investors SET driver_id=NULL WHERE id=$1`, owner)
							}
							responsibility = "Truck Owner"
						}
						truck = id(`INSERT INTO trucks(unit_number,owner_id) VALUES($1,$2) RETURNING id`, name, owner)
						exec(`UPDATE truck_ownership_history SET assigned_at='2026-09-01',start_known=true WHERE truck_id=$1`, truck)
						exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-09-01')`, truck, driver)
						if classification != "company" {
							exec(`INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES($1,$2,'2026-09-07',88)`, truck, owner)
						}
					}
					expense := id(`INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,payment_type,driver_id,truck_id,covered_by,owner_id) SELECT id,name,'Parking refund','2026-10-07',42.25,'Reimburse',$1,$2,$3,$4 FROM expense_settings WHERE kind='category' AND name='Other' RETURNING id`, driver, truck, responsibility, owner)
					card := read(driver, week)
					credit := row(card, expense)
					if card.EmploymentType != classification || credit == nil || credit.Source != "reimburse" || credit.Amount != "42.25" || credit.ExpenseDate != "2026-10-07" || len(card.AutoCharges) != 0 {
						t.Fatalf("wrong %s payout: %+v", name, card)
					}
					page := PaginatePay(DriverPayWeek{Drivers: []DriverPayDriver{card}}, PayPageQuery{})
					if page.Pagination.Payable != "42.25" {
						t.Fatal("credit not positive", page.Pagination)
					}
					if row(read(driver, week.AddDate(0, 0, -7)), expense) != nil {
						t.Fatal("following Thursday boundary leaked")
					}
					credit.Apply = true
					edits := card.Edits
					edits.ExpenseDeductions = []ExpenseDeduction{*credit}
					if _, e = pay.Save(ctx, edits); e != nil {
						t.Fatal(e)
					}
					if row(read(driver, week.AddDate(0, 0, 7)), expense) != nil {
						t.Fatal("paid reimbursement repeated next week")
					}
				}
			}
			// Exact same cut-off as ordinary expenses: following Wednesday in, Thursday out.
			driver := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Partial credit','partial credit','cpm',0.75) RETURNING id`)
			makeExpense := func(date string) string {
				return id(`INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,payment_type,driver_id,covered_by) SELECT id,name,'Partial refund',$2::date,100.25,'Reimburse',$1,'Company' FROM expense_settings WHERE kind='category' AND name='Other' RETURNING id`, driver, date)
			}
			expense := makeExpense("2026-10-07")
			outside := makeExpense("2026-10-08")
			card := read(driver, week)
			if row(card, outside) != nil {
				t.Fatal("Thursday entered prior statement")
			}
			if id(`SELECT count(*)::text FROM expense_reimbursement_payments WHERE expense_id=$1`, expense) != "0" {
				t.Fatal("read paid reimbursement")
			}
			original := *row(card, expense)
			original.Apply = true
			original.Amount = "0.00"
			card.Edits.ExpenseDeductions = []ExpenseDeduction{original}
			saved, e := pay.Save(ctx, card.Edits)
			if e != nil {
				t.Fatal(e)
			}
			next := read(driver, week.AddDate(0, 0, 7))
			if row(next, expense).Amount != "100.25" {
				t.Fatal("zero did not defer")
			}
			current := read(driver, week)
			partial := *row(current, expense)
			partial.Apply = true
			partial.Amount = "40.10"
			current.Edits.ExpenseDeductions = []ExpenseDeduction{partial}
			if _, e = pay.Save(ctx, current.Edits); e != nil {
				t.Fatal(e)
			}
			if _, e = pay.Save(ctx, saved); !errors.Is(e, ErrDriverPayConflict) {
				t.Fatal("stale save accepted", e)
			}
			next = read(driver, week.AddDate(0, 0, 7))
			if row(next, expense).Amount != "60.15" {
				t.Fatal("unpaid remainder did not carry", row(next, expense))
			}
			invoice, e := expenses.GetExpense(ctx, expense)
			if e != nil || invoice.PaidAmount == nil || *invoice.PaidAmount != "40.10" || invoice.RemainingAmount == nil || *invoice.RemainingAmount != "60.15" || len(invoice.Payments) != 1 {
				t.Fatal("expense payout balance/history mismatch", invoice, e)
			}
			part := *row(next, expense)
			part.Apply = true
			part.Amount = "10.00"
			next.Edits.ExpenseDeductions = []ExpenseDeduction{part}
			if _, e = pay.Save(ctx, next.Edits); e != nil {
				t.Fatal(e)
			}
			current = read(driver, week)
			if row(current, expense).Available != "90.25" {
				t.Fatal("future reservation ignored")
			}
			over := *row(current, expense)
			over.Apply = true
			over.Amount = "90.26"
			current.Edits.ExpenseDeductions = []ExpenseDeduction{over}
			if _, e = pay.Save(ctx, current.Edits); e == nil {
				t.Fatal("overpayment accepted")
			}
			current = read(driver, week)
			b, _ := json.Marshal(current)
			exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,$2::date,$3::jsonb)`, driver, week.Format(time.DateOnly), string(b))
			if _, e = pool.Exec(ctx, `UPDATE expense_reimbursement_payments SET amount=39 WHERE expense_id=$1 AND week_start=$2::date`, expense, week.Format(time.DateOnly)); e == nil {
				t.Fatal("frozen payment changed")
			}
			if _, e = pool.Exec(ctx, `UPDATE expenses SET amount=101 WHERE id=$1`, expense); e == nil {
				t.Fatal("frozen source changed")
			}
			history, e := pay.History(ctx, driver, Pagination{PageSize: 100})
			if e != nil || len(history.Items) == 0 {
				t.Fatal("payout history missing", e)
			}
			if mode == "migration" {
				frozen := read(legacyDriver, week)
				if len(frozen.AutoCharges) != 1 || frozen.AutoCharges[0].ExpenseDate != "2026-09-30" {
					t.Fatal("legacy snapshot/date changed")
				}
				if row(read(legacyDriver, week.AddDate(0, 0, 7)), legacyExpense) != nil {
					t.Fatal("old frozen automatic credit paid again")
				}
			}
			category := id(`SELECT id::text FROM expense_settings WHERE kind='category' AND name='Other'`)
			main := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Team main','team main','cpm',0.75) RETURNING id`)
			member := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Team member','team member','cpm',0.75) RETURNING id`)
			exec(`INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week) VALUES($1,$2,'2026-09-28')`, main, member)
			teamExpense := id(`INSERT INTO expenses(category_id,category,expense_date,expense_type,amount,payment_type,driver_id,covered_by) VALUES($1,'Other','2026-10-07','Team refund',15.10,'Reimburse',$2,'Company') RETURNING id`, category, member)
			team := read(main, week)
			teamRow := row(team, teamExpense)
			if teamRow == nil || teamRow.Source != "reimburse" {
				t.Fatal("team refund missing from the dated main statement", team)
			}
			teamRow.Apply = true
			team.Edits.ExpenseDeductions = []ExpenseDeduction{*teamRow}
			if _, e = pay.Save(ctx, team.Edits); e != nil {
				t.Fatal("team refund save failed", e)
			}
			if row(read(main, week.AddDate(0, 0, 7)), teamExpense) != nil {
				t.Fatal("team refund paid twice")
			}
			investor, e := pay.InvestorPay(ctx, week)
			if e != nil {
				t.Fatal(e)
			}
			for _, card := range investor.Drivers {
				for _, item := range card.Edits.ExpenseDeductions {
					if item.Source == "reimburse" {
						t.Fatal("personal refund routed to investor pay")
					}
				}
			}
			choices, e := expenses.ExpenseFormChoices(ctx, []string{category}, []string{category})
			if e != nil {
				t.Fatal(e)
			}
			if len(choices.Drivers) < 9 || len(choices.Trucks) != 6 || choices.Options.WorkOrdersCategoryID != "" {
				t.Fatal("incomplete or unscoped form choices", choices)
			}
			for _, item := range choices.Options.Settings {
				if item.Kind == "category" && item.ID != category || item.Kind == "name" && *item.CategoryID != category {
					t.Fatal("inaccessible category leaked into choices")
				}
			}
			encoded, _ := json.Marshal(choices)
			for _, private := range []string{`"payRate"`, `"dateOfBirth"`, `"licenseNumber"`, `"notes"`, `"gpsReportedAt"`} {
				if strings.Contains(string(encoded), private) {
					t.Fatal("profile field leaked into compact choices", private)
				}
			}
		})
	}
}
