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
						fmt.Fprintf(w, `{"matchedDeveloperMetadata":[{"developerMetadata":{"metadataId":%d,"metadataKey":%q,"metadataValue":%q,"location":{"dimensionRange":{"sheetId":7,"startIndex":2,"endIndex":3}}}}]}`, metadataID(e.ID), metadataKey, e.ID)
					} else {
						fmt.Fprint(w, `{}`)
					}
				case strings.Contains(r.URL.Path, "/values/"):
					if strings.Contains(r.URL.Path, "!A3:") && receipt && !(lateReceipt && searches == 2) {
						json.NewEncoder(w).Encode(map[string]any{"values": [][]any{testValues(e, Tab{IDColumn: 19})}})
					} else if strings.Contains(r.URL.Path, "!A3:") {
						fmt.Fprint(w, `{"values":[["","","","","","","","","","","","","",false,false]]}`)
					} else {
						fmt.Fprint(w, `{"values":[["header"],["","Other","","10/13/2026"],["","","","","","","","","","","","","",false,false]]}`)
					}
				case strings.HasSuffix(r.URL.Path, ":batchUpdate"):
					var body struct{ Requests []map[string]json.RawMessage }
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if len(body.Requests) < 4 || body.Requests[0]["insertDimension"] != nil || body.Requests[len(body.Requests)-1]["createDeveloperMetadata"] == nil {
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

func TestDeliveryUsesNextBlankRowAndLeavesAccountingAndSideListsAlone(t *testing.T) {
	for _, compact := range []bool{false, true} {
		for _, occupied := range []string{"", "company-label", "formula", "expense-data", "accounting", "entry-id", "full-tab", "corrupt-readback"} {
			t.Run(fmt.Sprintf("compact=%t/occupied=%s", compact, occupied), func(t *testing.T) {
				tab := Tab{ID: 7, Title: "Expenses", Rows: 100, IDColumn: 19, Compact: compact}
				if compact {
					tab.IDColumn = 27
				}
				accountant := len(baseHeaders(compact)) - 1
				width := tab.IDColumn + len(extraHeaders)
				e := testEntry()
				written := false
				batches := 0
				scan := make([][]any, 90) // preformatted blanks do not place the entry at row 91
				scan[0] = []any{"Header"}
				scan[4] = make([]any, width)
				scan[4][tab.IDColumn] = "previous-mserp-entry" // ID-only rows still count
				for i := 5; i < len(scan); i++ {
					scan[i] = make([]any, accountant+1)
					scan[i][accountant-1] = false
					scan[i][accountant] = false
				}
				if occupied == "full-tab" {
					tab.Rows = 5
					scan = scan[:5]
				}
				target := make([]any, width)
				target[accountant] = false
				target[accountant-1] = false
				target[accountant+1] = "unrelated dropdown catalog"
				switch occupied {
				case "company-label":
					target[0] = "MS Express"
				case "formula":
					target[0] = `=IF(FALSE,"","")`
				case "expense-data":
					target[5] = "concurrent expense"
				case "accounting":
					target[accountant] = true
				case "entry-id":
					target[tab.IDColumn] = "concurrent entry"
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					switch {
					case strings.HasSuffix(r.URL.Path, "/developerMetadata:search"):
						if written {
							fmt.Fprintf(w, `{"matchedDeveloperMetadata":[{"developerMetadata":{"metadataKey":%q,"metadataValue":%q,"location":{"dimensionRange":{"sheetId":7,"startIndex":5,"endIndex":6}}}}]}`, metadataKey, e.ID)
						} else {
							fmt.Fprint(w, `{}`)
						}
					case strings.Contains(r.URL.Path, "/values/"):
						if r.URL.Query().Get("valueRenderOption") != "FORMULA" {
							t.Error("must detect formulas even when their result is blank")
						}
						values := scan
						if strings.Contains(r.URL.Path, "!A6:") {
							values = [][]any{target}
							if written {
								values = [][]any{testValues(e, tab)}
								values[0][0] = target[0] // Company remains owned by the sheet.
								if occupied == "corrupt-readback" {
									values[0][2] = ""
								}
							}
						} else if !strings.Contains(r.URL.Path, "!A1:") {
							t.Errorf("unexpected row %s", r.URL.Path)
						}
						json.NewEncoder(w).Encode(map[string]any{"values": values})
					case strings.HasSuffix(r.URL.Path, ":batchUpdate"):
						batches++
						var body struct {
							Requests []map[string]map[string]json.RawMessage
						}
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						for _, request := range body.Requests {
							for kind, fields := range request {
								if kind == "insertDimension" || kind == "appendDimension" {
									t.Error("must not insert/append whole rows")
								}
								for _, name := range []string{"range", "source", "destination"} {
									if raw := fields[name]; raw != nil {
										var g struct{ StartRowIndex, EndRowIndex, StartColumnIndex, EndColumnIndex int }
										json.Unmarshal(raw, &g)
										if g.StartColumnIndex == 0 {
											t.Error("request touches the sheet's company column", kind, name)
										}
										if !(g.EndColumnIndex <= accountant-1 || g.StartColumnIndex >= tab.IDColumn) {
											t.Error("request touches accounting or side-list columns", kind, name)
										}
										if name != "source" && (g.StartRowIndex != 5 || g.EndRowIndex != 6) {
											t.Error("must write immediately after latest expense", g)
										}
									}
								}
							}
						}
						written = true
						fmt.Fprint(w, `{}`)
					default:
						t.Errorf("unexpected request %s", r.URL.Path)
					}
				}))
				defer server.Close()
				c := Client{HTTP: server.Client(), baseURL: server.URL + "/"}
				err := c.Deliver(context.Background(), e, tab)
				if occupied == "" || occupied == "company-label" {
					if err != nil || batches != 1 {
						t.Fatalf("err=%v batches=%d", err, batches)
					}
				} else if occupied == "corrupt-readback" {
					if err == nil || batches != 1 {
						t.Fatalf("readback mismatch must hold: err=%v batches=%d", err, batches)
					}
				} else if err == nil || batches != 0 {
					t.Fatalf("occupied row must be held without writes: err=%v batches=%d", err, batches)
				}
			})
		}
	}
}

// Simulated Sheets values for a completed literal write.
func testValues(e Entry, t Tab) []any {
	cells, _ := entryCells(e, t)
	v := make([]any, len(cells))
	for i, cell := range cells {
		value := cell.(map[string]any)["userEnteredValue"]
		if value == nil {
			continue
		}
		for _, x := range value.(map[string]any) {
			v[i] = x
		}
	}
	return v
}
