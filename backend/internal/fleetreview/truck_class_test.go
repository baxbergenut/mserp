package fleetreview

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTruckClassDraftUpgrade(t *testing.T) {
	for _, frozen := range []bool{false, true} {
		t.Run(map[bool]string{false: "draft", true: "frozen"}[frozen], func(t *testing.T) {
			source := Snapshot{Trucks: []Record{{"id": "local", "vin": "same-vin"}}}
			receiver := Snapshot{Trucks: []Record{{"id": "remote", "vin": "same-vin", "truckClass": "class_8"}}}
			s := initial(source, receiver, Policy{})
			if s.Rows[0].Values["truckClass"] != "8" || receiver.Trucks[0]["truckClass"] != "class_8" {
				t.Fatal("initial draft must normalize without changing snapshot")
			}
			s.Rows[0].Values["truckClass"] = "class_8" // Existing saved draft from the previous build.
			s.Rows[0].Reviewed = true
			if frozen {
				s.Batch = &Batch{ID: "batch", Hash: "immutable", State: "needs_attention"}
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "state.json")
			if err := writePrivate(path, s); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			cfg := Config{Origin: "https://example.test", Directory: dir, Username: "reviewer", PasswordHash: strings.Repeat("x", 60)}
			server, err := NewServer(context.Background(), nil, cfg)
			if err != nil {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(path)
			if frozen {
				if string(before) != string(after) || digest(server.state) != digest(s) {
					t.Fatal("frozen plan changed")
				}
				return
			}
			s.Rows[0].Values["truckClass"] = "8"
			s.Version++
			var saved State
			if err := json.Unmarshal(after, &saved); err != nil || digest(saved) != digest(s) || digest(server.state) != digest(s) {
				t.Fatal("upgrade must only change the class and version, preserving reviews and snapshots")
			}
			if _, err := NewServer(context.Background(), nil, cfg); err != nil {
				t.Fatal(err)
			}
			again, _ := os.ReadFile(path)
			if string(again) != string(after) {
				t.Fatal("upgrade must be idempotent")
			}
		})
	}
	for _, value := range []string{"8", "2a", "", "class_9", "not-a-class"} {
		if truckClass(value) != value {
			t.Fatalf("unexpected conversion of %q", value)
		}
	}
}
