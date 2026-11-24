package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// Usage is the workspace's metered compute.
type Usage struct{ *server }

// Show returns usage buckets and totals.
func (h Usage) Show(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.Usage(r.Context(), service.UsageRequest{
		From:        r.URL.Query().Get("from"),
		To:          r.URL.Query().Get("to"),
		Granularity: r.URL.Query().Get("granularity"),
		Org:         requestOrganization(r),
	})
	h.respond(w, http.StatusOK, resp, err)
}
