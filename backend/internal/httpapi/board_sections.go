package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/andybalholm/brotli"

	"mserp/internal/db"
)

// Keep compression off the database path and bound CPU during simultaneous openings.
// Excess requests use the existing proxy gzip path without waiting.
var boardCompressionSlot = make(chan struct{}, 1)

// The compression slot has one owner. Keep at most its last successful
// encoder, and release the workspace after the board has been idle.
var idleBoardEncoder atomic.Pointer[brotli.Writer]
var boardEncoderExpiry *time.Timer

func closeBoardEncoder(encoder *brotli.Writer, bodySize int) {
	if err := encoder.Close(); err != nil || bodySize > 1<<20 {
		return
	}
	// Close detaches the response writer. Reset starts a new stream on reuse.
	idleBoardEncoder.Store(encoder)
	if boardEncoderExpiry == nil {
		boardEncoderExpiry = time.AfterFunc(time.Minute, func() { idleBoardEncoder.Store(nil) })
	} else {
		boardEncoderExpiry.Reset(time.Minute)
	}
}

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
	w.Header().Add("Vary", "Accept-Encoding")
	body, err := json.Marshal(response)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to encode board")
		return
	}
	body = append(body, '\n')
	var dst io.Writer = w
	if len(body) >= 32*1024 && strings.Contains(strings.ToLower(r.Header.Get("Accept-Encoding")), "br") {
		select {
		case boardCompressionSlot <- struct{}{}:
			defer func() { <-boardCompressionSlot }()
			encoder := brotli.HTTPCompressorWithLevel(w, r, 5)
			if br, ok := encoder.(*brotli.Writer); ok {
				if previous := idleBoardEncoder.Swap(nil); previous != nil {
					previous.Reset(w)
					br = previous
				}
				dst = br
				defer closeBoardEncoder(br, len(body))
				db.Event(r.Context(), "board_response_brotli")
			} else {
				dst = encoder
				defer encoder.Close()
			}
		default:
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = dst.Write(body)
}
