package repository

import "context"

// Lookup reads select operational identity fields directly. The HTTP projection
// still enforces grants and the extra permission required for driver contacts.
func (r *FleetRepository) DriverOptions(ctx context.Context) ([]Driver, error) {
	rows, err := r.pool.Query(ctx, `SELECT d.id,d.full_name,d.first_name,d.active,d.status,
 driver_employment_type(d.id),d.pay_type,coalesce(NOT i.is_company,false),coalesce(i.driver_id=d.id,false),
 d.phone,d.email,CASE WHEN d.active AND team.role='team' THEN team.main_dispatcher_id ELSE d.dispatcher_id END,dp.full_name,
 a.truck_id,t.unit_number,coalesce(team.role,''),team.partner_id,team.partner_name,d.team_version,
 CASE WHEN d.active THEN team.truck_id END,CASE WHEN d.active THEN team.truck_unit END,team.start_week
 `+driverProfileJoinsSQL+` ORDER BY d.full_name,d.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Driver{}
	for rows.Next() {
		var d Driver
		var investorTruck, ownTruck bool
		if err = rows.Scan(&d.ID, &d.FullName, &d.FirstName, &d.Active, &d.Status, &d.EmploymentType, &d.PayType, &investorTruck, &ownTruck,
			&d.Phone, &d.Email, &d.DispatcherID, &d.DispatcherName, &d.TruckID, &d.TruckUnit, &d.TeamRole, &d.TeamPartnerID, &d.TeamPartnerName,
			&d.TeamVersion, &d.TeamTruckID, &d.TeamTruckUnit, &d.TeamStartWeek); err != nil {
			return nil, err
		}
		d.IsOwnerOperator = d.EmploymentType == "owner_operator"
		d.DriverType = driverBoardType(d.PayType, d.IsOwnerOperator, investorTruck, ownTruck)
		if d.FirstName == nil {
			d.FullName = formatPersonName(d.FullName)
		}
		d.DispatcherName = formatPersonNamePtr(d.DispatcherName)
		d.TruckUnit = normalizeTruckUnitPtr(d.TruckUnit)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *FleetRepository) TruckOptions(ctx context.Context) ([]Truck, error) {
	rows, err := r.pool.Query(ctx, `SELECT t.id,t.unit_number,t.active,t.status,s.operating_status,a.driver_id,d.full_name,
 t.owner_id,coalesce(od.full_name,oi.full_name),
 (SELECT CASE WHEN h.start_known THEN (h.assigned_at AT TIME ZONE 'America/New_York')::date::text END FROM truck_ownership_history h WHERE h.truck_id=t.id AND h.unassigned_at IS NULL),
 coalesce((SELECT md5(row_to_json(h)::text) FROM truck_ownership_history h WHERE h.truck_id=t.id AND h.unassigned_at IS NULL),''),
 t.is_company_owned,t.year,t.make,t.model FROM trucks t
 JOIN fleet_truck_read_state s ON s.id=t.id
 LEFT JOIN truck_driver_assignments a ON a.truck_id=t.id AND a.unassigned_at IS NULL
 LEFT JOIN drivers d ON d.id=a.driver_id JOIN investors oi ON oi.id=t.owner_id LEFT JOIN drivers od ON od.id=oi.driver_id
 ORDER BY t.unit_number,t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Truck{}
	for rows.Next() {
		var t Truck
		if err = rows.Scan(&t.ID, &t.UnitNumber, &t.Active, &t.Status, &t.OperatingStatus, &t.DriverID, &t.DriverName, &t.OwnerID, &t.OwnerName, &t.OwnerSince, &t.OwnershipVersion, &t.IsCompanyOwned, &t.Year, &t.Make, &t.Model); err != nil {
			return nil, err
		}
		t.UnitNumber = normalizeTruckUnit(t.UnitNumber)
		t.DriverName = formatPersonNamePtr(t.DriverName)
		out = append(out, t)
	}
	return out, rows.Err()
}
