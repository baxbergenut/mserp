package fleetreview

import (
	"encoding/json"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type profileChange struct {
	Field  string `json:"field"`
	System string `json:"system"`
	Before string `json:"before"`
	After  string `json:"after"`
}

type profileRow struct {
	IdentityConfirmed bool            `json:"identityConfirmed,omitempty"`
	Deferred          string          `json:"deferred,omitempty"`
	Kind              string          `json:"kind"`
	Label             string          `json:"label"`
	LocalID           string          `json:"localId,omitempty"`
	RemoteID          string          `json:"remoteId,omitempty"`
	Changes           []profileChange `json:"changes,omitempty"`
	Review            []string        `json:"review,omitempty"`
}

const truckStatusCrewConflict = "Clustie status differs and the FleetScope truck has an assignment"

const conflictingTruckModel = "Conflicting truck model"
const truckUnitReview = "Review truck unit number"

const conflictingTruckMake = "Conflicting truck make"

const uniqueFleetScopeIdentity = "Confirm a unique FleetScope identity"

const historicalDriverWithoutMatch = "Historical driver has no verified FleetScope match"

type profilePlan struct {
	Rows     []profileRow `json:"rows"`
	Drivers  int          `json:"drivers"`
	Trucks   int          `json:"trucks"`
	Review   int          `json:"review"`
	Matching int          `json:"matching"`
}

const clustie = "Clustie"
const fleetScope = "FleetScope"

func (r *profileRow) change(system, field, before, after string) {
	if before != after {
		r.Changes = append(r.Changes, profileChange{field, system, before, after})
	}
}

func (r profileRow) values(system string) Record {
	values := Record{}
	for _, c := range r.Changes {
		if c.System == system {
			values[c.Field] = c.After
		}
	}
	return values
}

func validVIN(v string) bool {
	return len(v) == 17 && strings.Trim(strings.ToUpper(v), "0123456789ABCDEFGHJKLMNPRSTUVWXYZ") == "" &&
		strings.Trim(v, "0") != "" && strings.Trim(strings.ToUpper(v), "B") != ""
}

func verifiedContacts(a, b Record, locals, remotes []Record) bool {
	for _, field := range []string{"email", "phone"} {
		normalize := norm
		if field == "phone" {
			normalize = phone
		}
		value := normalize(a[field])
		if !validDriverField(field, a[field]) || value != normalize(b[field]) {
			continue
		}
		unique := func(rows []Record) bool {
			n := 0
			for _, other := range rows {
				if normalize(other[field]) == value {
					n++
				}
			}
			return n == 1
		}
		if unique(locals) && unique(remotes) {
			return true
		}
	}
	return false
}

func reviewedIdentity(a, b, oldA, oldB Record) bool {
	for _, pair := range [][2]Record{{a, oldA}, {b, oldB}} {
		if pair[1] == nil {
			return false
		}
		for _, field := range []string{"name", "email", "phone"} {
			if pair[0][field] != pair[1][field] {
				return false
			}
		}
	}
	return true
}

func buildProfilePlan(saved State, source, receiver Snapshot) profilePlan {
	plan := profilePlan{Rows: []profileRow{}}
	drafts := map[string]Row{}
	for _, row := range saved.Rows {
		drafts[row.ID] = row
	}
	driverLinks, driverNames := Record{}, Record{}
	for _, kind := range []string{"driver", "truck"} {
		locals, remotes := source.Drivers, receiver.Drivers
		if kind == "truck" {
			locals, remotes = source.Trucks, receiver.Trucks
		}
		matches := Record{}
		paired := map[string]bool{}
		claims := map[string]int{}
		for _, a := range locals {
			draft := drafts[kind+":"+a["id"]]
			if draft.Reviewed && (draft.Action == "defer" || draft.Action == "create") {
				continue
			}
			var candidates []string
			for _, b := range remotes {
				if kind == "truck" {
					if validVIN(a["vin"]) && strings.EqualFold(a["vin"], b["vin"]) {
						candidates = append(candidates, b["id"])
					}
					continue
				}
				if a["identityIssue"] == "true" || slices.Contains(saved.Policy.RejectedPairs, [2]string{a["id"], b["id"]}) ||
					slices.ContainsFunc(locals, func(other Record) bool {
						return other["id"] != a["id"] && other["fleetScopeId"] == b["id"]
					}) {
					continue
				}
				if a["fleetScopeId"] != "" {
					if a["fleetScopeId"] == b["id"] {
						candidates = append(candidates, b["id"])
					}
					continue
				}
				if draft.Reviewed && draft.Action == "update" && draft.RemoteID != b["id"] {
					continue
				}
				if (draft.Reviewed && draft.Action == "update" && reviewedIdentity(a, b, find(saved.Source.Drivers, a["id"]), find(saved.Receiver.Drivers, b["id"]))) ||
					(norm(a["name"]) == norm(b["name"]) && verifiedContacts(a, b, locals, remotes)) {
					candidates = append(candidates, b["id"])
				}
			}
			for _, candidate := range candidates {
				claims[candidate]++
			}
			if len(candidates) == 1 {
				matches[a["id"]] = candidates[0]
			}
		}
		for _, a := range locals {
			r := profileRow{Kind: kind, Label: a["name"], LocalID: a["id"]}
			if kind == "truck" {
				r.Label = a["unit"]
			}
			b := find(remotes, matches[a["id"]])
			draft := drafts[kind+":"+a["id"]]
			switch {
			case draft.Reviewed && draft.Action == "defer":
				r.Review = append(r.Review, "Previously deferred")
			case draft.Reviewed && draft.Action == "create":
				r.Review = append(r.Review, "Review creation separately")
			case kind == "driver" && a["status"] == "terminated" && b == nil:
				r.Deferred = historicalDriverWithoutMatch
			case b == nil || claims[b["id"]] != 1 || (draft.Reviewed && draft.Action == "update" && draft.RemoteID != b["id"]):
				r.Review = append(r.Review, uniqueFleetScopeIdentity)
			default:
				r.RemoteID = b["id"]
				if kind == "driver" {
					driverLinks[a["id"]] = b["id"]
					planDriver(&r, a, b, source.Drivers, receiver.Drivers, receiver.Trucks)
					profile := r.values(clustie)
					if profile["firstName"] != "" || profile["lastName"] != "" {
						driverNames[a["id"]] = first(profile["firstName"], a["firstName"]) + " " + first(profile["lastName"], a["lastName"])
					}
				} else {
					planTruck(&r, a, b, source.Trucks, receiver.Trucks, driverLinks, driverNames)
				}
			}
			if r.RemoteID != "" {
				paired[r.RemoteID] = true
			}
			plan.add(r)
		}
		for _, b := range remotes {
			if paired[b["id"]] {
				continue
			}
			label := b["name"]
			if kind == "truck" {
				label = b["unit"]
			}
			plan.add(profileRow{Kind: kind, Label: label, RemoteID: b["id"], Review: []string{"No verified Clustie match"}})
		}
	}
	return plan
}

func (p *profilePlan) add(r profileRow) {
	slices.Sort(r.Review)
	r.Review = slices.Compact(r.Review)
	if len(r.Review) != 0 {
		p.Review++
	}
	if len(r.Changes) != 0 {
		if r.Kind == "driver" {
			p.Drivers++
		} else {
			p.Trucks++
		}
	}
	if r.Deferred == "" && len(r.Changes)+len(r.Review) == 0 {
		p.Matching++
		return
	}
	p.Rows = append(p.Rows, r)
}

// Display historical deferrals in older batches while preserving the stored
// plan and its receipt hash.
func (p profilePlan) forDisplay() profilePlan {
	p.Rows = slices.Clone(p.Rows)
	for i, row := range p.Rows {
		if row.Kind == "driver" && row.LocalID != "" && row.RemoteID == "" && len(row.Changes) == 0 &&
			len(row.Review) == 1 && row.Review[0] == historicalDriverWithoutMatch {
			row.Deferred = row.Review[0]
			row.Review = nil
			p.Rows[i] = row
			p.Review--
		}
	}
	return p
}

// applyIdentityConfirmations overlays the owner's review decisions on the
// completed plan. The frozen plan and both database receipts retain their hash.
func (p *profilePlan) applyIdentityConfirmations(pairs Record) {
	if len(pairs) == 0 {
		return
	}
	confirmed := make(map[string]bool, len(pairs))
	for i, row := range p.Rows {
		remote := pairs[row.LocalID]
		if row.Kind != "driver" || row.RemoteID != "" || remote == "" || !slices.Contains(row.Review, uniqueFleetScopeIdentity) {
			continue
		}
		row.RemoteID = remote
		row.IdentityConfirmed = true
		row.Review = slices.DeleteFunc(slices.Clone(row.Review), func(reason string) bool {
			return reason == uniqueFleetScopeIdentity
		})
		if len(row.Review) == 0 {
			p.Review--
		}
		p.Rows[i] = row
		confirmed[remote] = true
	}
	p.Rows = slices.DeleteFunc(p.Rows, func(row profileRow) bool {
		if row.Kind != "driver" || row.LocalID != "" || !confirmed[row.RemoteID] ||
			len(row.Review) != 1 || row.Review[0] != "No verified Clustie match" {
			return false
		}
		p.Review--
		return true
	})
}

func validDriverField(field, value string) bool {
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return false
	}
	switch field {
	case "email":
		address, err := mail.ParseAddress(value)
		return err == nil && address.Address == value && len(value) <= 100
	case "phone":
		n := phone(value)
		return len(n) == 10 && strings.Trim(n, n[:1]) != "" && len(value) <= 20
	default:
		return utf8.RuneCountInString(value) <= 50
	}
}

