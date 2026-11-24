package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// Clusters are the AerolVM clusters the operator runs. {cluster} is an ID or
// slug.
type Clusters struct{ *server }

// Index lists every cluster.
func (h Clusters) Index(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.ListClusters(r.Context())
	h.respond(w, http.StatusOK, resp, err)
}

// Store registers and validates a cluster.
func (h Clusters) Store(w http.ResponseWriter, r *http.Request) {
	var req service.AddClusterRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.AddCluster(r.Context(), req)
	h.respond(w, http.StatusCreated, resp, err)
}

// Show returns one cluster.
func (h Clusters) Show(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.GetCluster(r.Context(), r.PathValue("cluster"))
	h.respond(w, http.StatusOK, resp, err)
}

// Update changes a cluster's DNS and storage settings.
func (h Clusters) Update(w http.ResponseWriter, r *http.Request) {
	var req service.UpdateClusterRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.UpdateCluster(r.Context(), r.PathValue("cluster"), req)
	h.respond(w, http.StatusOK, resp, err)
}

// Destroy unregisters a cluster.
func (h Clusters) Destroy(w http.ResponseWriter, r *http.Request) {
	force, err := queryBool(r, "force")
	if err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.RemoveCluster(r.Context(), r.PathValue("cluster"), force)
	h.respond(w, http.StatusOK, resp, err)
}

// Check runs the cluster checks now.
func (h Clusters) Check(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.CheckCluster(r.Context(), r.PathValue("cluster"))
	h.respond(w, http.StatusOK, resp, err)
}

// Health returns a cluster's uptime history.
func (h Clusters) Health(w http.ResponseWriter, r *http.Request) {
	days, err := queryInt(r, "days", 0)
	if err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.ClusterHealth(r.Context(), r.PathValue("cluster"), days)
	h.respond(w, http.StatusOK, resp, err)
}

// Metrics returns a cluster's node metrics for a range.
func (h Clusters) Metrics(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.ClusterMetrics(r.Context(), r.PathValue("cluster"), r.URL.Query().Get("range"))
	h.respond(w, http.StatusOK, resp, err)
}

// Resolve finds a cluster by cluster or node reference. The reference may
// contain "/" (cluster/<x>), so it travels as a query parameter.
func (h Clusters) Resolve(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.ResolveCluster(r.Context(), r.URL.Query().Get("cluster"))
	h.respond(w, http.StatusOK, resp, err)
}

// ClusterNodeSettings is what a cluster's nodes must apply. The response
// carries the profiles' secrets, so it is admin-only.
type ClusterNodeSettings struct{ *server }

// Show returns the settings every node of the cluster must apply.
func (h ClusterNodeSettings) Show(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.ClusterNodeSettings(r.Context(), r.PathValue("cluster"))
	h.respond(w, http.StatusOK, resp, err)
}
