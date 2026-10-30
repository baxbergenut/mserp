package repository

// The short-lived shared-palette release accepted these aliases. Project their
// original display labels without rewriting historical or finalized records.
func LegacyGrossBoardDayStatus(value string) string {
	switch value {
	case "ENROUTE":
		return "IN TRANSIT"
	case "REST":
		return "RESET"
	default:
		return value
	}
}
