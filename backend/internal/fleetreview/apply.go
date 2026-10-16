package fleetreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"mserp/internal/repository"
	"strings"
	"time"
)

type receipt struct {
	Hash  string
	After Snapshot
	Links map[string]string
}

func loadReceipt(ctx context.Context, tx pgx.Tx, batch, company string) (*receipt, error) {
	var r receipt
	var after, links []byte
	e := tx.QueryRow(ctx, `SELECT plan_hash,after_state,identity_links FROM fleet_review.receipts WHERE batch_id=$1 AND company_id=$2`, batch, company).Scan(&r.Hash, &after, &links)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(after, &r.After); e != nil {
		return nil, e
	}
	e = json.Unmarshal(links, &r.Links)
	return &r, e
}
func saveReceipt(ctx context.Context, tx pgx.Tx, s State, company, actor string, before, after Snapshot, links map[string]string) error {
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	c, _ := json.Marshal(links)
	_, e := tx.Exec(ctx, `INSERT INTO fleet_review.receipts(batch_id,company_id,plan_hash,before_state,after_state,identity_links,actor) VALUES($1,$2,$3,$4,$5,$6,$7)`, s.Batch.ID, company, s.Batch.Hash, a, b, c, actor)
	return e
}
func batchHash(s State) string {
	return digest(struct {
		Source, Receiver Snapshot
		Rows             []Row
		Policy           Policy
	}{s.Source, s.Receiver, s.Rows, s.Policy})
}
func validateBatch(s State) error {
	seen := map[string]bool{}
	selected := 0
	rowIDs := map[string]bool{}
	for _, r := range s.Rows {
		if rowIDs[r.ID] {
			return errors.New("duplicate review row")
		}
		rowIDs[r.ID] = true
		if !r.Reviewed {
			return fmt.Errorf("review or defer every row before final submission: %s", r.ID)
		}
		if e := validateRow(r, s); e != nil {
			return fmt.Errorf("%s: %w", r.ID, e)
		}
		if r.Action == "defer" {
			continue
		}
		selected++
		if r.RemoteID != "" {
			k := r.Kind + ":" + r.RemoteID
			if seen[k] {
				return errors.New("two local records claim the same FleetScope identity")
			}
			seen[k] = true
		}
	}
	if selected == 0 {
		return errors.New("no records selected for reconciliation")
	}
	return nil
}
func fsDriverStatus(s string) string {
	switch s {
	case "vacation":
		return "on_vacation"
	case "home":
		return "inactive"
	}
	return s
}

