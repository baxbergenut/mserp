package repository

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrChargeConflict = errors.New("driver charges changed; reload before saving")

type ChargeValidationError struct{ Message string }

func (e *ChargeValidationError) Error() string { return e.Message }
func chargeInvalid(format string, args ...any) error {
	return &ChargeValidationError{fmt.Sprintf(format, args...)}
}

var chargeDecimal = regexp.MustCompile(`^-?[0-9]{1,10}(\.[0-9]{1,2})?$`)

func chargeCents(s string) (int64, error) {
	if !chargeDecimal.MatchString(s) {
		return 0, chargeInvalid("Enter an amount with at most two decimals")
	}
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	p := strings.SplitN(s, ".", 2)
	n, _ := strconv.ParseInt(p[0], 10, 64)
	n *= 100
	if len(p) == 2 {
		f, _ := strconv.ParseInt((p[1] + "00")[:2], 10, 64)
		n += f
	}
	if negative {
		n = -n
	}
	return n, nil
}
func chargeMoney(n int64) string {
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	return fmt.Sprintf("%s%d.%02d", sign, n/100, n%100)
}
func ChargeCurrentWeek() string {
	loc, _ := time.LoadLocation("America/New_York")
	t := time.Now().In(loc)
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return d.AddDate(0, 0, -(int(d.Weekday())+6)%7).Format(time.DateOnly)
}
func chargeWeek(s string) (time.Time, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil || t.Weekday() != time.Monday || s < "2000-01-03" || s > "2100-12-27" {
		return t, chargeInvalid("Choose a Monday between 2000 and 2100")
	}
	return t, nil
}

type ChargeType struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Direction string `json:"direction"`
	Amount    string `json:"amount"`
	Archived  bool   `json:"archived"`
	Version   int    `json:"version"`
}
type ChargePhase struct {
	WeekStart string `json:"weekStart"`
	Amount    string `json:"amount"`
	Paused    bool   `json:"paused"`
}
type ChargeOccurrence struct {
	ScheduleID      string     `json:"scheduleId"`
	WeekStart       string     `json:"weekStart"`
	Kind            string     `json:"kind"`
	Name            string     `json:"name"`
	ScheduledAmount string     `json:"scheduledAmount"`
	Amount          string     `json:"amount"`
	Overridden      bool       `json:"overridden"`
	ConfirmedAt     *time.Time `json:"confirmedAt"`
	ConfirmedBy     *string    `json:"confirmedBy"`
	Version         int        `json:"version"`
	ScheduleVersion int        `json:"scheduleVersion"`
	Reset           bool       `json:"reset,omitempty"`
}
type ChargeSchedule struct {
	InstallmentCount int                `json:"installmentCount"`
	ID               string             `json:"id"`
	DriverID         string             `json:"driverId"`
	DriverName       string             `json:"driverName"`
	TypeID           *string            `json:"typeId"`
	Kind             string             `json:"kind"`
	Name             string             `json:"name"`
	Direction        string             `json:"direction"`
	StartWeek        string             `json:"startWeek"`
	EndWeek          *string            `json:"endWeek"`
	Eligibility      string             `json:"eligibility"`
	Total            *string            `json:"total"`
	Version          int                `json:"version"`
	Phases           []ChargePhase      `json:"phases"`
	Occurrences      []ChargeOccurrence `json:"occurrences"`
	Confirmed        string             `json:"confirmed"`
	Remaining        string             `json:"remaining"`
	Scheduled        string             `json:"scheduled"`
	CompletionWeek   string             `json:"completionWeek"`
	Status           string             `json:"status"`
}
type ChargeEvent struct {
	ID        int64     `json:"id"`
	Action    string    `json:"action"`
	Actor     string    `json:"actor"`
	Details   any       `json:"details"`
	CreatedAt time.Time `json:"createdAt"`
}
type ChargeData struct {
	Types       []ChargeType     `json:"types"`
	Schedules   []ChargeSchedule `json:"schedules"`
	CurrentWeek string           `json:"currentWeek"`
}
type ChargeCreate struct {
	DriverIDs    []string `json:"driverIds"`
	TypeID       string   `json:"typeId"`
	Kind         string   `json:"kind"`
	Name         string   `json:"name"`
	Amount       string   `json:"amount"`
	Total        string   `json:"total"`
	Installments int      `json:"installments"`
	StartWeek    string   `json:"startWeek"`
	EndWeek      *string  `json:"endWeek"`
	Eligibility  string   `json:"eligibility"`
}
type ChargeTarget struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}
type ChargeBulk struct {
	Targets   []ChargeTarget `json:"targets"`
	Action    string         `json:"action"`
	WeekStart string         `json:"weekStart"`
	Amount    string         `json:"amount"`
}
type ChargeConfirm struct {
	DriverID  string             `json:"driverId"`
	WeekStart string             `json:"weekStart"`
	Rows      []ChargeOccurrence `json:"rows"`
	Reason    string             `json:"reason"`
}

