package httpapi

import "net/http"

// Instance answers the public instance details.
type Instance struct{ *server }

// Show returns the instance's name and sign-up mode.
func (h Instance) Show(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.Instance(r.Context())
	h.respond(w, http.StatusOK, resp, err)
}
