package repository

// Fuel follows confirmed Relay identities and merchant-local dates. Toll weeks
// follow crossing dates, with historical assignments on that date (New York,
// matching financial reporting). Never substitute today's truck assignment or
// guess from a nearby load. Ambiguous/unassigned tolls remain outside payroll.
// Aggregate before joining load slots so repeated board entries cannot multiply costs.
var driverPayCostsSQL = `WITH weekly_fuel AS (
 SELECT f.driver_id, SUM(i.total_amount_paid) AS total
 FROM fuel_transactions f
 JOIN fuel_transaction_items i ON i.fuel_transaction_id=f.id
 WHERE f.relay_environment='production' AND f.driver_id IS NOT NULL
 AND i.item_kind='fuel' AND lower(i.category)<>'def'
 AND (f.purchased_at AT TIME ZONE ` + fuelTimezoneExpression("f.timezone") + `)::date >= $1::date
 AND (f.purchased_at AT TIME ZONE ` + fuelTimezoneExpression("f.timezone") + `)::date < $1::date+7
 GROUP BY f.driver_id
), weekly_tolls AS (
 SELECT assignment.driver_id, SUM(t.amount) AS total
 FROM tolls t
 JOIN LATERAL (
   SELECT (array_agg(DISTINCT a.driver_id))[1] AS driver_id
   FROM truck_driver_assignments a
   WHERE a.truck_id=t.truck_id
   AND (a.assigned_at AT TIME ZONE 'America/New_York')::date <= t.exit_date
   AND (a.unassigned_at IS NULL OR t.exit_date < (a.unassigned_at AT TIME ZONE 'America/New_York')::date)
   HAVING count(DISTINCT a.driver_id)=1
 ) assignment ON true
 WHERE (t.prepass_environment IS NULL OR t.prepass_environment='production')
 AND t.exit_date >= $1::date AND t.exit_date < $1::date+7
 GROUP BY assignment.driver_id
)`
