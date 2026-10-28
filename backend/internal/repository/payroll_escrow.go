package repository

// Equivalent to escrow_collection_available, evaluated for the relevant escrow
// ledgers together. A release changes capacity only AFTER its week; a payment
// checkpoint in that same week must be evaluated before the release. The target
// week's existing collection is excluded while checking replacement capacity.
const payrollEscrowCapacityCTE = `pay_escrows AS MATERIALIZED (
 SELECT * FROM driver_escrows WHERE accounting_live_source('escrow',id,start_date) AND start_date<$1::date+7 AND ($3::uuid[] IS NULL OR driver_id=ANY($3::uuid[]))
), pay_escrow_events AS MATERIALIZED (
 SELECT e.id,p.week_start,0 phase,-sum(CASE WHEN p.week_start<>$1::date THEN p.amount ELSE 0 END) delta,p.week_start>=$1::date checkpoint
 FROM pay_escrows e JOIN driver_escrow_payments p ON p.escrow_id=e.id GROUP BY e.id,p.week_start
 UNION ALL SELECT e.id,r.week_start,1,sum(r.amount),false FROM pay_escrows e JOIN driver_escrow_releases r ON r.escrow_id=e.id AND NOT r.cancelled GROUP BY e.id,r.week_start
 UNION ALL SELECT id,$1::date,0,0::numeric,true FROM pay_escrows
), pay_escrow_steps AS (
 SELECT id,week_start,phase,sum(delta) delta,bool_or(checkpoint) checkpoint FROM pay_escrow_events GROUP BY id,week_start,phase
), pay_escrow_running AS (
 SELECT id,week_start,checkpoint,sum(delta) OVER(PARTITION BY id ORDER BY week_start,phase ROWS UNBOUNDED PRECEDING) delta FROM pay_escrow_steps
), pay_escrow_capacity AS MATERIALIZED (
 SELECT e.id,e.driver_id,min(e.amount-e.opening_paid+r.delta) available FROM pay_escrows e JOIN pay_escrow_running r ON r.id=e.id
 WHERE r.checkpoint AND r.week_start>=$1::date GROUP BY e.id,e.driver_id
), pay_escrow_opening AS (
 SELECT id,sum(delta) delta FROM pay_escrow_events WHERE week_start<$1::date GROUP BY id
)`