func planDriver(r *profileRow, a, b Record, locals, remotes, trucks []Record) {
	profile := Record{}
	for _, field := range []string{"firstName", "lastName", "email", "phone"} {
		value := strings.Join(strings.Fields(b[field]), " ")
		if value == "" {
			value = strings.Join(strings.Fields(a[field]), " ")
		}
		if !validDriverField(field, value) {
			if a[field] != "" || b[field] != "" || field == "firstName" || field == "lastName" {
				r.Review = append(r.Review, "Check driver "+field)
			}
			continue
		}
		profile[field] = value
	}
	if name := norm(profile["firstName"] + " " + profile["lastName"]); profile["firstName"] != "" && profile["lastName"] != "" &&
		(slices.ContainsFunc(locals, func(other Record) bool { return other["id"] != a["id"] && norm(other["name"]) == name }) ||
			slices.ContainsFunc(remotes, func(other Record) bool { return other["id"] != b["id"] && norm(other["name"]) == name })) {
		delete(profile, "firstName")
		delete(profile, "lastName")
		r.Review = append(r.Review, "Another driver has the proposed name")
	}
	for _, field := range []string{"firstName", "lastName", "email", "phone"} {
		value := profile[field]
		if value == "" || ((field == "firstName" || field == "lastName") && (profile["firstName"] == "" || profile["lastName"] == "")) {
			continue
		}
		if a["status"] != "terminated" && (field != "phone" || phone(a[field]) != phone(value)) {
			local := value
			if field == "phone" {
				local = phone(value)
			}
			r.change(clustie, field, a[field], local)
		}
		if a["status"] == "terminated" && a[field] != value && (field != "phone" || phone(a[field]) != phone(value)) {
			r.Review = append(r.Review, "Keep the historical Clustie driver profile and escrow record")
		}
		if field != "phone" || phone(b[field]) != phone(value) {
			r.change(fleetScope, field, b[field], value)
		}
	}
	want := fsDriverStatus(a["status"])
	switch {
	case b["terminationPending"] == "true" || !slices.Contains([]string{"active", "inactive", "on_vacation", "terminated"}, b["status"]):
		r.Review = append(r.Review, "Review FleetScope employment or pending termination")
	case a["status"] == "terminated" || b["status"] == "terminated":
		if want != b["status"] || a["terminationDate"] != b["terminationDate"] {
			r.Review = append(r.Review, "Review termination or rehire in the employment workflow")
		}
	case want != b["status"]:
		if want != "active" && slices.ContainsFunc(trucks, func(t Record) bool { return t["primaryId"] == b["id"] || t["secondaryId"] == b["id"] }) {
			r.Review = append(r.Review, "Clustie status differs and the FleetScope driver is assigned")
		} else {
			r.change(fleetScope, "status", b["status"], want)
		}
	}
}

