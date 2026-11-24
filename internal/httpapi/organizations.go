package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// Organizations are the tenants (workspaces). {organization} is an ID or
// slug. The member and admin-key routes act on the organization named by the
// Lewbox-Organization header, or on an admin key's own organization.
type Organizations struct{ *server }

// Index lists organizations.
func (h Organizations) Index(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.ListOrganizations(r.Context(), service.ListOrganizationsRequest{
		User:   r.URL.Query().Get("user"),
		Status: r.URL.Query().Get("status"),
	})
	h.respond(w, http.StatusOK, resp, err)
}

// Store creates an organization.
func (h Organizations) Store(w http.ResponseWriter, r *http.Request) {
	var req service.CreateOrganizationRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.CreateOrganization(r.Context(), req)
	h.respond(w, http.StatusCreated, resp, err)
}

// Show returns one organization.
func (h Organizations) Show(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.GetOrganization(r.Context(), r.PathValue("organization"))
	h.respond(w, http.StatusOK, resp, err)
}

// Update renames, suspends or restores an organization.
func (h Organizations) Update(w http.ResponseWriter, r *http.Request) {
	var req service.UpdateOrganizationRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.UpdateOrganization(r.Context(), r.PathValue("organization"), req)
	h.respond(w, http.StatusOK, resp, err)
}

// Current returns the organization the request acts on: the header's, or an
// admin key's own.
func (h Organizations) Current(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.GetOrganization(r.Context(), r.Header.Get(OrganizationHeader))
	h.respond(w, http.StatusOK, resp, err)
}
