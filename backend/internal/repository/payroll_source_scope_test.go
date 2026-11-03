package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollResourceSourceScopeDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		if _, err := pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date)
 VALUES('18200000-0000-0000-0000-000000009701','Unrelated source','unrelated source','cpm',0.65,'2026-01-01');
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles)
 VALUES('18200000-0000-0000-0000-000000009701','2026-10-05','UNRELATED-1',900,1100,300);
 INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,pickup_time,raw_payload)
 VALUES(18202,'UNRELATED-1','delivered',900,1100,300,'2026-10-05 12:00Z','{}');`); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources' WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		for _, test := range []struct {
			name, change, after string
			unrelated           bool
		}{
			{"unrelated_load", `UPDATE loads SET total_pay=total_pay+1 WHERE id=18202`, "", true},
			{"related_load", `UPDATE loads SET total_pay=total_pay+1 WHERE id=18201`, "", false},
			{"old_and_new_load_number", `UPDATE loads SET load_id='PROJECTION-1' WHERE id=18202`, "", false},
			{"restore_old_number", `UPDATE loads SET load_id='UNRELATED-1' WHERE id=18202`, "", false},
			{"unrelated_profile", `UPDATE drivers SET full_name=full_name||' changed' WHERE id='18200000-0000-0000-0000-000000009701'`, "", true},
			{"missing_capture", `UPDATE loads SET total_pay=total_pay+1 WHERE id=18202`, `DELETE FROM payroll_projection_changes WHERE table_name='loads' AND source_revision=(SELECT revision FROM payroll_cache_revisions WHERE table_name='loads')`, false},
			{"uncaptured_revision", `UPDATE payroll_cache_revisions SET revision=revision+1 WHERE table_name='loads'`, "", false},
		} {
			t.Run(test.name, func(t *testing.T) {
				beforeTx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				before, err := payrollProjectionFence(ctx, beforeTx)
				_ = beforeTx.Rollback(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = pool.Exec(ctx, test.change); err != nil {
					t.Fatal(err)
				}
				if test.after != "" {
					if _, err = admin.Exec(ctx, test.after); err != nil {
						t.Fatal(err)
					}
				}
				tx, closeWrite, err := repo.beginPayrollWriteProtocol(ctx, true, "", "", "driver:"+projectionDriver)
				if err != nil {
					t.Fatal(err)
				}
				defer closeWrite()
				current, err := payrollProjectionFence(ctx, tx)
				if err != nil {
					t.Fatal(err)
				}
				got, err := payrollSourcesUnrelated(ctx, tx, before, current)
				if err != nil || got != test.unrelated {
					t.Fatalf("unrelated=%v want %v: %v", got, test.unrelated, err)
				}
				if got, err := payrollSourcesUnrelated(ctx, tx.(*payrollResourceTransaction).Tx, before, current); err != nil || got {
					t.Fatal("proof bypassed resource/source gate", got, err)
				}
			})
		}
	})
}

func TestPayrollResourceSourceScopeSaveDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		if _, err := pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date)
 VALUES('18200000-0000-0000-0000-000000009701','Unrelated source','unrelated source','cpm',0.65,'2026-01-01');
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles)
 VALUES('18200000-0000-0000-0000-000000009701','2026-10-05','UNRELATED-1',900,1100,300);
 INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,pickup_time,raw_payload)
 VALUES(18202,'UNRELATED-1','delivered',900,1100,300,'2026-10-05 12:00Z','{}');`); err != nil {
			t.Fatal(err)
		}
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Source scope actor',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		repo.ConfigurePreparedReads(true)
		repo.ConfigurePreparedCommands(true)
		repo.ConfigurePreparedFinalization(true)
		repo.resourceConcurrency = true
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources' WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		for _, date := range []string{"2026-10-05", "2026-10-12"} {
			week, _ := chargeWeek(date)
			if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := repo.DispatchPayrollChanges(ctx); err != nil {
			t.Fatal(err)
		}
		for {
			worked, err := repo.PrepareNextPayrollWeek(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !worked {
				break
			}
		}
		week, _ := chargeWeek("2026-10-05")
		sequence := 9700
		for _, kind := range []string{"driver", "investor"} {
			id := projectionDriver
			if kind == "investor" {
				id = projectionTruck
			}
			for pass := 0; pass < 2; pass++ {
				page, err := repo.PreparedPayroll(ctx, kind, week, PayPageQuery{ID: id}, true)
				if err != nil || len(page.Drivers) != 1 {
					t.Fatal("prepared input", kind, err)
				}
				input := page.Drivers[0].Edits
				sequence++
				input.RequestID = fmt.Sprintf("18200000-0000-0000-0000-%012d", sequence)
				input.ExpectedStatementRevision = page.Drivers[0].StatementRevision
				input.Notes = fmt.Sprintf("Independent source save %d", pass)
				if _, err = NewGrossBoardRepository(pool).SaveChanges(WithPayrollPublication(ctx), []GrossBoardEntry{{DriverID: "18200000-0000-0000-0000-000000009701", Date: "2026-10-05", DriverRate: fmt.Sprint(901 + sequence), Fields: []string{"driverRate"}}}, actor); err != nil {
					t.Fatal(err)
				}
				visible, err := repo.PreparedPayroll(ctx, kind, week, PayPageQuery{}, true)
				if err != nil {
					t.Fatal(err)
				}
				for _, row := range visible.Drivers {
					if row.ID == id && row.PreparationState != "ready" {
						t.Fatal("unrelated statement disabled by board save", kind, row.PreparationState)
					}
					if row.ID == "18200000-0000-0000-0000-000000009701" && row.PreparationState == "ready" {
						t.Fatal("related statement enabled before preparation")
					}
				}
				detail, err := repo.PreparedPayroll(ctx, kind, week, PayPageQuery{ID: id}, true)
				if err != nil || detail.Preparation.State != "ready" {
					t.Fatal("unrelated detail blocked", kind, err)
				}
				var saved DriverPayEdits
				if kind == "driver" {
					saved, err = repo.Save(ctx, input, actor)
				} else {
					saved, err = repo.SaveInvestorPay(ctx, input, actor)
				}
				if err != nil {
					t.Fatal("unrelated source blocked save", kind, pass, err)
				}
				if saved.CommittedStatement == nil || saved.StatementRevision == input.ExpectedStatementRevision {
					t.Fatal("missing committed version")
				}
				var pending bool
				if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_projection_changes WHERE table_name='gross_board_entries' AND dispatched_at IS NULL)`).Scan(&pending); err != nil || !pending {
					t.Fatal("command acknowledged unrelated source work", err)
				}
				for _, date := range []string{"2026-10-05", "2026-10-12"} {
					period, _ := chargeWeek(date)
					tx, err := pool.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					full, err := readPayrollProjectionReference(ctx, tx, period, true)
					if err != nil {
						_ = tx.Rollback(ctx)
						t.Fatal(err)
					}
					for i, reportKind := range []string{"driver", "investor"} {
						identity := projectionDriver
						if reportKind == "investor" {
							identity = projectionTruck
						}
						for _, expected := range full[i].Drivers {
							if expected.ID != identity {
								continue
							}
							var body []byte
							if err = tx.QueryRow(ctx, `SELECT v.detail FROM payroll_statement_state s JOIN payroll_statement_versions v ON v.id=s.version_id WHERE s.kind=$1 AND s.statement_id=$2::uuid AND s.week_start=$3::date`, reportKind, identity, date).Scan(&body); err != nil {
								_ = tx.Rollback(ctx)
								t.Fatal(err)
							}
							var actual DriverPayDriver
							if err = json.Unmarshal(body, &actual); err != nil {
								_ = tx.Rollback(ctx)
								t.Fatal(err)
							}
							want, _ := json.Marshal(expected)
							got, _ := json.Marshal(actual)
							if string(want) != string(got) {
								_ = tx.Rollback(ctx)
								t.Fatalf("published source-scope result differs %s %s\nwant %s\ngot %s", reportKind, date, want, got)
							}
						}
					}
					_ = tx.Rollback(ctx)
				}
			}
			for _, reopen := range []bool{false, true} {
				page, err := repo.PreparedPayroll(ctx, kind, week, PayPageQuery{ID: id}, true)
				if err != nil || len(page.Drivers) != 1 {
					t.Fatal("settlement input", err)
				}
				if _, err = pool.Exec(ctx, `UPDATE loads SET total_pay=total_pay+1 WHERE id=18202`); err != nil {
					t.Fatal(err)
				}
				sequence++
				opt := PayrollSettlementCommand{RequestID: fmt.Sprintf("18200000-0000-0000-0000-%012d", sequence), ExpectedStatementRevision: page.Drivers[0].StatementRevision}
				var result DriverPayWeek
				if kind == "driver" {
					result, err = repo.Settle(WithPayrollReceipt(ctx), week, id, page.Revision, actor, "Source scope verification", reopen, !reopen, opt)
				} else {
					result, err = repo.SettleInvestor(WithPayrollReceipt(ctx), week, id, page.Revision, actor, "Source scope verification", reopen, !reopen, opt)
				}
				if err != nil || len(result.Drivers) != 1 || (result.Drivers[0].Settlement != nil && result.Drivers[0].Settlement.Finalized) == reopen {
					t.Fatal("unrelated source blocked settlement", kind, reopen, err)
				}
			}

		}
		for _, kind := range []string{"driver", "investor"} {
			id := projectionDriver
			if kind == "investor" {
				id = projectionTruck
			}
			page, err := repo.PreparedPayroll(ctx, kind, week, PayPageQuery{ID: id}, true)
			if err != nil || len(page.Drivers) != 1 {
				t.Fatal("related conflict input", err)
			}
			input := page.Drivers[0].Edits
			sequence++
			input.RequestID = fmt.Sprintf("18200000-0000-0000-0000-%012d", sequence)
			input.ExpectedStatementRevision = page.Drivers[0].StatementRevision
			input.Notes = "Must not commit"
			if _, err = pool.Exec(ctx, `UPDATE loads SET total_pay=total_pay+1 WHERE id=18201`); err != nil {
				t.Fatal(err)
			}
			if kind == "driver" {
				_, err = repo.Save(ctx, input, actor)
			} else {
				_, err = repo.SaveInvestorPay(ctx, input, actor)
			}
			if !errors.Is(err, ErrDriverPayConflict) {
				t.Fatal("related source did not reject stale financial input", kind, err)
			}
			var receipt bool
			if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_command_receipts WHERE actor_id=$1::uuid AND request_id=$2::uuid)`, actor, input.RequestID).Scan(&receipt); err != nil || receipt {
				t.Fatal("stale save committed a receipt", err)
			}
		}

	})
}
