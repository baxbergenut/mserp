package repository

import "time"

// Return dates are calendar dates in New York. A vacation month is deliberately
// kept at month precision; it never implies the first day of that month.
type DriverLeavePlan struct {
	HomeStart      string `json:"homeStart,omitempty"`
	HomeEnd        string `json:"homeEnd,omitempty"`
	VacationReturn string `json:"vacationReturn,omitempty"`
}

func (p *DriverLeavePlan) Validate() error {
	if p == nil {
		return nil
	}
	valid := func(value, layout string) bool {
		d, err := time.Parse(layout, value)
		return err == nil && d.Year() >= 2000 && d.Year() <= 2100 && d.Format(layout) == value
	}
	if (p.HomeStart == "") != (p.HomeEnd == "") {
		return chargeInvalid("Choose both home start and back dates")
	}
	if p.HomeStart != "" && (!valid(p.HomeStart, time.DateOnly) || !valid(p.HomeEnd, time.DateOnly) || p.HomeEnd < p.HomeStart) {
		return chargeInvalid("Back date must be on or after a valid home start date")
	}
	if p.VacationReturn != "" && !valid(p.VacationReturn, time.DateOnly) && !valid(p.VacationReturn, "2006-01") {
		return chargeInvalid("Choose a valid vacation return date or month")
	}
	return nil
}
