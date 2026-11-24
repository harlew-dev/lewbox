package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// Subscriptions are the operator's assignment of a plan in one region.
// {region} is a cluster ID or slug.
type Subscriptions struct{ *server }

// Index lists the header organization's subscriptions.
func (h Subscriptions) Index(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.ListSubscriptions(r.Context(), r.Header.Get(OrganizationHeader))
	h.respond(w, http.StatusOK, resp, err)
}

// Update creates or changes a subscription.
func (h Subscriptions) Update(w http.ResponseWriter, r *http.Request) {
	var req service.SetSubscriptionRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.SetSubscription(r.Context(), r.Header.Get(OrganizationHeader), r.PathValue("region"), req)
	h.respond(w, http.StatusOK, resp, err)
}

// Destroy removes a subscription.
func (h Subscriptions) Destroy(w http.ResponseWriter, r *http.Request) {
	force, err := queryBool(r, "force")
	if err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.DeleteSubscription(r.Context(), r.Header.Get(OrganizationHeader), r.PathValue("region"), force)
	h.respond(w, http.StatusOK, resp, err)
}
