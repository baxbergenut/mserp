package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/relay"
)

// Unknown labels fail closed. Gross Board planning/history never participates.
func relayStatusEnabled(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "READY", "REST", "COVERED", "DISPATCHED", "ENROUTE", "RESERVED", "DEADHEAD", "IN THE YARD", "HOLD FOR LOAD", "ISSUE WITH PU/DEL", "AT PU", "AT DEL", "34 RESET":
		return true
	}
	return false
}

// Configuration is startup-only; disabled means no reads or writes to Relay.
func (r *FuelRepository) ConfigureRelayStatusPolicy(enabled bool) { r.relayStatusPolicy = enabled }

type RelayStatusImpact struct {
	RelayDriverID  string `json:"relayDriverId"`
	DriverID       string `json:"driverId"`
	StatusDriverID string `json:"statusDriverId"`
	Status         string `json:"-"`
	Employment     string `json:"employment"`
	Active         bool   `json:"active"`
	DesiredEnabled bool   `json:"desiredEnabled"`
	CurrentEnabled *bool  `json:"currentEnabled"`
	Action         string `json:"action"`
	Revision       string `json:"-"`
	Context        string `json:"-"`
}

// Current team routing is date-sensitive, matching the Status Board. Both
// policies follow the main driver; each retains their own employment/security holds.
const relayStatusContextSQL = `SELECT d.id::text,main.id::text,d.status,d.active,
 CASE WHEN NOT d.active OR d.status='terminated' THEN 'Left'
 WHEN NOT main.active OR main.status='terminated' THEN 'Left'
 WHEN fleet_status_personal_label(main.availability_status)<>'' THEN fleet_status_personal_label(main.availability_status)
 ELSE coalesce(b.status,'') END,
 jsonb_build_object('driver',d.id,'employment',d.status,'active',d.active,'availability',d.availability_status,
 'statusVersion',d.status_version,'main',main.id,'mainEmployment',main.status,'mainActive',main.active,'mainAvailability',main.availability_status,'mainVersion',main.status_version,
 'boardStatus',coalesce(b.status,''),'boardVersion',coalesce(b.version,0))::text
 FROM relay_driver_links l JOIN drivers d ON d.id=l.driver_id
 JOIN drivers main ON main.id=payroll_driver_at(d.id,(now() AT TIME ZONE 'America/New_York')::date)
 LEFT JOIN driver_board b ON b.driver_id=main.id
 WHERE l.relay_environment=$1 AND l.relay_driver_id=$2`

func (r *FuelRepository) relayStatusContext(ctx context.Context, q relayStatusQuerier, id string) (RelayStatusImpact, error) {
	v := RelayStatusImpact{RelayDriverID: id}
	err := q.QueryRow(ctx, relayStatusContextSQL, r.relayEnvironment, id).Scan(&v.DriverID, &v.StatusDriverID, &v.Employment, &v.Active, &v.Status, &v.Context)
	v.DesiredEnabled = v.Active && v.Employment != "terminated" && relayStatusEnabled(v.Status)
	return v, err
}

type relayStatusQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (r *FuelRepository) relayStatusDecision(ctx context.Context, q relayStatusQuerier, v *RelayStatusImpact, a *relay.PolicyAssignment) error {
	var enabled bool
	if err := q.QueryRow(ctx, `SELECT fleet_status_enabled()`).Scan(&enabled); err != nil {
		return err
	}
	if !enabled {
		v.Action = "fleet_status_disabled"
		return nil
	}
	v.Action = "no_change"
	if a == nil {
		v.Action = "no_assignment"
		return nil
	}
	current := a.Enabled
	v.CurrentEnabled = &current
	v.Revision = relayPolicyRevision(a)
	if !v.Active || v.Employment == "terminated" {
		v.Action = "employment_hold"
		return nil
	}
	var pending, backoff bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM relay_driver_operations WHERE environment=$1 AND relay_driver_id=$2 AND state='pending'),
 EXISTS(SELECT 1 FROM relay_driver_operations WHERE environment=$1 AND relay_driver_id=$2 AND source='fleet_status' AND state='failed' AND NOT (status_context @> '{"aborted":true}'::jsonb) AND created_at>now()-interval '15 minutes')`, r.relayEnvironment, v.RelayDriverID).Scan(&pending, &backoff)
	if err != nil {
		return err
	}
	if pending {
		v.Action = "uncertain_hold"
		return nil
	}
	if backoff {
		v.Action = "failure_backoff"
		return nil
	}
	if a.Enabled == v.DesiredEnabled {
		return nil
	}
	if !v.DesiredEnabled {
		v.Action = "disable"
		return nil
	}
	var owned bool
	err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM relay_driver_operations o WHERE o.environment=$1 AND o.relay_driver_id=$2
 AND o.driver_id=$3 AND o.source='fleet_status' AND o.action='disable' AND o.state='confirmed'
 AND NOT o.status_ownership_revoked AND o.status_owned_revision=$4)`, r.relayEnvironment, v.RelayDriverID, v.DriverID, v.Revision).Scan(&owned)
	if err != nil {
		return err
	}
	v.Action = "manual_block_hold"
	if owned {
		v.Action = "enable"
	}
	return nil
}

// Preview uses only cached vendor evidence and current local data. It writes no
// receipts, refreshes no roster, and remains available while automation is off.
func (r *FuelRepository) PreviewRelayStatusPolicy(ctx context.Context) ([]RelayStatusImpact, error) {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	return r.relayStatusPreview(ctx, conn)
}
func (r *FuelRepository) relayStatusPreview(ctx context.Context, conn relayStatusQuerier) ([]RelayStatusImpact, error) {
	var raw []byte
	err := conn.QueryRow(ctx, `SELECT snapshot FROM relay_driver_rosters WHERE environment=$1`, r.relayEnvironment).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return []RelayStatusImpact{}, nil
	}
	if err != nil {
		return nil, err
	}
	var snap RelayRosterSnapshot
	if err = json.Unmarshal(raw, &snap); err != nil {
		return nil, err
	}
	out := []RelayStatusImpact{}
	for _, entry := range snap.Items {
		v, e := r.relayStatusContext(ctx, conn, entry.Driver.ID)
		if errors.Is(e, pgx.ErrNoRows) {
			out = append(out, RelayStatusImpact{RelayDriverID: entry.Driver.ID, Action: "unlinked_hold"})
			continue
		}
		if e != nil {
			return nil, e
		}
		if e = r.relayStatusDecision(ctx, conn, &v, entry.Assignment); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}

