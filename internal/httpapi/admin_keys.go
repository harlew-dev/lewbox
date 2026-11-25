package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// AdminKeys are an organization's own API keys. {admin_key} is an ID or live
// name.
type AdminKeys struct{ *server }

// Index lists the organization's admin keys.
func (h AdminKeys) Index(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	resp, err := h.svc.ListAdminKeys(r.Context(), service.ListAdminKeysRequest{
		ListQuery: listQuery(query),
		Org:       r.Header.Get(OrganizationHeader),
		Status:    query.Get("status"),
	})
	h.respond(w, http.StatusOK, resp, err)
}

// Store creates an admin key and returns its token once.
func (h AdminKeys) Store(w http.ResponseWriter, r *http.Request) {
	var req service.CreateAdminKeyRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.CreateAdminKey(r.Context(), r.Header.Get(OrganizationHeader), req)
	h.respond(w, http.StatusCreated, resp, err)
}

// Destroy revokes an admin key.
func (h AdminKeys) Destroy(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.RevokeAdminKey(r.Context(), r.Header.Get(OrganizationHeader), r.PathValue("admin_key"))
	h.respond(w, http.StatusOK, resp, err)
}
