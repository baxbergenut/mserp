package fleetreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	Source, Receiver            *pgxpool.Pool
	CompanyID, CompanyName, DOT string
	SourceCompanyName           string
}
type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func records(ctx context.Context, q queryer, sql string, args ...any) ([]Record, error) {
	rows, e := q.Query(ctx, sql, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		var v map[string]any
		if e = json.Unmarshal(b, &v); e != nil {
			return nil, e
		}
		r := Record{}
		for k, x := range v {
			if x != nil {
				switch y := x.(type) {
				case string:
					r[k] = y
				case map[string]any, []any:
					b, _ := json.Marshal(y)
					r[k] = string(b)
				default:
					r[k] = fmt.Sprint(y)
				}
			} else {
				r[k] = ""
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (d *DB) source(ctx context.Context, q queryer) (s Snapshot, e error) {
	if d.SourceCompanyName != "" {
		var valid bool
		e = q.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(full_name=$1) FROM investors WHERE is_company`, d.SourceCompanyName).Scan(&valid)
		if e != nil {
			return
		}
		if !valid {
			return s, errors.New("MSERP company identity does not match the pinned company")
		}
	}
	s.Drivers, e = records(ctx, q, `SELECT jsonb_build_object('id',d.id,'name',d.full_name,'firstName',d.first_name,'lastName',d.last_name,'email',d.email,'phone',d.phone,'status',d.status,'active',d.active,'terminationDate',d.termination_date::date,'updatedAt',d.updated_at,
 'fleetScopeId',(SELECT CASE WHEN count(DISTINCT i.fleetscope_driver_id)=1 THEN min(i.fleetscope_driver_id::text) END FROM fleetscope_driver_intake i WHERE i.company_id=$1::uuid AND i.driver_id=d.id AND i.completed_at IS NOT NULL),'identityIssue',(SELECT count(DISTINCT i.fleetscope_driver_id)>1 FROM fleetscope_driver_intake i WHERE i.company_id=$1::uuid AND i.driver_id=d.id AND i.completed_at IS NOT NULL)) FROM drivers d ORDER BY d.id`, d.CompanyID)
	if e != nil {
		return
	}
	s.Trucks, e = records(ctx, q, `SELECT jsonb_build_object('id',t.id,'unit',t.unit_number,'vin',t.vin,'year',t.year,'make',t.make,'model',t.model,'plate',t.license_plate,'plateState',t.license_state,'active',t.active,'status',t.status,'ownerId',t.owner_id,'ownerName',coalesce(od.full_name,o.full_name),'ownerKind',CASE WHEN o.is_company THEN 'company' WHEN o.driver_id IS NOT NULL THEN 'driver' ELSE 'investor' END,'ownerDriverRef',o.driver_id,'ownerDate',CASE WHEN h.start_known THEN (h.assigned_at AT TIME ZONE 'America/New_York')::date END,'ownershipVersion',md5(row_to_json(h)::text),'updatedAt',t.updated_at,'primaryId',a.driver_id,'secondaryId',team.team_driver_id) || CASE WHEN t.color IS NULL THEN '{}'::jsonb ELSE jsonb_build_object('color',t.color) END
 FROM trucks t JOIN investors o ON o.id=t.owner_id LEFT JOIN drivers od ON od.id=o.driver_id LEFT JOIN truck_ownership_history h ON h.truck_id=t.id AND h.unassigned_at IS NULL
 LEFT JOIN truck_driver_assignments a ON a.truck_id=t.id AND a.unassigned_at IS NULL
 LEFT JOIN driver_teams team ON team.main_driver_id=a.driver_id AND team.start_week<=date_trunc('week',now() AT TIME ZONE 'America/New_York')::date AND (team.end_week IS NULL OR team.end_week>date_trunc('week',now() AT TIME ZONE 'America/New_York')::date) ORDER BY t.id`)
	if e != nil {
		return
	}
	s.Owners, e = records(ctx, q, `SELECT jsonb_build_object('id',i.id,'name',coalesce(d.full_name,i.full_name),'kind',CASE WHEN i.is_company THEN 'company' WHEN i.driver_id IS NOT NULL THEN 'driver' ELSE 'investor' END,'driverId',i.driver_id,'updatedAt',i.updated_at) FROM investors i LEFT JOIN drivers d ON d.id=i.driver_id ORDER BY i.id`)
	return
}
func (d *DB) receiver(ctx context.Context, q queryer) (s Snapshot, e error) {
	var name, dot string
	e = q.QueryRow(ctx, `SELECT name,dot_number FROM companies WHERE id=$1`, d.CompanyID).Scan(&name, &dot)
	if e != nil {
		return
	}
	if name != d.CompanyName || dot != d.DOT {
		return s, errors.New("FleetScope company identity does not match the pinned company")
	}
	s.Drivers, e = records(ctx, q, `SELECT jsonb_build_object('id',id,'firstName',first_name,'lastName',last_name,'name',first_name||' '||last_name,'email',email,'phone',phone_number,'status',status,'terminationDate',termination_date::date,'updatedAt',updated_at) FROM drivers WHERE company_id=$1 ORDER BY id`, d.CompanyID)
	if e != nil {
		return
	}
	s.Trucks, e = records(ctx, q, `SELECT jsonb_build_object('id',v.id,'unit',v.unit_number,'vin',v.vin,'year',v.year,'make',v.make,'model',v.model,'plate',v.license_plate,'plateState',v.license_plate_state,'status',v.status,'ownerKind',v.ownership,'ownerDriverRef',v.driver_id,'leasedFrom',v.leased_from,'ownerSnapshot',to_jsonb(v)->'mserp_owner_snapshot','color',v.color,'truckClass',v.truck_class,'gvwr',v.gvwr_lbs,'updatedAt',v.updated_at,'primaryId',a.driver_id,'secondaryId',a.team_driver_id,'trailerId',a.trailer_id,'secondTrailerId',a.second_trailer_id,'coverageHold',to_jsonb(v)->'mserp_coverage_review','operationalActive',to_jsonb(v)->'mserp_operational_active','operationalStatus',to_jsonb(v)->'mserp_operational_status') FROM vehicles v LEFT JOIN fleet_assignments a ON a.vehicle_id=v.id AND a.company_id=v.company_id AND a.ended_at IS NULL WHERE v.company_id=$1 ORDER BY v.id`, d.CompanyID)
	s.Owners = []Record{}
	if e != nil {
		return
	}
	options, err := records(ctx, q, `SELECT jsonb_build_object('truckId',v.vehicle_id,'id',v.id,'kind','retain-policy','label',p.provider_name||' · '||p.policy_number||' · expires '||p.expiration_date::text)
 FROM insurance_coverage_vehicles v JOIN insurance_policy_coverages c ON c.id=v.coverage_id AND c.company_id=v.company_id JOIN insurance_policies p ON p.id=c.policy_id AND p.company_id=c.company_id
 WHERE v.company_id=$1 AND v.status='confirmed' AND c.type='auto_liability' AND p.status='active' AND p.expiration_date>=company_today(p.company_id)
 UNION ALL SELECT jsonb_build_object('truckId',entity_id,'id',id,'kind','external','label',coalesce(nullif(display_name,''),file_name)) FROM files WHERE company_id=$1 AND entity_type IN ('vehicle','vehicle_custom') ORDER BY 1`, d.CompanyID)
	if err != nil {
		return s, err
	}
	for _, truck := range s.Trucks {
		choices := []Record{}
		for _, option := range options {
			if option["truckId"] == truck["id"] {
				choices = append(choices, option)
			}
		}
		encoded, _ := json.Marshal(choices)
		truck["coverageOptions"] = string(encoded)
	}
	return
}
func (d *DB) Snapshot(ctx context.Context) (Snapshot, Snapshot, error) {
	a, e := d.Source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return Snapshot{}, Snapshot{}, e
	}
	defer a.Rollback(ctx)
	b, e := d.Receiver.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return Snapshot{}, Snapshot{}, e
	}
	defer b.Rollback(ctx)
	source, e := d.source(ctx, a)
	if e != nil {
		return source, Snapshot{}, e
	}
	remote, e := d.receiver(ctx, b)
	return source, remote, e
}

// Provisioned explicitly by the operator, never by an HTTP request or startup.
const ReceiptSchema = `CREATE SCHEMA IF NOT EXISTS fleet_review;
CREATE TABLE IF NOT EXISTS fleet_review.receipts (
 batch_id uuid PRIMARY KEY, company_id uuid NOT NULL, plan_hash text NOT NULL,
 before_state jsonb NOT NULL, after_state jsonb NOT NULL, identity_links jsonb NOT NULL,
 actor text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now());
REVOKE ALL ON SCHEMA fleet_review FROM PUBLIC; REVOKE ALL ON fleet_review.receipts FROM PUBLIC;
CREATE OR REPLACE FUNCTION fleet_review.receipt_immutable() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'reconciliation receipts are immutable'; END $$;
CREATE OR REPLACE TRIGGER receipt_immutable BEFORE UPDATE OR DELETE ON fleet_review.receipts FOR EACH ROW EXECUTE FUNCTION fleet_review.receipt_immutable();`

func (d *DB) CheckApplyReady(ctx context.Context) error {
	for _, p := range []*pgxpool.Pool{d.Source, d.Receiver} {
		var ok bool
		if e := p.QueryRow(ctx, `SELECT to_regclass('fleet_review.receipts') IS NOT NULL`).Scan(&ok); e != nil {
			return e
		}
		if !ok {
			return errors.New("reconciliation receipt storage is not installed")
		}
	}
	var ok bool
	if e := d.Receiver.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='vehicles' AND column_name='mserp_owner_snapshot')`).Scan(&ok); e != nil {
		return e
	}
	if !ok {
		return errors.New("FleetScope ownership migration is required before applying; drafts remain available")
	}
	return d.checkUnenrolled(ctx, d.Source, d.Receiver)
}

// An off mode alone is not sufficient: enrollment, reviewed links and old queues
// belong to the integration recovery workflow, not this initial-adoption portal.
func (d *DB) checkUnenrolled(ctx context.Context, source, receiver queryer) error {
	var active bool
	if e := receiver.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM mserp_sync_state WHERE company_id=$1) OR EXISTS(SELECT 1 FROM mserp_sync_links WHERE company_id=$1)`, d.CompanyID).Scan(&active); e != nil {
		return e
	}
	if active {
		return errors.New("FleetScope has integration enrollment or identity links; use the integration reconciliation workflow")
	}
	var installed bool
	if e := source.QueryRow(ctx, `SELECT to_regclass('public.fleetscope_sync_state') IS NOT NULL`).Scan(&installed); e != nil {
		return e
	}
	if installed {
		if e := source.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM fleetscope_sync_state WHERE mode<>'off' OR epoch IS NOT NULL OR activated_at IS NOT NULL) OR EXISTS(SELECT 1 FROM fleetscope_sync_links) OR EXISTS(SELECT 1 FROM fleetscope_sync_operations)`).Scan(&active); e != nil {
			return e
		}
		if active {
			return errors.New("MSERP has integration enrollment, links or operations; use the integration reconciliation workflow")
		}
	}
	return nil
}
