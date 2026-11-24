package httpapi

import "net/http"

// Profile is the caller's own account or key.
type Profile struct{ *server }

// Show describes the caller.
func (h Profile) Show(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.Me(r.Context())
	h.respond(w, http.StatusOK, resp, err)
}
