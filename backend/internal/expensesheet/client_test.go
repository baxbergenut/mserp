package expensesheet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testEntry() Entry {
	return Entry{ID: "123e4567-e89b-12d3-a456-426614174000", Spreadsheet: "test", SheetID: 7, Payload: json.RawMessage(`{"expense":{"id":"123e4567-e89b-12d3-a456-426614174000","expense_date":"2026-10-14","amount":503.03,"unit_number":"001","driver_name":"=IMPORTXML(\"secret\")","expense_type":"Electrical","description":"Lamp","created_at":"2026-10-14T12:00:00Z","manager_verified":true},"maintenance":{"id":"work-id","service_date":"2026-10-13","odometer":150001,"vendor_name":"Shop","location":"roadside","services":[{"typeCode":"electrical","amount":"503.03"}],"oil_type":"","status":"completed"}}`)}
}
func TestEntryPreservesAmountsIDsAndLiteralText(t *testing.T) {
	e := testEntry()
	cells, err := entryCells(e, Tab{IDColumn: 19})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(cells)
	if strings.Contains(string(data), "formulaValue") || !strings.Contains(string(data), `"numberValue":503.03`) {
		t.Fatal(string(data))
	}
	for i, want := range map[int]string{4: "001", 5: `=IMPORTXML("secret")`, 19: e.ID, 21: "work-id", 22: "2026-10-13", 23: "150001", 24: "Shop"} {
		if got := cells[i].(map[string]any)["userEnteredValue"].(map[string]any)["stringValue"]; got != want {
			t.Fatalf("column %d: %v, want %s", i, got, want)
		}
	}
	hr, err := entryCells(e, Tab{IDColumn: 27, Compact: true})
	if err != nil {
		t.Fatal(err)
	}
	if hr[27].(map[string]any)["userEnteredValue"].(map[string]any)["stringValue"] != e.ID {
		t.Fatal("HR identity column changed")
	}
}
func TestUncertainCommitReconcilesWithoutSecondInsert(t *testing.T) {
	for _, lateReceipt := range []bool{false, true} {
		t.Run(fmt.Sprint(lateReceipt), func(t *testing.T) {
			e := testEntry()
			receipt := false
			batches := 0
			inserted := 0
			searches := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/developerMetadata:search"):
					searches++
					// Simulate an old read racing the completion of the first uncertain batch.
					if receipt && !(lateReceipt && searches == 2) {
						fmt.Fprintf(w, `{"matchedDeveloperMetadata":[{"developerMetadata":{"metadataId":%d,"metadataKey":%q,"metadataValue":%q,"location":{"dimensionRange":{"sheetId":7}}}}]}`, metadataID(e.ID), metadataKey, e.ID)
					} else {
						fmt.Fprint(w, `{}`)
					}
				case strings.Contains(r.URL.Path, "/values/"):
					fmt.Fprint(w, `{"values":[["header"],["","Other","","10/13/2026"],["","","","","","","","","","","","","",false,false]]}`)
				case strings.HasSuffix(r.URL.Path, ":batchUpdate"):
					var body struct{ Requests []map[string]json.RawMessage }
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if len(body.Requests) < 4 || body.Requests[0]["insertDimension"] == nil || body.Requests[len(body.Requests)-1]["createDeveloperMetadata"] == nil {
						t.Error("row and receipt must share one batch")
					}
					batches++
					if receipt {
						w.WriteHeader(400)
						return
					} // Unique ID rejects the WHOLE duplicate batch.
					receipt = true
					inserted++
					w.WriteHeader(503) // Commit succeeded, response was lost.
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			client := Client{HTTP: server.Client(), baseURL: server.URL + "/"}
			tab := Tab{ID: 7, Title: "Expenses", Rows: 10, IDColumn: 19}
			if err := client.Deliver(context.Background(), e, tab); err == nil {
				t.Fatal("expected uncertain response")
			}
			err := client.Deliver(context.Background(), e, tab)
			if lateReceipt {
				if err == nil {
					t.Fatal("expected duplicate batch rejection")
				}
				err = client.Deliver(context.Background(), e, tab)
			}
			if err != nil || inserted != 1 {
				t.Fatalf("err=%v inserted=%d batches=%d", err, inserted, batches)
			}
		})
	}
}
func TestMetadataCollisionStopsBeforeWrite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/developerMetadata:search") {
			t.Fatal("collision must not write")
		}
		fmt.Fprint(w, `{"matchedDeveloperMetadata":[{"developerMetadata":{"metadataKey":"other","metadataValue":"other"}}]}`)
	}))
	defer server.Close()
	c := Client{HTTP: server.Client(), baseURL: server.URL + "/"}
	if err := c.Deliver(context.Background(), testEntry(), Tab{}); err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatal(err)
	}
}
func TestPrepareRefusesChangedHeaders(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			writes++
			fmt.Fprint(w, `{}`)
			return
		}
		if strings.Contains(r.URL.Path, "/values/") {
			fmt.Fprint(w, `{"values":[["Company","Wrong header"]]}`)
			return
		}
		fmt.Fprint(w, `{"sheets":[{"properties":{"sheetId":7,"title":"Changed title","gridProperties":{"rowCount":10,"columnCount":19}}},{"properties":{"sheetId":8,"title":"HR","gridProperties":{"rowCount":10,"columnCount":27}}}]}`)
	}))
	defer server.Close()
	c := Client{HTTP: server.Client(), baseURL: server.URL + "/"}
	if _, err := c.Prepare(context.Background(), "test", 7, 8, 9, true, true); err == nil || writes != 0 {
		t.Fatalf("err=%v writes=%d", err, writes)
	}
}
