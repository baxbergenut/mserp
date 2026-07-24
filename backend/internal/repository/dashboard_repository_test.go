package repository

import "testing"

func TestDriverSettlementAndContribution(t *testing.T) {
	tests := []struct {
		name             string
		isOwnerOperator  bool
		gross            float64
		pay              float64
		fuel             float64
		tolls            float64
		wantSettlement   float64
		wantContribution float64
	}{
		{
			name:             "owner operator deductions reduce settlement",
			isOwnerOperator:  true,
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settlement, contribution := driverSettlementAndContribution(
				test.isOwnerOperator,
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
