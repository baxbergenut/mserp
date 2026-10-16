// Package fleetreview implements the temporary, single-company reconciliation
// portal. Drafts are not source data. Only a reviewed, versioned batch may write.
package fleetreview

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Record map[string]string
type Snapshot struct {
	Drivers []Record `json:"drivers"`
	Trucks  []Record `json:"trucks"`
	Owners  []Record `json:"owners"`
}
type Row struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	LocalID  string `json:"localId"`
	RemoteID string `json:"remoteId"`
	Action   string `json:"action"`
	Values   Record `json:"values"`
	Reviewed bool   `json:"reviewed"`
	Note     string `json:"note"`
	Evidence string `json:"evidence"`
}
type Batch struct {
	ID       string `json:"id"`
	Hash     string `json:"hash"`
	State    string `json:"state"`
	Error    string `json:"error"`
	Started  string `json:"started"`
	Finished string `json:"finished"`
}
type Policy struct {
	RejectedPairs [][2]string       `json:"rejectedPairs"`
	Overrides     map[string]Record `json:"overrides"`
	Notes         map[string]string `json:"notes"`
}
type State struct {
	Policy     Policy   `json:"policy"`
	Version    int      `json:"version"`
	CapturedAt string   `json:"capturedAt"`
	Source     Snapshot `json:"source"`
	Receiver   Snapshot `json:"receiver"`
	Rows       []Row    `json:"rows"`
	Batch      *Batch   `json:"batch"`
}

