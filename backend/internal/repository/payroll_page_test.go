package repository

import "testing"

func TestPayPageFinalizedLastBeforePagination(t *testing.T) {
	report := DriverPayWeek{Drivers: []DriverPayDriver{
		{ID: "closed-a", FullName: "Alpha", Settlement: &PayrollSettlement{Finalized: true}},
		{ID: "draft-z", FullName: "Zulu"},
		{ID: "closed-b", FullName: "Beta", Settlement: &PayrollSettlement{Finalized: true}},
		{ID: "draft-b", FullName: "Beta", Settlement: &PayrollSettlement{Finalized: false}},
	}, SetupRequired: []InvestorTruckSetup{{TruckID: "setup", OwnerName: "Alpha"}}}
	assertOrder := func(want []string) {
		t.Helper()
		for i, id := range want {
			page := PaginatePay(report, PayPageQuery{Pagination: Pagination{Page: i + 1, PageSize: 1}, Summary: true})
			if len(page.Pagination.RowIDs) != 1 || page.Pagination.RowIDs[0] != id {
				t.Fatalf("page %d: got %v, want %s", i+1, page.Pagination.RowIDs, id)
			}
		}
	}
	assertOrder([]string{"setup", "draft-b", "draft-z", "closed-a", "closed-b"})
	report.Drivers[0].Settlement.Finalized = false
	assertOrder([]string{"closed-a", "setup", "draft-b", "draft-z", "closed-b"})
	report.Drivers[1].Settlement = &PayrollSettlement{Finalized: true}
	assertOrder([]string{"closed-a", "setup", "draft-b", "closed-b", "draft-z"})
}

func TestPayPageDetailsStayOnPage(t *testing.T) {
	r := DriverPayWeek{Revision: "same-snapshot", Drivers: []DriverPayDriver{
		{ID: "a", FullName: "Alpha", Loads: []DriverPayLoad{{Fee: "10.00"}}},
		{ID: "b", FullName: "Beta", Loads: []DriverPayLoad{{Fee: "20.00"}}},
		{ID: "c", FullName: "Charlie", Loads: []DriverPayLoad{{Fee: "30.00"}}},
	}}
	p := PaginatePay(r, PayPageQuery{Pagination: Pagination{Page: 1, PageSize: 2}, Summary: true, DetailIDs: map[string]bool{"a": true, "c": true}})
	if len(p.Drivers) != 2 || p.Drivers[0].Summary != nil || len(p.Drivers[0].Loads) != 1 || p.Drivers[1].Summary == nil || len(p.Drivers[1].Loads) != 0 || p.Revision != r.Revision || p.Pagination.Total != 3 {
		t.Fatalf("mixed detail page: %+v", p)
	}
}

func TestPayPageFiltersByInvestorIDBeforePaging(t *testing.T) {
	report := DriverPayWeek{Revision: "complete-week", Drivers: []DriverPayDriver{
		{ID: "truck-a", InvestorID: "owner-a", FullName: "Same Name", Loads: []DriverPayLoad{{Fee: "50.00"}}},
		{ID: "truck-b", InvestorID: "owner-b", FullName: "Same Name", Loads: []DriverPayLoad{{Fee: "100.00"}}, Settlement: &PayrollSettlement{Finalized: true}},
	}, SetupRequired: []InvestorTruckSetup{
		{TruckID: "setup-a", OwnerID: "owner-a", OwnerName: "Same Name"},
		{TruckID: "setup-b", OwnerID: "owner-b", OwnerName: "Same Name"},
	}}
	query := PayPageQuery{InvestorID: "owner-b", Pagination: Pagination{Page: 1, PageSize: 1}}
	page := PaginatePay(report, query)
	if len(page.SetupRequired) != 1 || page.SetupRequired[0].TruckID != "setup-b" || page.Pagination.Total != 2 ||
		page.Pagination.Statements != 1 || page.Pagination.Finalized != 1 || page.Pagination.Payable != "100.00" || page.Revision != report.Revision {
		t.Fatalf("filtered first page: %+v", page)
	}
	query.Page = 2
	page = PaginatePay(report, query)
	if len(page.Drivers) != 1 || page.Drivers[0].ID != "truck-b" {
		t.Fatalf("filtered statement page: %+v", page)
	}
}

func TestPaySummariesKeepTotalsAndOmitDetails(t *testing.T) {
	d := DriverPayDriver{ID: "driver", PayType: "cpm", Loads: []DriverPayLoad{{Fee: "100.00"}, {Fee: "", Issues: []string{"missing"}}}, Edits: DriverPayEdits{DriverID: "driver", WeekStart: "2026-10-12", Version: 3, Adjustments: []DriverPayAdjustment{{Kind: "deduction", Amount: "5.00"}}, ExpenseDeductions: []ExpenseDeduction{{Amount: "10.00"}}}}
	full := DriverPayWeek{Revision: "revision", Drivers: []DriverPayDriver{d}}
	page := PaginatePay(full, PayPageQuery{Summary: true})
	row := page.Drivers[0]
	if row.Summary == nil || row.Summary.Loads != 2 || row.Summary.MissingFees != 1 || row.Summary.Review != 1 || row.Summary.Fee != "100.00" || row.Summary.Payable != "85.00" {
		t.Fatalf("summary %+v", row.Summary)
	}
	if len(row.Loads) != 0 || len(row.Edits.ExpenseDeductions) != 0 || len(row.Edits.Adjustments) != 0 || row.Edits.Version != 3 || row.Edits.WeekStart != "2026-10-12" || page.Revision != full.Revision {
		t.Fatal("summary payload contains detail or lost identity")
	}
	if len(full.Drivers[0].Loads) != 2 || full.Drivers[0].Summary != nil {
		t.Fatal("summary mutated cached full report")
	}
}