// Use an abbreviation that is already present on one side of a verified pair.
func preferredTruckMake(a, b string) string {
	fullName := map[string]string{
		"frht": "freightliner",
		"intl": "international",
	}
	if fullName[norm(a)] != "" && fullName[norm(a)] == norm(b) {
		return a
	}
	if fullName[norm(b)] != "" && fullName[norm(b)] == norm(a) {
		return b
	}
	return ""
}

// Recheck make conflicts against live data in completed batches. The receipt
// still describes the original batch and omits the follow-up make changes.
func (p *profilePlan) resolveTruckMakeConflicts(source, receiver Snapshot) {
	for i, row := range p.Rows {
		if row.Kind != "truck" || !slices.Contains(row.Review, conflictingTruckMake) {
			continue
		}
		a, b := find(source.Trucks, row.LocalID), find(receiver.Trucks, row.RemoteID)
		makeName := norm(a["make"])
		if !validVIN(a["vin"]) || !strings.EqualFold(a["vin"], b["vin"]) ||
			!validTruckField("make", makeName) || makeName != norm(b["make"]) {
			continue
		}
		row.Review = slices.DeleteFunc(slices.Clone(row.Review), func(reason string) bool {
			return reason == conflictingTruckMake
		})
		if len(row.Review) == 0 {
			p.Review--
		}
		p.Rows[i] = row
	}
}

