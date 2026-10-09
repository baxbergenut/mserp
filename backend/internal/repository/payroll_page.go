package repository

import (
	"sort"
	"strconv"
	"strings"
)

type PayPageQuery struct {
	Pagination
	Search, DispatcherID, ID string
}
type PayPageInfo struct {
	Page        int         `json:"page"`
	PageSize    int         `json:"pageSize"`
	Total       int         `json:"total"`
	TotalPages  int         `json:"totalPages"`
	Dispatchers []PayPerson `json:"dispatchers"`
	RowIDs      []string    `json:"rowIds"`
	Finalized   int         `json:"finalized"`
	Statements  int         `json:"statements"`
	Loads       int         `json:"loads"`
	Review      int         `json:"review"`
	Fee         string      `json:"fee"`
	Payable     string      `json:"payable"`
}

// Page the shared consistent report after routing, carry and frozen settlement
// overlays. Filtering before those calculations would change financial results.
func PaginatePay(report DriverPayWeek, query PayPageQuery) DriverPayWeek {
	info := &PayPageInfo{Dispatchers: []PayPerson{}, RowIDs: []string{}}
	names := map[string]string{}
	type row struct {
		name, unit string
		driver     *DriverPayDriver
		setup      *InvestorTruckSetup
	}
	rows := []row{}
	search := strings.ToLower(strings.TrimSpace(query.Search))
	matches := func(id, dispatcher, text string) bool {
		return (query.ID == "" || query.ID == id) && (query.DispatcherID == "" || query.DispatcherID == dispatcher || (query.DispatcherID == "__unassigned" && dispatcher == "")) && strings.Contains(strings.ToLower(text), search)
	}
	var fee, payable int64
	for i := range report.Drivers {
		d := &report.Drivers[i]
		names[d.DispatcherID] = d.DispatcherName
		if !d.TruckInactive {
			info.Statements++
			if d.Settlement != nil && d.Settlement.Finalized {
				info.Finalized++
			}
		}
		text := d.FullName + " " + d.TruckUnit
		for _, l := range d.Loads {
			text += " " + l.LoadNumber
		}
		for _, person := range d.OperatingDrivers {
			text += " " + person.Name
		}
		if !matches(d.ID, d.DispatcherID, text) {
			continue
		}
		rows = append(rows, row{d.FullName, d.TruckUnit, d, nil})
		info.Loads += len(d.Loads)
		info.Review += len(d.Issues)
		for _, l := range d.Loads {
			n, _ := chargeCents(l.Fee)
			fee += n
			payable += n
			if len(l.Issues) > 0 {
				info.Review++
			}
		}
		for _, a := range d.Edits.Adjustments {
			n, _ := chargeCents(a.Amount)
			if a.Kind == "deduction" {
				n = -n
			}
			payable += n
		}
		for _, a := range d.AutoCharges {
			n, _ := chargeCents(a.Amount)
			payable += n
		}
		for _, a := range d.Edits.GeneratedCharges {
			n, _ := chargeCents(a.Amount)
			payable += n
		}
		for _, a := range d.Edits.ExpenseDeductions {
			n, _ := chargeCents(a.Amount)
			payable -= n
		}
		payable += payPageCosts(*d)
	}
	for i := range report.SetupRequired {
		s := &report.SetupRequired[i]
		names[s.DispatcherID] = s.DispatcherName
		if matches(s.TruckID, s.DispatcherID, s.OwnerName+" "+s.TruckUnit+" "+s.DriverName) {
			rows = append(rows, row{s.OwnerName, s.TruckUnit, nil, s})
		}
	}
	for id, name := range names {
		if strings.TrimSpace(name) == "" {
			name = "Unassigned"
		}
		info.Dispatchers = append(info.Dispatchers, PayPerson{ID: id, Name: name})
	}
	sort.Slice(info.Dispatchers, func(i, j int) bool { return info.Dispatchers[i].Name < info.Dispatchers[j].Name })
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.name != b.name {
			return a.name < b.name
		}
		x, xe := strconv.Atoi(a.unit)
		y, ye := strconv.Atoi(b.unit)
		if xe == nil && ye == nil && x != y {
			return x < y
		}
		return a.unit < b.unit
	})
	info.Total = len(rows)
	query.Pagination = query.Normalize(info.Total)
	info.Page, info.PageSize = query.Page, query.PageSize
	info.TotalPages = max(1, (info.Total+query.PageSize-1)/query.PageSize)
	info.Fee, info.Payable = chargeMoney(fee), chargeMoney(payable)
	report.Drivers = []DriverPayDriver{}
	report.SetupRequired = nil
	for _, r := range rows[query.Offset():min(query.Offset()+query.PageSize, len(rows))] {
		if r.driver != nil {
			report.Drivers = append(report.Drivers, *r.driver)
			info.RowIDs = append(info.RowIDs, r.driver.ID)
		} else {
			report.SetupRequired = append(report.SetupRequired, *r.setup)
			info.RowIDs = append(info.RowIDs, r.setup.TruckID)
		}
	}
	report.Pagination = info
	return report
}

func payPageCosts(d DriverPayDriver) int64 {
	var total int64
	for _, key := range []string{"fuel", "toll"} {
		override, source := d.Edits.FuelOverride, d.FuelTotal
		if key == "toll" {
			override, source = d.Edits.TollOverride, d.TollTotal
		}
		var due, carry int64
		if d.Edits.Costs != nil {
			if key == "fuel" {
				due, _ = chargeCents(d.Edits.Costs.FuelDue)
				carry, _ = chargeCents(d.Edits.Costs.FuelCarry)
			} else {
				due, _ = chargeCents(d.Edits.Costs.TollDue)
				carry, _ = chargeCents(d.Edits.Costs.TollCarry)
			}
		} else if d.IsOwnerOperator && d.PayType == "gross_percentage" {
			due, _ = chargeCents(source)
		}
		if d.PayType == "cpm" && carry == 0 {
			continue
		}
		if override != nil {
			n, _ := chargeCents(*override)
			total += n
		} else {
			total -= due
		}
	}
	return total
}
