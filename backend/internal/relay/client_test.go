package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchTransactions(t *testing.T) {
	start := time.Date(2026, time.July, 19, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "test-key" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.URL.Query().Get("dtstart"); got != "2026-07-19T00:00:00Z" {
			t.Fatalf("dtstart = %q", got)
		}
		if got := r.URL.Query().Get("dtend"); got != "2026-07-20T00:00:00Z" {
			t.Fatalf("dtend = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"transaction_id":"txn_1","created_at":"2026-07-19T12:30:00Z","total_amount_paid":"120.50","total_retail_price":"130.00","total_amount_saved":"9.50","is_direct_bill":false,"currency_code":"USD","driver":{"id":"drv_1","first_name":"Jane","last_name":"Doe","phone":"+15555550100","integration_id":"1234"},"merchant":{"id":"org_1","name":"Pilot","number":"9"},"location":{"id":"loc_1","name":"Pilot #1","fuel_merchant_location_id":"1","address":"1 Main St","city":"Atlanta","state":"GA","zip_code":"30301","latitude":33.7,"longitude":-84.3,"timezone":"America/New_York"},"fuel_items":[],"products":[],"fees":[]}]`))
	}))
	defer server.Close()

	client := NewClientWithHTTPClient(server.URL+"/api", "test-key", server.Client())
	transactions, err := client.FetchTransactions(context.Background(), start, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(transactions) != 1 || transactions[0].TransactionID != "txn_1" {
		t.Fatalf("transactions = %#v", transactions)
	}
}

func TestFetchTransactionsIncludesRelayError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"invalid date range"}`, http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewClientWithHTTPClient(server.URL, "test-key", server.Client())
	_, err := client.FetchTransactions(context.Background(), time.Now(), time.Now())
	if err == nil {
		t.Fatal("expected an error")
	}
}
