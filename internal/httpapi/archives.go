package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// Archives are stopped sandboxes kept in a cluster's bucket. Their IDs are
// unique, so only the organization is chosen. The list is part of
// GET /v0/snapshots, where their kind is "archive".
type Archives struct{ *server }

// Show returns one archive.
func (h Archives) Show(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.GetArchive(r.Context(), r.PathValue("archive"), service.RegionScope{
		Org: requestOrganization(r),
	})
	h.respond(w, http.StatusOK, resp, err)
}

// Restore creates a new sandbox from an archive.
func (h Archives) Restore(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.RestoreArchive(r.Context(), r.PathValue("archive"), service.RegionScope{
		Org: requestOrganization(r),
	})
	h.respond(w, http.StatusOK, resp, err)
}

// Destroy removes an archive.
func (h Archives) Destroy(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.DeleteArchive(r.Context(), r.PathValue("archive"), service.RegionScope{
		Org: requestOrganization(r),
	})
	h.respond(w, http.StatusOK, resp, err)
}
