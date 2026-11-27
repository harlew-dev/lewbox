package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// ClusterUpgrade is the rolling AerolVM upgrade of one cluster. It is a
// singleton under the cluster: status, plan, start and the rollout commands.
type ClusterUpgrade struct{ *server }

// Show returns the current rollout. A cluster that never had one answers 404.
func (h ClusterUpgrade) Show(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.GetClusterUpgrade(r.Context(), r.PathValue("cluster"))
	h.respond(w, http.StatusOK, resp, err)
}

// Plan says what an upgrade to the requested version would do, and changes
// nothing.
func (h ClusterUpgrade) Plan(w http.ResponseWriter, r *http.Request) {
	pauseAfter, err := queryInt(r, "pause_after", 0)
	if err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	req := service.PlanClusterUpgradeRequest{
		Version:    r.URL.Query().Get("version"),
		Batch:      r.URL.Query().Get("batch"),
		PauseAfter: pauseAfter,
	}
	resp, err := h.svc.PlanClusterUpgrade(r.Context(), r.PathValue("cluster"), req)
	h.respond(w, http.StatusOK, resp, err)
}

// Store starts an upgrade.
func (h ClusterUpgrade) Store(w http.ResponseWriter, r *http.Request) {
	var req service.StartClusterUpgradeRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.StartClusterUpgrade(r.Context(), r.PathValue("cluster"), req)
	h.respond(w, http.StatusCreated, resp, err)
}

// Stop finishes the nodes in progress and starts no more.
func (h ClusterUpgrade) Stop(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.StopClusterUpgrade(r.Context(), r.PathValue("cluster"))
	h.respond(w, http.StatusOK, resp, err)
}

// Resume goes on after a pause.
func (h ClusterUpgrade) Resume(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.ResumeClusterUpgrade(r.Context(), r.PathValue("cluster"))
	h.respond(w, http.StatusOK, resp, err)
}

// Rollback puts every node back on the previous version.
func (h ClusterUpgrade) Rollback(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.RollbackClusterUpgrade(r.Context(), r.PathValue("cluster"))
	h.respond(w, http.StatusOK, resp, err)
}

// Finalize turns the new version on for good.
func (h ClusterUpgrade) Finalize(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.FinalizeClusterUpgrade(r.Context(), r.PathValue("cluster"))
	h.respond(w, http.StatusOK, resp, err)
}
