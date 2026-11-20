package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"mserp/internal/repository"
)

func TestBoardSectionsFollowDataAndPermissions(t *testing.T) {
	data := map[string]any{
		"drivers": []any{map[string]any{"fullName": "Example", "payType": "cpm", "payRate": 0.7, "address": "Private address"}},
		"entries": []string{"original"}, "planningEntries": []string{"Dispatch plan"}, "weekStart": "2026-11-16",
	}
	read := func(offer string, permissions ...string) (map[string]json.RawMessage, map[string]string) {
		t.Helper()
		r := httptest.NewRequest("GET", "/gross-board?opening=tab", nil)
		r.Header.Set(boardSectionsHeader, offer)
		r = r.WithContext(context.WithValue(r.Context(), authContextKey{}, repository.AuthSession{User: repository.AuthUser{Permissions: permissions}}))
		out := httptest.NewRecorder()
		writeBoardSnapshot(&permissionResponseWriter{ResponseWriter: out, request: r}, r, data)
		var envelope struct {
			Opening string
			Board   map[string]json.RawMessage
		}
		if err := json.Unmarshal(out.Body.Bytes(), &envelope); err != nil || out.Code != 200 || envelope.Opening != "tab" {
			t.Fatal(out.Code, err)
		}
		var tags map[string]string
		if err := json.Unmarshal(envelope.Board["_sections"], &tags); err != nil {
			t.Fatal(err)
		}
		return envelope.Board, tags
	}
	full, tags := read("v1", "board.read", "drivers.private.read", "driver_compensation.read")
	if !strings.Contains(string(full["drivers"]), "Private address") || full["planningEntries"] == nil {
		t.Fatal("missing authorized fields")
	}
	offer := "v1"
	for k, v := range tags {
		offer += "," + k + "=" + v
	}
	repeat, _ := read(offer, "board.read", "drivers.private.read", "driver_compensation.read")
	if len(repeat) != 2 || repeat["weekStart"] == nil {
		t.Fatal("unchanged sections sent", repeat)
	}
	data["entries"] = []string{"updated"}
	limited, updated := read(offer)
	if string(limited["entries"]) != `["updated"]` || updated["entries"] == tags["entries"] || updated["drivers"] == tags["drivers"] ||
		limited["drivers"] == nil || strings.Contains(string(limited["drivers"]), "payRate") || strings.Contains(string(limited["drivers"]), "Private address") ||
		limited["planningEntries"] != nil || updated["planningEntries"] != "" {
		t.Fatal("stale or unauthorized section", limited)
	}
}
