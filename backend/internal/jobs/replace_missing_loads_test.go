package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"

	"mserp/internal/datatruck"
)

type replacementSyncRepository struct {
	*fakeLoadSyncRepository
	candidates []int
	appliedIDs []int
	evidence   []datatruck.Load
	calls      int
}

func (r *replacementSyncRepository) MissingBoardReplacementIDs(context.Context) ([]int, error) {
	return r.candidates, nil
}
func (r *replacementSyncRepository) ReplaceMissingBoardLoads(_ context.Context, ids []int, live []datatruck.Load) (int, error) {
	r.calls++
	r.appliedIDs = append([]int(nil), ids...)
	r.evidence = append([]datatruck.Load(nil), live...)
	return 1, nil
}
func TestMissingBoardReplacementLookupReuse(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		operational, candidates []int
		fail                    bool
		requests, calls         int
	}{
		{"reuse", []int{10, 11}, []int{10, 11}, false, 1, 1},
		{"outside window", []int{10}, []int{10, 11}, false, 2, 1},
		{"failed extra lookup", []int{10}, []int{10, 11}, true, 2, 0},
		{"nothing to replace", []int{10}, nil, false, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &replacementSyncRepository{fakeLoadSyncRepository: &fakeLoadSyncRepository{ids: tc.operational}, candidates: tc.candidates}
			c := &fakeLoadSyncClient{batchHook: func(ids []int) ([]datatruck.Load, error) {
				live := []datatruck.Load{}
				for _, id := range ids {
					if id == 11 {
						if tc.fail {
							return nil, errors.New("provider failed")
						}
						live = append(live, datatruck.Load{ID: id, Status: "invoiced"})
					}
				}
				return live, nil
			}}
			j := NewSyncLoadsJob(c, r, slog.New(slog.NewTextHandler(io.Discard, nil)))
			_, err := j.RunOperational(context.Background())
			if (err != nil) != tc.fail || len(c.batches) != tc.requests || r.calls != tc.calls {
				t.Fatal(err, len(c.batches), r.calls)
			}
			if tc.calls > 0 && (!reflect.DeepEqual(r.appliedIDs, tc.candidates) || len(r.evidence) != 1 || r.evidence[0].ID != 11) {
				t.Fatal("incomplete replacement evidence", r.appliedIDs, r.evidence)
			}
		})
	}
}
