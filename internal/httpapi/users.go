package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// Users are accounts. {user} is an ID or email.
type Users struct{ *server }

// Index lists every account.
func (h Users) Index(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.ListUsers(r.Context())
	h.respond(w, http.StatusOK, resp, err)
}

// Store creates an account.
func (h Users) Store(w http.ResponseWriter, r *http.Request) {
	var req service.CreateUserRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.CreateUser(r.Context(), req)
	h.respond(w, http.StatusCreated, resp, err)
}

// Show returns one account.
func (h Users) Show(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.GetUser(r.Context(), r.PathValue("user"))
	h.respond(w, http.StatusOK, resp, err)
}

// Destroy removes an account.
func (h Users) Destroy(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.RemoveUser(r.Context(), r.PathValue("user"))
	h.respond(w, http.StatusOK, resp, err)
}

// UserSuspension blocks or allows an account's sign-in.
type UserSuspension struct{ *server }

// Store suspends an account: it ends their sessions and revokes their tokens.
func (h UserSuspension) Store(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.SetUserDisabled(r.Context(), r.PathValue("user"), true)
	h.respond(w, http.StatusOK, resp, err)
}

// Destroy lifts an account's suspension.
func (h UserSuspension) Destroy(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.SetUserDisabled(r.Context(), r.PathValue("user"), false)
	h.respond(w, http.StatusOK, resp, err)
}

// UserPassword changes an account's password.
type UserPassword struct{ *server }

// Update sets an account's password.
func (h UserPassword) Update(w http.ResponseWriter, r *http.Request) {
	var req service.SetPasswordRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.SetPassword(r.Context(), r.PathValue("user"), req)
	h.respond(w, http.StatusOK, resp, err)
}
