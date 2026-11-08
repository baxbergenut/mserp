package httpapi

import (
	"errors"
	"strings"
	"time"

	"mserp/internal/repository"
)

type grossBoardPlanningRequest struct {
	WeekStart string                               `json:"weekStart"`
	Entries   []repository.GrossBoardPlanningEntry `json:"entries"`
}

func (request *grossBoardPlanningRequest) validate() error {
	week, err := grossBoardWeek(request.WeekStart)
	if err != nil {
		return err
	}
	if len(request.Entries) == 0 || len(request.Entries) > 1000 {
		return errors.New("save between 1 and 1000 planning entries at a time")
	}
	seen := map[string]bool{}
	for i := range request.Entries {
		entry := &request.Entries[i]
		if !isUUID(entry.DriverID) {
			return errors.New("invalid driver id")
		}
		compact := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(entry.DriverID), "-", ""))
		entry.DriverID = compact[:8] + "-" + compact[8:12] + "-" + compact[12:16] + "-" + compact[16:20] + "-" + compact[20:]
		date, err := time.Parse(time.DateOnly, entry.Date)
		if err != nil || date.Before(week) || !date.Before(week.AddDate(0, 0, 7)) {
			return errors.New("every planning day must belong to the selected week")
		}
		if err = repository.ValidatePlanningEntry(entry); err != nil {
			return err
		}
		key := entry.DriverID + entry.Date + entry.Kind
		if seen[key] {
			return errors.New("a driver planning kind can only appear once per day")
		}
		seen[key] = true
	}
	return nil
}