// Apply commits each database with an immutable receipt. A partial commit is
// deliberately recoverable, never reported as success or automatically reversed.
// Receipt readback and exact before/after fences protect retries after lost replies.
func (d *DB) Apply(ctx context.Context, s State, actor string) error {
	if s.Batch == nil || s.Batch.Hash != batchHash(s) {
		return errors.New("batch contents changed")
	}
	if e := validateBatch(s); e != nil {
		return e
	}
	if e := d.CheckApplyReady(ctx); e != nil {
		return e
	}
	a, e := d.Source.Begin(ctx)
	if e != nil {
		return e
	}
	defer a.Rollback(context.Background())
	b, e := d.Receiver.Begin(ctx)
	if e != nil {
		return e
	}
	defer b.Rollback(context.Background())
	for _, tx := range []pgx.Tx{a, b} {
		if _, e = tx.Exec(ctx, `SET LOCAL lock_timeout='5s'; SET LOCAL statement_timeout='30s'; SET LOCAL idle_in_transaction_session_timeout='90s'`); e != nil {
			return e
		}
	}
	// Match the ERP's existing lock order before taking fleet row/table locks.
	if _, e = a.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(736281940); SELECT pg_advisory_xact_lock(734912086); LOCK TABLE drivers,trucks,investors,truck_ownership_history,truck_driver_assignments,driver_teams,fleet_review.receipts IN SHARE ROW EXCLUSIVE MODE`); e != nil {
		return e
	}
	if _, e = b.Exec(ctx, `LOCK TABLE drivers,vehicles,fleet_assignments,mserp_sync_state,mserp_sync_links,fleet_review.receipts IN SHARE ROW EXCLUSIVE MODE; SELECT set_config('mserp.sync_write','on',true)`); e != nil {
		return e
	}
	if _, e = b.Exec(ctx, `SELECT set_config('fleetscope.actor_name',$1,true)`, "Fleet reconciliation · "+actor); e != nil {
		return e
	}
	var hasState bool
	if e = a.QueryRow(ctx, `SELECT to_regclass('public.fleetscope_sync_state') IS NOT NULL`).Scan(&hasState); e != nil {
		return e
	}
	if hasState {
		if _, e = a.Exec(ctx, `LOCK TABLE fleetscope_sync_state,fleetscope_sync_links,fleetscope_sync_operations IN SHARE ROW EXCLUSIVE MODE`); e != nil {
			return e
		}
	}
	if e = d.checkUnenrolled(ctx, a, b); e != nil {
		return e
	}
	ar, e := loadReceipt(ctx, a, s.Batch.ID, d.CompanyID)
	if e != nil {
		return e
	}
	br, e := loadReceipt(ctx, b, s.Batch.ID, d.CompanyID)
	if e != nil {
		return e
	}
	source, e := d.source(ctx, a)
	if e != nil {
		return e
	}
	remote, e := d.receiver(ctx, b)
	if e != nil {
		return e
	}
	wantSource, wantRemote := s.Source, s.Receiver
	links := map[string]string{}
	if ar != nil {
		if ar.Hash != s.Batch.Hash {
			return errors.New("source receipt mismatch")
		}
		wantSource, links = ar.After, ar.Links
	}
	if br != nil {
		if br.Hash != s.Batch.Hash {
			return errors.New("receiver receipt mismatch")
		}
		wantRemote, links = br.After, br.Links
	}
	if digest(source) != digest(wantSource) || digest(remote) != digest(wantRemote) {
		return errors.New("records changed since review or the partial commit; refresh before a new batch, or contact the operator for a partial-batch recovery")
	}
	for _, r := range s.Rows {
		if r.Action == "defer" {
			continue
		}
		if links[r.ID] == "" {
			if r.Action == "create" {
				links[r.ID] = id()
			} else {
				links[r.ID] = r.RemoteID
			}
		}
	}
	if ar == nil {
		for _, kind := range []string{"driver", "truck"} {
			for _, r := range s.Rows {
				if r.Action == "defer" || r.Kind != kind {
					continue
				}
				v := copyRecord(r.Values)
				if kind == "driver" {
					v["phone"] = phone(v["phone"])
					old := find(source.Drivers, r.LocalID)
					if old["firstName"] == v["firstName"] && old["lastName"] == v["lastName"] && old["name"] == strings.TrimSpace(v["firstName"]+" "+v["lastName"]) && old["email"] == v["email"] && old["phone"] == v["phone"] && old["status"] == v["status"] && old["terminationDate"] == v["terminationDate"] {
						continue
					}
					e = repository.ApplyReviewedDriver(ctx, a, r.LocalID, v)
				} else {
					v["ownershipVersion"] = find(source.Trucks, r.LocalID)["ownershipVersion"]
					e = repository.ApplyReviewedTruck(ctx, a, r.LocalID, v)
				}
				if e != nil {
					return fmt.Errorf("MSERP record %s: %w", r.LocalID, e)
				}
			}
		}
		source, e = d.source(ctx, a)
		if e != nil {
			return e
		}
	}
	if br == nil {
		for _, r := range s.Rows {
			if r.Action == "defer" || r.Kind != "driver" {
				continue
			}
			v := copyRecord(r.Values)
			v["phone"] = phone(v["phone"])
			target := links[r.ID]
			if r.Action != "create" && fsDriverStatus(v["status"]) != "active" {
				if e = d.releaseIneligibleDriver(ctx, b, target); e != nil {
					return e
				}
			}
			if r.Action == "create" {
				_, e = b.Exec(ctx, `INSERT INTO drivers(id,company_id,first_name,last_name,email,phone_number,ssn,ssn_last4,ssn_digest,address,status) VALUES($1,$2,$3,$4,$5,$6,'','','','',$7)`, target, d.CompanyID, v["firstName"], v["lastName"], v["email"], v["phone"], fsDriverStatus(v["status"]))
			} else {
				_, e = b.Exec(ctx, `UPDATE drivers SET first_name=$3,last_name=$4,email=$5,phone_number=$6,status=$7,termination_date=nullif($8,'')::date,termination_pending=false,updated_at=now() WHERE id=$1 AND company_id=$2`, target, d.CompanyID, v["firstName"], v["lastName"], v["email"], v["phone"], fsDriverStatus(v["status"]), v["terminationDate"])
			}
			if e != nil {
				return fmt.Errorf("FleetScope driver %s: %w", r.LocalID, e)
			}
		}
		for _, r := range s.Rows {
			if r.Action == "defer" || r.Kind != "truck" {
				continue
			}
			v := r.Values
			target := links[r.ID]
			t := find(source.Trucks, r.LocalID)
			old := find(remote.Trucks, r.RemoteID)
			var ownerDriver any
			if ref := t["ownerDriverRef"]; ref != "" {
				if linked := links["driver:"+ref]; linked != "" {
					ownerDriver = linked
				}
			}
			owner := map[string]any{"kind": t["ownerKind"], "mserpOwnerId": t["ownerId"], "displayName": t["ownerName"], "mserpOwnerDriverId": nullable(t["ownerDriverRef"]), "effectiveDate": nullable(t["ownerDate"])}
			ownerJSON, _ := json.Marshal(owner)
			status := "inactive"
			if v["active"] == "true" {
				status = "active"
			}
			if v["status"] == "out_of_service" || v["status"] == "maintenance" || (old != nil && old["status"] == "out_of_service") {
				status = "out_of_service"
			}
			if r.Action == "create" {
				_, e = b.Exec(ctx, `INSERT INTO vehicles(id,company_id,unit_number,vin,year,make,model,license_plate,license_plate_state,color,truck_class,gvwr_lbs,ownership,driver_id,status,mserp_managed,mserp_owner_snapshot,mserp_operational_active,mserp_operational_status,mserp_coverage_review) VALUES($1,$2,$3,$4,$5::int,$6,$7,$8,$9,$10,$11,$12::int,$13,$14,$15,true,$16,$17::boolean,$18,true)`, target, d.CompanyID, v["unit"], v["vin"], v["year"], v["make"], v["model"], v["plate"], v["plateState"], v["color"], v["truckClass"], v["gvwr"], t["ownerKind"], ownerDriver, status, ownerJSON, v["active"], v["status"])
			} else {
				_, e = b.Exec(ctx, `UPDATE vehicles SET unit_number=$3,vin=$4,year=$5::int,make=$6,model=$7,license_plate=$8,license_plate_state=$9,color=$10,truck_class=$11,gvwr_lbs=$12::int,ownership=$13,driver_id=$14,status=$15,leased_from=NULL,mserp_managed=true,mserp_owner_snapshot=$16,mserp_operational_active=$17::boolean,mserp_operational_status=$18,mserp_coverage_review=true,updated_at=now() WHERE id=$1 AND company_id=$2`, target, d.CompanyID, v["unit"], v["vin"], v["year"], v["make"], v["model"], v["plate"], v["plateState"], v["color"], v["truckClass"], v["gvwr"], t["ownerKind"], ownerDriver, status, ownerJSON, v["active"], v["status"])
			}
			if e != nil {
				return fmt.Errorf("FleetScope truck %s: %w", r.LocalID, e)
			}
			if v["coverageEvidence"] != "" {
				if e = d.reviewCoverage(ctx, b, target, v); e != nil {
					return fmt.Errorf("truck %s: %w", t["unit"], e)
				}
			}
		}
		// Crew changes are explicit. Ownership adoption alone must never create a
		// driver assignment or clear coverage/safety holds.
		for _, r := range s.Rows {
			if r.Action == "defer" || r.Kind != "truck" || r.Values["reconcileCrew"] != "true" {
				continue
			}
			t := find(source.Trucks, r.LocalID)
			target := links[r.ID]
			var hold bool
			var status string
			if e = b.QueryRow(ctx, `SELECT mserp_coverage_review,status FROM vehicles WHERE id=$1 AND company_id=$2`, target, d.CompanyID).Scan(&hold, &status); e != nil {
				return e
			}
			if (hold || status != "active") && t["primaryId"] != "" {
				return fmt.Errorf("truck %s requires coverage/safety review before crew reconciliation", t["unit"])
			}
			for _, key := range []string{"primaryId", "secondaryId"} {
				if t[key] != "" && links["driver:"+t[key]] == "" {
					return fmt.Errorf("truck %s has an unreviewed crew identity", t["unit"])
				}
			}
			// Clear all selected crews first to allow reviewed swaps. Preserve trailers.
			if _, e = b.Exec(ctx, `UPDATE fleet_assignments SET ended_at=now(),updated_at=now() WHERE company_id=$1 AND vehicle_id=$2 AND ended_at IS NULL AND trailer_id IS NULL`, d.CompanyID, target); e != nil {
				return e
			}
			if _, e = b.Exec(ctx, `UPDATE fleet_assignments SET driver_id=NULL,team_driver_id=NULL,updated_at=now() WHERE company_id=$1 AND vehicle_id=$2 AND ended_at IS NULL AND trailer_id IS NOT NULL`, d.CompanyID, target); e != nil {
				return e
			}
		}
		for _, r := range s.Rows {
			if r.Action == "defer" || r.Kind != "truck" || r.Values["reconcileCrew"] != "true" {
				continue
			}
			t := find(source.Trucks, r.LocalID)
			if t["primaryId"] == "" {
				continue
			}
			target := links[r.ID]
			primary, secondary := links["driver:"+t["primaryId"]], nullable(links["driver:"+t["secondaryId"]])
			var eligible bool
			if e = b.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM drivers WHERE company_id=$1 AND (id=$2::uuid OR id=$3::uuid) AND status<>'active')`, d.CompanyID, primary, secondary).Scan(&eligible); e != nil {
				return e
			}
			if !eligible {
				return errors.New("crew contains an ineligible FleetScope driver")
			}
			tag, err := b.Exec(ctx, `UPDATE fleet_assignments SET driver_id=$3,team_driver_id=$4,updated_at=now() WHERE company_id=$1 AND vehicle_id=$2 AND ended_at IS NULL`, d.CompanyID, target, primary, secondary)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				if _, e = b.Exec(ctx, `INSERT INTO fleet_assignments(company_id,vehicle_id,driver_id,team_driver_id) VALUES($1,$2,$3,$4)`, d.CompanyID, target, primary, secondary); e != nil {
					return e
				}
			}
		}
		remote, e = d.receiver(ctx, b)
		if e != nil {
			return e
		}
	}
	if ar == nil {
		if e = saveReceipt(ctx, a, s, d.CompanyID, actor, s.Source, source, links); e != nil {
			return e
		}
	}
	if br == nil {
		if e = saveReceipt(ctx, b, s, d.CompanyID, actor, s.Receiver, remote, links); e != nil {
			return e
		}
	}
	if e = a.Commit(ctx); e != nil {
		return fmt.Errorf("MSERP commit confirmation unavailable; recover using the same batch: %w", e)
	}
	if e = b.Commit(ctx); e != nil {
		return fmt.Errorf("FleetScope commit confirmation unavailable; recover using the same batch: %w", e)
	}
	// A new connection must observe both durable receipts before success.
	for _, pool := range []queryer{d.Source, d.Receiver} {
		var hash string
		if e = pool.QueryRow(ctx, `SELECT plan_hash FROM fleet_review.receipts WHERE batch_id=$1 AND company_id=$2`, s.Batch.ID, d.CompanyID).Scan(&hash); e != nil || hash != s.Batch.Hash {
			return errors.New("batch needs receipt readback; do not create a replacement batch")
		}
	}
	return nil
}
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (d *DB) releaseIneligibleDriver(ctx context.Context, tx pgx.Tx, driver string) error {
	rows, e := tx.Query(ctx, `SELECT id::text,vehicle_id::text,coalesce(driver_id::text,''),coalesce(team_driver_id::text,''),coalesce(trailer_id::text,''),coalesce(second_trailer_id::text,'') FROM fleet_assignments WHERE company_id=$1 AND ended_at IS NULL AND (driver_id=$2 OR team_driver_id=$2) ORDER BY vehicle_id FOR UPDATE`, d.CompanyID, driver)
	if e != nil {
		return e
	}
	type crew struct{ id, truck, main, team, trailer, second string }
	list := []crew{}
	for rows.Next() {
		var c crew
		if e = rows.Scan(&c.id, &c.truck, &c.main, &c.team, &c.trailer, &c.second); e != nil {
			rows.Close()
			return e
		}
		list = append(list, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, c := range list {
		remaining := c.main
		if remaining == driver {
			remaining = c.team
		}
		if remaining != "" {
			var eligible bool
			if e = tx.QueryRow(ctx, `SELECT status='active' FROM drivers WHERE id=$1 AND company_id=$2`, remaining, d.CompanyID).Scan(&eligible); e != nil {
				return e
			}
			if !eligible {
				remaining = ""
			}
		}
		if _, e = tx.Exec(ctx, `UPDATE fleet_assignments SET ended_at=now(),updated_at=now() WHERE id=$1 AND company_id=$2`, c.id, d.CompanyID); e != nil {
			return e
		}
		if remaining != "" || c.trailer != "" {
			if _, e = tx.Exec(ctx, `INSERT INTO fleet_assignments(company_id,vehicle_id,driver_id,trailer_id,second_trailer_id) VALUES($1,$2,$3,$4,$5)`, d.CompanyID, c.truck, nullable(remaining), nullable(c.trailer), nullable(c.second)); e != nil {
				return e
			}
		}
	}
	return nil
}
func (d *DB) reviewCoverage(ctx context.Context, tx pgx.Tx, truck string, v Record) error {
	var valid bool
	var e error
	switch v["coverageDecision"] {
	case "retain-policy":
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM insurance_coverage_vehicles v JOIN insurance_policy_coverages c ON c.id=v.coverage_id AND c.company_id=v.company_id JOIN insurance_policies p ON p.id=c.policy_id AND p.company_id=c.company_id WHERE v.company_id=$1 AND v.vehicle_id=$2 AND v.id=$3::uuid AND v.status='confirmed' AND c.type='auto_liability' AND p.status='active' AND p.expiration_date>=company_today(p.company_id))`, d.CompanyID, truck, v["coverageEvidence"]).Scan(&valid)
	case "external":
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM files WHERE id=$3::uuid AND company_id=$1 AND entity_id=$2 AND entity_type IN ('vehicle','vehicle_custom'))`, d.CompanyID, truck, v["coverageEvidence"]).Scan(&valid)
	default:
		return errors.New("choose an insurance evidence type")
	}
	if e != nil {
		return e
	}
	if !valid {
		return errors.New("insurance evidence is missing, expired or belongs to another truck")
	}
	_, e = tx.Exec(ctx, `UPDATE vehicles SET mserp_coverage_review=false WHERE id=$1 AND company_id=$2`, truck, d.CompanyID)
	return e
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }
