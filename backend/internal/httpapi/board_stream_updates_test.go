package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"mserp/internal/repository"
)

func TestBoardStreamProjectsAndReplacesChangedRows(t *testing.T) {
	h := newDatabaseEvents(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), "test", "driver_board.read")
	defer h.shutdown()
	h.snapshotEvent = "board"
	body := `{"entries":[{"notes":"one"},{"notes":"two"},{"notes":"three"}],"loads":{"a":{"next":[]},"b":{"next":[]}},"pendingDrivers":[{"fullName":"Private hire"}]}`
	h.snapshot = func(context.Context) ([]byte, error) { return []byte(body), nil }
	h.projectSnapshot = func(ctx context.Context, board map[string]any) error {
		session, _ := authSessionFromContext(ctx)
		if !session.User.Administrator {
			board["pendingDrivers"] = []any{}
		}
		return nil
	}
	updates := new(boardStreamEncoder)
	frame := func(admin bool) string {
		ctx := context.WithValue(context.Background(), authContextKey{}, repository.AuthSession{User: repository.AuthUser{Administrator: admin}})
		return h.viewerFrame(ctx, "changed", updates)
	}
	initial := frame(false)
	if !strings.HasPrefix(initial, "id: 1\nevent: board\n") || strings.Contains(initial, "Private hire") {
		t.Fatal("incorrect initial projection", initial)
	}
	if frame(false) != "" {
		t.Fatal("an unchanged board produced an update")
	}
	body = strings.ReplaceAll(body, `"two"`, `"edited"`)
	body = strings.Replace(body, `"b":{"next":[]}`, `"b":{"next":["new"]}`, 1)
	h.publish()
	change := frame(false)
	if !strings.HasPrefix(change, "id: 2\nevent: boardPatch\n") || strings.Contains(change, "three") || strings.Contains(change, "Private hire") {
		t.Fatal("unrelated row or sensitive field in patch", change)
	}
	var patch boardStreamPatch
	_, data, _ := strings.Cut(change, "\ndata: ")
	if err := json.Unmarshal([]byte(data), &patch); err != nil || patch.Base != 1 || len(patch.Rows["entries"]) != 1 || patch.Rows["entries"][1].(map[string]any)["notes"] != "edited" || len(patch.Fields["loads"]) != 1 || patch.Fields["loads"]["b"] == nil {
		t.Fatal("wrong patch", change, err)
	}
	if got := frame(true); !strings.Contains(got, "Private hire") {
		t.Fatal("grant did not restore visible row", got)
	}
	if got := frame(false); strings.Contains(got, "Private hire") || !strings.Contains(got, `"pendingDrivers":[]`) {
		t.Fatal("revocation did not remove row", got)
	}
	body = strings.Replace(body, `,"b":{"next":["new"]}`, "", 1)
	h.publish()
	if got := frame(false); !strings.Contains(got, `"set":{"loads":{"a":{"next":[]}}}`) {
		t.Fatal("removed map member persisted", got)
	}
	body = strings.Replace(body, `"entries":`, `"other":`, 1)
	h.publish()
	if got := frame(false); !strings.Contains(got, "event: board\n") || strings.Contains(got, `"entries":`) {
		t.Fatal("root change requires a new full board", got)
	}
}
