package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/fleetscope"
)

var ErrFleetSyncConflict = errors.New("FleetScope review changed; refresh the comparison before continuing")

type FleetSyncRepository struct {
	pool   *pgxpool.Pool
	client *fleetscope.SyncClient
}

func NewFleetSyncRepository(pool *pgxpool.Pool, o fleetscope.SyncOptions) *FleetSyncRepository {
	return &FleetSyncRepository{pool, fleetscope.NewSyncClient(o)}
}

type FleetSyncDriver struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	FleetScopeID string `json:"fleetScopeId"`
	HireEventID  string `json:"hireEventId"`
}
type FleetSyncTruck struct {
	Truck       fleetscope.TruckProjection `json:"truck"`
	PrimaryID   string                     `json:"primaryId"`
	SecondaryID string                     `json:"secondaryId"`
	Issues      []string                   `json:"issues"`
}
type FleetSyncLocal struct {
	SourceRevision int64             `json:"sourceRevision"`
	Week           string            `json:"week"`
	Trucks         []FleetSyncTruck  `json:"trucks"`
	Drivers        []FleetSyncDriver `json:"drivers"`
}
type FleetSyncLink struct {
	Kind               string                   `json:"kind"`
	LocalID            string                   `json:"localId"`
	RemoteID           string                   `json:"remoteId"`
	RemoteRevision     string                   `json:"remoteRevision"`
	AssignmentRevision string                   `json:"assignmentRevision"`
	SourceRevision     int64                    `json:"sourceRevision"`
	Acknowledged       json.RawMessage          `json:"acknowledged"`
	AcknowledgedHash   string                   `json:"acknowledgedHash"`
	Provisioning       *fleetscope.Provisioning `json:"provisioning"`
	ExcludedReason     string                   `json:"excludedReason"`
	LastError          string                   `json:"lastError"`
	LastSyncedAt       *time.Time               `json:"lastSyncedAt"`
	Version            int64                    `json:"version"`
}
type FleetSyncOperationInfo struct {
	ID        string     `json:"id"`
	EntityKey string     `json:"entityKey"`
	Kind      string     `json:"kind"`
	State     string     `json:"state"`
	Attempts  int        `json:"attempts"`
	LastError string     `json:"lastError"`
	CreatedAt time.Time  `json:"createdAt"`
	AppliedAt *time.Time `json:"appliedAt"`
}
type FleetSyncView struct {
	CanReview              bool                     `json:"canReview"`
	CanManage              bool                     `json:"canManage"`
	PendingCount           int                      `json:"pendingCount"`
	Configured             bool                     `json:"configured"`
	ConfiguredMode         string                   `json:"configuredMode"`
	CompanyID              string                   `json:"companyId"`
	Environment            string                   `json:"environment"`
	ContractHash           string                   `json:"contractHash"`
	Mode                   string                   `json:"mode"`
	Version                int64                    `json:"version"`
	Epoch                  string                   `json:"epoch"`
	LastError              string                   `json:"lastError"`
	LastCheckedAt          *time.Time               `json:"lastCheckedAt"`
	Local                  FleetSyncLocal           `json:"local"`
	Links                  []FleetSyncLink          `json:"links"`
	Operations             []FleetSyncOperationInfo `json:"operations"`
	SnapshotID             string                   `json:"snapshotId"`
	SnapshotSourceRevision int64                    `json:"snapshotSourceRevision"`
	SnapshotCreatedAt      *time.Time               `json:"snapshotCreatedAt"`
	Remote                 *fleetscope.Snapshot     `json:"remote"`
}
type syncQuery interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readSyncLocal(ctx context.Context, q syncQuery, company string) (FleetSyncLocal, error) {
	v := FleetSyncLocal{Week: ChargeCurrentWeek(), Trucks: []FleetSyncTruck{}, Drivers: []FleetSyncDriver{}}
	if e := q.QueryRow(ctx, `SELECT source_revision FROM fleetscope_sync_state WHERE singleton`).Scan(&v.SourceRevision); e != nil {
		return v, e
	}
	rows, e := q.Query(ctx, `SELECT d.id::text,d.full_name,d.status,coalesce(i.fleetscope_driver_id::text,''),coalesce((SELECT r.event_id::text FROM fleetscope_webhook_receipts r JOIN fleetscope_driver_intake x ON x.id=r.intake_id WHERE x.company_id=$1::uuid AND x.driver_id=d.id AND x.completed_at IS NOT NULL ORDER BY r.received_at LIMIT 1),'') FROM drivers d LEFT JOIN fleetscope_driver_identity i ON i.driver_id=d.id AND i.company_id=$1::uuid AND i.confirmed ORDER BY d.id`, nullString(company))
	if e != nil {
		return v, e
	}
	for rows.Next() {
		var d FleetSyncDriver
		if e = rows.Scan(&d.ID, &d.Name, &d.Status, &d.FleetScopeID, &d.HireEventID); e != nil {
			rows.Close()
			return v, e
		}
		v.Drivers = append(v.Drivers, d)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return v, e
	}
	rows, e = q.Query(ctx, `SELECT t.id::text,t.unit_number,t.vin,t.year,t.make,t.model,t.license_plate,t.license_state,t.active,CASE WHEN t.status='terminated' THEN 'available' ELSE t.status END,
 coalesce(i.id::text,''),coalesce(d.full_name,i.full_name,''),CASE WHEN i.is_company THEN 'company' WHEN i.driver_id IS NOT NULL THEN 'driver' ELSE 'investor' END,i.driver_id::text,
 (SELECT CASE WHEN h.start_known THEN (h.assigned_at AT TIME ZONE 'America/New_York')::date::text END FROM truck_ownership_history h WHERE h.truck_id=t.id AND h.unassigned_at IS NULL),
 coalesce(a.driver_id::text,''),coalesce((SELECT dt.team_driver_id::text FROM driver_teams dt JOIN drivers td ON td.id=dt.team_driver_id AND td.active WHERE dt.main_driver_id=a.driver_id AND dt.start_week<=$1::date AND (dt.end_week IS NULL OR dt.end_week>$1::date) ORDER BY dt.start_week DESC LIMIT 1),'')
 FROM trucks t LEFT JOIN investors i ON i.id=t.owner_id LEFT JOIN drivers d ON d.id=i.driver_id
 LEFT JOIN truck_driver_assignments a ON a.truck_id=t.id AND a.unassigned_at IS NULL ORDER BY t.id`, v.Week)
	if e != nil {
		return v, e
	}
	for rows.Next() {
		var t FleetSyncTruck
		p := &t.Truck
		e = rows.Scan(&p.MSERPTruckID, &p.UnitNumber, &p.VIN, &p.Year, &p.Make, &p.Model, &p.LicensePlate, &p.LicenseState, &p.OperationalActive, &p.OperationalStatus, &p.Ownership.MSERPOwnerID, &p.Ownership.DisplayName, &p.Ownership.Kind, &p.Ownership.MSERPOwnerDriverID, &p.Ownership.EffectiveDate, &t.PrimaryID, &t.SecondaryID)
		if e != nil {
			rows.Close()
			return v, e
		}
		t.Issues = truckSyncIssues(*p)
		v.Trucks = append(v.Trucks, t)
	}
	e = rows.Err()
	rows.Close()
	return v, e
}
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func syncHash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func truckSyncIssues(t fleetscope.TruckProjection) []string {
	r := []string{}
	if e := t.Ownership.Validate(); e != nil {
		r = append(r, "Review owner identity")
	}
	if t.VIN == nil || len(strings.TrimSpace(*t.VIN)) != 17 {
		r = append(r, "Valid VIN required")
	} else {
		for _, c := range *t.VIN {
			if !strings.ContainsRune("0123456789ABCDEFGHJKLMNPRSTUVWXYZ", c) {
				r = append(r, "VIN must contain 17 uppercase VIN characters")
				break
			}
		}
	}
	if strings.TrimSpace(t.UnitNumber) == "" || len(t.UnitNumber) > 50 {
		r = append(r, "Valid unit number required")
	}
	if t.Year == nil || *t.Year < 1900 || *t.Year > time.Now().Year()+2 {
		r = append(r, "Valid year required")
	}
	for label, p := range map[string]*string{"Make": t.Make, "Model": t.Model, "Plate": t.LicensePlate, "Plate state": t.LicenseState} {
		if p == nil || strings.TrimSpace(*p) == "" {
			r = append(r, label+" required")
		}
	}
	sort.Strings(r)
	return r
}
func readSyncLinks(ctx context.Context, q syncQuery) ([]FleetSyncLink, error) {
	rows, e := q.Query(ctx, `SELECT kind,local_id::text,coalesce(remote_id::text,''),remote_revision,assignment_revision,source_revision,acknowledged,acknowledged_hash,provisioning,excluded_reason,last_error,last_synced_at,version FROM fleetscope_sync_links ORDER BY kind,local_id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	v := []FleetSyncLink{}
	for rows.Next() {
		var l FleetSyncLink
		var p []byte
		if e = rows.Scan(&l.Kind, &l.LocalID, &l.RemoteID, &l.RemoteRevision, &l.AssignmentRevision, &l.SourceRevision, &l.Acknowledged, &l.AcknowledgedHash, &p, &l.ExcludedReason, &l.LastError, &l.LastSyncedAt, &l.Version); e != nil {
			return nil, e
		}
		if len(p) > 0 {
			if e = json.Unmarshal(p, &l.Provisioning); e != nil {
				return nil, e
			}
		}
		v = append(v, l)
	}
	return v, rows.Err()
}
func (r *FleetSyncRepository) View(ctx context.Context) (FleetSyncView, error) {
	o := r.client.Options
	v := FleetSyncView{Configured: o.Configured(), ConfiguredMode: o.Mode, CompanyID: o.CompanyID, Environment: o.Environment, ContractHash: fleetscope.ContractHash(), Operations: []FleetSyncOperationInfo{}}
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return v, e
	}
	defer tx.Rollback(ctx)
	e = tx.QueryRow(ctx, `SELECT mode,version,coalesce(epoch::text,''),last_error,last_checked_at FROM fleetscope_sync_state`).Scan(&v.Mode, &v.Version, &v.Epoch, &v.LastError, &v.LastCheckedAt)
	if e != nil {
		return v, e
	}
	v.Local, e = readSyncLocal(ctx, tx, o.CompanyID)
	if e != nil {
		return v, e
	}
	v.Links, e = readSyncLinks(ctx, tx)
	if e != nil {
		return v, e
	}
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM fleetscope_sync_operations WHERE state IN ('pending','processing','blocked')`).Scan(&v.PendingCount); e != nil {
		return v, e
	}
	rows, e := tx.Query(ctx, `SELECT id::text,entity_key,kind,state,attempts,last_error,created_at,applied_at FROM fleetscope_sync_operations ORDER BY (state IN ('pending','processing','blocked')) DESC,created_at DESC LIMIT 200`)
	if e != nil {
		return v, e
	}
	for rows.Next() {
		var op FleetSyncOperationInfo
		if e = rows.Scan(&op.ID, &op.EntityKey, &op.Kind, &op.State, &op.Attempts, &op.LastError, &op.CreatedAt, &op.AppliedAt); e != nil {
			rows.Close()
			return v, e
		}
		v.Operations = append(v.Operations, op)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return v, e
	}
	var payload []byte
	e = tx.QueryRow(ctx, `SELECT id::text,source_revision,created_at,payload FROM fleetscope_sync_snapshots ORDER BY created_at DESC LIMIT 1`).Scan(&v.SnapshotID, &v.SnapshotSourceRevision, &v.SnapshotCreatedAt, &payload)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return v, e
	}
	if len(payload) > 0 {
		if e = json.Unmarshal(payload, &v.Remote); e != nil {
			return v, e
		}
	}
	return v, tx.Commit(ctx)
}
func (r *FleetSyncRepository) CaptureReview(ctx context.Context, actor string) error {
	caps, e := r.client.Capabilities(ctx)
	if e != nil {
		return e
	}
	remote, e := r.client.Snapshot(ctx)
	if e != nil {
		return e
	}
	if remote.Epoch != caps.Epoch {
		return ErrFleetSyncConflict
	}
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var company, env, epoch string
	e = tx.QueryRow(ctx, `SELECT coalesce(company_id::text,''),coalesce(environment,''),coalesce(epoch::text,'') FROM fleetscope_sync_state FOR UPDATE`).Scan(&company, &env, &epoch)
	if e != nil {
		return e
	}
	o := r.client.Options
	if company != "" && (company != o.CompanyID || env != o.Environment || epoch != caps.Epoch) {
		return errors.New("FleetScope scope or epoch changed; explicit restore reconciliation is required")
	}
	local, e := readSyncLocal(ctx, tx, o.CompanyID)
	if e != nil {
		return e
	}
	b, _ := json.Marshal(remote)
	lb, _ := json.Marshal(local)
	if _, e = tx.Exec(ctx, `INSERT INTO fleetscope_sync_snapshots(remote_id,company_id,environment,epoch,source_revision,payload,local_payload,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, remote.ID, o.CompanyID, o.Environment, caps.Epoch, local.SourceRevision, b, lb, actor); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE fleetscope_sync_state SET company_id=$1,environment=$2,epoch=$3,mode=CASE WHEN mode='off' THEN 'reconcile' ELSE mode END,version=version+1,last_checked_at=now(),last_error=''`, o.CompanyID, o.Environment, caps.Epoch); e != nil {
		return e
	}
	if e = syncAudit(ctx, tx, actor, "snapshot", map[string]any{"remoteSnapshotId": remote.ID, "sourceRevision": local.SourceRevision}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func syncAudit(ctx context.Context, tx pgx.Tx, actor, action string, details any) error {
	b, e := json.Marshal(details)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO fleetscope_sync_audit(actor_id,action,details) VALUES($1,$2,$3)`, nullString(actor), action, b)
	return e
}
func enqueueSync(ctx context.Context, tx pgx.Tx, op fleetscope.Operation, key, desired, actor string, bootstrap bool) error {
	b, e := json.Marshal(op)
	if e != nil {
		return e
	}
	if len(b) > fleetscope.SyncBodyLimit {
		return errors.New("FleetScope operation exceeds the size limit")
	}
	h := sha256.Sum256(b)
	_, e = tx.Exec(ctx, `INSERT INTO fleetscope_sync_operations(id,entity_key,kind,company_id,environment,epoch,payload,body_sha256,desired_hash,bootstrap,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, op.OperationID, key, op.Kind, op.CompanyID, op.Environment, op.Epoch, b, hex.EncodeToString(h[:]), desired, bootstrap, nullString(actor))
	return e
}
func (r *FleetSyncRepository) operation(kind, epoch string) fleetscope.Operation {
	o := r.client.Options
	return fleetscope.Operation{Version: 1, OperationID: fleetscope.NewID(), CompanyID: o.CompanyID, Environment: o.Environment, Epoch: epoch, OccurredAt: time.Now().UTC(), Kind: kind}
}

type FleetSyncReviewInput struct {
	SnapshotID     string                   `json:"snapshotId"`
	SourceRevision int64                    `json:"sourceRevision"`
	Action         string                   `json:"action"`
	Kind           string                   `json:"kind"`
	LocalID        string                   `json:"localId"`
	RemoteID       string                   `json:"remoteId"`
	Reason         string                   `json:"reason"`
	Provisioning   *fleetscope.Provisioning `json:"provisioning"`
}

func (r *FleetSyncRepository) Review(ctx context.Context, in FleetSyncReviewInput, actor string) error {
	if !r.client.Options.Configured() {
		return errors.New("FleetScope sync is disabled")
	}
	if !fleetscope.ValidID(in.SnapshotID) || !fleetscope.ValidID(in.LocalID) || (in.Kind != "truck" && in.Kind != "driver") || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 1000 {
		return errors.New("select a record and enter a review reason")
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var rev int64
	var epoch, company, env string
	e = tx.QueryRow(ctx, `SELECT source_revision,coalesce(epoch::text,''),coalesce(company_id::text,''),coalesce(environment,'') FROM fleetscope_sync_state FOR UPDATE`).Scan(&rev, &epoch, &company, &env)
	if e != nil {
		return e
	}
	o := r.client.Options
	if rev != in.SourceRevision || company != o.CompanyID || env != o.Environment {
		return ErrFleetSyncConflict
	}
	var b []byte
	e = tx.QueryRow(ctx, `SELECT payload FROM fleetscope_sync_snapshots WHERE id=$1 AND source_revision=$2 AND company_id=$3 AND environment=$4 AND epoch=$5 AND created_at>now()-interval '24 hours'`, in.SnapshotID, rev, company, env, epoch).Scan(&b)
	if e != nil {
		return ErrFleetSyncConflict
	}
	var remote fleetscope.Snapshot
	if e = json.Unmarshal(b, &remote); e != nil {
		return e
	}
	local, e := readSyncLocal(ctx, tx, company)
	if e != nil {
		return e
	}
	var truck *FleetSyncTruck
	var driver *FleetSyncDriver
	for i := range local.Trucks {
		if local.Trucks[i].Truck.MSERPTruckID == in.LocalID {
			truck = &local.Trucks[i]
		}
	}
	for i := range local.Drivers {
		if local.Drivers[i].ID == in.LocalID {
			driver = &local.Drivers[i]
		}
	}
	if in.Kind == "truck" && truck == nil || in.Kind == "driver" && driver == nil {
		return ErrNotFound
	}
	var pending bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM fleetscope_sync_operations WHERE entity_key=$1 AND state IN ('pending','processing','blocked'))`, in.Kind+":"+in.LocalID).Scan(&pending); e != nil {
		return e
	}
	if pending {
		return errors.New("resolve the existing operation before changing this review")
	}
	var existingRemote string
	e = tx.QueryRow(ctx, `SELECT coalesce(remote_id::text,'') FROM fleetscope_sync_links WHERE kind=$1 AND local_id=$2`, in.Kind, in.LocalID).Scan(&existingRemote)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if in.Action == "exclude" {
		if existingRemote != "" {
			return errors.New("a linked record cannot be excluded; retire or reconcile it explicitly")
		}
		if truck != nil && truck.PrimaryID != "" {
			return errors.New("an assigned truck cannot be excluded")
		}
		if driver != nil {
			for _, t := range local.Trucks {
				if t.PrimaryID == driver.ID || t.SecondaryID == driver.ID {
					return errors.New("an assigned driver cannot be excluded")
				}
			}
		}
		_, e = tx.Exec(ctx, `INSERT INTO fleetscope_sync_links(kind,local_id,company_id,environment,excluded_reason) VALUES($1,$2,$3,$4,$5) ON CONFLICT(kind,local_id) DO UPDATE SET excluded_reason=$5,version=fleetscope_sync_links.version+1`, in.Kind, in.LocalID, company, env, in.Reason)
	} else if in.Action == "link" || in.Action == "create" || in.Action == "refresh" {
		if existingRemote != "" && (in.Action != "refresh" || in.Kind != "truck" || in.RemoteID != existingRemote) {
			return errors.New("record already linked; identity replacement requires a separate reviewed correction")
		}
		if in.Action == "refresh" && existingRemote == "" {
			return ErrFleetSyncConflict
		}
		op := r.operation(in.Kind+".link", epoch)
		op.SnapshotID = remote.ID
		op.MSERPID = in.LocalID
		op.FleetScopeID = in.RemoteID
		op.SourceRevision = 1
		if in.Action == "refresh" {
			var mode string
			if e = tx.QueryRow(ctx, `SELECT mode FROM fleetscope_sync_state`).Scan(&mode); e != nil {
				return e
			}
			if mode != "paused" && mode != "reconcile" {
				return errors.New("pause live synchronization before reviewing a replacement")
			}
			if e = tx.QueryRow(ctx, `SELECT source_revision+1 FROM fleetscope_sync_links WHERE kind='truck' AND local_id=$1`, in.LocalID).Scan(&op.SourceRevision); e != nil {
				return e
			}
		}
		if in.Kind == "driver" {
			if in.Action != "link" {
				return errors.New("drivers must already exist in FleetScope")
			}
			for _, d := range remote.Drivers {
				if d.ID == in.RemoteID {
					if d.MSERPID != "" && d.MSERPID != in.LocalID {
						return ErrFleetSyncConflict
					}
					op.ExpectedRevision = d.Revision
				}
			}
			if op.ExpectedRevision == "" {
				return errors.New("select an existing driver from the snapshot")
			}
			// Explicitly refuse conflicting local lifecycle identities, including completed intakes.
			var conflict bool
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM fleetscope_driver_identity WHERE company_id=$1 AND (fleetscope_driver_id=$2 OR driver_id=$3) AND (fleetscope_driver_id<>$2 OR driver_id<>$3))`, company, in.RemoteID, in.LocalID).Scan(&conflict); e != nil {
				return e
			}
			if conflict {
				return errors.New("driver identity already linked to a different person")
			}
			// Reserve both sides now so concurrent intake cannot claim this person
			// while delivery is pending. Lifecycle reads require confirmed=true.
			tag, err := tx.Exec(ctx, `INSERT INTO fleetscope_driver_identity(company_id,fleetscope_driver_id,driver_id,source,reviewed_by,confirmed) VALUES($1,$2,$3,'review',$4,false) ON CONFLICT(company_id,fleetscope_driver_id) DO UPDATE SET driver_id=fleetscope_driver_identity.driver_id WHERE fleetscope_driver_identity.driver_id=excluded.driver_id`, company, in.RemoteID, in.LocalID, actor)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return ErrFleetSyncConflict
			}
		} else {
			if len(truck.Issues) > 0 {
				return errors.New(strings.Join(truck.Issues, "; "))
			}
			op.Truck = &truck.Truck
			if in.Action == "link" || in.Action == "refresh" {
				if in.Action == "refresh" {
					op.Kind = "truck.upsert"
				}
				for _, t := range remote.Trucks {
					if t.ID == in.RemoteID {
						if t.MSERPID != "" && t.MSERPID != in.LocalID {
							return ErrFleetSyncConflict
						}
						if !strings.EqualFold(strings.TrimSpace(t.VIN), strings.TrimSpace(*truck.Truck.VIN)) {
							return errors.New("VIN mismatch; correct identity before linking")
						}
						op.ExpectedRevision = t.Revision
					}
				}
				if op.ExpectedRevision == "" {
					return errors.New("select an existing truck from the snapshot")
				}
			} else {
				if in.RemoteID != "" {
					return errors.New("create must not select an existing truck")
				}
				for _, t := range remote.Trucks {
					if strings.EqualFold(strings.TrimSpace(t.VIN), strings.TrimSpace(*truck.Truck.VIN)) {
						return errors.New("a truck with this VIN already exists; review a link")
					}
				}
				op.Kind = "truck.upsert"
				op.Provisioning = in.Provisioning
				if !validProvisioning(in.Provisioning) {
					return errors.New("color, truck class and positive GVWR are required for a new FleetScope truck")
				}
			}
		}
		var p []byte
		if in.Provisioning != nil {
			p, _ = json.Marshal(in.Provisioning)
		}
		if _, e = tx.Exec(ctx, `INSERT INTO fleetscope_sync_links(kind,local_id,company_id,environment,provisioning) VALUES($1,$2,$3,$4,$5) ON CONFLICT(kind,local_id) DO UPDATE SET provisioning=coalesce($5,fleetscope_sync_links.provisioning),excluded_reason='',version=fleetscope_sync_links.version+1`, in.Kind, in.LocalID, company, env, p); e != nil {
			return e
		}
		desired := ""
		if truck != nil {
			desired = syncHash(truck.Truck)
		}
		e = enqueueSync(ctx, tx, op, in.Kind+":"+in.LocalID, desired, actor, true)
	} else {
		return errors.New("unsupported review action")
	}
	if e != nil {
		return e
	}
	if e = syncAudit(ctx, tx, actor, "review", in); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE fleetscope_sync_state SET version=version+1`)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func validProvisioning(p *fleetscope.Provisioning) bool {
	return p != nil && strings.TrimSpace(p.Color) != "" && len(p.Color) <= 30 && strings.TrimSpace(p.TruckClass) != "" && len(p.TruckClass) <= 20 && p.GVWRLbs > 0 && p.GVWRLbs <= 200000
}

func (r *FleetSyncRepository) ResumeReview(ctx context.Context, version int64, actor string) error {
	caps, err := r.client.Capabilities(ctx)
	if err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE fleetscope_sync_state SET mode='reconcile',version=version+1,last_error='' WHERE version=$1 AND mode='paused' AND company_id=$2 AND environment=$3 AND epoch=$4`, version, r.client.Options.CompanyID, r.client.Options.Environment, caps.Epoch)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrFleetSyncConflict
	}
	if err = syncAudit(ctx, tx, actor, "resume_review", map[string]any{"version": version}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *FleetSyncRepository) Pause(ctx context.Context, version int64, actor string) error {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, `UPDATE fleetscope_sync_state SET mode='paused',version=version+1 WHERE version=$1`, version)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrFleetSyncConflict
	}
	if e = syncAudit(ctx, tx, actor, "pause", map[string]any{"version": version}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (r *FleetSyncRepository) Retry(ctx context.Context, id, actor string) error {
	if !fleetscope.ValidID(id) {
		return ErrNotFound
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, `UPDATE fleetscope_sync_operations SET state='pending',review_delivery=true,next_attempt_at=now(),last_error='',lease_token=NULL,lease_until=NULL WHERE id=$1 AND (state IN ('blocked','pending') OR (state='processing' AND lease_until<now()))`, id)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrFleetSyncConflict
	}
	if e = syncAudit(ctx, tx, actor, "retry", map[string]string{"operationId": id}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// A known rejection can be replaced with a new reviewed operation; uncertain
// outcomes must first be resolved by idempotent replay. Assignment sequence is
// only advanced by acknowledged commits, never by local supersession.
func (r *FleetSyncRepository) Supersede(ctx context.Context, id, actor string) error {
	if !fleetscope.ValidID(id) {
		return ErrNotFound
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var mode string
	if e = tx.QueryRow(ctx, `SELECT mode FROM fleetscope_sync_state FOR UPDATE`).Scan(&mode); e != nil {
		return e
	}
	if mode != "paused" {
		return errors.New("pause synchronization before replacing a rejected operation")
	}
	tag, e := tx.Exec(ctx, `UPDATE fleetscope_sync_operations SET state='superseded' WHERE id=$1 AND state='blocked' AND last_http_status IN (409,422) AND result IS NULL`, id)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return errors.New("only a confirmed 409/422 rejection can be superseded; retry uncertain outcomes first")
	}
	if _, e = tx.Exec(ctx, `DELETE FROM fleetscope_driver_identity i USING fleetscope_sync_operations o WHERE o.id=$1 AND o.kind='driver.link' AND i.company_id=o.company_id AND i.driver_id=substring(o.entity_key from 8)::uuid AND NOT i.confirmed`, id); e != nil {
		return e
	}
	if e = syncAudit(ctx, tx, actor, "supersede", map[string]string{"operationId": id}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (r *FleetSyncRepository) String() string {
	return fmt.Sprintf("FleetScope sync (%s)", r.client.Options.Mode)
}
