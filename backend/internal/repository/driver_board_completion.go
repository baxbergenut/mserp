package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Read completion details only for the load panel and its action responses.
// A final advance clears or replaces the current load. Mark completed leaves
// both its text and its selected plan intact, including when neither is set.
func annotateBoardCompletions(ctx context.Context, tx pgx.Tx, driver string, loads []BoardLoad) error {
	var positions []int
	var loadKeys, planKeys, aliasKeys []string
	for i, load := range loads {
		if load.Completed {
			positions = append(positions, i)
			loadKeys = append(loadKeys, boardCompletionKey(load))
			planKeys = append(planKeys, "plan:"+load.PlanID)
			aliasKeys = append(aliasKeys, "plan:"+load.queueAlias)
		}
	}
	if len(positions) == 0 {
		return nil
	}

	// The key must still be completed locally. Undoing a restore brings back
	// the original completion and should still credit its original actor.
	rows, err := tx.Query(ctx, `SELECT candidate.ordinal, event.actor_name, event.marked_as_completed
 FROM driver_board_load_state s
 CROSS JOIN unnest($2::text[], $3::text[], $4::text[]) WITH ORDINALITY AS candidate(load_key, plan_key, alias_key, ordinal)
 CROSS JOIN LATERAL (
   SELECT ARRAY(SELECT key FROM unnest(ARRAY[candidate.load_key, candidate.plan_key, candidate.alias_key]) AS keys(key)
     WHERE (s.payload->'completed') ? key) AS active
 ) keys
 JOIN LATERAL (
   SELECT CASE WHEN h.source='board' AND h.actor_id<>'' AND h.actor_name<>'System' THEN h.actor_name ELSE '' END AS actor_name,
     h.source='board' AND NOT (h.after_values ? 'currentLoad')
       AND ROW((h.before_values->>'loadPlan')::jsonb#>>'{current,planId}', (h.before_values->>'loadPlan')::jsonb#>>'{current,loadId}')
         IS NOT DISTINCT FROM
           ROW((h.after_values->>'loadPlan')::jsonb#>>'{current,planId}', (h.after_values->>'loadPlan')::jsonb#>>'{current,loadId}')
       AS marked_as_completed
   FROM driver_board_history h
   WHERE h.driver_id=s.driver_id AND h.undo_of IS NULL
     AND h.before_values ? 'loadPlan' AND h.after_values ? 'loadPlan'
     AND ((h.after_values->>'loadPlan')::jsonb->'completed') ?| keys.active
     AND NOT coalesce(((h.before_values->>'loadPlan')::jsonb->'completed') ?| keys.active, false)
   ORDER BY h.id DESC LIMIT 1
 ) event ON cardinality(keys.active)>0
 WHERE s.driver_id=$1`, driver, loadKeys, planKeys, aliasKeys)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ordinal int
		var name string
		var markedAsCompleted bool
		if err := rows.Scan(&ordinal, &name, &markedAsCompleted); err != nil {
			return err
		}
		load := &loads[positions[ordinal-1]]
		load.CompletedByName = name
		load.MarkedAsCompleted = markedAsCompleted
	}
	return rows.Err()
}
