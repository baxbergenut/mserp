package repository

import "slices"

type Permission struct {
	Key       string   `json:"key"`
	Label     string   `json:"label"`
	Group     string   `json:"group"`
	Action    string   `json:"action"`
	Requires  []string `json:"requires"`
	Sensitive bool     `json:"sensitive"`
}

// Code-owned capabilities. Unknown keys and incomplete prerequisites never grant access.
var Permissions = []Permission{
	{Key: "pti.read", Label: "View Weekly PTI", Group: "Weekly PTI", Action: "read", Requires: []string{"drivers.read", "trucks.read"}},
	{Key: "pti.edit", Label: "Update Weekly PTI", Group: "Weekly PTI", Action: "edit", Requires: []string{"pti.read"}},
	{Key: "service_programs.manage", Label: "Manage Service programs", Group: "Service programs", Action: "edit", Requires: []string{}},
	{Key: "inventory.read", Label: "View Inventory", Group: "Inventory", Action: "read", Requires: []string{}},
	{Key: "inventory.manage", Label: "Manage Inventory", Group: "Inventory", Action: "edit", Requires: []string{"inventory.read"}},
	{Key: "inventory.import", Label: "Review and Import Inventory (Administrator)", Group: "Inventory", Action: "create", Requires: []string{"inventory.read", "inventory.manage", "trailers.manage", "trucks.read", "tolls.read"}, Sensitive: true},
	{Key: "trailers.read", Label: "View Stored Trailers", Group: "Trailers", Action: "read", Requires: []string{}},
	{Key: "trailers.manage", Label: "Manage Stored Trailers", Group: "Trailers", Action: "edit", Requires: []string{"trailers.read"}},
	{Key: "drivers.read", Label: "View Drivers", Group: "Drivers", Action: "read", Requires: []string{}, Sensitive: false},
	{Key: "drivers.create", Label: "Add Drivers", Group: "Drivers", Action: "create", Requires: []string{"drivers.read"}, Sensitive: false},
	{Key: "drivers.edit", Label: "Edit Drivers", Group: "Drivers", Action: "edit", Requires: []string{"drivers.read"}, Sensitive: false},
	{Key: "drivers.delete", Label: "Delete Drivers", Group: "Drivers", Action: "delete", Requires: []string{"drivers.read"}, Sensitive: false},
	{Key: "trucks.read", Label: "View Trucks", Group: "Trucks", Action: "read", Requires: []string{}, Sensitive: false},
	{Key: "trucks.create", Label: "Add Trucks", Group: "Trucks", Action: "create", Requires: []string{"trucks.read"}, Sensitive: false},
	{Key: "trucks.edit", Label: "Edit Trucks", Group: "Trucks", Action: "edit", Requires: []string{"trucks.read"}, Sensitive: false},
	{Key: "trucks.delete", Label: "Delete Trucks", Group: "Trucks", Action: "delete", Requires: []string{"trucks.read"}, Sensitive: false},
	{Key: "dispatchers.read", Label: "View Dispatchers", Group: "Dispatchers", Action: "read", Requires: []string{}, Sensitive: false},
	{Key: "dispatchers.create", Label: "Add Dispatchers", Group: "Dispatchers", Action: "create", Requires: []string{"dispatchers.read"}, Sensitive: false},
	{Key: "dispatchers.edit", Label: "Edit Dispatchers", Group: "Dispatchers", Action: "edit", Requires: []string{"dispatchers.read"}, Sensitive: false},
	{Key: "dispatchers.delete", Label: "Delete Dispatchers", Group: "Dispatchers", Action: "delete", Requires: []string{"dispatchers.read"}, Sensitive: false},
	{Key: "updaters.read", Label: "View Updaters", Group: "Updaters", Action: "read", Requires: []string{}, Sensitive: false},
	{Key: "updaters.create", Label: "Add Updaters", Group: "Updaters", Action: "create", Requires: []string{"updaters.read"}, Sensitive: false},
	{Key: "updaters.edit", Label: "Edit Updaters", Group: "Updaters", Action: "edit", Requires: []string{"updaters.read"}, Sensitive: false},
	{Key: "updaters.delete", Label: "Delete Updaters", Group: "Updaters", Action: "delete", Requires: []string{"updaters.read"}, Sensitive: false},
	{Key: "investors.read", Label: "View Investors", Group: "Investors", Action: "read", Requires: []string{}, Sensitive: false},
	{Key: "investors.create", Label: "Add Investors", Group: "Investors", Action: "create", Requires: []string{"investors.read"}, Sensitive: false},
	{Key: "investors.edit", Label: "Edit Investors", Group: "Investors", Action: "edit", Requires: []string{"investors.read"}, Sensitive: false},
	{Key: "drivers.private.read", Label: "View Driver private details", Group: "Driver private details", Action: "read", Requires: []string{"drivers.read"}, Sensitive: true},
	{Key: "drivers.private.edit", Label: "Edit Driver private details", Group: "Driver private details", Action: "edit", Requires: []string{"drivers.read", "drivers.private.read"}, Sensitive: true},
	{Key: "drivers.documents.read", Label: "View Driver documents", Group: "Driver documents", Action: "read", Requires: []string{"drivers.read"}, Sensitive: true},
	{Key: "drivers.documents.edit", Label: "Edit Driver documents", Group: "Driver documents", Action: "edit", Requires: []string{"drivers.read", "drivers.documents.read"}, Sensitive: true},
	{Key: "trucks.documents.read", Label: "View Truck documents", Group: "Truck documents", Action: "read", Requires: []string{"trucks.read"}, Sensitive: true},
	{Key: "trucks.documents.edit", Label: "Edit Truck documents", Group: "Truck documents", Action: "edit", Requires: []string{"trucks.read", "trucks.documents.read"}, Sensitive: true},
	{Key: "driver_compensation.read", Label: "View Driver compensation", Group: "Driver compensation", Action: "read", Requires: []string{}, Sensitive: true},
	{Key: "driver_compensation.edit", Label: "Edit Driver compensation", Group: "Driver compensation", Action: "edit", Requires: []string{"driver_compensation.read"}, Sensitive: true},
	{Key: "dispatcher_compensation.read", Label: "View Dispatcher compensation", Group: "Dispatcher compensation", Action: "read", Requires: []string{}, Sensitive: true},
	{Key: "dispatcher_compensation.edit", Label: "Edit Dispatcher compensation", Group: "Dispatcher compensation", Action: "edit", Requires: []string{"dispatcher_compensation.read"}, Sensitive: true},
	{Key: "investor_terms.read", Label: "View Investor agreements", Group: "Investor agreements", Action: "read", Requires: []string{}, Sensitive: true},
	{Key: "investor_terms.edit", Label: "Edit Investor agreements", Group: "Investor agreements", Action: "edit", Requires: []string{"investor_terms.read"}, Sensitive: true},
	{Key: "driver_pay.read", Label: "View Driver Pay", Group: "Driver Pay", Action: "read", Requires: []string{}, Sensitive: true},
	{Key: "driver_pay.edit", Label: "Edit Driver Pay", Group: "Driver Pay", Action: "edit", Requires: []string{"driver_pay.read"}, Sensitive: true},
	{Key: "driver_pay.finalize", Label: "Finalize Driver Pay", Group: "Driver Pay", Action: "finalize", Requires: []string{"driver_pay.read"}, Sensitive: true},
	{Key: "driver_pay.reopen", Label: "Reopen Driver Pay", Group: "Driver Pay", Action: "reopen", Requires: []string{"driver_pay.read"}, Sensitive: true},
	{Key: "investor_pay.read", Label: "View Investor Pay", Group: "Investor Pay", Action: "read", Requires: []string{}, Sensitive: true},
	{Key: "investor_pay.edit", Label: "Edit Investor Pay", Group: "Investor Pay", Action: "edit", Requires: []string{"investor_pay.read"}, Sensitive: true},
	{Key: "investor_pay.finalize", Label: "Finalize Investor Pay", Group: "Investor Pay", Action: "finalize", Requires: []string{"investor_pay.read"}, Sensitive: true},
	{Key: "investor_pay.reopen", Label: "Reopen Investor Pay", Group: "Investor Pay", Action: "reopen", Requires: []string{"investor_pay.read"}, Sensitive: true},
	{Key: "dispatcher_pay.read", Label: "View Dispatcher Pay", Group: "Dispatcher Pay", Action: "read", Requires: []string{}, Sensitive: true},
	{Key: "driver_board.read", Label: "View Status Board", Group: "Status Board", Action: "read", Requires: []string{}, Sensitive: false},
	{Key: "driver_board.write", Label: "Edit Status Board", Group: "Status Board", Action: "write", Requires: []string{"driver_board.read"}, Sensitive: false},
	{Key: "board.read", Label: "View Gross Board", Group: "Gross Board", Action: "read", Requires: []string{}, Sensitive: false},
	{Key: "board.write", Label: "Edit Gross Board", Group: "Gross Board", Action: "write", Requires: []string{"board.read"}, Sensitive: false},
	{Key: "loads.read", Label: "View Loads", Group: "Loads", Action: "read", Requires: []string{}, Sensitive: false},
	{Key: "loads.sync", Label: "Sync Loads", Group: "Loads", Action: "sync", Requires: []string{"loads.read"}, Sensitive: false},
	{Key: "fuel.read", Label: "View Fuel", Group: "Fuel", Action: "read", Requires: []string{}, Sensitive: false},
	{Key: "fuel.sync", Label: "Sync Fuel", Group: "Fuel", Action: "sync", Requires: []string{"fuel.read"}, Sensitive: false},
	{Key: "fuel.drivers.sync", Label: "Refresh Relay roster", Group: "Relay drivers", Action: "sync", Requires: []string{"fuel.read"}},
	{Key: "fuel.drivers.recover", Label: "Review uncertain Relay changes", Group: "Relay drivers", Action: "recover", Requires: []string{"fuel.read"}},
	{Key: "fuel.drivers.create", Label: "Add Relay drivers", Group: "Relay drivers", Action: "create", Requires: []string{"fuel.read"}, Sensitive: false},
	{Key: "fuel.drivers.edit", Label: "Edit Relay drivers", Group: "Relay drivers", Action: "edit", Requires: []string{"fuel.read"}, Sensitive: false},
	{Key: "fuel.drivers.delete", Label: "Delete Relay drivers", Group: "Relay drivers", Action: "delete", Requires: []string{"fuel.read"}, Sensitive: false},
	{Key: "tolls.read", Label: "View Tolls", Group: "Tolls", Action: "read", Requires: []string{}, Sensitive: false},
	{Key: "tolls.sync", Label: "Sync Tolls", Group: "Tolls", Action: "sync", Requires: []string{"tolls.read"}, Sensitive: false},
	{Key: "tolls.link", Label: "Link Tolls", Group: "Tolls", Action: "link", Requires: []string{"tolls.read"}, Sensitive: false},
	{Key: "tolls.fleet.sync", Label: "Refresh PrePass fleet", Group: "PrePass fleet", Action: "sync", Requires: []string{"tolls.read"}},
	{Key: "tolls.fleet.create", Label: "Add PrePass fleet", Group: "PrePass fleet", Action: "create", Requires: []string{"tolls.read"}, Sensitive: false},
	{Key: "tolls.fleet.edit", Label: "Edit PrePass fleet", Group: "PrePass fleet", Action: "edit", Requires: []string{"tolls.read"}, Sensitive: false},
	{Key: "tolls.fleet.delete", Label: "Delete PrePass fleet", Group: "PrePass fleet", Action: "delete", Requires: []string{"tolls.read"}, Sensitive: false},
	{Key: "tolls.fleet.recover", Label: "Recover PrePass fleet", Group: "PrePass fleet", Action: "recover", Requires: []string{"tolls.read"}, Sensitive: false},
	{Key: "weighmytruck.read", Label: "View Scale", Group: "Scale", Action: "read", Requires: []string{}, Sensitive: false},
	{Key: "weighmytruck.create", Label: "Add Scale", Group: "Scale", Action: "create", Requires: []string{"weighmytruck.read"}, Sensitive: false},
	{Key: "weighmytruck.delete", Label: "Delete Scale", Group: "Scale", Action: "delete", Requires: []string{"weighmytruck.read"}, Sensitive: false},
	{Key: "weighmytruck.sync", Label: "Sync Scale", Group: "Scale", Action: "sync", Requires: []string{"weighmytruck.read"}, Sensitive: false},
	{Key: "weighmytruck.link", Label: "Link Scale", Group: "Scale", Action: "link", Requires: []string{"weighmytruck.read"}, Sensitive: false},
	{Key: "weighmytruck.recover", Label: "Recover Scale", Group: "Scale", Action: "recover", Requires: []string{"weighmytruck.read"}, Sensitive: false},
	{Key: "escrow.read", Label: "View Escrow", Group: "Escrow", Action: "read", Requires: []string{}, Sensitive: true},
	{Key: "escrow.create", Label: "Add Escrow", Group: "Escrow", Action: "create", Requires: []string{"escrow.read"}, Sensitive: true},
	{Key: "escrow.edit", Label: "Edit Escrow", Group: "Escrow", Action: "edit", Requires: []string{"escrow.read"}, Sensitive: true},
	{Key: "escrow.delete", Label: "Cancel Escrow releases", Group: "Escrow", Action: "delete", Requires: []string{"escrow.read"}, Sensitive: true},
	{Key: "charges.read", Label: "View Recurring Charges", Group: "Recurring Charges", Action: "read", Requires: []string{}, Sensitive: true},
	{Key: "charges.create", Label: "Add Recurring Charges", Group: "Recurring Charges", Action: "create", Requires: []string{"charges.read"}, Sensitive: true},
	{Key: "charges.edit", Label: "Edit Recurring Charges", Group: "Recurring Charges", Action: "edit", Requires: []string{"charges.read"}, Sensitive: true},
	{Key: "charges.delete", Label: "Delete Recurring Charges", Group: "Recurring Charges", Action: "delete", Requires: []string{"charges.read"}, Sensitive: true},
	{Key: "tasks.read", Label: "View Tasks", Group: "Tasks", Action: "read", Requires: []string{}, Sensitive: false},
	{Key: "tasks.create", Label: "Add Tasks", Group: "Tasks", Action: "create", Requires: []string{"tasks.read"}, Sensitive: false},
	{Key: "tasks.edit", Label: "Edit Tasks", Group: "Tasks", Action: "edit", Requires: []string{"tasks.read"}, Sensitive: false},
	{Key: "tasks.delete", Label: "Delete Tasks", Group: "Tasks", Action: "delete", Requires: []string{"tasks.read"}, Sensitive: false},
	{Key: "tasks.review", Label: "Review Tasks", Group: "Tasks", Action: "review", Requires: []string{"tasks.read"}, Sensitive: false},
	{Key: "fleetscope.read", Label: "View FleetScope", Group: "FleetScope", Action: "read", Requires: []string{}, Sensitive: false},
	{Key: "fleetscope.manage", Label: "Manage FleetScope", Group: "FleetScope", Action: "manage", Requires: []string{"fleetscope.read"}, Sensitive: false},
	{Key: "fleetscope.reconcile", Label: "Review and activate FleetScope", Group: "FleetScope", Action: "reconcile", Requires: []string{"fleetscope.read"}, Sensitive: false},
	{Key: "reports.read", Label: "View Financial reports", Group: "Financial reports", Action: "read", Requires: []string{}, Sensitive: true},
	{Key: "expense_settings.manage", Label: "Manage Expense settings", Group: "Expense settings", Action: "manage", Requires: []string{}, Sensitive: false},
	{Key: "expenses.accounting.verify", Label: "Verify Accounting verification", Group: "Accounting verification", Action: "verify", Requires: []string{}, Sensitive: false},
	{Key: "access.manage", Label: "Manage Access administration", Group: "Access administration", Action: "manage", Requires: []string{}, Sensitive: false},
}

func PermissionKeys() []string {
	keys := make([]string, 0, len(Permissions))
	for _, p := range Permissions {
		keys = append(keys, p.Key)
	}
	return keys
}
func ValidPermissions(keys []string) bool {
	seen := map[string]bool{}
	for _, key := range keys {
		if seen[key] {
			return false
		}
		seen[key] = true
		found := false
		for _, p := range Permissions {
			if p.Key == key {
				found = true
				for _, required := range p.Requires {
					if !slices.Contains(keys, required) {
						return false
					}
				}
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func HasPermission(user AuthUser, key string) bool { return slices.Contains(user.Permissions, key) }
