package config

import (
	"fmt"
	"strconv"
)

// Capabilities are opt-in and ordered. The resource gate also requires an
// independently switched database protocol, checked before startup workers.
func loadPayrollProjectionMode() (string, error) {
	mode := envOrDefault("PAYROLL_PROJECTION_MODE", "off")
	if mode != "off" && mode != "shadow" {
		return "", fmt.Errorf("PAYROLL_PROJECTION_MODE must be off or shadow")
	}
	previous := ""
	priorEnabled := true
	for _, key := range []string{"PAYROLL_PREPARED_READS", "PAYROLL_PREPARED_COMMANDS", "PAYROLL_PREPARED_FINALIZATION", "PAYROLL_RESOURCE_LOCKS"} {
		enabled, err := strconv.ParseBool(envOrDefault(key, "false"))
		if err != nil {
			return "", fmt.Errorf("%s must be true or false", key)
		}
		if enabled {
			if mode != "shadow" {
				return "", fmt.Errorf("%s requires the payroll preparation worker", key)
			}
			if !priorEnabled {
				return "", fmt.Errorf("%s requires %s", key, previous)
			}
		}
		previous, priorEnabled = key, enabled
	}
	return mode, nil
}
