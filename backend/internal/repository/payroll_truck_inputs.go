package repository

import "slices"

// Routing mutates its own reports. No map, slice, byte buffer or nullable
// destination in the retained post-write input may become caller-owned state.
func copyTruckPayInputs(v truckPayInputs) truckPayInputs {
	out := truckPayInputs{
		metadata: map[string]truckPayMetadata{}, people: slices.Clone(v.people),
		assigned: map[string]map[string]bool{}, edits: map[string]investorWeekEdit{},
		expenses: map[string][]truckExpense{},
	}
	for id, value := range v.metadata {
		out.metadata[id] = value
	}
	for truck, drivers := range v.assigned {
		out.assigned[truck] = map[string]bool{}
		for driver, assigned := range drivers {
			out.assigned[truck][driver] = assigned
		}
	}
	for id, value := range v.edits {
		value.edits, value.report = slices.Clone(value.edits), slices.Clone(value.report)
		if value.meta.ReopenedAt != nil {
			reopened := *value.meta.ReopenedAt
			value.meta.ReopenedAt = &reopened
		}
		out.edits[id] = value
	}
	for id, values := range v.expenses {
		out.expenses[id] = slices.Clone(values)
		for i, value := range values {
			if value.destination != nil {
				destination := *value.destination
				out.expenses[id][i].destination = &destination
			}
		}
	}
	return out
}
