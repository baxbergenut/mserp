package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestParsePagination(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		wantPage     int
		wantPageSize int
		wantError    bool
	}{
		{name: "defaults", wantPage: 1, wantPageSize: 25},
		{name: "requested page", query: "?page=3&pageSize=50", wantPage: 3, wantPageSize: 50},
		{name: "rejects zero page", query: "?page=0&pageSize=25", wantError: true},
		{name: "rejects oversized page", query: "?page=1&pageSize=101", wantError: true},
		{name: "rejects nonnumeric size", query: "?pageSize=many", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/records"+test.query, nil)
			got, err := parsePagination(request)
			if test.wantError {
				if err == nil {
					t.Fatal("parsePagination() expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePagination() error = %v", err)
			}
			if got.Page != test.wantPage || got.PageSize != test.wantPageSize {
				t.Fatalf("parsePagination() = %+v, want page %d and size %d", got, test.wantPage, test.wantPageSize)
			}
		})
	}
}
