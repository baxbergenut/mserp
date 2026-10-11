package repository

import "context"

type SidebarCounts struct {
	Fuel         int    `json:"fuel"`
	WeighMyTruck int    `json:"weighMyTruck"`
	Tolls        int    `json:"tolls"`
	TollsFrom    string `json:"tollsFrom"`
}

// Count only accounts in the latest complete roster, using current local status
// and confirmed links. Removed accounts retain links for history but never count.
func (r *FuelRepository) TerminatedRelayCount(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT count(*)
 FROM relay_driver_rosters r CROSS JOIN LATERAL jsonb_array_elements(r.snapshot->'items') entry
 JOIN relay_driver_links l ON l.relay_environment=r.environment AND l.relay_driver_id=entry->'driver'->>'id'
 JOIN drivers d ON d.id=l.driver_id
 WHERE r.environment=$1 AND d.status='terminated'`, r.relayEnvironment).Scan(&count)
	return count, err
}

func (r *WeighMyTruckRepository) TerminatedMembershipCount(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM weighmytruck_memberships m
 JOIN drivers d ON d.id=m.driver_id
 WHERE d.status='terminated' AND (m.enrolled OR m.state<>'confirmed')`).Scan(&count)
	return count, err
}

// Posting date matches the Tolls page's date filters. Recalculate the cutoff on
// every read, including when the New York calendar crosses a month/year boundary.
func (r *TollRepository) RecentUnlinkedCount(ctx context.Context) (int, string, error) {
	var count int
	var from string
	err := r.pool.QueryRow(ctx, `WITH cutoff AS (
 SELECT (date_trunc('month',now() AT TIME ZONE 'America/New_York')-interval '1 month')::date AS day
 ) SELECT (SELECT count(*) FROM tolls WHERE truck_id IS NULL AND posting_date>=cutoff.day),
 to_char(cutoff.day,'YYYY-MM-DD') FROM cutoff`).Scan(&count, &from)
	return count, from, err
}
