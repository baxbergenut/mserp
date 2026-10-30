package repository

import "slices"

// Legacy finalized statements retain their recorded layovers and exclusions.
// Open statements never generate layovers.
type DriverPayLayover struct {
	Date   string `json:"date"`
	Amount string `json:"amount"`
}

func driverLayoverTotal(d DriverPayDriver) int64 {
	var total int64
	for _, row := range d.Layovers {
		if !slices.Contains(d.Edits.LayoverExcludedDates, row.Date) {
			n, _ := chargeCents(row.Amount)
			total += n
		}
	}
	return total
}
