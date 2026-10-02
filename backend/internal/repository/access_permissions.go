package repository

import "slices"

type Permission struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Permission keys are a code-owned contract. Administrators compose roles from
// this catalog; inventing a database key must never grant new API capabilities.
var Permissions = []Permission{
	{"driver_board.read", "View Driver Board, contact details and weekly totals"}, {"driver_board.write", "Edit Driver Board and driver home"},
	{"fleet.read", "View drivers, trucks, dispatchers, investors and documents"},
	{"fleet.write", "Manage fleet, ownership, intake and documents"},
	{"loads.read", "View imported loads"}, {"loads.sync", "Sync imported loads"},
	{"board.read", "View Gross Board and balances"}, {"board.write", "Edit Gross Board"},
	{"fuel.read", "View fuel transactions and dashboard"}, {"fuel.sync", "Sync fuel"},
	{"tolls.read", "View toll transactions and dashboard"}, {"tolls.sync", "Sync tolls"},
	{"expenses.read", "View expenses and expense settings"}, {"expenses.write", "Manage expenses and expense settings"},
	{"payroll.read", "View payroll and settlement history"}, {"payroll.write", "Edit payroll and refresh linked loads"},
	{"payroll.finalize", "Finalize and reopen settlements"},
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
