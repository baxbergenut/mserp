package repository

// Read-only GPS health for the fleet directory/profile. Use the latest trusted
// provider observation, never a cache refresh or profile edit timestamp. This
// intentionally does not change the map or Status Board's provider selection.
const truckGPSJoinSQL = `
LEFT JOIN LATERAL (
 SELECT reported_at,source FROM (
  SELECT f.reported_at,'five_eld'::text AS source
  FROM five_eld_locations f
  WHERE f.vin=upper(regexp_replace(t.vin,'[^A-Za-z0-9]','','g'))
   AND f.reported_at<=now()
   AND NOT EXISTS(SELECT 1 FROM five_eld_sync_state s WHERE f.vin=ANY(s.ambiguous_vins))
   AND (SELECT count(*) FROM trucks x WHERE upper(regexp_replace(x.vin,'[^A-Za-z0-9]','','g'))=f.vin)=1
  UNION ALL
  SELECT s.gps_at,'samsara'::text FROM samsara_telemetry s
  WHERE s.available AND s.vin=upper(btrim(t.vin)) AND s.gps_at<=now()
   AND s.latitude IS NOT NULL AND s.longitude IS NOT NULL
   AND NOT EXISTS(SELECT 1 FROM samsara_sync_state c WHERE s.vin=ANY(c.ambiguous_vins))
   AND (SELECT count(*) FROM trucks x WHERE upper(btrim(x.vin))=s.vin)=1
 ) reports ORDER BY reported_at DESC,source LIMIT 1
) gps ON true
LEFT JOIN LATERAL (
 SELECT CASE WHEN fleet_truck_operating_status(t.id)<>'in_use' THEN ''
  WHEN gps.reported_at IS NULL THEN 'missing'
  WHEN gps.reported_at<now()-interval '168 hours' THEN 'overdue'
  ELSE '' END AS alert
) gps_health ON true
`

type TruckFilters struct {
	Status   string
	GPSAlert string
}