// A completed status correction can clear the old flag once the live
// restrictions agree and neither system has a current working crew.
func (p *profilePlan) resolveTruckStatusCrewConflicts(source, receiver Snapshot) {
	for i, row := range p.Rows {
		if row.Kind != "truck" || !slices.Contains(row.Review, truckStatusCrewConflict) {
			continue
		}
		a, b := find(source.Trucks, row.LocalID), find(receiver.Trucks, row.RemoteID)
		if !validVIN(a["vin"]) || !strings.EqualFold(a["vin"], b["vin"]) ||
			a["status"] != b["operationalStatus"] || a["active"] != b["operationalActive"] {
			continue
		}
		if a["status"] == "maintenance" && b["status"] == "active" && a["primaryId"] != "" && b["primaryId"] != "" {
			row.Review = slices.Clone(row.Review)
			row.Review[slices.Index(row.Review, truckStatusCrewConflict)] =
				"Clustie maintenance recorded. FleetScope keeps the current crew while its truck status is Active."
			p.Rows[i] = row
			continue
		}
		if b["primaryId"] != "" || b["secondaryId"] != "" ||
			(a["primaryId"] != "" && find(source.Drivers, a["primaryId"])["status"] != "terminated") ||
			(a["secondaryId"] != "" && find(source.Drivers, a["secondaryId"])["status"] != "terminated") {
			continue
		}
		expected := fleetScopeTruckStatus(a, b)
		if expected == "active" || b["status"] != expected {
			continue
		}
		row.Review = slices.DeleteFunc(slices.Clone(row.Review), func(reason string) bool {
			return reason == truckStatusCrewConflict
		})
		if len(row.Review) == 0 {
			p.Review--
		}
		p.Rows[i] = row
	}
}

