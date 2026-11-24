package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// Settings is the instance's own configuration.
type Settings struct{ *server }

// Show returns the instance settings.
func (h Settings) Show(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.GetSettings(r.Context())
	h.respond(w, http.StatusOK, resp, err)
}

// Update changes the instance settings.
func (h Settings) Update(w http.ResponseWriter, r *http.Request) {
	var req service.UpdateSettingsRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.UpdateSettings(r.Context(), req)
	h.respond(w, http.StatusOK, resp, err)
}