func TestPayPaginationFiltersBeforePagingAndPreservesRevision(t *testing.T) {
	zero := "0.00"
	report := DriverPayWeek{Revision: "complete-week", Drivers: []DriverPayDriver{
		{ID: "a", FullName: "Alpha", TruckUnit: "2", DispatcherID: "dispatch", DispatcherName: "Dispatcher", PayType: "cpm", Loads: []DriverPayLoad{{LoadNumber: "FIRST", Fee: "100.00"}}, Edits: DriverPayEdits{FuelOverride: &zero}},
		{ID: "b", FullName: "Beta", TruckUnit: "3", DispatcherID: "dispatch", DispatcherName: "Dispatcher", PayType: "gross_percentage", IsOwnerOperator: true, FuelTotal: "10.00", Loads: []DriverPayLoad{{LoadNumber: "SECOND", Fee: "200.00"}}, Edits: DriverPayEdits{Adjustments: []DriverPayAdjustment{{Kind: "deduction", Amount: "5.00"}}}},
		{ID: "c", FullName: "Gamma", TruckUnit: "4", DispatcherName: "Unassigned", PayType: "cpm", Loads: []DriverPayLoad{{LoadNumber: "THIRD", Fee: "300.00"}}, Settlement: &PayrollSettlement{Finalized: true}},
	}, SetupRequired: []InvestorTruckSetup{{TruckID: "setup", OwnerName: "Alpha", TruckUnit: "10"}}}
	paged := PaginatePay(report, PayPageQuery{Pagination: Pagination{Page: 2, PageSize: 1}})
	if len(paged.Drivers) != 0 || len(paged.SetupRequired) != 1 || paged.SetupRequired[0].TruckUnit != "10" {
		t.Fatalf("combined ordering/page: %+v", paged)
	}
	if paged.Revision != report.Revision || paged.Pagination.Total != 4 || paged.Pagination.Payable != "585.00" || paged.Pagination.Finalized != 1 {
		t.Fatalf("week metadata: %+v", paged.Pagination)
	}
	filtered := PaginatePay(report, PayPageQuery{Pagination: Pagination{Page: 9, PageSize: 1}, Search: "second", DispatcherID: "dispatch"})
	if len(filtered.Drivers) != 1 || filtered.Drivers[0].ID != "b" || filtered.Pagination.Page != 1 || filtered.Pagination.Total != 1 || filtered.Pagination.Payable != "185.00" || len(filtered.Pagination.Dispatchers) != 2 {
		t.Fatalf("filtering/clamp/global options: %+v", filtered)
	}
	missing := PaginatePay(report, PayPageQuery{Pagination: Pagination{Page: 9, PageSize: 1}, Search: "no match"})
	if missing.Pagination.Total != 0 || len(missing.Drivers) != 0 || missing.Pagination.TotalPages != 1 {
		t.Fatal("empty page", missing)
	}
	if len(report.Drivers) != 3 || len(report.SetupRequired) != 1 {
		t.Fatal("pagination mutated shared report")
	}
}

func TestPayPageFiltersByStatementDriverType(t *testing.T) {
	report := DriverPayWeek{Drivers: []DriverPayDriver{
		{ID: "unassigned", FullName: "Alpha", EmploymentType: "unassigned"},
		summarizePayStatement(DriverPayDriver{
			ID: "mixed", FullName: "Beta", EmploymentType: "owner_operator",
			DisplayEmploymentTypes: []string{"company", "investor_driver"},
			Loads:                  []DriverPayLoad{{Fee: "100.00"}},
		}),
		{ID: "legacy", FullName: "Gamma", IsOwnerOperator: true, Settlement: &PayrollSettlement{Finalized: true}},
	}}
	company := PaginatePay(report, PayPageQuery{DriverType: "company", Pagination: Pagination{Page: 1, PageSize: 1}})
	if len(company.Drivers) != 1 || company.Drivers[0].ID != "mixed" || company.Pagination.Total != 1 || company.Pagination.Payable != "100.00" {
		t.Fatalf("dated type should match the compact statement before pagination: %+v", company.Pagination)
	}
	owners := PaginatePay(report, PayPageQuery{DriverType: "owner_operator"})
	if len(owners.Drivers) != 1 || owners.Drivers[0].ID != "legacy" || owners.Pagination.Finalized != 1 || owners.Pagination.Statements != 1 {
		t.Fatalf("owner operators should use displayed types or the legacy fallback: %+v", owners.Pagination)
	}
}

func TestEscrowReviewOutcomeFromActualReleasedFunds(t *testing.T) {
	for _, test := range []struct {
		held, released int64
		want           string
	}{{250000, 0, "kept"}, {0, 0, "kept"}, {200000, 50000, "partially_released"}, {0, 250000, "released"}} {
		if got := escrowReviewDecision(test.held, test.released); got != test.want {
			t.Fatalf("held=%d released=%d: %s", test.held, test.released, got)
		}
	}
}
