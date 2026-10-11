package repository

import (
	"context"
	"errors"
	"time"
)

// All termination paths converge on current driver status and its termination
// date. Only explicitly linked Relay identities are eligible. Rehires cancel
// further actions; they never silently restore fuel access.
func (r *FuelRepository) ProcessRelayOffboarding(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	conn, done, err := r.relayDriverLock(ctx)
	if err != nil {
		return err
	}
	defer done()
	var stale bool
	if err = conn.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM relay_driver_rosters WHERE environment=$1 AND refreshed_at>now()-interval '5 minutes')`, r.relayEnvironment).Scan(&stale); err != nil {
		return err
	}
	if stale {
		if _, err = r.refreshRelayDrivers(ctx, conn); err != nil {
			return err
		}
	}
	rows, err := conn.Query(ctx, `SELECT l.relay_driver_id,d.id::text,d.termination_date::text,
 d.termination_date+10<=(now() AT TIME ZONE 'America/New_York')::date
 FROM relay_driver_rosters r CROSS JOIN LATERAL jsonb_array_elements(r.snapshot->'items') entry
 JOIN relay_driver_links l ON l.relay_environment=r.environment AND l.relay_driver_id=entry->'driver'->>'id'
 JOIN drivers d ON d.id=l.driver_id WHERE r.environment=$1 AND d.status='terminated' AND d.termination_date IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM relay_driver_operations o WHERE o.environment=r.environment AND o.relay_driver_id=l.relay_driver_id
 AND (o.state='pending' OR (o.source='termination' AND o.state='failed' AND o.created_at>now()-interval '15 minutes')))
 ORDER BY d.termination_date,l.relay_driver_id`, r.relayEnvironment)
	if err != nil {
		return err
	}
	type candidate struct {
		id, driver, date string
		due              bool
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.id, &c.driver, &c.date, &c.due); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var failures []error
	changed := false
	for _, c := range candidates {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Read live policy state even when the roster is cached, so a portal re-enable
		// is noticed on the next minute rather than waiting for roster refresh.
		d, err := r.relayDrivers.Driver(ctx, c.id)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		a, err := r.relayDrivers.Assignment(ctx, c.id)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		for _, action := range []string{"disable", "remove"} {
			if action == "disable" && (a == nil || !a.Enabled) {
				continue
			}
			if action == "remove" && !c.due {
				continue
			}
			var requestID string
			if err = conn.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&requestID); err != nil {
				return err
			}
			input := RelayDriverChange{RequestID: requestID, Action: action, RelayDriverID: c.id, Revision: relayRosterEntry(d, a).Revision}
			if err = r.changeRelayDriverLocked(ctx, conn, input, "", c.driver, c.date, false); err != nil {
				failures = append(failures, err)
				break
			}
			changed = true
			if action == "disable" && c.due {
				d, err = r.relayDrivers.Driver(ctx, c.id)
				if err != nil {
					failures = append(failures, err)
					break
				}
				a, err = r.relayDrivers.Assignment(ctx, c.id)
				if err != nil {
					failures = append(failures, err)
					break
				}
			}
		}
	}
	if changed {
		if _, err = r.refreshRelayDrivers(ctx, conn); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
