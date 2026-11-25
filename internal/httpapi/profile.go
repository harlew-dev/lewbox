package httpapi

import "net/http"

// Profile is the caller's own account or key.
type Profile struct{ *server }

// Show describes the caller.
func (h Profile) Show(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.Me(r.Context())
	// A browser session gets its CSRF token back here, so a reloaded console
	// (which kept it only in memory) can write again.
	if err == nil {
		if token := sessionTokenFrom(r); token != "" {
			resp.CSRFToken = h.svc.CSRFToken(token)
		}
	}
	h.respond(w, http.StatusOK, resp, err)
}
