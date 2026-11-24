package httpapi

import "net/http"

// Overview is the console home's fleet summary.
type Overview struct{ *server }

// Show returns the console home.
func (h Overview) Show(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.Overview(r.Context())
	h.respond(w, http.StatusOK, resp, err)
}
