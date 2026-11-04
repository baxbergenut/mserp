package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"mserp/internal/datatruck"
)

type LoadReconciliationSource struct {
	ID       int    `json:"id"`
	Number   string `json:"number"`
	Status   string `json:"status"`
	Present  bool   `json:"present"`
	Hash     string `json:"hash"`
	Customer string `json:"customer"`
	Pickup   string `json:"pickup"`
	Delivery string `json:"delivery"`
	Route    string `json:"route"`
	Total    string `json:"total"`
	Miles    string `json:"miles"`
	Split    bool   `json:"split"`
}
type LoadReconciliationGroup struct {
	Number      string                     `json:"number"`
	Sources     []LoadReconciliationSource `json:"sources"`
	CurrentID   int                        `json:"currentId"`
	Before      []GrossBoardEntry          `json:"before"`
	After       []GrossBoardEntry          `json:"after"`
	DriverGross string                     `json:"driverGross"`
	Hold        string                     `json:"hold,omitempty"`
}
type LoadReconciliationPlan struct {
	Since  string                    `json:"since"`
	Groups []LoadReconciliationGroup `json:"groups"`
	Digest string                    `json:"digest"`
}

// Candidate enumeration is bounded by service dates. It does not establish
// replacement identity; every ID must be checked with the provider first.
func (r *LoadRepository) ReconciliationIDs(ctx context.Context, since string) ([]int, error) {
	rows, err := r.pool.Query(ctx, `WITH numbers AS (
 SELECT lower(btrim(l.load_id)) number FROM loads l JOIN load_source_availability a ON a.load_id=l.id
 WHERE NOT a.present AND coalesce(l.delivery_time,l.delivery_appointment_time,l.pickup_time,l.created_datetime)>=$1::date
 ), duplicates AS (SELECT lower(btrim(load_id)) number FROM loads WHERE lower(btrim(load_id)) IN (SELECT number FROM numbers) GROUP BY 1 HAVING count(*)>1)
 SELECT id FROM loads WHERE lower(btrim(load_id)) IN (SELECT number FROM duplicates) ORDER BY id`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int{}
	for rows.Next() {
		var id int
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// A preview is read-only. Live results must come from complete validated
// ID-filtered responses, never a status/date-filtered list.
func (r *GrossBoardRepository) PreviewLoadReconciliation(ctx context.Context, since string, ids []int, live []datatruck.Load) (LoadReconciliationPlan, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return LoadReconciliationPlan{}, err
	}
	defer tx.Rollback(ctx)
	return buildLoadReconciliation(ctx, tx, since, ids, live)
}

func buildLoadReconciliation(ctx context.Context, tx pgx.Tx, since string, ids []int, live []datatruck.Load) (LoadReconciliationPlan, error) {
	return buildLoadReconciliationPolicy(ctx, tx, since, ids, live, false)
}

func buildLoadReconciliationPolicy(ctx context.Context, tx pgx.Tx, since string, ids []int, live []datatruck.Load, replaceMissing bool) (LoadReconciliationPlan, error) {
	plan := LoadReconciliationPlan{Since: since, Groups: []LoadReconciliationGroup{}}
	present := map[int]datatruck.Load{}
	for _, l := range live {
		present[l.ID] = l
	}
	rows, err := tx.Query(ctx, `SELECT l.id,l.load_id,l.status,encode(digest((to_jsonb(l)-'synced_at')::text,'sha256'),'hex'),coalesce(l.customer_name,''),
 coalesce((coalesce(pickup_time,pickup_appointment_time) AT TIME ZONE 'UTC')::date::text,''),
 coalesce((coalesce(delivery_time,delivery_appointment_time) AT TIME ZONE 'UTC')::date::text,''),
 coalesce((SELECT jsonb_agg(jsonb_build_array(s->>'stop_type',lower(btrim(s#>>'{location,city}')),lower(btrim(s#>>'{location,state}'))) ORDER BY ord)::text FROM jsonb_array_elements(coalesce(l.raw_payload->'stops','[]')) WITH ORDINALITY x(s,ord)),''),
 l.total_pay::text,coalesce(l.total_miles::text,''),l.split_expected OR jsonb_array_length(coalesce(l.raw_payload->'trips','[]'))>1
 FROM loads l WHERE id=ANY($1::int[]) ORDER BY lower(btrim(load_id)),id`, ids)
	if err != nil {
		return plan, err
	}
	groups := map[string]*LoadReconciliationGroup{}
	for rows.Next() {
		var s LoadReconciliationSource
		if err = rows.Scan(&s.ID, &s.Number, &s.Status, &s.Hash, &s.Customer, &s.Pickup, &s.Delivery, &s.Route, &s.Total, &s.Miles, &s.Split); err != nil {
			rows.Close()
			return plan, err
		}
		l, ok := present[s.ID]
		s.Present = ok
		key := strings.ToLower(strings.TrimSpace(s.Number))
		g := groups[key]
		if g == nil {
			g = &LoadReconciliationGroup{Number: s.Number, Before: []GrossBoardEntry{}, After: []GrossBoardEntry{}}
			groups[key] = g
		}
		if ok {
			// Refuse a stale local financial source, even when provider presence agrees.
			amount := ""
			if l.TotalPay != nil {
				amount = l.TotalPay.String()
			}
			a, ae := chargeCents(amount)
			b, be := chargeCents(s.Total)
			miles := ""
			if l.TotalMiles != nil {
				miles = l.TotalMiles.String()
			}
			m, me := chargeCents(miles)
			n, ne := chargeCents(s.Miles)
			var oldRoute [][]string
			_ = json.Unmarshal([]byte(s.Route), &oldRoute)
			newRoute := make([][]string, 0, len(l.Stops))
			for _, stop := range l.Stops {
				newRoute = append(newRoute, []string{stop.StopType, strings.ToLower(strings.TrimSpace(stop.Location.City)), strings.ToLower(strings.TrimSpace(stop.Location.State))})
			}
			date := func(actual, appointment *time.Time) string {
				if actual == nil {
					actual = appointment
				}
				if actual == nil {
					return ""
				}
				return actual.UTC().Format(time.DateOnly)
			}
			if l.LoadID == nil || !strings.EqualFold(strings.TrimSpace(*l.LoadID), strings.TrimSpace(s.Number)) || ae != nil || be != nil || a != b || me != nil || ne != nil || m != n || l.Status != s.Status || l.CustomerCompanyName == nil || *l.CustomerCompanyName != s.Customer || date(l.PickupTime, l.PickupAppointmentTime) != s.Pickup || date(l.DeliveryTime, l.DeliveryAppointmentTime) != s.Delivery || !reflect.DeepEqual(oldRoute, newRoute) {
				g.Hold = "Current provider values differ from the imported source; refresh and review"
			}
		}
		g.Sources = append(g.Sources, s)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return plan, err
	}
	// Reject incomplete candidate sets: a same-number record outside the reviewed
	// IDs would make the unique-current determination unsafe.
	for _, g := range groups {
		var allIDs []int
		if err = tx.QueryRow(ctx, `SELECT array_agg(id ORDER BY id) FROM loads WHERE lower(btrim(load_id))=lower(btrim($1))`, g.Number).Scan(&allIDs); err != nil {
			return plan, err
		}
		known := []int{}
		for _, s := range g.Sources {
			known = append(known, s.ID)
		}
		if !reflect.DeepEqual(known, allIDs) {
			return plan, fmt.Errorf("candidate identity set changed for %s", g.Number)
		}
		var body []byte
		err = tx.QueryRow(ctx, `SELECT coalesce(jsonb_agg(jsonb_build_object('driverId',e.driver_id,'date',e.service_date::text,'slot',e.slot,'loadNumber',e.load_number,'loadRecordId',e.load_record_id,'loadTripId',e.load_trip_id,'driverRate',coalesce(e.driver_rate::text,''),'originalRate',coalesce(e.original_rate::text,''),'miles',coalesce(e.miles::text,''),'enteredOriginalRate',coalesce(e.entered_original_rate::text,''),'enteredMiles',coalesce(e.entered_miles::text,''),'version',e.version,'note',e.note) ORDER BY e.driver_id,e.service_date,e.slot),'[]') FROM `+grossBoardEntriesSQL+` e WHERE NOT e.deleted AND e.day_status='' AND (e.load_record_id=ANY($1::int[]) OR lower(btrim(e.load_number))=lower(btrim($2)))`, known, g.Number).Scan(&body)
		if err != nil {
			return plan, err
		}
		if err = json.Unmarshal(body, &g.Before); err != nil {
			return plan, err
		}
		if replaceMissing {
			prepareMissingLoadReplacement(g)
		} else {
			prepareLoadReconciliation(g)
		}
		if g.Hold == "" && len(g.After) > 0 {
			for _, e := range g.Before {
				var protected bool
				err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM gross_board_transfers WHERE driver_id=$1 AND active AND ((source_date=$2::date AND source_slot=$3) OR (dispatch_date=$2::date AND dispatch_slot=$3))) OR EXISTS(SELECT 1 FROM payroll_load_placements WHERE load_record_id=ANY($4::int[])) OR EXISTS(SELECT 1 FROM driver_pay_weeks WHERE driver_id=$1 AND week_start=date_trunc('week',$2::date)::date AND (coalesce(comments,'{}') ? ($2||':'||$3::text||':'||lower(btrim($5)))))`, e.DriverID, e.Date, e.Slot, known, g.Number).Scan(&protected)
				if err != nil {
					return plan, err
				}
				if protected {
					g.Hold = "Finalized statement, transfer, placement or load comment requires individual review"
					break
				}
				if err = assertPaySourceOpen(ctx, tx, PaySourceAcceptance{DriverID: e.DriverID, Date: e.Date, Slot: e.Slot}); err != nil {
					var validation *ChargeValidationError
					if !errors.As(err, &validation) {
						return plan, err
					}
					g.Hold = err.Error()
					break
				}
			}
		}
		if g.Hold != "" {
			g.After = []GrossBoardEntry{}
		}
		plan.Groups = append(plan.Groups, *g)
	}
	sort.Slice(plan.Groups, func(i, j int) bool { return plan.Groups[i].Number < plan.Groups[j].Number })
	body, err := json.Marshal(plan)
	if err != nil {
		return plan, err
	}
	sum := sha256.Sum256(body)
	plan.Digest = hex.EncodeToString(sum[:])
	return plan, nil
}

func prepareLoadReconciliation(g *LoadReconciliationGroup) {
	if g.Hold != "" {
		return
	}
	var current *LoadReconciliationSource
	missing := 0
	for i := range g.Sources {
		s := &g.Sources[i]
		if s.Present {
			if current != nil {
				g.Hold = "More than one provider record remains present"
				return
			}
			current = s
		} else {
			missing++
		}
	}
	if current == nil || missing == 0 {
		g.Hold = "No unique current replacement with missing predecessors"
		return
	}
	g.CurrentID = current.ID
	for _, s := range g.Sources {
		if s.Split || current.Split {
			g.Hold = "Trip or split allocations require individual review"
			return
		}
		if s.Pickup == "" || s.Delivery == "" || s.Route == "" || s.Route == "[]" || s.Customer == "" || s.Pickup != current.Pickup || s.Delivery != current.Delivery || s.Route != current.Route || !strings.EqualFold(strings.TrimSpace(s.Customer), strings.TrimSpace(current.Customer)) || s.Total != current.Total {
			g.Hold = "Dates, route, customer or revenue differ; replacement identity needs review"
			return
		}
	}
	if len(g.Before) == 0 {
		return
	}
	first := g.Before[0]
	seen := map[int]bool{}
	var gross int64
	hasMissing := false
	survivor := 0
	for i, e := range g.Before {
		if e.DriverID != first.DriverID || e.Date != first.Date {
			g.Hold = "Multiple drivers or board dates require individual review"
			return
		}
		if e.LoadRecordID == nil || seen[*e.LoadRecordID] || e.LoadTripID != nil || (e.Note != nil && *e.Note != "") || e.EnteredOriginalRate != "" || e.EnteredMiles != "" {
			g.Hold = "Unresolved/repeated identity, trip, note or manual override requires review"
			return
		}
		known := false
		for _, s := range g.Sources {
			if s.ID == *e.LoadRecordID {
				known = true
			}
		}
		if !known {
			g.Hold = "Board link is outside the reviewed source identities"
			return
		}
		seen[*e.LoadRecordID] = true
		if *e.LoadRecordID == current.ID {
			survivor = i
		} else {
			hasMissing = true
		}
		cents, err := chargeCents(e.DriverRate)
		if err != nil || cents < 0 {
			g.Hold = "Missing or negative driver gross requires review"
			return
		}
		gross += cents
	}
	g.DriverGross = chargeMoney(gross)
	if !hasMissing {
		return
	}
	keep := g.Before[survivor]
	keep.LoadRecordID = &g.CurrentID
	keep.DriverRate = g.DriverGross
	keep.OriginalRate = current.Total
	keep.Miles = current.Miles
	keep.AcceptSystemValues = true
	g.After = append(g.After, keep)
	for i, e := range g.Before {
		if i == survivor {
			continue
		}
		g.After = append(g.After, GrossBoardEntry{DriverID: e.DriverID, Date: e.Date, Slot: e.Slot, Version: e.Version, Deleted: e.Slot > 0})
	}
}

// Apply only an exact reviewed preview. Source locks, row versions, native
// finalized guards, canonical writes, payroll publication and retry receipt all
// share one transaction. Neither source records nor financial history are deleted.
func (r *GrossBoardRepository) ApplyLoadReconciliation(ctx context.Context, plan LoadReconciliationPlan, live []datatruck.Load, actor, requestID string) (LoadReconciliationPlan, error) {
	ctx = WithPayrollPublication(ctx)
	tx, err := beginPayrollMutation(ctx, r.pool)
	if err != nil {
		return plan, err
	}
	defer tx.Rollback(ctx)
	var authorized bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app_users u JOIN app_roles r ON r.id=u.role_id WHERE u.id=$1::uuid AND u.active AND r.system_role)`, actor).Scan(&authorized); err != nil {
		return plan, err
	}
	if !authorized {
		return plan, fmt.Errorf("an active administrator actor is required")
	}
	command, replay, err := beginPayrollCommand(ctx, tx, actor, requestID, "load.reconcile", "", plan)
	if err != nil {
		return plan, err
	}
	if command == nil {
		return plan, fmt.Errorf("actor and request identity required")
	}
	if replay != nil {
		err = json.Unmarshal(replay, &plan)
		return plan, err
	}
	ids := []int{}
	for _, g := range plan.Groups {
		for _, s := range g.Sources {
			ids = append(ids, s.ID)
		}
	}
	sort.Ints(ids)
	current, err := buildLoadReconciliation(ctx, tx, plan.Since, ids, live)
	if err != nil {
		return plan, err
	}
	if current.Digest != plan.Digest {
		return plan, fmt.Errorf("reconciliation preview changed; regenerate and review before applying")
	}
	entries := []GrossBoardEntry{}
	for _, g := range current.Groups {
		if g.Hold == "" {
			entries = append(entries, g.After...)
		}
	}
	if len(entries) == 0 {
		return plan, fmt.Errorf("no confirmed board repairs")
	}
	if _, err = r.saveEntriesTx(ctx, tx, entries, actor); err != nil {
		return plan, err
	}
	for _, g := range current.Groups {
		if g.Hold != "" || len(g.After) == 0 {
			continue
		}
		e := g.Before[0]
		var total string
		var count int
		err = tx.QueryRow(ctx, `SELECT coalesce(sum(driver_rate),0)::text,count(*) FROM `+grossBoardEntriesSQL+` WHERE driver_id=$1 AND service_date=$2::date AND NOT deleted AND lower(btrim(load_number))=lower(btrim($3))`, e.DriverID, e.Date, g.Number).Scan(&total, &count)
		if err != nil {
			return plan, err
		}
		a, _ := chargeCents(total)
		b, _ := chargeCents(g.DriverGross)
		if a != b || count != 1 {
			return plan, fmt.Errorf("driver gross preservation failed for %s", g.Number)
		}
	}
	if err = commitPayrollSourceCommand(ctx, tx, command, current); err != nil {
		return plan, err
	}
	return current, nil
}
