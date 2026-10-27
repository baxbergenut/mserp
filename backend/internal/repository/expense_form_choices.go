package repository

import "context"

type ExpenseDriverChoice struct {
	ID       string  `json:"id"`
	FullName string  `json:"fullName"`
	TruckID  *string `json:"truckId"`
}
type ExpenseTruckChoice struct {
	ID             string  `json:"id"`
	UnitNumber     string  `json:"unitNumber"`
	DriverID       *string `json:"driverId"`
	OwnerID        *string `json:"ownerId"`
	IsCompanyOwned bool    `json:"isCompanyOwned"`
}
type ExpenseOwnerChoice struct {
	ID        string  `json:"id"`
	FullName  string  `json:"fullName"`
	DriverID  *string `json:"driverId"`
	IsCompany bool    `json:"isCompany"`
}
type ExpenseFormChoices struct {
	Options ExpenseFilterOptions  `json:"options"`
	Drivers []ExpenseDriverChoice `json:"drivers"`
	Trucks  []ExpenseTruckChoice  `json:"trucks"`
	Owners  []ExpenseOwnerChoice  `json:"owners"`
}

// Entry needs identities and current assignments, not profile status, GPS,
// compensation, documents, expense totals or directory filter scans.
func (r *ExpenseRepository) ExpenseFormChoices(ctx context.Context, visible, accessible []string) (ExpenseFormChoices, error) {
	result := ExpenseFormChoices{Drivers: []ExpenseDriverChoice{}, Trucks: []ExpenseTruckChoice{}, Owners: []ExpenseOwnerChoice{}}
	options := &result.Options
	options.Settings = []ExpenseSetting{}
	options.Categories = []ExpenseFilterCategory{}
	options.PaymentTypes = []string{}
	options.ExpenseTypes = []string{}
	options.CoveredBy = []string{"Company", "Driver", "Truck Owner"}
	options.Filters = ExpenseFilterValues{Drivers: []string{}, Trucks: []string{}, PaymentTypes: []string{}, PaidBy: []string{}, CoveredBy: []string{}}
	settings, err := r.ListExpenseSettings(ctx)
	if err != nil {
		return result, err
	}
	allowed, shown := map[string]bool{}, map[string]bool{}
	for _, id := range accessible {
		allowed[id] = true
	}
	for _, id := range visible {
		shown[id] = true
	}
	for _, item := range settings {
		if item.Kind == "category" && allowed[item.ID] || item.Kind == "name" && item.CategoryID != nil && allowed[*item.CategoryID] || item.Kind == "payment_method" || item.Kind == "payer" {
			options.Settings = append(options.Settings, item)
		}
		if item.Kind == "category" && shown[item.ID] {
			options.Categories = append(options.Categories, ExpenseFilterCategory{ID: item.ID, Name: item.Name})
		}
		if item.Kind == "payment_method" && item.Active {
			options.PaymentTypes = append(options.PaymentTypes, item.Name)
		}
	}
	options.Applicants, err = r.ListExpenseApplicants(ctx)
	if err != nil {
		return result, err
	}
	if err = r.pool.QueryRow(ctx, `SELECT coalesce(array_agg(username ORDER BY lower(username)),'{}') FROM app_users WHERE active`).Scan(&options.PaidBy); err != nil {
		return result, err
	}
	category, err := NewMaintenanceRepository(r.pool).CategoryID(ctx)
	if err != nil {
		return result, err
	}
	if allowed[category] {
		options.WorkOrdersCategoryID = category
	}
	rows, err := r.pool.Query(ctx, `SELECT d.id,d.full_name,a.truck_id FROM drivers d LEFT JOIN truck_driver_assignments a ON a.driver_id=d.id AND a.unassigned_at IS NULL ORDER BY lower(d.full_name),d.id`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var v ExpenseDriverChoice
		if err = rows.Scan(&v.ID, &v.FullName, &v.TruckID); err != nil {
			rows.Close()
			return result, err
		}
		v.FullName = formatPersonName(v.FullName)
		result.Drivers = append(result.Drivers, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	rows, err = r.pool.Query(ctx, `SELECT t.id,t.unit_number,a.driver_id,t.owner_id,t.is_company_owned FROM trucks t LEFT JOIN truck_driver_assignments a ON a.truck_id=t.id AND a.unassigned_at IS NULL ORDER BY t.unit_number,t.id`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var v ExpenseTruckChoice
		if err = rows.Scan(&v.ID, &v.UnitNumber, &v.DriverID, &v.OwnerID, &v.IsCompanyOwned); err != nil {
			rows.Close()
			return result, err
		}
		v.UnitNumber = normalizeTruckUnit(v.UnitNumber)
		result.Trucks = append(result.Trucks, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	rows, err = r.pool.Query(ctx, `SELECT i.id,coalesce(d.full_name,i.full_name),i.driver_id,i.is_company FROM investors i LEFT JOIN drivers d ON d.id=i.driver_id ORDER BY lower(coalesce(d.full_name,i.full_name)),i.id`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var v ExpenseOwnerChoice
		if err = rows.Scan(&v.ID, &v.FullName, &v.DriverID, &v.IsCompany); err != nil {
			return result, err
		}
		result.Owners = append(result.Owners, v)
	}
	return result, rows.Err()
}
