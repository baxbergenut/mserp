-- Read projection only: writers and their lock/status functions stay authoritative.
CREATE INDEX trucks_normalized_five_eld_vin ON trucks ((upper(regexp_replace(vin,'[^A-Za-z0-9]','','g'))));
CREATE INDEX trucks_normalized_samsara_vin ON trucks ((upper(btrim(vin))));
CREATE INDEX loads_coverage_unit ON loads ((upper(regexp_replace(btrim(truck_unit),'[[:space:]]+',' ','g'))));
CREATE INDEX loads_coverage_team_name ON loads ((lower(regexp_replace(btrim(team_driver_name),'[[:space:]]+',' ','g'))));

CREATE VIEW fleet_truck_read_state WITH (security_invoker = true) AS
WITH identities AS MATERIALIZED (
 SELECT id,status,status_reason,upper(regexp_replace(vin,'[^A-Za-z0-9]','','g')) five_vin,upper(btrim(vin)) samsara_vin,
 count(*) OVER (PARTITION BY upper(regexp_replace(vin,'[^A-Za-z0-9]','','g'))) five_count,
 count(*) OVER (PARTITION BY upper(btrim(vin))) samsara_count FROM trucks
), driver_trucks AS MATERIALIZED (
 SELECT fleet_status_driver_truck(id) truck_id,availability_status FROM drivers WHERE active
), operators AS (
 SELECT truck_id,bool_or(availability_status NOT IN ('home','vacation')) needs_gps FROM driver_trucks GROUP BY truck_id
), holds AS (
 SELECT truck_id,bool_or(dispatch_status='out_of_service') oos,bool_or(dispatch_status='shop') shop,
 string_agg('Work order: '||coalesce(nullif(btrim(notes),''),'Existing restriction; original reason not recorded.'),E'\n' ORDER BY id) reason
 FROM maintenance_records WHERE dispatch_status<>'' GROUP BY truck_id
), states AS (
 SELECT t.*,coalesce(o.needs_gps,false) needs_gps,
 CASE WHEN t.status='terminated' THEN 'terminated'
 WHEN t.status='out_of_service' OR h.oos THEN 'out_of_service'
 WHEN t.status='maintenance' OR h.shop THEN 'shop'
 WHEN o.truck_id IS NOT NULL THEN 'in_use' ELSE 'ready' END operating_status,
 concat_ws(E'\n',nullif(t.status_reason,''),h.reason) reason
 FROM identities t LEFT JOIN operators o ON o.truck_id=t.id LEFT JOIN holds h ON h.truck_id=t.id
)
SELECT t.id,t.operating_status,t.reason,gps.reported_at,gps.source,
 CASE WHEN t.operating_status<>'in_use' OR NOT t.needs_gps THEN ''
 WHEN gps.reported_at IS NULL THEN 'missing'
 WHEN gps.reported_at<now()-interval '168 hours' THEN 'overdue' ELSE '' END gps_alert
FROM states t LEFT JOIN LATERAL (
 SELECT reported_at,source FROM (
  SELECT f.reported_at,'five_eld'::text source FROM five_eld_locations f
  WHERE t.five_count=1 AND f.vin=t.five_vin AND f.reported_at<=now()
   AND NOT EXISTS(SELECT 1 FROM five_eld_sync_state s WHERE f.vin=ANY(s.ambiguous_vins))
  UNION ALL
  SELECT s.gps_at,'samsara'::text FROM samsara_telemetry s
  WHERE t.samsara_count=1 AND s.vin=t.samsara_vin AND s.available AND s.gps_at<=now()
   AND s.latitude IS NOT NULL AND s.longitude IS NOT NULL
   AND NOT EXISTS(SELECT 1 FROM samsara_sync_state c WHERE s.vin=ANY(c.ambiguous_vins))
 ) reports ORDER BY reported_at DESC,source LIMIT 1
) gps ON true;

DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER VIEW fleet_truck_read_state OWNER TO mserp_app;
 END IF;
END $$;
