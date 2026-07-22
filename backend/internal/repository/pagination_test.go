package repository

import "testing"

func TestPaginationNormalize(t *testing.T) {
	tests := []struct {
		name       string
		pagination Pagination
		total      int
		want       Pagination
	}{
		{name: "defaults invalid values", pagination: Pagination{}, total: 200, want: Pagination{Page: 1, PageSize: 25}},
		{name: "keeps valid page", pagination: Pagination{Page: 2, PageSize: 50}, total: 120, want: Pagination{Page: 2, PageSize: 50}},
		{name: "clamps page after deletion", pagination: Pagination{Page: 9, PageSize: 25}, total: 51, want: Pagination{Page: 3, PageSize: 25}},
		{name: "empty result has page one", pagination: Pagination{Page: 4, PageSize: 100}, total: 0, want: Pagination{Page: 1, PageSize: 100}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.pagination.Normalize(test.total); got != test.want {
				t.Fatalf("Normalize(%d) = %+v, want %+v", test.total, got, test.want)
			}
		})
	}
}
