package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// AccessTokens are an account's own API tokens. They act as the account
// whose ID or email may be named by ?user= (instance admins only).
type AccessTokens struct{ *server }

// Index lists an account's access tokens.
func (h AccessTokens) Index(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	resp, err := h.svc.ListAccessTokens(r.Context(), service.ListAccessTokensRequest{
		ListQuery: listQuery(query),
		User:      query.Get("user"),
		Status:    query.Get("status"),
	})
	h.respond(w, http.StatusOK, resp, err)
}

// Store creates an access token and returns it once.
func (h AccessTokens) Store(w http.ResponseWriter, r *http.Request) {
	var req service.CreateAccessTokenRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.CreateAccessToken(r.Context(), req)
	h.respond(w, http.StatusCreated, resp, err)
}

// Destroy revokes an access token by ID or live name.
func (h AccessTokens) Destroy(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.RevokeAccessToken(r.Context(), r.URL.Query().Get("user"), r.PathValue("access_token"))
	h.respond(w, http.StatusOK, resp, err)
}
