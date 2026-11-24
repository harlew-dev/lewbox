package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// JoinTokens are AerolVM tokens a new node joins its cluster with.
type JoinTokens struct{ *server }

// Store mints a join token on the cluster.
func (h JoinTokens) Store(w http.ResponseWriter, r *http.Request) {
	var req service.JoinTokenRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.CreateJoinToken(r.Context(), r.PathValue("cluster"), req)
	h.respond(w, http.StatusCreated, resp, err)
}
