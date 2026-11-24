package httpapi

import "net/http"

// Limits is a workspace's limits and live use.
type Limits struct{ *server }

// Show returns the limits report.
func (h Limits) Show(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.Limits(r.Context(), r.Header.Get(OrganizationHeader))
	h.respond(w, http.StatusOK, resp, err)
}
