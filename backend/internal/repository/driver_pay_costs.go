package repository

// Fuel follows confirmed Relay identities and merchant-local dates and includes
// diesel and DEF line items. Toll weeks follow PrePass posting dates so payroll
// reconciles with the Tolls report, while driver attribution follows the actual
// crossing date and its historical truck assignment (New York). Never substitute
// today's truck assignment or guess from a nearby load. Ambiguous/unassigned
// tolls remain outside payroll. Aggregate before joining load slots so repeated
// board entries cannot multiply costs.
var driverPayCostsSQL = `WITH weekly_fuel AS (
 SELECT coalesce(team.main_driver_id,f.driver_id) AS driver_id, SUM(i.total_amount_paid) AS total
 FROM fuel_transactions f
 JOIN fuel_transaction_items i ON i.fuel_transaction_id=f.id
 LEFT JOIN driver_teams team ON team.team_driver_id=f.driver_id AND team.start_week<=f.purchased_on AND (team.end_week IS NULL OR f.purchased_on<team.end_week)
 WHERE f.relay_environment='production' AND f.driver_id IS NOT NULL
 AND ($3::uuid[] IS NULL OR f.driver_id=ANY($3::uuid[]))
 AND i.item_kind='fuel'
 AND f.purchased_on >= $1::date
 AND f.purchased_on < $1::date+7
 GROUP BY coalesce(team.main_driver_id,f.driver_id)
), toll_days AS MATERIALIZED (
 SELECT truck_id,exit_date,sum(amount) amount FROM tolls
 WHERE (prepass_environment IS NULL OR prepass_environment='production')
 AND posting_date >= $1::date AND posting_date < $1::date+7
 AND (accounting_cutover_date() IS NULL OR exit_date >= accounting_cutover_date())
 AND ($3::uuid[] IS NULL OR truck_id IN (SELECT truck_id FROM truck_driver_assignments WHERE driver_id=ANY($3::uuid[])))
 GROUP BY truck_id,exit_date
), toll_assignments AS MATERIALIZED (
 SELECT t.truck_id,t.exit_date,t.amount,(array_agg(DISTINCT a.driver_id))[1] driver_id
 FROM toll_days t JOIN truck_driver_assignments a ON a.truck_id=t.truck_id
 AND a.assigned_at < ((t.exit_date+1)::timestamp AT TIME ZONE 'America/New_York')
 AND (a.unassigned_at IS NULL OR a.unassigned_at >= ((t.exit_date+1)::timestamp AT TIME ZONE 'America/New_York'))
 GROUP BY t.truck_id,t.exit_date,t.amount HAVING count(DISTINCT a.driver_id)=1
), weekly_tolls AS (
 SELECT driver_id,sum(amount) total FROM toll_assignments GROUP BY driver_id
)`
