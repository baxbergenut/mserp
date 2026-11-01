package config

import "testing"

func TestPayrollProjectionCapabilities(t *testing.T) {
	for _, mode := range []string{"off", "shadow", "", "live", "prepared"} {
		t.Run("mode_"+mode, func(t *testing.T) {
			t.Setenv("PAYROLL_PROJECTION_MODE", mode)
			got, err := loadPayrollProjectionMode()
			valid := mode == "off" || mode == "shadow" || mode == ""
			if valid != (err == nil) || (valid && got == "") {
				t.Fatalf("mode %q: %q %v", mode, got, err)
			}
		})
	}
	for _, key := range []string{"PAYROLL_PREPARED_COMMANDS", "PAYROLL_PREPARED_FINALIZATION", "PAYROLL_RESOURCE_LOCKS"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv("PAYROLL_PROJECTION_MODE", "shadow")
			t.Setenv(key, "true")
			if _, err := loadPayrollProjectionMode(); err == nil {
				t.Fatal("cutover without prerequisite capabilities accepted")
			}
		})
	}
	t.Run("reads_need_worker", func(t *testing.T) {
		t.Setenv("PAYROLL_PREPARED_READS", "true")
		t.Setenv("PAYROLL_PROJECTION_MODE", "off")
		if _, err := loadPayrollProjectionMode(); err == nil {
			t.Fatal("prepared reads without worker")
		}
		t.Setenv("PAYROLL_PROJECTION_MODE", "shadow")
		if _, err := loadPayrollProjectionMode(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("ordered_capabilities", func(t *testing.T) {
		t.Setenv("PAYROLL_PROJECTION_MODE", "shadow")
		for _, key := range []string{"PAYROLL_PREPARED_READS", "PAYROLL_PREPARED_COMMANDS", "PAYROLL_PREPARED_FINALIZATION", "PAYROLL_RESOURCE_LOCKS"} {
			t.Setenv(key, "true")
			if _, err := loadPayrollProjectionMode(); err != nil {
				t.Fatal(key, err)
			}
		}
	})
}
