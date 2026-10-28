package repository

import (
	"strings"
	"testing"
)

func TestDriverSettlementAndContribution(t *testing.T) {
	tests := []struct {
		name             string
		deductsExpenses  bool
		gross            float64
		pay              float64
		fuel             float64
		tolls            float64
		wantSettlement   float64
		wantContribution float64
	}{
		{
			name:             "owner operator deductions reduce settlement",
			deductsExpenses:  true,
			gross:            10_000,
			pay:              8_800,
			fuel:             2_000,
			tolls:            300,
			wantSettlement:   6_500,
			wantContribution: 1_200,
		},
		{
			name:             "company driver expenses reduce contribution",
			gross:            10_000,
			pay:              3_000,
			fuel:             2_000,
			tolls:            300,
			wantSettlement:   3_000,
			wantContribution: 4_700,
		},
		{
			name:             "cpm owner operator does not pay expenses",
			deductsExpenses:  false,
			gross:            10_000,
			pay:              3_000,
			fuel:             2_000,
			tolls:            300,
			wantSettlement:   3_000,
			wantContribution: 4_700,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settlement, contribution := driverSettlementAndContribution(
				test.deductsExpenses,
				test.gross,
				test.pay,
				test.fuel,
				test.tolls,
			)
			if settlement != test.wantSettlement {
				t.Fatalf("settlement = %v, want %v", settlement, test.wantSettlement)
			}
			if contribution != test.wantContribution {
				t.Fatalf("contribution = %v, want %v", contribution, test.wantContribution)
			}
		})
	}
}

func TestFinancialDashboardUsesDataTruckUTCDateEncoding(t *testing.T) {
	if count := strings.Count(financialDashboardBaseSQL, "AT TIME ZONE 'UTC'"); count != 3 {
		t.Fatalf("DataTruck UTC date expressions = %d, want 3", count)
	}
}

func TestFinancialDashboardGrossIncludesInvoicedAndDeliveredLoads(t *testing.T) {
	if !strings.Contains(
		financialDashboardBaseSQL,
		"lower(trim(l.status)) IN ('invoiced', 'delivered')",
	) {
		t.Fatal("financial dashboard gross must include invoiced and delivered loads")
	}
}

func TestFinancialDashboardPeriodUsesPickupDateOnly(t *testing.T) {
	if !strings.Contains(financialDashboardBaseSQL, "pickup_date >= $1") ||
		!strings.Contains(financialDashboardBaseSQL, "pickup_date < $2") {
		t.Fatal("period loads must be selected by pickup date")
	}
	if strings.Contains(financialDashboardBaseSQL, "delivery_date <= $2") {
		t.Fatal("period loads must not be limited by delivery date")
	}
}
