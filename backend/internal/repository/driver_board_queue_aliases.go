package repository

import "strings"

// Keep a manual number and its unresolved Gross Board counterpart as one queue
// item. This changes only the view: accounting plans and the manual action/order
// identity survive. Never attach an ambiguous manual number to a pinned source,
// or merge conflicting source IDs, missing records, or multiple manual entries.
func manualBoardPlanAliases(plans []BoardLoad) map[string]string {
	key := func(p BoardLoad) string { return strings.ToLower(strings.TrimSpace(p.Number)) }
	manual := map[string]BoardLoad{}
	blocked := map[string]bool{}
	for _, p := range plans {
		number := key(p)
		if number == "" {
			continue
		}
		if p.SourceMissing {
			blocked[number] = true
		}
		if p.Manual && p.PlanID != "" {
			if old, ok := manual[number]; ok && old.PlanID != p.PlanID {
				blocked[number] = true
			}
			manual[number] = p
		}
	}
	for _, p := range plans {
		number := key(p)
		if m, ok := manual[number]; ok && ((m.LoadID == nil) != (p.LoadID == nil) || m.LoadID != nil && p.LoadID != nil && *m.LoadID != *p.LoadID) {
			blocked[number] = true
		}
	}
	aliases := map[string]string{}
	for _, p := range plans {
		number := key(p)
		if m, ok := manual[number]; ok && !blocked[number] && p.PlanID != "" {
			aliases[p.PlanID] = m.PlanID
		}
	}
	return aliases
}

// Old planning history must not displace a deliberately requeued manual load.
// For an eligible counterpart, use its actual Gross Board date/slot and identity.
func preferredGrossBoardQueuePlans(plans []BoardLoad, aliases map[string]string, state boardLoadState, from string) map[string]string {
	preferred := map[string]string{}
	for _, p := range plans {
		alias := aliases[p.PlanID]
		if alias != "" && !p.Manual && boardPlanQueued(p, state, from) && preferred[alias] == "" {
			preferred[alias] = p.PlanID
		}
	}
	return preferred
}

func markBoardLoadCompleted(state *boardLoadState, load BoardLoad) {
	state.Completed = append(state.Completed, boardCompletionKey(load))
	if load.Manual {
		state.Completed = append(state.Completed, "plan:"+load.PlanID)
	}
	if load.queueAlias != "" && load.queueAlias != load.PlanID {
		state.Completed = append(state.Completed, "plan:"+load.queueAlias)
	}
}
