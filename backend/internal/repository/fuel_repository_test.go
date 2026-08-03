package repository

import (
	"strings"
	"testing"
)

func TestFuelDashboardGrossUsesPickupDateOnly(t *testing.T) {
	if count := strings.Count(fuelDashboardWeeklySQL, "AT TIME ZONE 'UTC'"); count != 3 {
		t.Fatalf("DataTruck UTC pickup date expressions = %d, want 3", count)
	}
	if strings.Contains(fuelDashboardWeeklySQL, "delivery_time") ||
		strings.Contains(fuelDashboardWeeklySQL, "delivery_appointment_time") {
		t.Fatal("fuel dashboard gross must not be grouped by delivery date")
	}
	if strings.Contains(fuelDashboardWeeklySQL, "AT TIME ZONE 'America/New_York'") {
		t.Fatal("fuel dashboard gross must use DataTruck's encoded UTC pickup date")
	}
}

func TestFuelDashboardGrossIncludesInvoicedAndDeliveredLoads(t *testing.T) {
	if !strings.Contains(
		fuelDashboardWeeklySQL,
		"lower(trim(status)) IN ('invoiced', 'delivered')",
	) {
		t.Fatal("fuel dashboard gross must include invoiced and delivered loads")
	}
}

func TestFuelDashboardUsesCurrentContinuousLoadCoverage(t *testing.T) {
	if !strings.Contains(fuelDashboardWeeklySQL, "AS coverage_group") ||
		!strings.Contains(fuelDashboardWeeklySQL, "ORDER BY week_start DESC") {
		t.Fatal("fuel dashboard must find the current continuous run of load weeks")
	}
	if !strings.Contains(fuelDashboardWeeklySQL, "first_week + 7") {
		t.Fatal("fuel dashboard must begin after the first partially covered load week")
	}
	if strings.Contains(fuelDashboardWeeklySQL, "MIN(service_date)") {
		t.Fatal("isolated historical loads must not define the start of load-data coverage")
	}
}
