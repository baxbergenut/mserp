BEGIN;

-- Imported assignment labels remain on loads for source provenance only.
-- Read surfaces use native load plans and dated fleet history instead.
CREATE VIEW clustie_load_operators WITH (security_invoker=true) AS
WITH entries AS (
 SELECT driver_id,service_date,0 slot,load_number,load_record_id,load_trip_id
 FROM gross_board_entries WHERE day_status='' AND btrim(load_number)<>''
 UNION ALL
 SELECT driver_id,service_date,slot,load_number,load_record_id,load_trip_id
 FROM gross_board_extra_entries WHERE NOT deleted AND day_status='' AND btrim(load_number)<>''
), numbers AS (
 SELECT lower(btrim(load_id)) number,min(id) id FROM loads
 WHERE lower(btrim(load_id)) IN (SELECT lower(btrim(load_number)) FROM entries WHERE load_record_id IS NULL)
 GROUP BY lower(btrim(load_id)) HAVING count(*)=1
), planned AS (
 SELECT coalesce(e.load_record_id,n.id) id,e.driver_id,e.service_date,e.slot,e.load_trip_id
 FROM entries e LEFT JOIN numbers n ON n.number=lower(btrim(e.load_number))
 UNION ALL
 SELECT l.id,b.driver_id,(now() AT TIME ZONE 'America/New_York')::date,0,NULL::integer
 FROM driver_board b JOIN loads l ON lower(btrim(l.load_id))=lower(btrim(b.current_load))
 WHERE btrim(b.current_load)<>''
 AND (SELECT count(*) FROM loads other WHERE lower(btrim(other.load_id))=lower(btrim(b.current_load)))=1
 AND NOT EXISTS(SELECT 1 FROM entries e WHERE e.load_record_id=l.id OR
  (e.load_record_id IS NULL AND lower(btrim(e.load_number))=lower(btrim(l.load_id))))
), first_occurrences AS (
 SELECT DISTINCT ON (id,driver_id,load_trip_id) * FROM planned WHERE id IS NOT NULL
 ORDER BY id,driver_id,load_trip_id,service_date,slot
), native AS (
 SELECT p.id,p.driver_id,p.service_date,d.full_name driver_name,a.truck_id,t.unit_number,dp.dispatcher_id,
 dp.dispatcher_name,teammate.full_name team_driver_name
 FROM first_occurrences p JOIN drivers d ON d.id=p.driver_id
 LEFT JOIN LATERAL (
  SELECT (array_agg(DISTINCT h.truck_id))[1] truck_id FROM truck_driver_assignments h
  WHERE h.driver_id=p.driver_id
  AND h.assigned_at<((p.service_date+1)::timestamp AT TIME ZONE 'America/New_York')
  AND (h.unassigned_at IS NULL OR h.unassigned_at>=((p.service_date+1)::timestamp AT TIME ZONE 'America/New_York'))
  HAVING count(DISTINCT h.truck_id)=1
 ) a ON true LEFT JOIN trucks t ON t.id=a.truck_id
 LEFT JOIN LATERAL (
  SELECT h.dispatcher_id,h.dispatcher_name FROM driver_dispatcher_assignments h WHERE h.driver_id=p.driver_id
  AND (NOT h.start_known OR h.assigned_at<((p.service_date+1)::timestamp AT TIME ZONE 'America/New_York'))
  AND (h.unassigned_at IS NULL OR h.unassigned_at>=((p.service_date+1)::timestamp AT TIME ZONE 'America/New_York'))
  ORDER BY h.assigned_at DESC LIMIT 1
 ) dp ON true
 LEFT JOIN driver_teams team ON team.main_driver_id=p.driver_id AND team.start_week<=p.service_date
 AND (team.end_week IS NULL OR p.service_date<team.end_week)
 LEFT JOIN drivers teammate ON teammate.id=team.team_driver_id
)
SELECT * FROM native;

CREATE VIEW clustie_loads WITH (security_invoker=true) AS
WITH assignments AS (
 SELECT id,
 CASE WHEN count(DISTINCT driver_id)=1 THEN (array_agg(driver_id))[1] END driver_id,
 CASE WHEN count(dispatcher_id)=count(*) AND count(DISTINCT dispatcher_id)=1 THEN (array_agg(dispatcher_id))[1] END dispatcher_id,
 CASE WHEN count(truck_id)=count(*) AND count(DISTINCT truck_id)=1 THEN (array_agg(truck_id))[1] END truck_id,
 string_agg(DISTINCT driver_name,', ' ORDER BY driver_name) driver_name,
 string_agg(DISTINCT dispatcher_name,', ' ORDER BY dispatcher_name) dispatcher_name,
 string_agg(DISTINCT unit_number,', ' ORDER BY unit_number) truck_unit,
 string_agg(DISTINCT team_driver_name,', ' ORDER BY team_driver_name) team_driver_name
 FROM clustie_load_operators GROUP BY id
)
SELECT l.id,l.load_id,a.driver_id,a.dispatcher_id,l.shipment_id,l.status,
 l.load_pay,l.total_other_pay,l.total_pay,l.total_miles,l.per_mile_revenue,
 a.dispatcher_name,a.driver_name,a.team_driver_name,a.truck_unit,l.customer_name,
 l.pickup_time,l.delivery_time,l.pickup_appointment_time,l.delivery_appointment_time,
 l.created_datetime,l.synced_at,l.raw_payload,a.truck_id,l.split_expected
FROM loads l LEFT JOIN assignments a ON a.id=l.id;

DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER VIEW clustie_load_operators OWNER TO mserp_app;
  ALTER VIEW clustie_loads OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
