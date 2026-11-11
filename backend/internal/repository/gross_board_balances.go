package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Use the latest checkpoint as an adjusted opening for its week. The
// checkpoint cancels the net of loads already present when it was recorded.
// Later load changes still count, while carry before the checkpoint is sealed.
const grossBoardOpenings = `, checkpoints AS (
 SELECT DISTINCT ON (driver_id) driver_id,week_start,target-week_change AS opening
 FROM gross_board_rate_resets WHERE week_start <= $1::date
 ORDER BY driver_id,week_start DESC,id DESC
), openings AS (
 SELECT driver_id,coalesce(sum(change),0)::text AS opening,
 count(*) FILTER (WHERE incomplete)::int AS incomplete,bool_or(reset_this_week) AS reset_this_week
 FROM (
   SELECT driver_id,opening AS change,false AS incomplete,week_start=$1::date AS reset_this_week FROM checkpoints
   UNION ALL
   SELECT e.driver_id,e.balance_change,NOT e.deleted AND e.day_status='' AND e.balance_change IS NULL AND NOT e.duplicate
     AND (e.load_number<>'' OR e.effective_original IS NOT NULL OR e.driver_rate IS NOT NULL),false
   FROM effective e LEFT JOIN checkpoints c ON c.driver_id=e.driver_id
   WHERE e.service_date < $1::date AND (c.driver_id IS NULL OR e.service_date >= c.week_start)
 ) changes GROUP BY driver_id
) `

// ResetRateBalance returns the new adjusted opening. An empty driver ID is
// reserved for the one-time operator reset of the entire roster to zero.
func (r *GrossBoardRepository) ResetRateBalance(ctx context.Context, week time.Time, driverID, target, actorID string) (GrossBoardBalance, int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return GrossBoardBalance{}, 0, err
	}
	defer tx.Rollback(ctx)
	if err = assertGrossBoardWeekEditable(ctx, tx, week); err != nil {
		return GrossBoardBalance{}, 0, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('gross_board_rate_reset'))`); err != nil {
		return GrossBoardBalance{}, 0, err
	}
	rows, err := tx.Query(ctx, grossBoardEffective+`
 INSERT INTO gross_board_rate_resets(driver_id,week_start,target,week_change,actor_id)
 SELECT d.id,$1::date,$3::numeric,coalesce(sum(e.balance_change),0),nullif($4,'')::uuid
 FROM drivers d LEFT JOIN effective e ON e.driver_id=d.id AND e.service_date >= $1::date
 WHERE ($2<>'' AND d.id=nullif($2,'')::uuid)
    OR ($2='' AND NOT EXISTS (SELECT 1 FROM gross_board_rate_resets WHERE actor_id IS NULL))
 GROUP BY d.id
 RETURNING driver_id::text,(target-week_change)::text`, week, driverID, target, actorID)
	if err != nil {
		return GrossBoardBalance{}, 0, err
	}
	balance := GrossBoardBalance{ResetThisWeek: true}
	count := 0
	for rows.Next() {
		if err = rows.Scan(&balance.DriverID, &balance.OpeningBalance); err != nil {
			rows.Close()
			return GrossBoardBalance{}, 0, err
		}
		count++
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return GrossBoardBalance{}, 0, err
	}
	if driverID != "" && count == 0 {
		return GrossBoardBalance{}, 0, pgx.ErrNoRows
	}
	if count != 0 {
		if _, err = tx.Exec(ctx, `SELECT pg_notify('mserp_gross_board','')`); err != nil {
			return GrossBoardBalance{}, 0, err
		}
	}
	return balance, count, tx.Commit(ctx)
}