func token() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func id() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func norm(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
func copyRecord(r Record) Record {
	c := Record{}
	for k, v := range r {
		c[k] = v
	}
	return c
}
func find(rows []Record, key string) Record {
	for _, r := range rows {
		if r["id"] == key {
			return r
		}
	}
	return nil
}
func fields(kind string) []string {
	if kind == "driver" {
		return []string{"firstName", "lastName", "email", "phone", "status", "terminationDate"}
	}
	return []string{"unit", "vin", "year", "make", "model", "plate", "plateState", "active", "status", "ownerId", "ownerDriverId", "ownerDate", "color", "truckClass", "gvwr"}
}
func names(s string) []string { v := strings.Fields(norm(s)); sort.Strings(v); return v }
func matchScore(a, b Record, kind string) int {
	if kind == "truck" {
		if a["vin"] != "" && strings.EqualFold(a["vin"], b["vin"]) {
			return 100
		}
		return 0
	}
	if a["fleetScopeId"] == b["id"] {
		return 200
	}
	if norm(a["name"]) == norm(b["name"]) {
		return 100
	}
	if strings.Join(names(a["name"]), " ") == strings.Join(names(b["name"]), " ") {
		return 90
	}
	shorter, longer := names(a["name"]), names(b["name"])
	if len(shorter) > len(longer) {
		shorter, longer = longer, shorter
	}
	if len(shorter) >= 2 {
		all := true
		for _, n := range shorter {
			found := false
			for _, m := range longer {
				if n == m {
					found = true
				}
			}
			all = all && found
		}
		if all {
			return 80
		}
	}
	if a["email"] != "" && norm(a["email"]) == norm(b["email"]) {
		return 30
	}
	if a["phone"] != "" && phone(a["phone"]) == phone(b["phone"]) {
		return 20
	}
	return 0
}
func phone(s string) string {
	s = strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
	if len(s) == 11 && s[0] == '1' {
		s = s[1:]
	}
	return s
}
func initial(source, remote Snapshot, policy Policy) State {
	s := State{Policy: policy, Version: 1, CapturedAt: time.Now().UTC().Format(time.RFC3339), Source: source, Receiver: remote, Rows: []Row{}}
	for _, kind := range []string{"driver", "truck"} {
		locals, remotes := source.Drivers, remote.Drivers
		if kind == "truck" {
			locals, remotes = source.Trucks, remote.Trucks
		}
		for _, a := range locals {
			r := Row{ID: kind + ":" + a["id"], Kind: kind, LocalID: a["id"], Action: "update", Values: copyRecord(a)}
			best, n := 0, 0
			var target Record
			for _, b := range remotes {
				score := matchScore(a, b, kind)
				for _, pair := range policy.RejectedPairs {
					if pair[0] == a["id"] && pair[1] == b["id"] {
						score = 0
					}
				}
				if score > best {
					best, n, target = score, 1, b
				} else if score == best && score > 0 {
					n++
				}
			}
			if best > 0 && n == 1 {
				r.RemoteID = target["id"]
				r.Evidence = "Suggested match; confirm this is the same record"
				if best >= 100 {
					r.Evidence = "Exact identity/name candidate; review required"
				}
				if best < 80 {
					r.Evidence = "Contact-only candidate with different name: identity confirmation required"
				}
			} else {
				r.Action = "create"
				r.Evidence = "No unique match; check the full FleetScope list before creating"
			}
			if kind == "driver" {
				// Never guess given/family names from a full name. Reuse an existing structured
				// name only when it has the exact same token set; otherwise the reviewer enters it.
				r.Values["firstName"], r.Values["lastName"] = "", ""
				if target != nil && strings.Join(names(a["name"]), " ") == strings.Join(names(target["name"]), " ") {
					r.Values["firstName"], r.Values["lastName"] = target["firstName"], target["lastName"]
				}
				if a["status"] == "terminated" && r.RemoteID == "" {
					r.Action = "defer"
					r.Note = "Historical terminated driver: do not create in FleetScope"
				}
			} else {
				if target != nil {
					for _, k := range []string{"year", "make", "model", "plate", "plateState", "color", "truckClass", "gvwr"} {
						if r.Values[k] == "" {
							r.Values[k] = target[k]
						}
					}
				}
			}
			for k, v := range policy.Overrides[r.ID] {
				r.Values[k] = v
			}
			if note := policy.Notes[r.ID]; note != "" {
				r.Note = note
			}
			s.Rows = append(s.Rows, r)
		}
	}
	return s
}
func validateRow(r Row, s State) error {
	source, remote := s.Source.Drivers, s.Receiver.Drivers
	if r.Kind == "truck" {
		source, remote = s.Source.Trucks, s.Receiver.Trucks
	} else if r.Kind != "driver" {
		return errors.New("invalid record type")
	}
	a := find(source, r.LocalID)
	if a == nil || r.ID != r.Kind+":"+r.LocalID {
		return errors.New("unknown source record")
	}
	if r.Action == "defer" {
		if strings.TrimSpace(r.Note) == "" {
			return errors.New("give a reason for deferring this record")
		}
		return nil
	}
	if r.Action != "update" && r.Action != "create" {
		return errors.New("choose update, create, or defer")
	}
	if r.Action == "update" && find(remote, r.RemoteID) == nil {
		return errors.New("select an existing FleetScope record")
	}
	if r.Action == "create" && r.RemoteID != "" {
		return errors.New("new records cannot claim an existing identity")
	}
	v := r.Values
	if r.Kind == "driver" {
		if a["identityIssue"] == "true" {
			return errors.New("multiple completed hire identities require operator review before this driver can be reconciled")
		}
		for _, other := range source {
			if r.RemoteID != "" && other["id"] != r.LocalID && other["fleetScopeId"] == r.RemoteID {
				return errors.New("this FleetScope identity already belongs to another MSERP driver")
			}
		}
		if r.Action == "create" {
			candidate := copyRecord(a)
			candidate["name"] = strings.TrimSpace(v["firstName"] + " " + v["lastName"])
			for _, existing := range remote {
				if matchScore(a, existing, "driver") >= 80 || matchScore(candidate, existing, "driver") >= 80 {
					return errors.New("a matching name already exists in FleetScope; review that identity before creating another driver")
				}
			}
		}
		for _, k := range []string{"firstName", "lastName"} {
			if len([]rune(v[k])) < 1 || len([]rune(v[k])) > 50 {
				return errors.New("enter given and family names, each up to 50 characters")
			}
		}
		if _, e := mail.ParseAddress(v["email"]); e != nil || len(v["email"]) > 100 || strings.ContainsAny(v["email"], "<>\r\n") {
			return errors.New("a valid email address is required")
		}
		if len(phone(v["phone"])) != 10 || len(v["phone"]) > 20 {
			return errors.New("a valid phone number is required")
		}
		if v["status"] != "active" && v["status"] != "terminated" && v["status"] != "home" && v["status"] != "vacation" {
			return errors.New("choose active, home, vacation or terminated")
		}
		if v["status"] == "terminated" {
			if r.Action == "create" {
				return errors.New("do not create missing terminated drivers")
			}
			if _, e := time.Parse(time.DateOnly, v["terminationDate"]); e != nil {
				return errors.New("termination date is required")
			}
		}
		if a["fleetScopeId"] != "" && a["fleetScopeId"] != r.RemoteID {
			return errors.New("existing hire identity cannot be replaced here")
		}
		for _, pair := range s.Policy.RejectedPairs {
			if pair[0] == r.LocalID && pair[1] == r.RemoteID {
				return errors.New("this identity pairing was explicitly rejected by the owner")
			}
		}
	} else {
		for k, max := range map[string]int{"unit": 20, "make": 50, "model": 50, "plate": 15, "color": 30, "truckClass": 20} {
			if len(v[k]) < 1 || len(v[k]) > max {
				return errors.New(k + " is required and must fit FleetScope's field limit")
			}
		}
		if len(v["vin"]) != 17 || strings.ContainsAny(v["vin"], "IOQioq") || strings.Trim(v["vin"], "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
			return errors.New("a real 17-character uppercase VIN is required")
		}
		y, e := strconv.Atoi(v["year"])
		if e != nil || y < 1900 || y > time.Now().Year()+2 {
			return errors.New("valid model year required")
		}
		if !strings.Contains("|AL|AK|AZ|AR|CA|CO|CT|DE|DC|FL|GA|HI|ID|IL|IN|IA|KS|KY|LA|ME|MD|MA|MI|MN|MS|MO|MT|NE|NV|NH|NJ|NM|NY|NC|ND|OH|OK|OR|PA|RI|SC|SD|TN|TX|UT|VT|VA|WA|WV|WI|WY|AB|BC|MB|NB|NL|NS|NT|NU|ON|PE|QC|SK|YT|", "|"+v["plateState"]+"|") || len(v["plateState"]) != 2 {
			return errors.New("valid plate state/province required")
		}
		weight, e := strconv.Atoi(v["gvwr"])
		if e != nil || weight <= 0 || weight > 200000 {
			return errors.New("valid GVWR in pounds required")
		}
		if !strings.Contains("|1|2|2a|2b|3|4|5|6|7|8|", "|"+strings.ToLower(v["truckClass"])+"|") {
			return errors.New("choose a valid truck class (1–8)")
		}
		for _, k := range []string{"vin", "plate", "make", "model"} {
			value := strings.ToUpper(strings.TrimSpace(v[k]))
			if value == "UNKNOWN" || value == "TBD" || value == "N/A" || value == "NONE" || value == "TEST" || strings.Trim(value, "0") == "" || strings.Trim(value, "B") == "" {
				return errors.New(k + " needs a real value; save an incomplete draft instead of a placeholder")
			}
		}
		if (v["ownerId"] == "") == (v["ownerDriverId"] == "") {
			return errors.New("choose exactly one owner")
		}
		if v["ownerId"] != "" && find(s.Source.Owners, v["ownerId"]) == nil {
			return errors.New("unknown owner")
		}
		if v["ownerDriverId"] != "" && find(s.Source.Drivers, v["ownerDriverId"]) == nil {
			return errors.New("unknown owner driver")
		}
		if v["ownerId"] != a["ownerId"] || v["ownerDriverId"] != "" {
			if _, e := time.Parse(time.DateOnly, v["ownerDate"]); e != nil {
				return errors.New("ownership changes require an effective date")
			}
		}
		if r.Action == "update" && norm(v["vin"]) != norm(find(remote, r.RemoteID)["vin"]) {
			return errors.New("VIN does not match the selected FleetScope truck; correct identity before linking")
		}
		if v["active"] != "true" && v["active"] != "false" {
			return errors.New("choose active or inactive")
		}
		switch v["status"] {
		case "available", "assigned", "maintenance", "out_of_service":
		default:
			return errors.New("invalid operational status")
		}
	}
	return nil
}
