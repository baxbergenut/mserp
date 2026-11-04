package jobs

import (
	"context"
	"fmt"

	"mserp/internal/datatruck"
)

type missingBoardReplacementRepository interface {
	MissingBoardReplacementIDs(context.Context) ([]int, error)
	ReplaceMissingBoardLoads(context.Context, []int, []datatruck.Load) (int, error)
}

// Reuse the complete ID lookup from slow reconciliation. Modified/date-filtered
// feeds never establish absence. Only candidates outside that lookup cost extra
// requests, and only successful canonical repairs trigger payroll publication.
func (j *SyncLoadsJob) replaceMissingBoardLoads(ctx context.Context, checked map[int]bool, live map[int]datatruck.Load) error {
	repo, ok := j.repo.(missingBoardReplacementRepository)
	if !ok {
		return nil
	}
	ids, err := repo.MissingBoardReplacementIDs(ctx)
	if err != nil || len(ids) == 0 {
		return err
	}
	client, ok := j.client.(interface {
		FetchLoadsByIDs(context.Context, []int) ([]datatruck.Load, error)
	})
	if !ok {
		return fmt.Errorf("batch load lookup unavailable")
	}
	if live == nil {
		live = map[int]datatruck.Load{}
	}
	extra := []int{}
	for _, id := range ids {
		if !checked[id] {
			extra = append(extra, id)
		}
	}
	for start := 0; start < len(extra); start += 25 {
		requested := extra[start:min(start+25, len(extra))]
		loads, err := client.FetchLoadsByIDs(ctx, requested)
		if err != nil {
			return err
		}
		batch := map[int]datatruck.Load{}
		addLoadsByID(batch, loads)
		if _, err = j.save(ctx, batch, j.repo.RefreshLoads); err != nil {
			return err
		}
		returned := make([]int, 0, len(loads))
		for _, l := range loads {
			returned = append(returned, l.ID)
		}
		if err = j.repo.RecordLoadSourceLookup(ctx, requested, returned); err != nil {
			return err
		}
		addLoadsByID(live, loads)
	}
	evidence := make([]datatruck.Load, 0, len(ids))
	for _, id := range ids {
		if l, ok := live[id]; ok {
			evidence = append(evidence, l)
		}
	}
	replaced, err := repo.ReplaceMissingBoardLoads(ctx, ids, evidence)
	if err != nil {
		return err
	}
	if replaced > 0 {
		j.logger.Info("replaced missing Gross Board load links", "groups", replaced)
	}
	return nil
}