// Follow-up corrections use live evidence and leave the original batch receipts intact.
func (p *profilePlan) resolveTruckReviewDecisions(source, receiver Snapshot, links Record) {
	var reconciledTrucks []string
	for i, row := range p.Rows {
		if row.Kind != "truck" || len(row.Review) == 0 {
			continue
		}
		a := find(source.Trucks, row.LocalID)
		b := find(receiver.Trucks, row.RemoteID)
		if row.RemoteID == "" && a["unit"] == "042" && validVIN(a["vin"]) {
			matches := 0
			for _, candidate := range receiver.Trucks {
				if candidate["unit"] == a["unit"] && strings.EqualFold(candidate["vin"], a["vin"]) {
					b = candidate
					matches++
				}
			}
			if matches != 1 {
				b = nil
			}
		}
		var owner map[string]any
		_ = json.Unmarshal([]byte(b["ownerSnapshot"]), &owner)
		exactTruck := validVIN(a["vin"]) && strings.EqualFold(a["vin"], b["vin"])
		ownerMatches := exactTruck && a["ownerId"] != "" && a["ownerId"] == owner["mserpOwnerId"] &&
			a["ownerKind"] == b["ownerKind"] && a["ownerKind"] == owner["kind"] &&
			nullable(a["ownerDriverRef"]) == owner["mserpOwnerDriverId"] && nullable(a["ownerDate"]) == owner["effectiveDate"]
		registrationMatches := exactTruck && a["plate"] != "" && a["plate"] == b["plate"] && a["plateState"] == b["plateState"]
		reconciled042 := ownerMatches && a["unit"] == "042" && a["unit"] == b["unit"] &&
			a["status"] == b["operationalStatus"] && a["active"] == b["operationalActive"] && registrationMatches
		retain088Crew := ownerMatches && a["unit"] == "088" && a["status"] == "maintenance" &&
			b["status"] == "active" && b["operationalStatus"] == a["status"] && b["operationalActive"] == a["active"] &&
			a["primaryId"] != "" && b["primaryId"] != "" && links["driver:"+a["primaryId"]] == b["primaryId"] &&
			((a["secondaryId"] == "" && b["secondaryId"] == "") ||
				(a["secondaryId"] != "" && b["secondaryId"] != "" && links["driver:"+a["secondaryId"]] == b["secondaryId"]))
		row.Review = slices.DeleteFunc(slices.Clone(row.Review), func(reason string) bool {
			switch reason {
			case truckStatusCrewConflict:
				return retain088Crew
			case "Confirm the FleetScope driver-owner identity", "Review existing FleetScope lease against Clustie ownership":
				return ownerMatches
			case "Conflicting truck plate", "Verify truck registration":
				return registrationMatches
			case "Previously deferred":
				if reconciled042 {
					row.RemoteID = b["id"]
					reconciledTrucks = append(reconciledTrucks, b["id"])
				} else {
					row.Deferred = reason
				}
				return true
			default:
				return false
			}
		})
		if len(row.Review) == 0 {
			p.Review--
		}
		p.Rows[i] = row
	}
	p.Rows = slices.DeleteFunc(p.Rows, func(row profileRow) bool {
		if row.Kind == "truck" && row.LocalID == "" && slices.Contains(reconciledTrucks, row.RemoteID) &&
			len(row.Review) == 1 && row.Review[0] == "No verified Clustie match" {
			p.Review--
			return true
		}
		return false
	})
}

func fleetScopeTruckStatus(a, b Record) string {
	switch {
	case b["status"] == "out_of_service":
		return "out_of_service"
	case a["status"] == "terminated":
		return "inactive"
	case a["status"] == "out_of_service", a["status"] == "maintenance", a["conditionStatus"] == "shop", a["conditionStatus"] == "out_of_service":
		return "out_of_service"
	case a["active"] == "false":
		return "inactive"
	default:
		return "active"
	}
}

func isAntonUnitPair(a, b Record) bool {
	unit := a["unit"]
	return (unit == "1" || unit == "2" || unit == "3") && norm(b["unit"]) == unit+" anton" &&
		validVIN(a["vin"]) && strings.EqualFold(a["vin"], b["vin"])
}

