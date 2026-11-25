package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// SandboxKeys are AerolVM runner tokens for a workspace. {sandbox_key} is an
// ID or live name.
type SandboxKeys struct{ *server }

// Index lists the organization's sandbox keys.
func (h SandboxKeys) Index(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	resp, err := h.svc.ListSandboxKeys(r.Context(), service.ListSandboxKeysRequest{
		ListQuery: listQuery(query),
		Org:       r.Header.Get(OrganizationHeader),
		Status:    query.Get("status"),
	})
	h.respond(w, http.StatusOK, resp, err)
}

// Store mints a sandbox key and returns its token once.
func (h SandboxKeys) Store(w http.ResponseWriter, r *http.Request) {
	var req service.CreateSandboxKeyRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.CreateSandboxKey(r.Context(), r.Header.Get(OrganizationHeader), req)
	h.respond(w, http.StatusCreated, resp, err)
}

// Destroy revokes a sandbox key.
func (h SandboxKeys) Destroy(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.RevokeSandboxKey(r.Context(), r.Header.Get(OrganizationHeader), r.PathValue("sandbox_key"))
	h.respond(w, http.StatusOK, resp, err)
}
