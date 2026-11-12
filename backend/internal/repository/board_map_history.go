package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// MapTruckIdentity uses the same active assignment and Five ELD position as the board.
func (r *DriverBoardRepository) MapTruckIdentity(ctx context.Context, driverID string) (truckID, vin string, err error) {
	err = r.pool.QueryRow(ctx, `
 SELECT t.id::text, upper(regexp_replace(t.vin,'[^A-Za-z0-9]','','g'))
 FROM drivers d JOIN truck_driver_assignments a ON a.driver_id=d.id AND a.unassigned_at IS NULL
 JOIN trucks t ON t.id=a.truck_id
 `+statusBoardELDLocationJoinSQL+`
 WHERE d.id=$1 AND d.active AND payroll_driver_at(d.id,(now() AT TIME ZONE 'America/New_York')::date)=d.id
 AND el.reported_at<=now()
 AND NOT EXISTS (
   SELECT 1 FROM trucks other WHERE other.id<>t.id
   AND upper(regexp_replace(other.vin,'[^A-Za-z0-9]','','g'))=upper(regexp_replace(t.vin,'[^A-Za-z0-9]','','g'))
 )`, driverID).Scan(&truckID, &vin)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return
}
