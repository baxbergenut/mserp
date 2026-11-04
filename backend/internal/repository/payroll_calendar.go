package repository

import "time"

// PayrollLatestWeek is the last completed Monday-Sunday week. Operational
// calendars and expense cutoffs continue to use the actual current week.
func PayrollLatestWeek() string { return payrollLatestWeekAt(time.Now()) }

func payrollLatestWeekAt(now time.Time) string {
	loc, _ := time.LoadLocation("America/New_York")
	now = now.In(loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return day.AddDate(0, 0, -(int(day.Weekday())+6)%7-7).Format(time.DateOnly)
}
