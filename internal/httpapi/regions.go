package httpapi

import "net/http"

// Regions are the clusters customers can use.
type Regions struct{ *server }

// Index lists the regions the caller can use.
func (h Regions) Index(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.ListRegions(r.Context(), r.Header.Get(OrganizationHeader))
	h.respond(w, http.StatusOK, resp, err)
}
