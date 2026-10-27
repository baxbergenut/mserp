package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExpenseReimbursementsDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_DRIVER_PAY_TEST_DATABASE_URL to a disposable _test database")
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
	migration, err := os.ReadFile("../../sql/153_expense_reimburse.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("expense_reimburse_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, e := admin.Exec(ctx, sql, args...); e != nil {
					t.Fatal(e)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public`)
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`) }()
			sql := strings.ReplaceAll(string(source), "\r\n", "\n")
			if mode == "migration" {
				body := strings.ReplaceAll(string(migration), "\r\n", "\n")
				if !strings.Contains(sql, body) {
					t.Fatal("migration missing from init.sql")
				}
				sql = strings.Replace(sql, body, "", 1)
			}
			exec(sql)
			if mode == "migration" {
				exec(`INSERT INTO expense_settings(kind,name,active) VALUES('payment_method','reimburse',false)`)
				exec(string(migration))
			}
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app`)
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
			id := func(query string, args ...any) string {
				t.Helper()
				var value string
				if e := pool.QueryRow(ctx, query, args...).Scan(&value); e != nil {
					t.Fatal(e)
				}
				return value
			}
			setting := id(`SELECT id::text FROM expense_settings WHERE kind='payment_method' AND name='Reimburse' AND active`)
			for _, query := range []string{
				`UPDATE expense_settings SET name='Refund' WHERE id=$1`,
				`UPDATE expense_settings SET active=false WHERE id=$1`,
				`UPDATE expense_settings SET kind='payer' WHERE id=$1`,
				`DELETE FROM expense_settings WHERE id=$1`,
			} {
				if _, e := pool.Exec(ctx, query, setting); e == nil {
					t.Fatal("permanent setting mutation accepted", query)
				}
			}
			_, e = NewExpenseRepository(pool).SaveExpenseSetting(ctx, setting, ExpenseSetting{Kind: "payment_method", Name: "Refund", Active: true, Version: 1})
			if !errors.Is(e, ErrExpenseSettingPermanent) {
				t.Fatal("repository allowed permanent setting edit", e)
			}
			week, _ := time.Parse(time.DateOnly, "2026-09-28")
			pay := NewDriverPayRepository(pool)
			var first, firstExpense string
			for i, classification := range []string{"company", "investor_driver", "owner_operator", "unassigned"} {
				for _, tariff := range []string{"cpm", "gross_percentage"} {
					name := classification + " " + tariff
					driver := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,hire_date,roster_start_week) VALUES($1,$1,$2,0.75,'2026-09-01','2026-09-07') RETURNING id`, name, tariff)
					var truck, owner string
					if classification != "unassigned" {
						owner = "00000000-0000-0000-0000-000000000001"
						if classification != "company" {
							owner = id(`INSERT INTO investors(full_name,driver_id) VALUES($1,$2) RETURNING id`, name, driver)
							if classification == "investor_driver" {
								exec(`UPDATE investors SET driver_id=NULL WHERE id=$1`, owner)
							}
						}
						truck = id(`INSERT INTO trucks(unit_number,owner_id) VALUES($1,$2) RETURNING id`, name, owner)
						exec(`UPDATE truck_ownership_history SET assigned_at='2026-09-01',start_known=true WHERE truck_id=$1`, truck)
						exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-09-01')`, truck, driver)
						if classification != "company" {
							exec(`INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES($1,$2,'2026-09-07',88)`, truck, owner)
						}
					}
					// Truck Owner responsibility must still reimburse the selected hired
					// driver rather than deduct from the owner or Investor Pay.
					responsibility := "Company"
					var ownerArg, truckArg any
					if truck != "" {
						truckArg = truck
					}
					if classification != "company" && classification != "unassigned" {
						responsibility, ownerArg = "Truck Owner", owner
					}
					expense := id(`INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,payment_type,driver_id,truck_id,covered_by,owner_id)
 SELECT id,name,'Parking refund','2026-09-30',42.25,' ReImBuRsE ',$1,$2,$3,$4 FROM expense_settings WHERE kind='category' AND name='Other' RETURNING id`, driver, truckArg, responsibility, ownerArg)
					if i == 0 && tariff == "cpm" {
						first, firstExpense = driver, expense
					}
					report, err := pay.GetDriverWeek(ctx, week, driver)
					if err != nil || len(report.Drivers) != 1 {
						t.Fatalf("missing no-load %s reimbursement: %+v %v", name, report, err)
					}
					card := report.Drivers[0]
					if card.EmploymentType != classification || len(card.AutoCharges) != 1 || card.AutoCharges[0].Source != "expense-reimburse:"+expense || card.AutoCharges[0].Amount != "42.25" || len(card.Edits.ExpenseDeductions) != 0 {
						t.Fatalf("wrong %s reimbursement: %+v", name, card)
					}
					page := PaginatePay(report, PayPageQuery{})
					if page.Pagination.Payable != "42.25" {
						t.Fatal("credit did not increase payable", page.Pagination)
					}
					for _, offset := range []int{-7, 7} {
						other, err := pay.GetDriverWeek(ctx, week.AddDate(0, 0, offset), driver)
						if err != nil {
							t.Fatal(err)
						}
						for _, d := range other.Drivers {
							for _, credit := range d.AutoCharges {
								if credit.Source == "expense-reimburse:"+expense {
									t.Fatal("reimbursement repeated outside its week")
								}
							}
						}
					}
				}
			}
			investor, err := pay.InvestorPay(ctx, week)
			if err != nil {
				t.Fatal(err)
			}
			for _, card := range investor.Drivers {
				if len(card.Edits.ExpenseDeductions) != 0 {
					t.Fatal("reimbursement deducted from Investor Pay", card)
				}
			}
			history, err := pay.History(ctx, first, Pagination{PageSize: 100})
			if err != nil || len(history.Items) == 0 {
				t.Fatal("reimbursement-only history missing", err)
			}
			main := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Team main','team main','cpm',0.75) RETURNING id`)
			member := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Team member','team member','cpm',0.75) RETURNING id`)
			exec(`INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week) VALUES($1,$2,'2026-09-28')`, main, member)
			teamExpense := id(`INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,payment_type,driver_id,covered_by)
 SELECT id,name,'Team reimbursement','2026-09-30',15.10,'Reimburse',$1,'Company' FROM expense_settings WHERE kind='category' AND name='Other' RETURNING id`, member)
			teamReport, err := pay.GetDriverWeek(ctx, week, main)
			if err != nil || len(teamReport.Drivers) != 1 || len(teamReport.Drivers[0].AutoCharges) != 1 || teamReport.Drivers[0].AutoCharges[0].Source != "expense-reimburse:"+teamExpense {
				t.Fatal("team reimbursement did not follow dated statement routing", teamReport, err)
			}
			// Freeze a real report, then prove both API reads and SQL source guards.
			report, err := pay.GetDriverWeek(ctx, week, first)
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(report.Drivers[0])
			if err != nil {
				t.Fatal(err)
			}
			exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-09-28',$2::jsonb)`, first, string(body))
			frozen, err := pay.GetDriverWeek(ctx, week, first)
			if err != nil || frozen.Drivers[0].Settlement == nil || !frozen.Drivers[0].Settlement.Finalized || frozen.Drivers[0].AutoCharges[0].Amount != "42.25" {
				t.Fatal("frozen credit snapshot changed", frozen, err)
			}
			for _, query := range []string{
				`UPDATE expenses SET amount=43 WHERE id=$1`,
				`UPDATE expenses SET expense_date='2026-10-05' WHERE id=$1`,
				`UPDATE expenses SET payment_type='EFS' WHERE id=$1`,
				`DELETE FROM expenses WHERE id=$1`,
			} {
				if _, e := pool.Exec(ctx, query, firstExpense); e == nil {
					t.Fatal("frozen reimbursement changed", query)
				}
			}
			exec(`UPDATE payroll_settlements SET finalized=false WHERE driver_id=$1`, first)
			exec(`UPDATE expenses SET amount=43 WHERE id=$1`, firstExpense)
			report, err = pay.GetDriverWeek(ctx, week, first)
			if err != nil || report.Drivers[0].AutoCharges[0].Amount != "43.00" {
				t.Fatal("reopened reimbursement did not refresh", err)
			}
		})
	}
}
