package repository

import "slices"

type Permission struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Permission keys are a code-owned contract. Administrators compose roles from
// this catalog; inventing a database key must never grant new API capabilities.
var Permissions = []Permission{
	{"weighmytruck.read", "View WeighMyTruck membership and driver contact details"},
	{"weighmytruck.write", "Add, remove, link and verify WeighMyTruck drivers"},
	{"driver_board.read", "View Status Board, history, contact details and weekly totals"}, {"driver_board.write", "Edit Status Board and driver home, and undo history changes"},
	{"fleet.read", "View drivers, trucks, dispatchers, investors and documents"},
	{"fleet.write", "Manage fleet, ownership, intake and documents"},
	{"loads.read", "View imported loads"}, {"loads.sync", "Sync imported loads"},
	{"board.read", "View Gross Board and balances"}, {"board.write", "Edit Gross Board"},
	{"fuel.read", "View fuel transactions and dashboard"}, {"fuel.sync", "Sync fuel"},
	{"tolls.read", "View toll transactions and dashboard"}, {"tolls.sync", "Sync tolls"},
	{"expense_settings.manage", "Manage Expenses & Charges categories and entry options"},
	{"payroll.read", "View payroll and settlement history"}, {"payroll.write", "Edit payroll and refresh linked loads"},
	{"payroll.finalize", "Finalize and reopen settlements"},
	{"escrow.read", "View driver escrow balances and collections"}, {"escrow.write", "Manage escrow releases and the new-driver default"},
	{"charges.read", "View driver and truck charges"}, {"charges.write", "Manage charges and collections"},
	{"tasks.read", "View tasks and Relay identity reviews"}, {"tasks.write", "Manage tasks and review Relay identities"},
	{"reports.read", "View financial reports"},
	{"access.manage", "Manage users, passwords, roles and permissions"},
}

func PermissionKeys() []string {
	keys := make([]string, 0, len(Permissions))
	for _, p := range Permissions {
		keys = append(keys, p.Key)
	}
	return keys
}

func ValidPermissions(keys []string) bool {
	allowed := PermissionKeys()
	for _, key := range keys {
		if !slices.Contains(allowed, key) {
			return false
		}
	}
	return true
}
