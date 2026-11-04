package repository

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/datatruck"
)

func TestPrepareLoadReconciliation(t *testing.T) {
	seed := func() LoadReconciliationGroup {
		a, b := 1, 2
		return LoadReconciliationGroup{Number: "DUP", Sources: []LoadReconciliationSource{
			{ID: a, Number: "DUP", Customer: "Broker", Pickup: "2026-10-26", Delivery: "2026-10-27", Route: `[["pickup","a","tx"],["delivery","b","tx"]]`, Total: "3408.00", Miles: "842.66"},
			{ID: b, Number: "DUP", Present: true, Customer: "Broker", Pickup: "2026-10-26", Delivery: "2026-10-27", Route: `[["pickup","a","tx"],["delivery","b","tx"]]`, Total: "3408.00", Miles: "713.00"},
		}, Before: []GrossBoardEntry{{DriverID: "driver", Date: "2026-10-26", LoadRecordID: &a, DriverRate: "1691.62", Version: 3}, {DriverID: "driver", Date: "2026-10-26", Slot: 2, LoadRecordID: &b, DriverRate: "1691.62", Version: 4}}}
	}
	g := seed()
	prepareLoadReconciliation(&g)
	if g.Hold != "" || len(g.After) != 2 || g.DriverGross != "3383.24" || g.After[0].Slot != 2 || g.After[0].Miles != "713.00" || g.After[0].DriverRate != "3383.24" || g.After[1].LoadRecordID != nil {
		t.Fatalf("bad reconciliation: %+v", g)
	}
	cases := map[string]func(*LoadReconciliationGroup){
		"two present":   func(g *LoadReconciliationGroup) { g.Sources[0].Present = true },
		"both missing":  func(g *LoadReconciliationGroup) { g.Sources[1].Present = false },
		"date":          func(g *LoadReconciliationGroup) { g.Sources[0].Pickup = "2026-10-25" },
		"revenue":       func(g *LoadReconciliationGroup) { g.Sources[0].Total = "2000.00" },
		"route":         func(g *LoadReconciliationGroup) { g.Sources[0].Route = "different" },
		"customer":      func(g *LoadReconciliationGroup) { g.Sources[0].Customer = "Other" },
		"split":         func(g *LoadReconciliationGroup) { g.Sources[1].Split = true },
		"driver":        func(g *LoadReconciliationGroup) { g.Before[0].DriverID = "other" },
		"board date":    func(g *LoadReconciliationGroup) { g.Before[0].Date = "2026-10-25" },
		"manual miles":  func(g *LoadReconciliationGroup) { g.Before[0].EnteredMiles = "900" },
		"missing gross": func(g *LoadReconciliationGroup) { g.Before[0].DriverRate = "" },
		"note":          func(g *LoadReconciliationGroup) { s := "Keep this"; g.Before[0].Note = &s },
		"same ID twice": func(g *LoadReconciliationGroup) { g.Before[0].LoadRecordID = g.Before[1].LoadRecordID },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			g := seed()
			mutate(&g)
			prepareLoadReconciliation(&g)
			if g.Hold == "" || len(g.After) != 0 {
				t.Fatalf("ambiguous case accepted: %+v", g)
			}
		})
	}
}

func TestLoadReconciliationDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash,role_id) SELECT 'Reconcile test',crypt('test-only',gen_salt('bf')),id FROM app_roles WHERE system_role RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		stops := `{"stops":[{"stop_type":"pickup","location":{"city":"Austin","state":"TX"}},{"stop_type":"delivery","location":{"city":"Dallas","state":"TX"}}]}`
		_, err := pool.Exec(ctx, `INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,customer_name,pickup_time,delivery_time,raw_payload) VALUES
 (88201,'DUP-REPAIR','dispatched',3408,3408,842.66,'Broker','2026-10-05 12:00Z','2026-10-06 12:00Z',$1),
 (88202,'DUP-REPAIR','invoiced',3408,3408,713,'Broker','2026-10-05 12:00Z','2026-10-06 12:00Z',$1);
 `, stops)
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `UPDATE gross_board_entries SET load_number='DUP-REPAIR',load_record_id=88201,driver_rate=1691.62,original_rate=3408,miles=842.66,entered_original_rate=NULL,entered_miles=NULL WHERE driver_id=$1;
 `, projectionDriver)
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,load_record_id,driver_rate,original_rate,miles) VALUES($1,'2026-10-05',2,'DUP-REPAIR',88202,1691.62,3408,713)`, projectionDriver)
		if err != nil {
			t.Fatal(err)
		}
		source := NewLoadRepository(pool)
		board := NewGrossBoardRepository(pool)
		if err = source.RecordLoadSourceLookup(ctx, []int{88201, 88202}, []int{88202}); err != nil {
			t.Fatal(err)
		}
		page, err := source.GetLoadsPage(ctx, LoadPageQuery{Search: "DUP-REPAIR"})
		if err != nil || page.Total != 1 || page.Items[0].ID != 88202 {
			t.Fatal("missing source remained visible", page, err)
		}
		page, err = source.GetLoadsPage(ctx, LoadPageQuery{Search: "DUP-REPAIR", IncludeMissing: true})
		if err != nil || page.Total != 2 {
			t.Fatal("history unavailable", page, err)
		}
		missing := 0
		for _, row := range page.Items {
			if row.SourceMissing {
				missing++
			}
		}
		if missing != 1 {
			t.Fatal("missing source not labelled")
		}
		suggestions, err := board.SearchLoads(ctx, "DUP-REPAIR")
		if err != nil || len(suggestions) != 1 || suggestions[0].ID != 88202 {
			t.Fatal(suggestions, err)
		}
		old := 88201
		if _, err = board.SaveEntries(ctx, []GrossBoardEntry{{DriverID: projectionDriver, Date: "2026-10-07", LoadNumber: "DUP-REPAIR", LoadRecordID: &old, DriverRate: "10"}}); err == nil {
			t.Fatal("new missing selection accepted")
		}
		// An existing pinned link cannot silently move when an older client omits ID.
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		id, err := resolveAvailableBoardLoad(ctx, tx, GrossBoardEntry{DriverID: projectionDriver, Date: "2026-10-05", LoadNumber: "DUP-REPAIR"})
		tx.Rollback(ctx)
		if err != nil || id == nil || *id != old {
			t.Fatal("historical link replaced", id, err)
		}
		var live datatruck.Load
		payload := `{"id":88202,"load_id":"DUP-REPAIR","status":"invoiced","customer__company_name":"Broker","total_pay":3408,"total_miles":713,"pickup_time":"2026-10-05T12:00:00Z","delivery_time":"2026-10-06T12:00:00Z","stops":[{"stop_type":"pickup","location":{"city":"Austin","state":"TX"}},{"stop_type":"delivery","location":{"city":"Dallas","state":"TX"}}]}`
		if err = json.Unmarshal([]byte(payload), &live); err != nil {
			t.Fatal(err)
		}
		preview := func() LoadReconciliationPlan {
			t.Helper()
			p, e := board.PreviewLoadReconciliation(ctx, "2026-10-01", []int{88201, 88202}, []datatruck.Load{live})
			if e != nil {
				t.Fatal(e)
			}
			return p
		}
		plan := preview()
		if len(plan.Groups) != 1 || len(plan.Groups[0].After) != 2 || plan.Groups[0].DriverGross != "3383.24" {
			t.Fatalf("bad preview %+v", plan)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-05','{}')`, projectionDriver); err != nil {
			t.Fatal(err)
		}
		if held := preview(); held.Groups[0].Hold == "" || len(held.Groups[0].After) != 0 {
			t.Fatal("finalized driver statement was not held", held)
		}
		if _, err = pool.Exec(ctx, `UPDATE payroll_settlements SET finalized=false WHERE driver_id=$1 AND week_start='2026-10-05'`, projectionDriver); err != nil {
			t.Fatal(err)
		}
		if _, err = board.ApplyLoadReconciliation(ctx, plan, []datatruck.Load{live, {ID: 88201}}, actor, "88200000-0000-0000-0000-000000000009"); err == nil {
			t.Fatal("reappeared predecessor was merged")
		}
		// A changed board row must invalidate the exact reviewed plan.
		if _, err = pool.Exec(ctx, `UPDATE gross_board_entries SET version=version+1 WHERE driver_id=$1`, projectionDriver); err != nil {
			t.Fatal(err)
		}
		const request = "88200000-0000-0000-0000-000000000001"
		if _, err = board.ApplyLoadReconciliation(ctx, plan, []datatruck.Load{live}, actor, request); err == nil {
			t.Fatal("stale preview accepted")
		}
		plan = preview()
		pay := NewDriverPayRepository(pool)
		if err = pay.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		week, _ := time.Parse(time.DateOnly, "2026-10-05")
		if err = pay.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err = pay.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		// Failure after canonical writes must roll back board and receipt together.
		_, err = admin.Exec(ctx, `CREATE FUNCTION fail_reconcile_publication() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='investor' THEN RAISE EXCEPTION 'test second statement publication failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER fail_reconcile_publication BEFORE INSERT OR UPDATE ON payroll_statement_current FOR EACH ROW EXECUTE FUNCTION fail_reconcile_publication()`)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = board.ApplyLoadReconciliation(ctx, plan, []datatruck.Load{live}, actor, request); err == nil {
			t.Fatal("publication failure committed")
		}
		if next := preview(); !reflect.DeepEqual(next, plan) {
			t.Fatal("failed repair changed board/source")
		}
		if _, err = admin.Exec(ctx, `DROP TRIGGER fail_reconcile_publication ON payroll_statement_current; DROP FUNCTION fail_reconcile_publication()`); err != nil {
			t.Fatal(err)
		}
		if _, err = board.ApplyLoadReconciliation(ctx, plan, []datatruck.Load{live}, actor, request); err != nil {
			t.Fatal(err)
		}
		// Same receipt replays after state changed, without repeating a repair.
		if _, err = board.ApplyLoadReconciliation(ctx, plan, nil, actor, request); err != nil {
			t.Fatal("exact retry failed", err)
		}
		next := preview()
		if len(next.Groups[0].Before) != 1 || next.Groups[0].Before[0].DriverRate != "3383.24" || *next.Groups[0].Before[0].LoadRecordID != 88202 {
			t.Fatal(next)
		}
		var receiptCount int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM payroll_command_receipts WHERE request_id=$1`, request).Scan(&receiptCount); err != nil || receiptCount != 1 {
			t.Fatal(receiptCount, err)
		}
		for i := range next.Groups[0].Sources {
			if next.Groups[0].Sources[i].Hash != plan.Groups[0].Sources[i].Hash {
				t.Fatal("historical source changed")
			}
		}
		prepared, err := pay.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{})
		if err != nil || prepared.State != "ready" {
			t.Fatal("repair committed without ready payroll", prepared.State, err)
		}
		read, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		full, err := readPayrollProjectionReference(ctx, read, week, true)
		_ = read.Rollback(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for i, kind := range []string{"driver", "investor"} {
			page, e := pay.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
			a, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
			b, _ := json.Marshal(page.Report)
			if e != nil || page.State != "ready" || string(a) != string(b) {
				t.Fatal("published repair differs from independent full calculation", kind, e)
			}
		}
		// Reappearance restores normal visibility without replacing any linked ID.
		if err = source.RecordLoadSourceLookup(ctx, []int{88201, 88202}, []int{88201, 88202}); err != nil {
			t.Fatal(err)
		}
		page, err = source.GetLoadsPage(ctx, LoadPageQuery{Search: "DUP-REPAIR"})
		if err != nil || page.Total != 2 {
			t.Fatal("reappearance lost", page, err)
		}
	})
}
