package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// Sessions signs accounts in and out.
type Sessions struct{ *server }

// Login checks an email and password and returns a session token.
func (h Sessions) Login(w http.ResponseWriter, r *http.Request) {
	var req service.LoginRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.Login(r.Context(), req)
	h.respond(w, http.StatusOK, resp, err)
}

// Store signs a browser in and sets the session cookie.
func (h Sessions) Store(w http.ResponseWriter, r *http.Request) {
	var req service.LoginRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	token, resp, err := h.svc.WebLogin(r.Context(), req)
	if err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	setSessionCookie(w, r, h.opts, token, resp.ExpiresAt)
	h.respond(w, http.StatusOK, resp, nil)
}

// Destroy ends the caller's session and clears the cookie.
func (h Sessions) Destroy(w http.ResponseWriter, r *http.Request) {
	// Clear the cookie even when the session is already gone, so a stale
	// browser cookie never keeps a request looking signed in.
	clearSessionCookie(w, r, h.opts)
	resp, err := h.svc.Logout(r.Context())
	h.respond(w, http.StatusOK, resp, err)
}
