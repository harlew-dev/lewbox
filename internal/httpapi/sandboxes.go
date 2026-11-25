package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// Sandboxes are the mirrored sandboxes in a workspace's region.
type Sandboxes struct{ *server }

// Index lists mirrored sandboxes, optionally filtered by region and status.
func (h Sandboxes) Index(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	resp, err := h.svc.ListSandboxes(r.Context(), service.ListSandboxesRequest{
		ListQuery:    listQuery(query),
		Region:       query.Get("region"),
		Status:       query.Get("status"),
		Org:          requestOrganization(r),
		Organization: query.Get("organization"),
	})
	h.respond(w, http.StatusOK, resp, err)
}

// Stop stops a sandbox on its cluster.
func (h Sandboxes) Stop(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.StopSandbox(r.Context(), r.PathValue("sandbox"), service.RegionScope{
		Region: r.URL.Query().Get("region"),
		Org:    requestOrganization(r),
	})
	h.respond(w, http.StatusOK, resp, err)
}

// Destroy deletes a sandbox on its cluster.
func (h Sandboxes) Destroy(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.DeleteSandbox(r.Context(), r.PathValue("sandbox"), service.RegionScope{
		Region: r.URL.Query().Get("region"),
		Org:    requestOrganization(r),
	})
	h.respond(w, http.StatusOK, resp, err)
}
