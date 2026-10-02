package httpapi

import "testing"

func TestContactPhoneValidation(t *testing.T) {
	for _, value := range []string{"", "0123456789", "+1 (470) 334-4443"} {
		t.Run(value, func(t *testing.T) {
			driver, err := (driverRequest{FullName: "Driver", PayType: "cpm", Phone: value}).validate()
			if err != nil {
				t.Fatal(err)
			}
			dispatcher, err := (dispatcherRequest{FullName: "Dispatcher", Phone: value}).validate()
			if err != nil {
				t.Fatal(err)
			}
			investor, err := (investorRequest{FullName: "Investor", Phone: value}).validate()
			if err != nil {
				t.Fatal(err)
			}
			if value == "" {
				if driver.Phone != nil || dispatcher.Phone != nil || investor.Phone != nil {
					t.Fatal("blank phones must be null")
				}
			} else {
				want := value
				if value[0] == '+' {
					want = "4703344443"
				}
				if *driver.Phone != want || *dispatcher.Phone != want || *investor.Phone != want {
					t.Fatal("noncanonical phone")
				}
			}
		})
	}
	for _, value := range []string{"123456789", "123456789012", "bad4703344443", "4703344443 x1", "4703344443/4703344444"} {
		if _, err := (driverRequest{FullName: "Driver", PayType: "cpm", Phone: value}).validate(); err == nil {
			t.Errorf("driver accepted %q", value)
		}
		if _, err := (dispatcherRequest{FullName: "Dispatcher", Phone: value}).validate(); err == nil {
			t.Errorf("dispatcher accepted %q", value)
		}
		if _, err := (investorRequest{FullName: "Investor", Phone: value}).validate(); err == nil {
			t.Errorf("investor accepted %q", value)
		}
	}
}
