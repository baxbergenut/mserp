package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// A manual baseline corrects the operational PM cycle without editing an invoice.
// A changed/new latest completed work order automatically supersedes it.
const maintenancePMJoin = ` LEFT JOIN LATERAL (
 SELECT v.id,v.version,v.service_date,v.odometer,v.oil_type FROM maintenance_records v
 WHERE v.truck_id=t.id AND v.status='completed' AND ('00000000-0000-4000-8000-000000000020'::uuid=ANY(v.program_ids) OR (v.program_ids IS NULL AND EXISTS(SELECT 1 FROM jsonb_array_elements(v.services) s WHERE s->>'trackingCode' IN ('pm','oil'))))
 ORDER BY v.service_date DESC,v.created_at DESC,v.id DESC LIMIT 1
 ) original_pm ON true
 LEFT JOIN maintenance_pm_baselines baseline_pm ON baseline_pm.truck_id=t.id
 LEFT JOIN LATERAL (
 SELECT original_pm.id,coalesce(original_pm.version,0) AS record_version,coalesce(baseline_pm.version,0) AS baseline_version,
 CASE WHEN baseline_pm.source_record_id IS NOT DISTINCT FROM original_pm.id AND baseline_pm.source_version=coalesce(original_pm.version,0) THEN baseline_pm.service_date ELSE original_pm.service_date END AS service_date,
 CASE WHEN baseline_pm.source_record_id IS NOT DISTINCT FROM original_pm.id AND baseline_pm.source_version=coalesce(original_pm.version,0) THEN baseline_pm.odometer ELSE original_pm.odometer END AS odometer,
 CASE WHEN baseline_pm.source_record_id IS NOT DISTINCT FROM original_pm.id AND baseline_pm.source_version=coalesce(original_pm.version,0) THEN baseline_pm.cycle_id ELSE original_pm.id END AS cycle_id,
 original_pm.oil_type
 ) m ON true `

type MaintenancePMInput struct {
	MemberVersion   *int    `json:"-"`
	TruckID         string  `json:"truckId"`
	LastDate        string  `json:"lastDate"`
	LastMiles       *int    `json:"lastMiles"`
	RecordID        *string `json:"recordId"`
	RecordVersion   int     `json:"recordVersion"`
	BaselineVersion int     `json:"baselineVersion"`
	ActorID         string  `json:"-"`
	ActorName       string  `json:"-"`
}

func (r *MaintenanceRepository) SavePMBaseline(ctx context.Context, in MaintenancePMInput) error {
	date, err := time.Parse(time.DateOnly, in.LastDate)
	zone, _ := time.LoadLocation("America/New_York")
	if err != nil || in.LastDate > time.Now().In(zone).Format(time.DateOnly) || in.LastMiles != nil && (*in.LastMiles < 0 || *in.LastMiles > 2000000000) {
		return fmt.Errorf("%w: enter a PM date through today and a nonnegative service odometer", ErrMaintenanceInvalid)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var truck string
	if err = tx.QueryRow(ctx, "SELECT id FROM trucks WHERE id=$1 FOR UPDATE", in.TruckID).Scan(&truck); err != nil {
		return mapNotFound(err)
	}
	if in.MemberVersion != nil {
		var version int
		if err = tx.QueryRow(ctx, `SELECT version FROM service_program_trucks WHERE program_id=$1 AND truck_id=$2`, OilChangeProgramID, truck).Scan(&version); err != nil {
			return mapNotFound(err)
		}
		if version != *in.MemberVersion {
			return ErrMaintenanceConflict
		}
	}
	var record *string
	var recordVersion, baselineVersion int
	var before []byte
	err = tx.QueryRow(ctx, `SELECT m.id::text,m.record_version,m.baseline_version,to_jsonb(m) FROM trucks t `+maintenancePMJoin+` WHERE t.id=$1`, truck).Scan(&record, &recordVersion, &baselineVersion, &before)
	if err != nil {
		return err
	}
	if pmRecordID(record) != pmRecordID(in.RecordID) || recordVersion != in.RecordVersion || baselineVersion != in.BaselineVersion {
		return ErrMaintenanceConflict
	}
	if in.LastMiles != nil {
		if err = checkMaintenanceReading(ctx, tx, truck, date, *in.LastMiles, pmRecordID(record), ""); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO maintenance_pm_baselines(truck_id,service_date,odometer,source_record_id,source_version,actor_name)
 VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(truck_id) DO UPDATE SET service_date=excluded.service_date,odometer=excluded.odometer,
 source_record_id=excluded.source_record_id,source_version=excluded.source_version,actor_name=excluded.actor_name,
 cycle_id=gen_random_uuid(),version=maintenance_pm_baselines.version+1,updated_at=now()`, truck, date, in.LastMiles, record, recordVersion, in.ActorName)
	if err != nil {
		return err
	}
	after, _ := json.Marshal(in)
	_, err = tx.Exec(ctx, `INSERT INTO access_audit(actor_id,action,target_id,details) VALUES(NULLIF($1,'')::uuid,'maintenance_pm_baseline',$2,jsonb_build_object('actorName',$3::text,'before',$4::jsonb,'after',$5::jsonb))`, in.ActorID, truck, in.ActorName, before, after)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func pmRecordID(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}
