package httpapi

import "testing"

func TestDriverEscrowAmountValidation(t *testing.T) {
	for _, value := range []string{"2500", "2500.00", "0.01", "999999999999.99"} {
		if _, err := (driverRequest{FullName: "Driver", PayType: "cpm", EscrowAmount: value}).validate(); err != nil {
			t.Errorf("valid escrow %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{"0", "-1", "1.001", "", "not money", "1000000000000"} {
		request := driverRequest{FullName: "Driver", PayType: "cpm", EscrowAmount: value}
		_, err := request.validate()
		if value == "" {
			if err != nil {
				t.Errorf("omitted escrow should use the configured default: %v", err)
			}
		} else if err == nil {
			t.Errorf("invalid escrow %q accepted", value)
		}
	}
}