func phaseAt(s ChargeSchedule, week string) ChargePhase {
	p := ChargePhase{Paused: true, Amount: "0.00"}
	for _, v := range s.Phases {
		if v.WeekStart <= week {
			p = v
		} else {
			break
		}
	}
	return p
}

// Fixed occurrences reserve their amounts before automatic allocation. This
// protects explicit overrides and confirmations even when visited out of order.
// Reads never persist or collect money; all elapsed eligible weeks participate.
func projectCharges(s ChargeSchedule, through string, loads map[string]bool, forecast bool) ([]ChargeOccurrence, error) {
	start, err := chargeWeek(s.StartWeek)
	if err != nil {
		return nil, err
	}
	remaining := int64(0)
	if s.Total != nil {
		remaining, _ = chargeCents(*s.Total)
	}
	fixed := map[string]ChargeOccurrence{}
	for _, o := range s.Occurrences {
		o.Kind = s.Kind
		o.ScheduleVersion = s.Version
		fixed[o.WeekStart] = o
		if s.Kind == "installment" {
			n, _ := chargeCents(o.Amount)
			if n > 0 {
				return nil, chargeInvalid("Installments must be charges or zero")
			}
			remaining += n
		}
	}
	if s.Kind == "installment" && remaining < 0 {
		weeks := []string{}
		for w := range fixed {
			weeks = append(weeks, w)
		}
		sort.Strings(weeks)
		return nil, chargeInvalid("Plan is overallocated; correct saved weeks: %s", strings.Join(weeks, ", "))
	}
	out := []ChargeOccurrence{}
	position := 0
	for d := start; d.Format(time.DateOnly) <= through; d = d.AddDate(0, 0, 7) {
		week := d.Format(time.DateOnly)
		if o, ok := fixed[week]; ok {
			out = append(out, o)
			position++
			continue
		}
		if s.EndWeek != nil && week > *s.EndWeek {
			continue
		}
		p := phaseAt(s, week)
		if p.Paused || (s.Eligibility == "loads" && !loads[week] && !(forecast && week >= ChargeCurrentWeek())) {
			continue
		}
		n, _ := chargeCents(p.Amount)
		if s.InstallmentCount > 0 && p.WeekStart == s.StartWeek && position == s.InstallmentCount-1 {
			total, _ := chargeCents(*s.Total)
			n += total % int64(s.InstallmentCount)
		}
		position++
		if s.Kind == "installment" {
			if remaining == 0 {
				continue
			}
			if n > remaining {
				n = remaining
			}
			remaining -= n
		}
		if s.Direction == "charge" {
			n = -n
		}
		out = append(out, ChargeOccurrence{ScheduleID: s.ID, WeekStart: week, Kind: s.Kind, Name: s.Name, Amount: chargeMoney(n), ScheduledAmount: chargeMoney(n), ScheduleVersion: s.Version})
	}
	return out, nil
}
func summarizeCharge(s *ChargeSchedule, loads map[string]bool) error {
	s.Status = "active"
	current := ChargeCurrentWeek()
	p := phaseAt(*s, current)
	if s.StartWeek > current {
		s.Status = "scheduled"
	} else if p.Paused {
		s.Status = "paused"
	}
	if s.EndWeek != nil && *s.EndWeek < current {
		s.Status = "ended"
	}
	s.Confirmed = "0.00"
	s.Remaining = "0.00"
	s.Scheduled = "0.00"
	if s.Kind != "installment" {
		return nil
	}
	total, _ := chargeCents(*s.Total)
	confirmed := int64(0)
	for _, o := range s.Occurrences {
		if o.ConfirmedAt != nil {
			n, _ := chargeCents(o.Amount)
			confirmed -= n
		}
	}
	s.Confirmed = chargeMoney(confirmed)
	s.Remaining = chargeMoney(total - confirmed)
	rows, err := projectCharges(*s, "2100-12-27", loads, true)
	if err != nil {
		return err
	}
	scheduled := int64(0)
	for _, o := range rows {
		if o.ConfirmedAt == nil {
			n, _ := chargeCents(o.Amount)
			scheduled -= n
		}
		if o.Amount != "0.00" {
			s.CompletionWeek = o.WeekStart
		}
	}
	s.Scheduled = chargeMoney(scheduled)
	if scheduled < total-confirmed {
		s.CompletionWeek = ""
	}
	if confirmed == total {
		s.Status = "completed"
	}
	return nil
}
