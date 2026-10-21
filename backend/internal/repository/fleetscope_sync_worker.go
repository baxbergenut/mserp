package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"mserp/internal/fleetscope"
)

func syncLinkMap(links []FleetSyncLink) map[string]FleetSyncLink {
	m := map[string]FleetSyncLink{}
	for _, l := range links {
		m[l.Kind+":"+l.LocalID] = l
	}
	return m
}

// All enrolled trucks form one component in v1. This includes empty crews, so
// a driver move never leaves a forgotten old truck assigned at the receiver.
func desiredSyncCrews(local FleetSyncLocal, links []FleetSyncLink) ([]fleetscope.Crew, error) {
	m := syncLinkMap(links)
	trucks := map[string]FleetSyncTruck{}
	drivers := map[string]FleetSyncDriver{}
	for _, t := range local.Trucks {
		trucks[t.Truck.MSERPTruckID] = t
	}
	for _, d := range local.Drivers {
		drivers[d.ID] = d
	}
	crews := []fleetscope.Crew{}
	used := map[string]bool{}
	for _, t := range local.Trucks {
		l := m["truck:"+t.Truck.MSERPTruckID]
		if t.PrimaryID != "" && (l.RemoteID == "" || l.ExcludedReason != "") {
			return nil, errors.New("waiting_for_truck_link")
		}
	}
	for _, l := range links {
		if l.Kind != "truck" || l.RemoteID == "" || l.ExcludedReason != "" {
			continue
		}
		t, exists := trucks[l.LocalID]
		c := fleetscope.Crew{MSERPTruckID: l.LocalID, FleetScopeVehicleID: l.RemoteID, ExpectedRevision: l.AssignmentRevision}
		if c.ExpectedRevision == "" {
			return nil, errors.New("waiting_for_assignment_revision")
		}
		if exists && t.PrimaryID != "" {
			if !t.Truck.OperationalActive || t.Truck.OperationalStatus == "maintenance" || t.Truck.OperationalStatus == "out_of_service" || t.Truck.OperationalStatus == "hold" {
				return nil, errors.New("operational_truck_conflict")
			}
			for i, id := range []string{t.PrimaryID, t.SecondaryID} {
				if id == "" {
					continue
				}
				d, ok := drivers[id]
				dl := m["driver:"+id]
				if !ok || d.Status == "terminated" {
					return nil, errors.New("terminated_driver")
				}
				if dl.RemoteID == "" || dl.ExcludedReason != "" {
					return nil, errors.New("waiting_for_driver_link")
				}
				if used[id] {
					return nil, errors.New("ambiguous_driver_assignment")
				}
				used[id] = true
				ref := &fleetscope.DriverRef{MSERPDriverID: id, FleetScopeDriverID: dl.RemoteID}
				if i == 0 {
					c.Primary = ref
				} else {
					c.Secondary = ref
				}
			}
		}
		crews = append(crews, c)
	}
	sort.Slice(crews, func(i, j int) bool { return crews[i].MSERPTruckID < crews[j].MSERPTruckID })
	return crews, nil
}
func crewStateHash(crews []fleetscope.Crew) string {
	copyCrews := append([]fleetscope.Crew{}, crews...)
	for i := range copyCrews {
		copyCrews[i].ExpectedRevision = ""
	}
	return syncHash(copyCrews)
}
func (r *FleetSyncRepository) PrepareAssignments(ctx context.Context, snapshotID string, revision int64, actor string) error {
	if !r.client.Options.Configured() {
		return errors.New("FleetScope sync is disabled")
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var epoch, mode string
	var rev, sequence int64
	e = tx.QueryRow(ctx, `SELECT coalesce(epoch::text,''),mode,source_revision,assignment_sequence FROM fleetscope_sync_state FOR UPDATE`).Scan(&epoch, &mode, &rev, &sequence)
	if e != nil {
		return e
	}
	if rev != revision || mode == "off" || mode == "live" {
		return ErrFleetSyncConflict
	}
	var raw []byte
	o := r.client.Options
	e = tx.QueryRow(ctx, `SELECT payload FROM fleetscope_sync_snapshots WHERE id=$1 AND source_revision=$2 AND company_id=$3 AND environment=$4 AND epoch=$5 AND created_at>now()-interval '24 hours'`, snapshotID, rev, o.CompanyID, o.Environment, epoch).Scan(&raw)
	if e != nil {
		return ErrFleetSyncConflict
	}
	var snap fleetscope.Snapshot
	if e = json.Unmarshal(raw, &snap); e != nil {
		return e
	}
	local, e := readSyncLocal(ctx, tx, o.CompanyID)
	if e != nil {
		return e
	}
	links, e := readSyncLinks(ctx, tx)
	if e != nil {
		return e
	}
	remote := map[string]fleetscope.RemoteTruck{}
	for _, t := range snap.Trucks {
		remote[t.ID] = t
	}
	for i := range links {
		l := &links[i]
		if l.Kind != "truck" || l.RemoteID == "" {
			continue
		}
		t, ok := remote[l.RemoteID]
		if !ok || t.MSERPID != l.LocalID || t.SafetyHold {
			return errors.New("refresh the snapshot and resolve missing trucks or safety holds")
		}
		l.AssignmentRevision = t.AssignmentRevision
	}
	crews, e := desiredSyncCrews(local, links)
	if e != nil {
		return e
	}
	if len(crews) == 0 {
		return errors.New("link trucks before reviewing assignments")
	}
	op := r.operation("assignments.apply", epoch)
	op.AssignmentSequence = sequence + 1
	op.Crews = crews
	op.SnapshotID = snap.ID
	if e = enqueueSync(ctx, tx, op, "assignments", crewStateHash(crews), actor, true); e != nil {
		return e
	}
	if e = syncAudit(ctx, tx, actor, "prepare_assignments", map[string]any{"snapshotId": snapshotID, "sourceRevision": revision, "operationId": op.OperationID}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (r *FleetSyncRepository) Activate(ctx context.Context, snapshotID string, revision, version int64, actor string) error {
	if r.client.Options.Mode != "live" {
		return errors.New("operator must configure FLEETSCOPE_SYNC_MODE=live before activation")
	}
	caps, e := r.client.Capabilities(ctx)
	if e != nil {
		return e
	}
	if caps.Mode != "live" {
		return errors.New("FleetScope must confirm live authority mode before activation")
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var currentRevision, currentVersion int64
	var epoch, ackHash, company, environment string
	e = tx.QueryRow(ctx, `SELECT source_revision,version,coalesce(epoch::text,''),assignment_hash,coalesce(company_id::text,''),coalesce(environment,'') FROM fleetscope_sync_state FOR UPDATE`).Scan(&currentRevision, &currentVersion, &epoch, &ackHash, &company, &environment)
	if e != nil {
		return e
	}
	if currentRevision != revision || currentVersion != version || epoch != caps.Epoch || company != caps.CompanyID || environment != caps.Environment {
		return ErrFleetSyncConflict
	}
	var pending bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM fleetscope_sync_operations WHERE state IN ('pending','processing','blocked'))`).Scan(&pending); e != nil {
		return e
	}
	if pending {
		return errors.New("resolve pending or blocked operations before activation")
	}
	var raw []byte
	e = tx.QueryRow(ctx, `SELECT payload FROM fleetscope_sync_snapshots WHERE id=$1 AND source_revision=$2 AND epoch=$3 AND created_at>now()-interval '15 minutes'`, snapshotID, revision, epoch).Scan(&raw)
	if e != nil {
		return ErrFleetSyncConflict
	}
	var snap fleetscope.Snapshot
	if e = json.Unmarshal(raw, &snap); e != nil {
		return e
	}
	local, e := readSyncLocal(ctx, tx, r.client.Options.CompanyID)
	if e != nil {
		return e
	}
	links, e := readSyncLinks(ctx, tx)
	if e != nil {
		return e
	}
	lm := syncLinkMap(links)
	for _, t := range local.Trucks {
		l := lm["truck:"+t.Truck.MSERPTruckID]
		if l.ExcludedReason != "" && t.PrimaryID == "" {
			continue
		}
		if l.RemoteID == "" || l.AcknowledgedHash != syncHash(t.Truck) {
			return errors.New("every truck must be synchronized or explicitly excluded")
		}
	}
	crews, e := desiredSyncCrews(local, links)
	if e != nil {
		return e
	}
	if ackHash == "" || ackHash != crewStateHash(crews) {
		return errors.New("apply and verify the reviewed assignment baseline first")
	}
	rm := map[string]fleetscope.RemoteTruck{}
	for _, t := range snap.Trucks {
		rm[t.ID] = t
	}
	for _, c := range crews {
		rt, ok := rm[c.FleetScopeVehicleID]
		primary, secondary := "", ""
		if c.Primary != nil {
			primary = c.Primary.FleetScopeDriverID
		}
		if c.Secondary != nil {
			secondary = c.Secondary.FleetScopeDriverID
		}
		if !ok || rt.MSERPID != c.MSERPTruckID || rt.PrimaryID != primary || rt.SecondaryID != secondary || rt.AssignmentRevision != c.ExpectedRevision || rt.SafetyHold {
			return errors.New("FleetScope readback does not match the reviewed crew baseline")
		}
		var owner fleetscope.Ownership
		if e = json.Unmarshal(rt.Ownership, &owner); e != nil {
			return errors.New("FleetScope owner readback is missing")
		}
		var acknowledged fleetscope.TruckProjection
		if e = json.Unmarshal(lm["truck:"+c.MSERPTruckID].Acknowledged, &acknowledged); e != nil || syncHash(owner) != syncHash(acknowledged.Ownership) {
			return errors.New("FleetScope owner readback does not match")
		}
		if lm["truck:"+c.MSERPTruckID].AcknowledgedHash != "retired" && (rt.Projection == nil || syncHash(rt.Projection) != syncHash(acknowledged)) {
			return errors.New("FleetScope truck readback does not match")
		}
	}
	_, e = tx.Exec(ctx, `UPDATE fleetscope_sync_state SET mode='live',version=version+1,last_error='',activated_at=now()`)
	if e != nil {
		return e
	}
	if e = syncAudit(ctx, tx, actor, "activate", map[string]any{"snapshotId": snapshotID, "sourceRevision": revision, "contractHash": fleetscope.ContractHash()}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (r *FleetSyncRepository) Process(ctx context.Context) error {
	if !r.client.Options.Configured() {
		return nil
	}
	bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	ctx = bounded
	conn, e := r.pool.Acquire(ctx)
	if e != nil {
		return e
	}
	defer conn.Release()
	var locked bool
	if e = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended('fleetscope:sync-worker',0))`).Scan(&locked); e != nil || !locked {
		return e
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := conn.Exec(c, `SELECT pg_advisory_unlock(hashtextextended('fleetscope:sync-worker',0))`); err != nil {
			_ = conn.Conn().Close(c)
		}
	}()
	var mode, epoch, company, env string
	if e = r.pool.QueryRow(ctx, `SELECT mode,coalesce(epoch::text,''),coalesce(company_id::text,''),coalesce(environment,'') FROM fleetscope_sync_state`).Scan(&mode, &epoch, &company, &env); e != nil {
		return e
	}
	if mode == "off" || mode == "paused" {
		return nil
	}
	caps, e := r.client.Capabilities(ctx)
	if e != nil {
		return r.recordSyncError(ctx, e)
	}
	if caps.Epoch != epoch || company != r.client.Options.CompanyID || env != r.client.Options.Environment || (mode == "live" && caps.Mode != "live") {
		return r.recordSyncError(ctx, errors.New("scope_or_epoch_mismatch"))
	}
	if mode == "live" && r.client.Options.Mode == "live" {
		if e = r.checkDrift(ctx, epoch); e != nil {
			return r.recordSyncError(ctx, e)
		}
		if e = r.pool.QueryRow(ctx, `SELECT mode FROM fleetscope_sync_state`).Scan(&mode); e != nil {
			return e
		}
		if mode != "live" {
			return nil
		}
		if e = r.project(ctx, epoch); e != nil {
			return r.recordSyncError(ctx, e)
		}
	}
	// Bounded work, no unbounded network fan-out. A later tick continues the queue.
	if r.client.Options.Mode != "live" {
		mode = "reconcile"
	}
	for i := 0; i < 2; i++ {
		did, err := r.deliverOne(ctx, epoch, mode)
		if err != nil {
			return err
		}
		if !did {
			break
		}
	}
	_, e = r.pool.Exec(ctx, `UPDATE fleetscope_sync_state SET last_checked_at=now() WHERE singleton`)
	return e
}

// Readback is independent of browser activity. Never repair unexplained drift.
// Compare committed receipts, not newer local edits waiting for projection.
func (r *FleetSyncRepository) checkDrift(ctx context.Context, epoch string) error {
	var due bool
	if e := r.pool.QueryRow(ctx, `SELECT (last_verified_at IS NULL OR last_verified_at<now()-interval '15 minutes') AND NOT EXISTS(SELECT 1 FROM fleetscope_sync_operations WHERE state IN ('pending','processing','blocked')) FROM fleetscope_sync_state`).Scan(&due); e != nil {
		return e
	}
	if !due {
		return nil
	}
	snap, e := r.client.Snapshot(ctx)
	if e != nil {
		return e
	}
	if snap.Epoch != epoch {
		return errors.New("scope_or_epoch_mismatch")
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var ackHash, mode string
	if e = tx.QueryRow(ctx, `SELECT assignment_hash,mode FROM fleetscope_sync_state FOR UPDATE`).Scan(&ackHash, &mode); e != nil {
		return e
	}
	if mode != "live" {
		return nil
	}
	var pending bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM fleetscope_sync_operations WHERE state IN ('pending','processing','blocked'))`).Scan(&pending); e != nil {
		return e
	}
	if pending {
		return nil
	}
	links, e := readSyncLinks(ctx, tx)
	if e != nil {
		return e
	}
	trucks := map[string]fleetscope.RemoteTruck{}
	drivers := map[string]fleetscope.RemoteDriver{}
	for _, t := range snap.Trucks {
		trucks[t.ID] = t
	}
	for _, d := range snap.Drivers {
		drivers[d.ID] = d
	}
	drift := false
	crews := []fleetscope.Crew{}
	for _, l := range links {
		if l.RemoteID == "" {
			continue
		}
		if l.Kind == "driver" {
			d, ok := drivers[l.RemoteID]
			if !ok || d.MSERPID != l.LocalID {
				drift = true
			}
			continue
		}
		t, ok := trucks[l.RemoteID]
		if !ok || t.MSERPID != l.LocalID || t.Revision != l.RemoteRevision || t.AssignmentRevision != l.AssignmentRevision {
			drift = true
			continue
		}
		if l.AcknowledgedHash != "retired" {
			var ack fleetscope.TruckProjection
			var owner fleetscope.Ownership
			if json.Unmarshal(l.Acknowledged, &ack) != nil || json.Unmarshal(t.Ownership, &owner) != nil || t.Projection == nil || syncHash(t.Projection) != syncHash(ack) || syncHash(owner) != syncHash(ack.Ownership) {
				drift = true
			}
		}
		c := fleetscope.Crew{MSERPTruckID: l.LocalID, FleetScopeVehicleID: l.RemoteID}
		if t.PrimaryID != "" {
			c.Primary = &fleetscope.DriverRef{MSERPDriverID: drivers[t.PrimaryID].MSERPID, FleetScopeDriverID: t.PrimaryID}
		}
		if t.SecondaryID != "" {
			c.Secondary = &fleetscope.DriverRef{MSERPDriverID: drivers[t.SecondaryID].MSERPID, FleetScopeDriverID: t.SecondaryID}
		}
		crews = append(crews, c)
	}
	sort.Slice(crews, func(i, j int) bool { return crews[i].MSERPTruckID < crews[j].MSERPTruckID })
	if ackHash != "" && crewStateHash(crews) != ackHash {
		drift = true
	}
	if drift {
		_, e = tx.Exec(ctx, `UPDATE fleetscope_sync_state SET mode='paused',version=version+1,last_error='receiver_drift_requires_review',last_verified_at=now()`)
		if e == nil {
			e = syncAudit(ctx, tx, "", "drift_paused", map[string]string{"remoteSnapshotId": snap.ID})
		}
	} else {
		_, e = tx.Exec(ctx, `UPDATE fleetscope_sync_state SET last_verified_at=now()`)
	}
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (r *FleetSyncRepository) recordSyncError(ctx context.Context, cause error) error {
	code := "sync_check_failed"
	var re *fleetscope.RemoteError
	if errors.As(cause, &re) {
		code = re.Code
	}
	if cause.Error() == "scope_or_epoch_mismatch" {
		code = cause.Error()
	}
	_, e := r.pool.Exec(ctx, `UPDATE fleetscope_sync_state SET last_error=$1,last_checked_at=now(),mode=CASE WHEN $1 IN ('scope_or_epoch_mismatch','contract_mismatch') THEN 'paused' ELSE mode END,version=version+CASE WHEN $1 IN ('scope_or_epoch_mismatch','contract_mismatch') THEN 1 ELSE 0 END`, code)
	return e
}

func (r *FleetSyncRepository) project(ctx context.Context, epoch string) error {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var sequence int64
	var ackHash, mode string
	if e = tx.QueryRow(ctx, `SELECT assignment_sequence,assignment_hash,mode FROM fleetscope_sync_state FOR UPDATE`).Scan(&sequence, &ackHash, &mode); e != nil {
		return e
	}
	if mode != "live" {
		return nil
	}
	local, e := readSyncLocal(ctx, tx, r.client.Options.CompanyID)
	if e != nil {
		return e
	}
	links, e := readSyncLinks(ctx, tx)
	if e != nil {
		return e
	}
	lm := syncLinkMap(links)
	rows, e := tx.Query(ctx, `SELECT entity_key FROM fleetscope_sync_operations WHERE state IN ('pending','processing','blocked')`)
	if e != nil {
		return e
	}
	pending := map[string]bool{}
	for rows.Next() {
		var k string
		if e = rows.Scan(&k); e != nil {
			rows.Close()
			return e
		}
		pending[k] = true
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	o := r.client.Options
	for _, d := range local.Drivers {
		key := "driver:" + d.ID
		if pending[key] || lm[key].RemoteID != "" || lm[key].ExcludedReason != "" || d.FleetScopeID == "" || d.HireEventID == "" {
			continue
		}
		if _, e = tx.Exec(ctx, `INSERT INTO fleetscope_sync_links(kind,local_id,company_id,environment) VALUES('driver',$1,$2,$3) ON CONFLICT DO NOTHING`, d.ID, o.CompanyID, o.Environment); e != nil {
			return e
		}
		op := r.operation("driver.link", epoch)
		op.MSERPID = d.ID
		op.FleetScopeID = d.FleetScopeID
		op.HireEventID = d.HireEventID
		op.SourceRevision = 1
		if e = enqueueSync(ctx, tx, op, key, "", "", false); e != nil {
			return e
		}
	}
	for _, t := range local.Trucks {
		key := "truck:" + t.Truck.MSERPTruckID
		l := lm[key]
		if pending[key] || l.ExcludedReason != "" || l.AcknowledgedHash == syncHash(t.Truck) {
			continue
		}
		if l.LocalID == "" {
			if _, e = tx.Exec(ctx, `INSERT INTO fleetscope_sync_links(kind,local_id,company_id,environment,last_error) VALUES('truck',$1,$2,$3,'waiting_for_truck_setup') ON CONFLICT DO NOTHING`, t.Truck.MSERPTruckID, o.CompanyID, o.Environment); e != nil {
				return e
			}
			continue
		}
		if len(t.Issues) > 0 || (l.RemoteID == "" && !validProvisioning(l.Provisioning)) {
			if _, e = tx.Exec(ctx, `UPDATE fleetscope_sync_links SET last_error='waiting_for_truck_setup' WHERE kind='truck' AND local_id=$1`, l.LocalID); e != nil {
				return e
			}
			continue
		}
		op := r.operation("truck.upsert", epoch)
		op.Truck = &t.Truck
		op.MSERPID = t.Truck.MSERPTruckID
		op.FleetScopeID = l.RemoteID
		op.ExpectedRevision = l.RemoteRevision
		op.SourceRevision = l.SourceRevision + 1
		if l.RemoteID == "" {
			op.Provisioning = l.Provisioning
		}
		if e = enqueueSync(ctx, tx, op, key, syncHash(t.Truck), "", false); e != nil {
			return e
		}
	}
	if !pending["assignments"] {
		crews, err := desiredSyncCrews(local, links)
		if err != nil {
			_, e = tx.Exec(ctx, `UPDATE fleetscope_sync_state SET last_error=$1`, err.Error())
			if e != nil {
				return e
			}
		} else if hash := crewStateHash(crews); hash != ackHash && len(crews) > 0 {
			op := r.operation("assignments.apply", epoch)
			op.AssignmentSequence = sequence + 1
			op.Crews = crews
			if e = enqueueSync(ctx, tx, op, "assignments", hash, "", false); e != nil {
				return e
			}
		}
	}
	// Deleted trucks retain their last identity. Release the crew first, then retire.
	existing := map[string]bool{}
	for _, t := range local.Trucks {
		existing[t.Truck.MSERPTruckID] = true
	}
	crews, crewErr := desiredSyncCrews(local, links)
	for _, l := range links {
		key := "truck:" + l.LocalID
		if l.Kind != "truck" || l.RemoteID == "" || existing[l.LocalID] || pending[key] || l.AcknowledgedHash == "retired" || crewErr != nil || crewStateHash(crews) != ackHash {
			continue
		}
		op := r.operation("truck.retire", epoch)
		op.MSERPID = l.LocalID
		op.FleetScopeID = l.RemoteID
		op.ExpectedRevision = l.RemoteRevision
		op.SourceRevision = l.SourceRevision + 1
		if e = enqueueSync(ctx, tx, op, key, "retired", "", false); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}

func (r *FleetSyncRepository) deliverOne(ctx context.Context, epoch, mode string) (bool, error) {
	lease := fleetscope.NewID()
	var id, key, desired, bodyHash string
	var payload []byte
	var bootstrap bool
	var attempts int
	// Reconciliation mode sends only explicitly reviewed operations. Pausing after
	// claim is checked again immediately before the request.
	e := r.pool.QueryRow(ctx, `WITH candidate AS(SELECT id FROM fleetscope_sync_operations WHERE epoch=$1 AND ((state='pending' AND next_attempt_at<=now()) OR (state='processing' AND lease_until<now())) AND ($2='live' OR created_by IS NOT NULL OR review_delivery) ORDER BY CASE WHEN kind='assignments.apply' THEN 1 ELSE 0 END,created_at FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE fleetscope_sync_operations o SET state='processing',attempts=attempts+1,lease_token=$3,lease_until=now()+interval '1 minute' FROM candidate c WHERE o.id=c.id RETURNING o.id::text,entity_key,payload,desired_hash,bootstrap,attempts,body_sha256`, epoch, mode, lease).Scan(&id, &key, &payload, &desired, &bootstrap, &attempts, &bodyHash)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	var allowed bool
	if e = r.pool.QueryRow(ctx, `SELECT mode IN ('reconcile','live') AND epoch=$1 FROM fleetscope_sync_state`, epoch).Scan(&allowed); e != nil {
		return true, e
	}
	if !allowed {
		return true, nil
	}
	var op fleetscope.Operation
	hash := sha256.Sum256(payload)
	if hex.EncodeToString(hash[:]) != bodyHash {
		_, e = r.pool.Exec(ctx, `UPDATE fleetscope_sync_operations SET state='blocked',last_error='payload_integrity_failed',lease_token=NULL,lease_until=NULL WHERE id=$1 AND lease_token=$2`, id, lease)
		return true, e
	}
	if e = json.Unmarshal(payload, &op); e != nil {
		return true, e
	}
	result, sendErr := r.client.Deliver(ctx, op, payload, bootstrap)
	if sendErr == nil {
		sendErr = validateSyncResult(op, result)
	}
	if sendErr != nil {
		code, status := "delivery_failed", 0
		retry := false
		delay := time.Minute
		var remote *fleetscope.RemoteError
		if errors.As(sendErr, &remote) {
			code, status, retry = remote.Code, remote.Status, remote.Retryable()
			if remote.RetryAfter > delay {
				delay = remote.RetryAfter
			}
		}
		if retry {
			backoff := time.Duration(1<<min(attempts, 10))*time.Second + time.Duration(time.Now().UnixNano()%1000)*time.Millisecond
			if backoff > delay {
				delay = backoff
			}
		}
		state := "blocked"
		if retry && attempts < 20 {
			state = "pending"
		}
		_, e = r.pool.Exec(ctx, `UPDATE fleetscope_sync_operations SET state=$3,last_error=$4,last_http_status=$5,next_attempt_at=now()+make_interval(secs=>$6),lease_token=NULL,lease_until=NULL WHERE id=$1 AND lease_token=$2`, id, lease, state, code, status, delay.Seconds())
		return true, e
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return true, e
	}
	defer tx.Rollback(ctx)
	var valid bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM fleetscope_sync_operations WHERE id=$1 AND lease_token=$2 AND state='processing')`, id, lease).Scan(&valid)
	if e != nil {
		return true, e
	}
	if !valid {
		return true, errors.New("FleetScope operation lease changed")
	}
	var actor *string
	_ = tx.QueryRow(ctx, `SELECT created_by::text FROM fleetscope_sync_operations WHERE id=$1`, id).Scan(&actor)
	rb, _ := json.Marshal(result)
	if op.Kind == "assignments.apply" {
		for _, c := range result.Crews {
			if _, e = tx.Exec(ctx, `UPDATE fleetscope_sync_links SET assignment_revision=$2 WHERE kind='truck' AND local_id=$1 AND remote_id=$3`, c.MSERPTruckID, c.ExpectedRevision, c.FleetScopeVehicleID); e != nil {
				return true, e
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE fleetscope_sync_state SET assignment_sequence=$1,assignment_hash=$2,last_error='' WHERE epoch=$3 AND assignment_sequence=$4`, op.AssignmentSequence, desired, op.Epoch, op.AssignmentSequence-1)
		e = err
		if e == nil && tag.RowsAffected() != 1 {
			e = ErrFleetSyncConflict
		}
	} else {
		kind := "truck"
		if op.Kind == "driver.link" {
			kind = "driver"
		}
		var ack []byte
		if op.Truck != nil {
			ack, _ = json.Marshal(op.Truck)
		}
		_, e = tx.Exec(ctx, `UPDATE fleetscope_sync_links SET remote_id=$3,remote_revision=$4,assignment_revision=CASE WHEN $5='' THEN assignment_revision ELSE $5 END,source_revision=$6,acknowledged=coalesce($7,acknowledged),acknowledged_hash=$8,last_error='',last_synced_at=now(),version=version+1 WHERE kind=$1 AND local_id=$2 AND company_id=$9 AND environment=$10`, kind, op.MSERPID, result.FleetScopeID, result.Revision, result.AssignmentRevision, op.SourceRevision, ack, desired, op.CompanyID, op.Environment)
		if e == nil && kind == "driver" {
			// Acquire the same intake lock as the incoming lifecycle receiver. Identity is
			// never changed by retries, and deleted local drivers are not resurrected.
			_, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('fleetscope:intake',0))`)
			if e == nil {
				tag, err := tx.Exec(ctx, `INSERT INTO fleetscope_driver_identity(company_id,fleetscope_driver_id,driver_id,source,reviewed_by) VALUES($1,$2,$3,$4,$5) ON CONFLICT(company_id,fleetscope_driver_id) DO UPDATE SET confirmed=true WHERE fleetscope_driver_identity.driver_id=excluded.driver_id`, op.CompanyID, result.FleetScopeID, op.MSERPID, map[bool]string{true: "review", false: "intake"}[bootstrap], actor)
				e = err
				if e == nil && tag.RowsAffected() != 1 {
					e = ErrFleetSyncConflict
				}
			}
		}
	}
	if e != nil {
		return true, e
	}
	_, e = tx.Exec(ctx, `UPDATE fleetscope_sync_operations SET state='applied',result=$3,applied_at=now(),last_error='',lease_token=NULL,lease_until=NULL WHERE id=$1 AND lease_token=$2`, id, lease, rb)
	if e != nil {
		return true, e
	}
	if e = syncAudit(ctx, tx, "", "acknowledged", map[string]string{"operationId": id, "entityKey": key}); e != nil {
		return true, e
	}
	return true, tx.Commit(ctx)
}
func validateSyncResult(op fleetscope.Operation, r fleetscope.Result) error {
	bad := func() error { return &fleetscope.RemoteError{Status: 502, Code: "invalid_receipt"} }
	if op.Kind != "assignments.apply" {
		if !fleetscope.ValidID(r.FleetScopeID) || r.Revision == "" || (op.FleetScopeID != "" && r.FleetScopeID != op.FleetScopeID) {
			return bad()
		}
		if op.Kind != "driver.link" && r.AssignmentRevision == "" {
			return bad()
		}
		return nil
	}
	if len(op.Crews) != len(r.Crews) {
		return bad()
	}
	expected := map[string]fleetscope.Crew{}
	for _, c := range op.Crews {
		expected[c.MSERPTruckID] = c
	}
	for _, c := range r.Crews {
		old, ok := expected[c.MSERPTruckID]
		if !ok || c.ExpectedRevision == "" || old.FleetScopeVehicleID != c.FleetScopeVehicleID || syncHash(old.Primary) != syncHash(c.Primary) || syncHash(old.Secondary) != syncHash(c.Secondary) {
			return bad()
		}
		delete(expected, c.MSERPTruckID)
	}
	if len(expected) != 0 {
		return fmt.Errorf("invalid crew receipt")
	}
	return nil
}
