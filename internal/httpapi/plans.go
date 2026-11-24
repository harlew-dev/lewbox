package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// Plans are the quotas a subscription can use. {plan} is an ID or slug.
type Plans struct{ *server }

// Index lists every plan.
func (h Plans) Index(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.ListPlans(r.Context())
	h.respond(w, http.StatusOK, resp, err)
}

// Store adds a plan.
func (h Plans) Store(w http.ResponseWriter, r *http.Request) {
	var req service.CreatePlanRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.CreatePlan(r.Context(), req)
	h.respond(w, http.StatusCreated, resp, err)
}

// Show returns one plan.
func (h Plans) Show(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.GetPlan(r.Context(), r.PathValue("plan"))
	h.respond(w, http.StatusOK, resp, err)
}

// Update changes a plan.
func (h Plans) Update(w http.ResponseWriter, r *http.Request) {
	var req service.UpdatePlanRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.UpdatePlan(r.Context(), r.PathValue("plan"), req)
	h.respond(w, http.StatusOK, resp, err)
}

// Destroy removes a plan that no subscription uses.
func (h Plans) Destroy(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.DeletePlan(r.Context(), r.PathValue("plan"))
	h.respond(w, http.StatusOK, resp, err)
}
