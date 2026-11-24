package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// Signups creates accounts.
type Signups struct{ *server }

// Store creates an account, signs it in and sets the session cookie.
func (h Signups) Store(w http.ResponseWriter, r *http.Request) {
	var req service.SignupRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	token, resp, err := h.svc.Signup(r.Context(), req)
	if err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	setSessionCookie(w, r, h.opts, token, resp.ExpiresAt)
	h.respond(w, http.StatusOK, resp, nil)
}
