package httpapi

import "strings"

// Mask structured investor display fields on every JSON response. Operational
// IDs and owner/driver relationships remain available for expense routing; they
// do not grant access to the investor directory, agreements or payroll.
// This operates on a JSON copy, never shared caches or saved financial reports.
func projectInvestorNames(value any, ownerChoice bool) {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			projectInvestorNames(item, ownerChoice)
		}
	case map[string]any:
		company := v["isCompany"] == true || v["isCompanyOwned"] == true || v["kind"] == "company"
		label := "Investor"
		if company {
			label = "Company"
		}
		for _, key := range []string{"ownerName", "investorName", "owner_name", "investor_name"} {
			if name, ok := v[key].(string); ok && name != "" {
				v[key] = label
			}
		}
		if ownerChoice {
			if _, ok := v["fullName"]; ok {
				v["fullName"] = label
			}
			// A lookup is an operational choice, never an investor profile.
			for _, key := range []string{"phone", "email", "notes"} {
				delete(v, key)
			}
		}
		// Investor truck escrow and installments use the legacy driverName field.
		// Keep the truck label, without touching actual operating-driver names.
		_, escrow := v["heldAmount"]
		_, schedule := v["installmentCount"]
		if (escrow || schedule) && v["ownerId"] != nil && v["ownerId"] != "" {
			if name, ok := v["driverName"].(string); ok {
				v["driverName"] = label
				if at := strings.LastIndex(name, " - Truck "); at >= 0 {
					v["driverName"] = label + name[at:]
				}
			}
		}
		// FleetScope ownership snapshots use a distinct wire display field.
		if _, ok := v["mserpOwnerId"]; ok {
			if _, named := v["displayName"]; named {
				v["displayName"] = label
			}
		}
		// Offboarding includes the owner under a legacy driver label and generated
		// prose. Mask those known templates too, without altering operator notes.
		if _, review := v["activeDrivers"]; review && v["terminationId"] != nil {
			v["driverName"] = "Investor"
		}
		if v["systemTaskKind"] == "driver_offboarding" {
			if notes, ok := v["notes"].(string); ok && strings.HasPrefix(notes, "Owner ") {
				if _, rest, found := strings.Cut(notes, " is terminated."); found {
					rest, _, _ = strings.Cut(rest, "\nOwner: /drivers/detail?id=")
					v["notes"] = "Investor is terminated." + rest
				}
			}
		}
		if body, ok := v["body"].(string); ok && strings.HasPrefix(body, "Truck terminated after owner ") {
			if _, rest, found := strings.Cut(body, " was terminated."); found {
				v["body"] = "Truck terminated after investor was terminated." + rest
			}
		}
		for key, item := range v {
			projectInvestorNames(item, key == "owners" || key == "investors" || key == "investor")
		}
	}
}
