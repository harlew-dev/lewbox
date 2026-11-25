package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// Events is the instance's audit trail. Operators only.
type Events struct{ *server }

// Index returns one cursor page of events, newest first.
func (h Events) Index(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, err := queryInt(r, "limit", 0)
	if err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.ListEvents(r.Context(), service.ListEventsRequest{
		Before:       query.Get("before"),
		Actor:        query.Get("actor"),
		Organization: query.Get("organization"),
		Action:       query.Get("action"),
		From:         query.Get("from"),
		To:           query.Get("to"),
		Limit:        limit,
	})
	h.respond(w, http.StatusOK, resp, err)
}
