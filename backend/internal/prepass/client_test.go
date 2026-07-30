package prepass

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestFetchTransactionsAuthenticatesDiscoversAccountsAndPaginates(t *testing.T) {
	tokenCalls := 0
	accountCalls := 0
	transactionCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/v1/token":
			tokenCalls++
			if r.Header.Get("client_id") != "client" || r.Header.Get("client_secret") != "secret" {
				t.Fatal("missing client credentials")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3599}`))
		case "/accounts/v1/accounts":
			accountCalls++
			if r.Header.Get("Authorization") != "Bearer token" {
				t.Fatal("missing bearer token")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"accounts":[
				{"accountNumber":"123","accountStatus":"Active"},
				{"accountNumber":"999","accountStatus":"Inactive"}
			]}`))
		case "/tolltransaction/v1/transactions":
			transactionCalls++
			query := r.URL.Query()
			if query.Get("startPostDate") != "2026-07-01" ||
				(query.Get("endPostDate") != "2026-07-03" &&
					query.Get("endPostDate") != "2026-07-02") ||
				query.Get("accountNumbers") != "123" ||
				query.Get("pageSize") != "10000" {
				t.Fatalf("unexpected query: %s", r.URL.RawQuery)
			}
			page, _ := strconv.Atoi(query.Get("pageNumber"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"pageInfo":{"pageNumber":` + strconv.Itoa(page) + `,"totalPages":2,"totalRecords":2},
				"transactions":[{"tollId":` + strconv.Itoa(page) + `,"accountNumber":123,"tollCharge":12.34}]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClientWithHTTPClient(server.URL, "client", "secret", server.Client())
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	values, err := client.FetchTransactions(context.Background(), start, start.AddDate(0, 0, 2))
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0].TollID != 1 || values[1].TollID != 2 {
		t.Fatalf("transactions = %#v", values)
	}

	_, err = client.FetchTransactions(context.Background(), start, start.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if tokenCalls != 1 || accountCalls != 1 || transactionCalls != 4 {
		t.Fatalf(
			"calls token=%d accounts=%d transactions=%d",
			tokenCalls,
			accountCalls,
			transactionCalls,
		)
	}
}

func TestFetchTransactionsAcceptsNoContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/v1/token":
			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3599}`))
		case "/accounts/v1/accounts":
			_, _ = w.Write([]byte(`{"accounts":[{"accountNumber":"123","accountStatus":"Active"}]}`))
		case "/tolltransaction/v1/transactions":
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	client := NewClientWithHTTPClient(server.URL, "client", "secret", server.Client())
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	values, err := client.FetchTransactions(context.Background(), start, start.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Fatalf("transactions = %#v", values)
	}
}

func TestParseTimestampAcceptsZonedAndUnzonedValues(t *testing.T) {
	for _, value := range []string{
		"2026-07-23T03:24:55Z",
		"2026-07-23T03:24:55",
		"2026-07-23T03:24:55.123",
	} {
		if _, err := ParseTimestamp(value); err != nil {
			t.Fatalf("ParseTimestamp(%q): %v", value, err)
		}
	}
}
