package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"

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

func TestBoardCompressionUsesProjectedBytes(t *testing.T) {
	board := map[string]any{
		"drivers": []any{map[string]any{"fullName": "Example", "payType": "cpm", "address": "PRIVATE", "payRate": 0.7}},
		"entries": []string{strings.Repeat("dispatch note ", 4000)},
	}
	read := func(enc string, permissions ...string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/gross-board?opening=tab", nil)
		r.Header.Set(boardSectionsHeader, "v1")
		r.Header.Set("Accept-Encoding", enc)
		r = r.WithContext(context.WithValue(r.Context(), authContextKey{}, repository.AuthSession{User: repository.AuthUser{Permissions: permissions}}))
		out := httptest.NewRecorder()
		writeBoardSnapshot(&permissionResponseWriter{ResponseWriter: out, request: r}, r, board)
		return out
	}
	private := read("br", "board.read", "drivers.private.read")
	first, err := io.ReadAll(brotli.NewReader(private.Body))
	if err != nil || !bytes.Contains(first, []byte("PRIVATE")) {
		t.Fatal("privileged board did not decode with its authorized address", err)
	}
	identity := read("identity")
	compressed := read("gzip, br")
	if compressed.Code != 200 || compressed.Header().Get("Content-Encoding") != "br" || compressed.Body.Len() >= identity.Body.Len() {
		t.Fatal("expected a smaller negotiated board", compressed.Code, compressed.Header(), compressed.Body.Len())
	}
	body, err := io.ReadAll(brotli.NewReader(compressed.Body))
	if err != nil || !bytes.Equal(body, identity.Body.Bytes()) {
		t.Fatalf("compressed representation differs from the authorized snapshot: decoded=%d original=%d err=%v", len(body), identity.Body.Len(), err)
	}
	if bytes.Contains(body, []byte("PRIVATE")) {
		t.Fatal("private fields appeared in compressed snapshot")
	}
	if read("gzip;q=1, br;q=0.5").Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("preferred gzip coding was lost")
	}
	if read("br;q=0").Header().Get("Content-Encoding") != "" {
		t.Fatal("unacceptable coding")
	}
	board["entries"] = []string{strings.Repeat("large board ", 100000)}
	read("br")
	if idleBoardEncoder.Load() != nil {
		t.Fatal("oversized board retained its compression workspace")
	}
	board["entries"] = []string{strings.Repeat("dispatch note ", 4000)}
	boardCompressionSlot <- struct{}{}
	defer func() { <-boardCompressionSlot }()
	if fallback := read("br"); fallback.Header().Get("Content-Encoding") != "" || !bytes.Equal(fallback.Body.Bytes(), body) {
		t.Fatal("busy encoder delayed or changed board response")
	}
}
