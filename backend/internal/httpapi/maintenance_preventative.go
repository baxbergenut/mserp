package httpapi

import "net/http"

func (h maintenanceHandler) preventative(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "view"); !ok {
		return
	}
	service := r.URL.Query().Get("serviceCode")
	if service != "pm" && service != "oil" {
		writeAPIError(w, 400, "select PM service or oil change")
		return
	}
	p, err := parsePagination(r)
	if err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	v, err := h.repo.Preventative(r.Context(), service, r.URL.Query().Get("search"), r.URL.Query().Get("includeInactive") == "true", p.Page, p.PageSize)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}
