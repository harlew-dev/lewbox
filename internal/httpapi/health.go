package httpapi

import "net/http"

// Health answers the public health check.
type Health struct{ *server }

// Show reports whether the server is up.
func (h Health) Show(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.Health(r.Context())
	h.respond(w, http.StatusOK, resp, err)
}
