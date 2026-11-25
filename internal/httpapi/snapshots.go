package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// Snapshots are a workspace's snapshots and archived sandboxes, in one list.
type Snapshots struct{ *server }

// Index lists snapshots, optionally filtered by region, kind and status.
func (h Snapshots) Index(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	resp, err := h.svc.ListSnapshots(r.Context(), service.ListSnapshotsRequest{
		ListQuery:    listQuery(query),
		Region:       query.Get("region"),
		Org:          requestOrganization(r),
		Organization: query.Get("organization"),
		Kind:         query.Get("kind"),
		Status:       query.Get("status"),
		Summary:      query.Get("summary"),
	})
	h.respond(w, http.StatusOK, resp, err)
}

// Destroy deletes a snapshot on its cluster. {snapshot} is its name, which is
// unique per workspace.
func (h Snapshots) Destroy(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.DeleteSnapshot(r.Context(), r.PathValue("snapshot"), service.RegionScope{
		Region: r.URL.Query().Get("region"),
		Org:    requestOrganization(r),
	})
	h.respond(w, http.StatusOK, resp, err)
}
