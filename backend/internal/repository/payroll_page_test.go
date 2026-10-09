package repository

import "testing"

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
