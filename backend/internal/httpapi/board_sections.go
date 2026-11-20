package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/http"
	"strings"

	"mserp/internal/db"
)

const boardSectionsHeader = "X-Clustie-Board-Sections"

var boardSectionNames = [...]string{"drivers", "entries", "grossEntries", "plans", "loads", "balances", "planningEntries", "nextDayDestinations"}

// Compare the exact representation authorized for this request. A missing
// field carries no tag, even if the previous viewer could see that field.
func writeBoardSections(w http.ResponseWriter, r *http.Request, value any, previous string) {
	defer db.Phase(r.Context(), "board_section_encoding")()
	if projector, ok := w.(interface{ ProjectJSON(any) any }); ok {
		value = projector.ProjectJSON(value)
	}
	fields, ok := value.(map[string]any)
	if ok {
		fields = maps.Clone(fields)
	} else {
		// With full field access the projector can return the original typed board.
		encoded, err := json.Marshal(value)
		var raw map[string]json.RawMessage
		if err != nil || json.Unmarshal(encoded, &raw) != nil || raw == nil {
			writeAPIError(w, http.StatusInternalServerError, "Unable to encode board")
			return
		}
		fields = make(map[string]any, len(raw)+1)
		for key, body := range raw {
			fields[key] = body
		}
	}
	tags := make(map[string]string)
	offered := "," + previous + ","
	for _, name := range boardSectionNames {
		part, exists := fields[name]
		if !exists {
			continue
		}
		body, encoded := part.(json.RawMessage)
		if !encoded {
			var err error
			body, err = json.Marshal(part)
			if err != nil {
				writeAPIError(w, http.StatusInternalServerError, "Unable to encode board section")
				return
			}
			fields[name] = body
		}
		sum := sha256.Sum256(body)
		tags[name] = base64.RawURLEncoding.EncodeToString(sum[:])
		tag := name + "=" + tags[name]
		if strings.Contains(offered, ","+tag+",") {
			delete(fields, name)
			db.Event(r.Context(), "board_section_reused")
		}
	}
	fields["_sections"] = tags
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Add("Vary", boardSectionsHeader)
	var response any = fields
	if opening := r.URL.Query().Get("opening"); opening != "" {
		response = map[string]any{"opening": opening, "board": fields}
	}
	// Projection happened before computing the digests above.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}
