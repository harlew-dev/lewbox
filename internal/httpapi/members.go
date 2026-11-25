package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// Members are accounts in an organization. {member} is a membership ID, an
// account ID or an email.
type Members struct{ *server }

// Index lists the header organization's members.
func (h Members) Index(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	resp, err := h.svc.ListMembers(r.Context(), service.ListMembersRequest{
		ListQuery: listQuery(query),
		Org:       r.Header.Get(OrganizationHeader),
		Role:      query.Get("role"),
	})
	h.respond(w, http.StatusOK, resp, err)
}

// Store adds an account to the organization.
func (h Members) Store(w http.ResponseWriter, r *http.Request) {
	var req service.AddMemberRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.AddMember(r.Context(), r.Header.Get(OrganizationHeader), req)
	h.respond(w, http.StatusCreated, resp, err)
}

// Update changes a member's role.
func (h Members) Update(w http.ResponseWriter, r *http.Request) {
	var req service.SetMemberRoleRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.SetMemberRole(r.Context(), r.Header.Get(OrganizationHeader), r.PathValue("member"), req)
	h.respond(w, http.StatusOK, resp, err)
}

// Destroy removes a member.
func (h Members) Destroy(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.RemoveMember(r.Context(), r.Header.Get(OrganizationHeader), r.PathValue("member"))
	h.respond(w, http.StatusOK, resp, err)
}
