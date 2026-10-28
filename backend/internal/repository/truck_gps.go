package repository

// Read-only GPS health for the fleet directory/profile. Use the latest trusted
// provider observation, never a cache refresh or profile edit timestamp. This
// intentionally does not change the map or Status Board's provider selection.
const truckGPSJoinSQL = `
JOIN fleet_truck_read_state fleet_read ON fleet_read.id=t.id
LEFT JOIN LATERAL (SELECT fleet_read.reported_at,fleet_read.source) gps ON true
LEFT JOIN LATERAL (SELECT fleet_read.gps_alert AS alert) gps_health ON true
`

type TruckFilters struct {
	Status   string
	GPSAlert string
}
