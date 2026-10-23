package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testBoardLoadReconnection(t *testing.T, ctx context.Context, pool *pgxpool.Pool, original *DriverBoardRepository, fleet *FleetRepository) {
	t.Helper()
	repo := *original
	repo.now = func() time.Time { return time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC) }
	for _, scenario := range []string{"later plan", "repeated plans", "replaced plan", "pickup replacement", "older replacement", "manual destination", "manual stop time", "changed source", "changed replacement", "changed unplanned source", "ambiguous unplanned source", "ambiguous plans", "removed plan", "changed text"} {
		t.Run(scenario, func(t *testing.T) {
			d, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Reconnect " + scenario, PayType: "cpm", Active: true})
			if err != nil {
				t.Fatal(err)
			}
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := pool.Exec(ctx, query, args...); err != nil {
					t.Fatal(err)
				}
			}
			defer func() {
				exec(`DELETE FROM drivers WHERE id=$1`, d.ID)
				exec(`DELETE FROM loads WHERE id IN (989101,989102)`)
			}()
			exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES(989101,'RECONNECT','dispatched',0,0,'{"stops":[{"id":1,"ordering":1,"stop_type":"pickup","schedule_type":"appointment","scheduled_begin_date":"2026-10-23","scheduled_begin_time":"08:00:00","location":{"city":"Atlanta","state":"GA"}},{"id":2,"ordering":2,"stop_type":"delivery","schedule_type":"appointment","scheduled_begin_date":"2026-10-24","scheduled_begin_time":"14:00:00","location":{"city":"Boston","state":"MA"}}]}')`)
			planned := scenario != "later plan" && scenario != "repeated plans" && scenario != "ambiguous plans" && scenario != "changed unplanned source" && scenario != "ambiguous unplanned source"
			if planned {
				exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id) VALUES($1,'2026-10-23','RECONNECT',989101)`, d.ID)
			}
			status, schedule := "ENROUTE", "DEL 14:00 SAT"
			if scenario == "pickup replacement" {
				status, schedule = "DISPATCHED", "PU 08:00 FRI"
			}
			rows, err := repo.Save(ctx, []DriverBoardEntry{{DriverID: d.ID, CurrentLoad: "RECONNECT", Status: status, StatusEdited: true, Notes: "Keep dispatch notes", ETA: "15:00"}})
			if err != nil {
				t.Fatal(err)
			}
			entry := rows[0]
			view := func() BoardLoads {
				t.Helper()
				v, err := repo.Loads(ctx, d.ID, "")
				if err != nil {
					t.Fatal(err)
				}
				return v
			}
			before := view()
			if before.Automation == nil || before.Automation.Appointment == nil || before.Automation.Appointment.Label != schedule {
				t.Fatal("initial delivery selection", before)
			}
			if scenario == "manual destination" || scenario == "manual stop time" {
				action := BoardLoadAction{DriverID: d.ID, Version: entry.Version, Revision: before.Revision, FromDate: before.FromDate, Action: "manual"}
				if scenario == "manual stop time" {
					action.Action, action.ManualStopTime = "stop_time", "16:00 SAT"
				}
				result, err := repo.ChangeLoads(ctx, action, "")
				if err != nil {
					t.Fatal(err)
				}
				entry, before = result.Entry, result.Loads
			}
			switch scenario {
			case "later plan", "repeated plans", "ambiguous plans":
				exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id) VALUES($1,'2026-10-23','RECONNECT',989101)`, d.ID)
				if scenario != "later plan" {
					recordID := 989101
					if scenario == "ambiguous plans" {
						exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES(989102,'RECONNECT','dispatched',0,0,'{}')`)
						recordID = 989102
					}
					exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,load_record_id) VALUES($1,'2026-10-23',1,'RECONNECT',$2)`, d.ID, recordID)
				}
			case "removed plan":
				exec(`DELETE FROM gross_board_entries WHERE driver_id=$1`, d.ID)
			case "changed source":
				exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES(989102,'RECONNECT','dispatched',0,0,'{}')`)
				exec(`UPDATE gross_board_entries SET load_record_id=989102 WHERE driver_id=$1`, d.ID)
			case "changed replacement", "changed unplanned source", "ambiguous unplanned source":
				exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES(989102,'RECONNECT','dispatched',0,0,'{}')`)
				if scenario != "ambiguous unplanned source" {
					exec(`UPDATE loads SET load_id='RENAMED' WHERE id=989101`)
				}
				if scenario == "changed replacement" {
					exec(`UPDATE gross_board_entries SET plan_id=gen_random_uuid(),load_record_id=989102 WHERE driver_id=$1`, d.ID)
				}
			case "changed text":
				exec(`UPDATE driver_board SET current_load='DIFFERENT' WHERE driver_id=$1`, d.ID)
			default:
				exec(`UPDATE gross_board_entries SET plan_id=gen_random_uuid() WHERE driver_id=$1`, d.ID)
				if scenario == "older replacement" {
					exec(`UPDATE gross_board_entries SET service_date='2026-10-12' WHERE driver_id=$1`, d.ID)
					exec(`UPDATE loads SET status='delivered' WHERE id=989101`)
				}
			}
			// Projection must repair the view without changing dispatch, financial
			// plans, selected-stop state, versions, or personal history on reads.
			snapshot := func() string {
				t.Helper()
				var value string
				if err := pool.QueryRow(ctx, `SELECT jsonb_build_object('board',to_jsonb(b),'state',s.payload,'history',(SELECT jsonb_agg(h) FROM driver_board_history h WHERE h.driver_id=b.driver_id),'plans',(SELECT jsonb_agg(e) FROM gross_board_entries e WHERE e.driver_id=b.driver_id))::text FROM driver_board b JOIN driver_board_load_state s USING(driver_id) WHERE b.driver_id=$1`, d.ID).Scan(&value); err != nil {
					t.Fatal(err)
				}
				return value
			}
			stored := snapshot()
			for range 2 {
				v := view()
				blocked := strings.HasPrefix(scenario, "changed ") || strings.HasPrefix(scenario, "ambiguous ") || scenario == "removed plan"
				if blocked {
					if v.Current == nil || v.Current.Warning == "" || v.Automation == nil || v.Automation.Appointment != nil || v.Automation.Context != "" {
						t.Fatalf("unsafe selection reconnected: %+v", v)
					}
					continue
				}
				if v.Current == nil || v.Current.LoadID == nil || *v.Current.LoadID != 989101 || v.Current.PlanID != before.Current.PlanID || strings.Contains(v.Current.Warning, "review it") || v.StopKey != before.StopKey {
					t.Fatalf("load identity or stop lost on reconnection: %+v", v)
				}
				if scenario == "manual destination" {
					if v.DestinationSource || v.Automation.Appointment != nil {
						t.Fatal("manual destination overwritten", v)
					}
				} else {
					want := schedule
					if scenario == "manual stop time" {
						want = "DEL 16:00 SAT"
					}
					destination := "Boston, MA"
					if scenario == "pickup replacement" {
						destination = "Atlanta, GA"
					}
					if v.SourceDestination != destination || v.Automation == nil || v.Automation.Appointment == nil || v.Automation.Appointment.Label != want || v.Automation.Context != before.Automation.Context {
						t.Fatalf("delivery schedule/context lost on reconnection: %+v", v)
					}
				}
				if len(v.Next) != 0 {
					t.Fatal("current load duplicated in next queue", v.Next)
				}
			}
			if snapshot() != stored {
				t.Fatal("read-only reconciliation changed stored records")
			}
			if scenario == "replaced plan" || scenario == "manual stop time" {
				// New provider schedules must refresh through the reconnected view,
				// including the board snapshot used by SSE and the cache worker.
				exec(`UPDATE loads SET raw_payload=jsonb_set(raw_payload,'{stops,1,scheduled_begin_time}','"15:00:00"') WHERE id=989101`)
				if err := repo.ReconcileAutomation(ctx, nil, nil, ""); err != nil {
					t.Fatal(err)
				}
				board, err := repo.Get(ctx, repo.now())
				if err != nil {
					t.Fatal(err)
				}
				want := "DEL 15:00 SAT"
				if scenario == "manual stop time" {
					want = "DEL 16:00 SAT"
				}
				if a := board.Loads[d.ID].Automation; a == nil || a.Appointment == nil || a.Appointment.Label != want {
					t.Fatal("reconnected board did not refresh its schedule", a)
				}
			}
		})
	}
}
