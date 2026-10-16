package httpapi

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestGrossLiveWeekCacheAndPermissions(t *testing.T) {
	h := newDatabaseEvents(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), "mserp_gross_board", "board.read")
	defer h.shutdown()
	h.snapshotEvent = "grossBoard"
	h.snapshotKey = func(ctx context.Context) string {
		return ctx.Value(grossWeekContext{}).(time.Time).Format(time.DateOnly)
	}
	calls := 0
	h.snapshot = func(ctx context.Context) ([]byte, error) {
		calls++
		return []byte(fmt.Sprintf(`{"weekStart":%q}`, h.snapshotKey(ctx))), nil
	}
	a, _ := time.Parse(time.DateOnly, "2026-10-12")
	b := a.AddDate(0, 0, 7)
	for _, w := range []time.Time{a, b, a, b} {
		if !strings.Contains(h.frame(context.WithValue(context.Background(), grossWeekContext{}, w), "changed"), w.Format(time.DateOnly)) {
			t.Fatal("snapshot crossed week boundaries")
		}
	}
	if calls != 2 {
		t.Fatal("week snapshots were not shared", calls)
	}
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		path := "/gross-board/presence"
		if method == "DELETE" {
			path += "/tab"
		}
		if routePermission(method, path) != "board.read" {
			t.Fatal("presence requires board visibility", method)
		}
	}
	if routePermission("GET", "/gross-board/events") != "board.read" || routePermission("PUT", "/gross-board") != "board.write" {
		t.Fatal("live presence granted data writes")
	}
}
