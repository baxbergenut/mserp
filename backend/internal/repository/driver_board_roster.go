package repository

// Offboarding releases live assignments immediately. The departure week's board
// retains their last recorded dispatcher and truck for gross visibility only.
// Weekly assignment edits may have closed those intervals on Monday, so include
// that boundary, but never resurrect older assignments or zero-length edits.
const driverBoardDispatcherJoinSQL = `
 LEFT JOIN LATERAL (
  SELECT h.dispatcher_id,h.unassigned_at FROM driver_dispatcher_assignments h
  WHERE NOT d.active AND h.driver_id=d.id AND h.dispatcher_id IS NOT NULL
   AND h.assigned_at < ((d.termination_date+1)::timestamp AT TIME ZONE 'America/New_York')
   AND (h.unassigned_at IS NULL OR (h.unassigned_at>h.assigned_at
    AND h.unassigned_at >= (date_trunc('week',d.termination_date)::timestamp AT TIME ZONE 'America/New_York')))
  ORDER BY h.assigned_at DESC,h.id DESC LIMIT 1
 ) departure_dispatcher ON true
`

const driverBoardDispatcherSQL = `CASE WHEN d.active THEN d.dispatcher_id ELSE departure_dispatcher.dispatcher_id END`

const driverBoardRosterPredicateSQL = `(
 (d.active AND payroll_driver_at(d.id,board_day.today)=d.id)
 OR (NOT d.active AND d.termination_date BETWEEN date_trunc('week',board_day.today)::date AND board_day.today
  AND payroll_driver_at(d.id,d.termination_date)=d.id)
)`

const driverBoardTruckJoinSQL = `
 LEFT JOIN LATERAL (
  SELECT h.truck_id FROM truck_driver_assignments h WHERE h.driver_id=d.id
   AND ((d.active AND h.unassigned_at IS NULL) OR (NOT d.active
    AND h.assigned_at < ((d.termination_date+1)::timestamp AT TIME ZONE 'America/New_York')
    AND (h.unassigned_at IS NULL OR (h.unassigned_at>h.assigned_at
     AND h.unassigned_at >= (date_trunc('week',d.termination_date)::timestamp AT TIME ZONE 'America/New_York')))))
  ORDER BY h.assigned_at DESC,h.id DESC LIMIT 1
 ) a ON true
`