// Clear verified live model and unit discrepancies from the completed review.
func (p *profilePlan) resolveTruckModelAndUnitConflicts(source, receiver Snapshot) {
	for i, row := range p.Rows {
		if row.Kind != "truck" || row.Deferred != "" || len(row.Review) == 0 {
			continue
		}
		a, b := find(source.Trucks, row.LocalID), find(receiver.Trucks, row.RemoteID)
		if !validVIN(a["vin"]) || !strings.EqualFold(a["vin"], b["vin"]) {
			continue
		}
		modelMatches := validTruckField("model", strings.TrimSpace(b["model"])) && norm(a["model"]) == norm(b["model"])
		unitMatches := a["unit"] != "" && a["unit"] == b["unit"]
		row.Review = slices.DeleteFunc(slices.Clone(row.Review), func(reason string) bool {
			return (modelMatches && (reason == conflictingTruckModel || reason == "Check truck model")) ||
				(unitMatches && reason == truckUnitReview)
		})
		if len(row.Review) == 0 {
			p.Review--
		}
		p.Rows[i] = row
	}
}

func planTruck(r *profileRow, a, b Record, locals, remotes []Record, driverLinks, driverNames Record) {
	for _, field := range []string{"year", "make", "model", "color", "plate", "plateState"} {
		av, bv := strings.TrimSpace(a[field]), strings.TrimSpace(b[field])
		if field == "model" && validTruckField(field, bv) {
			if norm(av) != norm(bv) {
				r.change(clustie, field, a[field], bv)
			}
			continue
		}
		if av != "" && !validTruckField(field, av) || bv != "" && !validTruckField(field, bv) {
			r.Review = append(r.Review, "Check truck "+field)
			continue
		}
		if av == "" && bv == "" && field != "model" {
			r.Review = append(r.Review, "Missing truck "+field)
		}
		if norm(av) == norm(bv) {
			continue
		}
		if field == "make" {
			if makeAbbreviation := preferredTruckMake(av, bv); makeAbbreviation != "" {
				r.change(clustie, field, a[field], makeAbbreviation)
				r.change(fleetScope, field, b[field], makeAbbreviation)
				continue
			}
		}
		if av != "" && bv != "" {
			r.Review = append(r.Review, "Conflicting truck "+field)
			continue
		}
		if field == "plate" || field == "plateState" {
			// Keep registration as a pair and prevent duplicate plates.
			if !validTruckField("plate", first(a["plate"], b["plate"])) ||
				!validTruckField("plateState", first(a["plateState"], b["plateState"])) ||
				(a["plate"] != "" && b["plate"] != "" && norm(a["plate"]) != norm(b["plate"])) ||
				(a["plateState"] != "" && b["plateState"] != "" && norm(a["plateState"]) != norm(b["plateState"])) ||
				(field == "plate" && ((bv == "" && hasOtherPlate(remotes, b["id"], av)) ||
					(av == "" && hasOtherPlate(locals, a["id"], bv)))) {
				r.Review = append(r.Review, "Verify truck registration")
				continue
			}
		}
		if av == "" {
			r.change(clustie, field, a[field], bv)
		} else {
			r.change(fleetScope, field, b[field], av)
		}
	}
	if norm(a["unit"]) != norm(b["unit"]) {
		if isAntonUnitPair(a, b) && !slices.ContainsFunc(remotes, func(other Record) bool {
			return other["id"] != b["id"] && norm(other["unit"]) == norm(a["unit"])
		}) {
			r.change(fleetScope, "unit", b["unit"], a["unit"])
		} else {
			r.Review = append(r.Review, truckUnitReview)
		}
	}
	planTruckOwnership(r, a, b, driverLinks, driverNames)
	status := fleetScopeTruckStatus(a, b)
	if status != b["status"] && status != "active" && (b["primaryId"] != "" || b["secondaryId"] != "" || b["trailerId"] != "" || b["secondTrailerId"] != "") {
		r.Review = append(r.Review, truckStatusCrewConflict)
	} else {
		r.change(fleetScope, "status", b["status"], status)
		r.change(fleetScope, "operationalStatus", b["operationalStatus"], a["status"])
		r.change(fleetScope, "operationalActive", b["operationalActive"], a["active"])
		if b["status"] == "out_of_service" && a["active"] == "true" && a["conditionStatus"] != "out_of_service" && a["conditionStatus"] != "shop" {
			r.Review = append(r.Review, "FleetScope out-of-service hold stays in effect")
		}
	}
}

