package httpapi

import (
	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
	"net/http"
)

func (h maintenanceHandler) programs(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "view"); !ok {
		return
	}
	programs, err := h.repo.Programs(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	trucks, err := h.repo.ProgramTrucks(r.Context(), "", "")
	if err != nil {
		h.fail(w, err)
		return
	}
	for i := range programs {
		for _, t := range trucks {
			if t.ProgramID != programs[i].ID || !t.Enrolled || !t.Active {
				continue
			}
			switch t.Status {
			case "upcoming":
				programs[i].UpcomingCount++
			case "due", "overdue":
				programs[i].OverdueCount++
			}
		}
	}
	writeJSON(w, 200, programs)
}

func (h maintenanceHandler) programSave(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "edit"); !ok {
		return
	}
	session, _ := authSessionFromContext(r.Context())
	if !repository.HasPermission(session.User, "service_programs.manage") {
		writeAPIError(w, 403, "your role cannot manage service programs")
		return
	}
	var p repository.ServiceProgram
	if err := decodeJSON(r, &p); err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	if r.Method == "PUT" {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		p.ID = id
		if p.Version < 1 {
			writeAPIError(w, 400, "program version is required")
			return
		}
	} else {
		p.ID = ""
		if p.Version != 0 {
			writeAPIError(w, 400, "new programs require version zero")
			return
		}
	}
	if len(p.AssigneeIDs) > 500 || len(p.AssigneeRoleIDs) > 100 {
		writeAPIError(w, 400, "too many reminder assignees")
		return
	}
	for _, id := range append(append([]string{}, p.AssigneeIDs...), p.AssigneeRoleIDs...) {
		if !isUUID(id) {
			writeAPIError(w, 400, "choose valid reminder assignees")
			return
		}
	}
	v, err := h.repo.SaveProgram(r.Context(), p, session.User.ID)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}

func (h maintenanceHandler) programTrucks(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "view"); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	truck := r.URL.Query().Get("truckId")
	if id != "" && !isUUID(id) || truck != "" && !isUUID(truck) {
		writeAPIError(w, 400, "select a valid program and truck")
		return
	}
	values, err := h.repo.ProgramTrucks(r.Context(), id, truck)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, values)
}

func (h maintenanceHandler) programTrucksSave(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "edit"); !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Items []repository.ProgramTruckInput `json:"items"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	for _, v := range in.Items {
		if !isUUID(v.TruckID) {
			writeAPIError(w, 400, "select valid trucks")
			return
		}
	}
	session, _ := authSessionFromContext(r.Context())
	if err := h.repo.SaveProgramTrucks(r.Context(), id, in.Items, session.User.ID); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(204)
}

func (h maintenanceHandler) programBaseline(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "edit"); !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	truck := chi.URLParam(r, "truckId")
	if !isUUID(truck) {
		writeAPIError(w, 400, "select a valid truck")
		return
	}
	var in struct {
		repository.MaintenancePMInput
		MemberVersion int `json:"memberVersion"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	if in.RecordID != nil && !isUUID(*in.RecordID) || in.MemberVersion < 1 || in.RecordVersion < 0 || in.BaselineVersion < 0 {
		writeAPIError(w, 400, "invalid service version")
		return
	}
	session, _ := authSessionFromContext(r.Context())
	in.TruckID = truck
	in.ActorID = session.User.ID
	in.ActorName = session.User.Username
	if err := h.repo.SaveProgramBaseline(r.Context(), id, in.MaintenancePMInput, in.MemberVersion); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(204)
}

func (h maintenanceHandler) programAssignees(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "edit"); !ok {
		return
	}
	session, _ := authSessionFromContext(r.Context())
	if !repository.HasPermission(session.User, "service_programs.manage") {
		writeAPIError(w, 403, "your role cannot manage service programs")
		return
	}
	people, roles, err := h.repo.ProgramAssignees(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"people": people, "roles": roles})
}

func (h maintenanceHandler) programWorkspace(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "view"); !ok {
		return
	}
	q := r.URL.Query()
	program, truck, status := q.Get("program"), q.Get("truckId"), q.Get("status")
	if program != "" && !isUUID(program) || truck != "" && !isUUID(truck) || status != "" && status != "due" {
		writeAPIError(w, 400, "select a valid program, truck and status")
		return
	}
	p, err := parsePagination(r)
	if err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	out, err := h.repo.ProgramWorkspace(r.Context(), program, truck, q.Get("search"), status, p)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, out)
}
