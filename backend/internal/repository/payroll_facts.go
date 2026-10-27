package repository

// Same end-of-week boundaries as weeklyAssignmentJoins and
// driver_employment_type(day=Sunday), evaluated as sets once per report. Keep
// classification's owner comparison against the original driver (including teams).
const payDriverFactsSQL = `, day_owners AS MATERIALIZED (
 SELECT DISTINCT ON (truck_id) truck_id,owner_id FROM truck_ownership_history
 WHERE (NOT start_known OR assigned_at < (($1::date+7)::timestamp AT TIME ZONE 'America/New_York'))
 AND (unassigned_at IS NULL OR unassigned_at >= (($1::date+7)::timestamp AT TIME ZONE 'America/New_York'))
 ORDER BY truck_id,assigned_at DESC
), day_employment AS MATERIALIZED (
 SELECT DISTINCT ON (a.driver_id) a.driver_id,i.is_company,i.driver_id AS owner_driver
 FROM truck_driver_assignments a JOIN trucks t ON t.id=a.truck_id
 JOIN day_owners h ON h.truck_id=t.id JOIN investors i ON i.id=h.owner_id
 WHERE a.assigned_at < (($1::date+7)::timestamp AT TIME ZONE 'America/New_York')
 AND (a.unassigned_at IS NULL OR a.unassigned_at >= (($1::date+7)::timestamp AT TIME ZONE 'America/New_York'))
 ORDER BY a.driver_id,a.assigned_at DESC
), week_trucks AS MATERIALIZED (
 SELECT DISTINCT ON (driver_id) driver_id,truck_id FROM truck_driver_assignments
 WHERE assigned_at < (($1::date+7)::timestamp AT TIME ZONE 'America/New_York')
 AND (unassigned_at IS NULL OR unassigned_at >= (($1::date+7)::timestamp AT TIME ZONE 'America/New_York'))
 ORDER BY driver_id,assigned_at DESC
), week_dispatchers AS MATERIALIZED (
 SELECT DISTINCT ON (driver_id) driver_id,dispatcher_id,dispatcher_name FROM driver_dispatcher_assignments
 WHERE (NOT start_known OR assigned_at < (($1::date+7)::timestamp AT TIME ZONE 'America/New_York'))
 AND (unassigned_at IS NULL OR unassigned_at >= (($1::date+7)::timestamp AT TIME ZONE 'America/New_York'))
 ORDER BY driver_id,assigned_at DESC
), pay_drivers AS MATERIALIZED (
 SELECT d.*, CASE WHEN employment.driver_id IS NULL THEN 'unassigned'
 WHEN employment.is_company THEN 'company' WHEN employment.owner_driver=d.id THEN 'owner_operator' ELSE 'investor_driver' END pay_employment_type,
 coalesce(t.id::text,'') pay_truck_id,coalesce(t.unit_number,'') pay_truck_unit,
 coalesce(dp.id::text,'') pay_dispatcher_id,coalesce(dp.full_name,h.dispatcher_name,'Unassigned') pay_dispatcher_name
 FROM drivers d LEFT JOIN driver_teams team ON team.team_driver_id=d.id AND team.start_week<=$1::date+6 AND (team.end_week IS NULL OR $1::date+6<team.end_week)
 LEFT JOIN day_employment employment ON employment.driver_id=coalesce(team.main_driver_id,d.id)
 LEFT JOIN week_trucks a ON a.driver_id=d.id LEFT JOIN trucks t ON t.id=a.truck_id
 LEFT JOIN week_dispatchers h ON h.driver_id=d.id LEFT JOIN dispatchers dp ON dp.id=h.dispatcher_id
 WHERE (NULLIF($2,'')::uuid IS NULL OR d.id=NULLIF($2,'')::uuid) AND ($3::uuid[] IS NULL OR d.id=ANY($3::uuid[]))
)`

// Eligibility is per driver, not per load slot. In particular escrow capacity
// must not be recomputed for every load row during a cold weekly calculation.
var payDriverEligibilitySQL = `, eligible_pay_drivers AS MATERIALIZED (
 SELECT d.*,
 (d.active AND (` + weeklyDriverStartedSQL + ` OR EXISTS(SELECT 1 FROM driver_teams dt WHERE (dt.main_driver_id=d.id OR dt.team_driver_id=d.id) AND dt.start_week<=$1::date AND (dt.end_week IS NULL OR $1::date<dt.end_week)))) pay_enabled,
 EXISTS(SELECT 1 FROM driver_escrow_releases r JOIN driver_escrows x ON x.id=r.escrow_id WHERE x.driver_id=d.id AND r.week_start=$1::date AND NOT r.cancelled) pay_release,
 EXISTS(SELECT 1 FROM expenses x WHERE x.driver_id=d.id AND lower(btrim(x.payment_type))='reimburse' AND x.expense_date < $1::date+10 AND x.amount>0) pay_reimburse,
 EXISTS(SELECT 1 FROM weighmytruck_transactions wt WHERE payroll_driver_at(wt.driver_id,wt.transaction_date)=d.id AND wt.transaction_date >= $1::date AND wt.transaction_date < $1::date+7) pay_scale,
 EXISTS(SELECT 1 FROM driver_charge_schedules cs WHERE cs.driver_id=d.id AND cs.start_week<=$1::date) pay_charge,
 EXISTS(SELECT 1 FROM expenses x WHERE x.charge_driver_id=d.id AND NOT x.driver_settled AND x.expense_date<$1::date+10) pay_expense,
 EXISTS(SELECT 1 FROM pay_escrow_capacity x WHERE x.driver_id=d.id AND (x.available>0 OR EXISTS(SELECT 1 FROM driver_escrow_payments p WHERE p.escrow_id=x.id AND p.week_start=$1::date))) pay_escrow
 FROM pay_drivers d
)`