// A convergent sweep observes every actor/source, including date-effective team
// changes. Each action is revalidated under the native fleet lock and identity
// row lock; a receipt is committed before sending. No callbacks send vendor writes.
func (r *FuelRepository) ProcessRelayStatusPolicy(ctx context.Context) error {
	if !r.relayStatusPolicy {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	conn, done, err := r.relayDriverLock(ctx)
	if err != nil {
		return err
	}
	defer done()
	var enabled bool
	if err = conn.QueryRow(ctx, `SELECT fleet_status_enabled()`).Scan(&enabled); err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	var stale bool
	if err = conn.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM relay_driver_rosters WHERE environment=$1 AND refreshed_at>now()-interval '5 minutes')`, r.relayEnvironment).Scan(&stale); err != nil {
		return err
	}
	if stale {
		if _, err = r.refreshRelayDrivers(ctx, conn); err != nil {
			return err
		}
	}
	candidates, err := r.relayStatusPreview(ctx, conn)
	if err != nil {
		return err
	}
	// Resume after the last attempted account so a slow/unavailable provider
	// cannot starve the tail of a large roster across bounded passes/restarts.
	var cursor string
	if err = conn.QueryRow(ctx, `SELECT status_scan_cursor FROM relay_driver_rosters WHERE environment=$1`, r.relayEnvironment).Scan(&cursor); err != nil {
		return err
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].RelayDriverID < candidates[j].RelayDriverID })
	cut := sort.Search(len(candidates), func(i int) bool { return candidates[i].RelayDriverID > cursor })
	candidates = append(candidates[cut:], candidates[:cut]...)
	var failures []error
	for _, v := range candidates {
		if v.DriverID == "" || v.Employment == "terminated" || !v.Active {
			continue
		}
		if _, err = conn.Exec(ctx, `UPDATE relay_driver_rosters SET status_scan_cursor=$2 WHERE environment=$1`, r.relayEnvironment, v.RelayDriverID); err != nil {
			return err
		}
		// Read every linked active assignment, even cached no-ops, to detect portal
		// changes. Never assume the cached enabled bit is remote confirmation.
		if err = r.applyRelayStatusPolicy(ctx, conn, v); err != nil {
			failures = append(failures, err)
		}
		if ctx.Err() != nil {
			break
		}
	}
	return errors.Join(failures...)
}

func (r *FuelRepository) applyRelayStatusPolicy(ctx context.Context, conn *pgxpool.Conn, candidate RelayStatusImpact) error {
	v, err := r.relayStatusContext(ctx, conn, candidate.RelayDriverID)
	if err != nil {
		return err
	}
	if v.DriverID != candidate.DriverID || v.Context != candidate.Context {
		return nil
	}
	readCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	a, err := r.relayDrivers.Assignment(readCtx, v.RelayDriverID)
	if err != nil {
		return err
	}
	if err = r.relayStatusDecision(ctx, conn, &v, a); err != nil {
		return err
	}
	if v.Action != "enable" && v.Action != "disable" {
		return nil
	}
	var receipt string
	if err = conn.QueryRow(ctx, `INSERT INTO relay_driver_operations(id,environment,target,action,relay_driver_id,driver_id,policy_id,source,status_context)
 VALUES(gen_random_uuid(),$1,$2,$3,$2,$4,$5,'fleet_status',$6::jsonb) RETURNING id::text`, r.relayEnvironment, v.RelayDriverID, v.Action, v.DriverID, a.PolicyID, v.Context).Scan(&receipt); err != nil {
		return err
	}
	operationCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer stop()
	// Known-unsent failures can safely back off; only a dispatched request
	// (or failed receipt persistence) creates an uncertain hold.
	sent := false
	defer func() {
		if !sent {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cleanupCancel()
			_, _ = conn.Exec(cleanupCtx, `UPDATE relay_driver_operations SET state='failed',resolved_at=now(),status_context=status_context||'{"aborted":true}'::jsonb WHERE id=$1 AND state='pending'`, receipt)
		}
	}()
	var linked string
	tx, err := conn.Begin(operationCtx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(operationCtx, `SET LOCAL lock_timeout='5s'; SELECT fleet_status_lock()`); err != nil {
		return err
	}
	if err = tx.QueryRow(operationCtx, `SELECT driver_id::text FROM relay_driver_links WHERE relay_environment=$1 AND relay_driver_id=$2 FOR SHARE`, r.relayEnvironment, v.RelayDriverID).Scan(&linked); err != nil {
		return err
	}
	current, err := r.relayStatusContext(operationCtx, tx, v.RelayDriverID)
	if err != nil {
		return err
	}
	if current.Context != v.Context || linked != v.DriverID {
		_, err = tx.Exec(operationCtx, `UPDATE relay_driver_operations SET state='failed',resolved_at=now(),status_context=status_context||'{"aborted":true}'::jsonb WHERE id=$1`, receipt)
		if err != nil {
			return err
		}
		return tx.Commit(operationCtx)
	}
	var fleetEnabled bool
	if err = tx.QueryRow(operationCtx, `SELECT fleet_status_enabled()`).Scan(&fleetEnabled); err != nil {
		return err
	}
	if !fleetEnabled {
		return nil
	}
	if v.Action == "enable" {
		var owned bool
		if err = tx.QueryRow(operationCtx, `SELECT EXISTS(SELECT 1 FROM relay_driver_operations WHERE environment=$1 AND relay_driver_id=$2 AND driver_id=$3 AND source='fleet_status' AND action='disable' AND state='confirmed' AND NOT status_ownership_revoked AND status_owned_revision=$4)`, r.relayEnvironment, v.RelayDriverID, v.DriverID, v.Revision).Scan(&owned); err != nil {
			return err
		}
		if !owned {
			return nil
		}
	}
	// Remote state is not conditionally writable. This second fresh read narrows
	// the portal race, but cannot eliminate it (documented provider limitation).
	assignment, err := r.relayDrivers.Assignment(operationCtx, v.RelayDriverID)
	if err != nil {
		return err
	}
	if relayPolicyRevision(assignment) != v.Revision {
		_, err = tx.Exec(operationCtx, `UPDATE relay_driver_operations SET status_ownership_revoked=true WHERE environment=$2 AND relay_driver_id=$3 AND source='fleet_status' AND id<>$1`, receipt, r.relayEnvironment, v.RelayDriverID)
		if err != nil {
			return err
		}
		return tx.Commit(operationCtx)
	}
	sent = true
	err = r.relayDrivers.EnablePolicy(operationCtx, v.RelayDriverID, v.DesiredEnabled)
	if err != nil {
		var upstream *relay.APIError
		if errors.As(err, &upstream) && !upstream.Uncertain {
			if _, e := tx.Exec(operationCtx, `UPDATE relay_driver_operations SET state='failed',resolved_at=now() WHERE id=$1`, receipt); e != nil {
				return e
			}
			if e := tx.Commit(operationCtx); e != nil {
				return e
			}
		}
		return err
	}
	confirmed, err := r.relayDrivers.Assignment(operationCtx, v.RelayDriverID)
	if err != nil {
		return err
	}
	if confirmed == nil || confirmed.ID != a.ID || confirmed.PolicyID != a.PolicyID || confirmed.Enabled != v.DesiredEnabled {
		return ErrRelayDriverConflict
	}
	if _, err = tx.Exec(operationCtx, `UPDATE relay_driver_operations SET status_ownership_revoked=true WHERE environment=$1 AND relay_driver_id=$2 AND source='fleet_status' AND id<>$3 AND NOT status_ownership_revoked`, r.relayEnvironment, v.RelayDriverID, receipt); err != nil {
		return err
	}
	owned := ""
	if !v.DesiredEnabled {
		owned = relayPolicyRevision(confirmed)
	}
	if _, err = tx.Exec(operationCtx, `UPDATE relay_driver_operations SET state='confirmed',resolved_at=now(),status_owned_revision=$2 WHERE id=$1`, receipt, owned); err != nil {
		return err
	}
	return tx.Commit(operationCtx)
}

// Cached evidence freshness is explicit; preview never contacts the provider.
type RelayStatusPreviewMetadata struct {
	AutomationEnabled  bool       `json:"automationEnabled"`
	FleetStatusEnabled bool       `json:"fleetStatusEnabled"`
	RefreshedAt        *time.Time `json:"rosterRefreshedAt"`
	Evidence           string     `json:"evidence"`
}

type RelayStatusPolicyReport struct {
	Metadata RelayStatusPreviewMetadata `json:"metadata"`
	Items    []RelayStatusImpact        `json:"items"`
}

func (r *FuelRepository) PreviewRelayStatusPolicyReport(ctx context.Context) (RelayStatusPolicyReport, error) {
	result := RelayStatusPolicyReport{Metadata: RelayStatusPreviewMetadata{AutomationEnabled: r.relayStatusPolicy, Evidence: "cached_relay_roster"}}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `SELECT fleet_status_enabled(),(SELECT refreshed_at FROM relay_driver_rosters WHERE environment=$1)`, r.relayEnvironment).Scan(&result.Metadata.FleetStatusEnabled, &result.Metadata.RefreshedAt)
	if err != nil {
		return result, err
	}
	result.Items, err = r.relayStatusPreview(ctx, tx)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

// Ownership is assignment-specific, independent of profile timestamps and usage.
func relayPolicyRevision(a *relay.PolicyAssignment) string {
	if a == nil {
		return ""
	}
	b, _ := json.Marshal(struct {
		ID, PolicyID string
		Enabled      bool
	}{a.ID, a.PolicyID, a.Enabled})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
