package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// Digests belong to one authorized stream. The first frame and reconnects
// carry the whole projected board. Subsequent frames replace changed rows in
// the exact preceding frame, independently of edits in the dispatcher's UI.
type boardStreamEncoder struct {
	sequence      int
	baselineBytes int
	last          map[string]boardSectionDigest
}

type boardSectionDigest struct {
	value  [32]byte
	rows   [][32]byte
	fields map[string][32]byte
}

type boardStreamPatch struct {
	Base   int                       `json:"base"`
	Set    map[string]any            `json:"set,omitempty"`
	Rows   map[string]map[int]any    `json:"rows,omitempty"`
	Fields map[string]map[string]any `json:"fields,omitempty"`
}

func boardValueDigest(value any) ([32]byte, error) {
	b, err := json.Marshal(value)
	return sha256.Sum256(b), err
}

func (s *boardStreamEncoder) frame(event string, board map[string]any) (string, error) {
	full := s.last == nil || len(board) != len(s.last)
	next := make(map[string]boardSectionDigest, len(board))
	p := boardStreamPatch{Base: s.sequence, Set: make(map[string]any), Rows: make(map[string]map[int]any), Fields: make(map[string]map[string]any)}
	for key, value := range board {
		prev, exists := s.last[key]
		if !exists {
			full = true
		}
		var digest boardSectionDigest
		switch section := value.(type) {
		case []any:
			digest.rows = make([][32]byte, len(section))
			changed := make(map[int]any)
			for i, row := range section {
				hash, err := boardValueDigest(row)
				if err != nil {
					return "", err
				}
				digest.rows[i] = hash
				if i >= len(prev.rows) || prev.rows[i] != hash {
					changed[i] = row
				}
			}
			if prev.rows == nil || len(prev.rows) != len(section) || len(changed) > len(section)/2 {
				p.Set[key] = section
			} else if len(changed) != 0 {
				p.Rows[key] = changed
			}
		case map[string]any:
			digest.fields = make(map[string][32]byte, len(section))
			changed := make(map[string]any)
			replace := prev.fields == nil || len(prev.fields) != len(section)
			for name, member := range section {
				hash, err := boardValueDigest(member)
				if err != nil {
					return "", err
				}
				digest.fields[name] = hash
				old, known := prev.fields[name]
				if !known {
					replace = true
				}
				if old != hash || !known {
					changed[name] = member
				}
			}
			if replace || len(changed) > len(section)/2 {
				p.Set[key] = section
			} else if len(changed) != 0 {
				p.Fields[key] = changed
			}
		default:
			hash, err := boardValueDigest(value)
			if err != nil {
				return "", err
			}
			digest.value = hash
			if prev.value != hash {
				p.Set[key] = value
			}
		}
		next[key] = digest
	}
	if !full && len(p.Set)+len(p.Rows)+len(p.Fields) == 0 {
		return "", nil
	}
	var payload any = p
	if full {
		payload = board
	} else {
		event = "boardPatch"
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	if full {
		s.baselineBytes = len(body)
	}
	s.last = next
	s.sequence++
	return fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", s.sequence, event, body), nil
}