func hasOtherPlate(trucks []Record, id, plate string) bool {
	return slices.ContainsFunc(trucks, func(other Record) bool {
		return other["id"] != id && norm(other["plate"]) == norm(plate)
	})
}

func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func validTruckField(field, value string) bool {
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return false
	}
	switch field {
	case "year":
		n, err := strconv.Atoi(value)
		return err == nil && n >= 1900 && n <= time.Now().Year()+2
	case "plateState":
		return len(value) == 2 && strings.Contains("|AL|AK|AZ|AR|CA|CO|CT|DE|DC|FL|GA|HI|ID|IL|IN|IA|KS|KY|LA|ME|MD|MA|MI|MN|MS|MO|MT|NE|NV|NH|NJ|NM|NY|NC|ND|OH|OK|OR|PA|RI|SC|SD|TN|TX|UT|VT|VA|WA|WV|WI|WY|AB|BC|MB|NB|NL|NS|NT|NU|ON|PE|QC|SK|YT|", "|"+value+"|")
	}
	limit := map[string]int{"make": 50, "model": 50, "plate": 15, "color": 30}[field]
	upper := strings.ToUpper(value)
	return utf8.RuneCountInString(value) <= limit &&
		!slices.Contains([]string{"UNKNOWN", "N/A", "NONE", "TBD", "TEST"}, upper) &&
		strings.Trim(upper, "0") != "" && strings.Trim(upper, "B") != ""
}

func planTruckOwnership(r *profileRow, a, b Record, driverLinks, driverNames Record) {
	kind, ref := a["ownerKind"], a["ownerDriverRef"]
	if a["ownerId"] == "" || a["ownerName"] == "" || !slices.Contains([]string{"company", "driver", "investor"}, kind) || (kind == "driver" && ref == "") {
		r.Review = append(r.Review, "Verify Clustie ownership")
		return
	}
	if b["ownerKind"] == "lease" {
		r.Review = append(r.Review, "Review existing FleetScope lease against Clustie ownership")
		return
	}
	driver := driverLinks[ref]
	var prior map[string]any
	_ = json.Unmarshal([]byte(b["ownerSnapshot"]), &prior)
	// With a missing local-to-remote owner link, never clear a real FleetScope owner.
	if kind == "driver" && driver == "" && b["ownerDriverRef"] != "" {
		r.Review = append(r.Review, "Confirm the FleetScope driver-owner identity")
		return
	}
	owner := map[string]any{"kind": kind, "mserpOwnerId": a["ownerId"], "displayName": first(driverNames[ref], a["ownerName"]), "mserpOwnerDriverId": nullable(ref), "effectiveDate": nullable(a["ownerDate"])}
	want, _ := json.Marshal(owner)
	old, _ := json.Marshal(prior)
	sameOwner := kind == b["ownerKind"] && (prior["mserpOwnerId"] == a["ownerId"] ||
		(prior == nil && (kind == "company" || (kind == "driver" && driver != "" && driver == b["ownerDriverRef"]))))
	if !sameOwner || (prior != nil && prior["effectiveDate"] != nullable(a["ownerDate"])) {
		r.change(fleetScope, "coverageHold", b["coverageHold"], "true")
	}
	r.change(fleetScope, "ownerKind", b["ownerKind"], kind)
	r.change(fleetScope, "ownerDriverRef", b["ownerDriverRef"], driver)
	r.change(fleetScope, "ownerSnapshot", string(old), string(want))
}
