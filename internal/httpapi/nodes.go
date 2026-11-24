package httpapi

import (
	"net/http"

	"github.com/harlew-dev/lewbox/internal/service"
)

// Nodes are machines in a cluster. {node} is an ID or name.
type Nodes struct{ *server }

// Index lists nodes, optionally for one cluster.
func (h Nodes) Index(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.ListNodes(r.Context(), service.ListNodesRequest{Cluster: r.URL.Query().Get("cluster")})
	h.respond(w, http.StatusOK, resp, err)
}

// Store records a node by URL.
func (h Nodes) Store(w http.ResponseWriter, r *http.Request) {
	var req service.AddNodeRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.AddNode(r.Context(), req)
	h.respond(w, http.StatusCreated, resp, err)
}

// Show returns one node.
func (h Nodes) Show(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.GetNode(r.Context(), r.PathValue("node"))
	h.respond(w, http.StatusOK, resp, err)
}

// Destroy removes a node.
func (h Nodes) Destroy(w http.ResponseWriter, r *http.Request) {
	force, err := queryBool(r, "force")
	if err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.RemoveNode(r.Context(), r.PathValue("node"), service.RemoveNodeRequest{
		Force: force,
	})
	h.respond(w, http.StatusOK, resp, err)
}

// NodeSettings is what a node reports it applied.
type NodeSettings struct{ *server }

// Update records the profile versions a node applied.
func (h NodeSettings) Update(w http.ResponseWriter, r *http.Request) {
	var req service.ReportNodeSettingsRequest
	if err := h.decode(w, r, &req); err != nil {
		h.respond(w, 0, nil, err)
		return
	}
	resp, err := h.svc.ReportNodeSettings(r.Context(), r.PathValue("node"), req)
	h.respond(w, http.StatusOK, resp, err)
}
