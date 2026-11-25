package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// SetupLinks are single-use onboarding links. Every action here is
// instance-admin only, except Check, which is the public page's lookup.
type SetupLinks struct{ *server }

// Index lists setup links, newest first, without tokens.
func (h SetupLinks) Index(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	resp, err := h.svc.ListSetupLinks(r.Context(), service.ListSetupLinksRequest{
		ListQuery: listQuery(query),
		Status:    query.Get("status"),
		Plan:      query.Get("plan"),
		Region:    query.Get("region"),
	})
	h.respond(w, http.StatusOK, resp, err)
}

// Store mints a setup link; its token is only in this response.
func (h SetupLinks) Store(w http.ResponseWriter, r *http.Request) {
	var req service.CreateSetupLinkRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.CreateSetupLink(r.Context(), req)
	h.respond(w, http.StatusCreated, resp, err)
}

// Destroy revokes an unused setup link.
func (h SetupLinks) Destroy(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.RevokeSetupLink(r.Context(), r.PathValue("setup_link"))
	h.respond(w, http.StatusOK, resp, err)
}

// Check is the public lookup behind a setup link's page. The token travels
// in the body so it never reaches a URL, a log line or a proxy.
func (h SetupLinks) Check(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.SetupLinkInfo(r.Context(), req.Token)
	h.respond(w, http.StatusOK, resp, err)
}
