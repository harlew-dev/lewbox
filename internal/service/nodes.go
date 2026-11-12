package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"uuid"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/db"
)

// Node is an AerolVM node registered with Lewbox. Nodes that were added by
// URL before clusters existed leave the cluster fields empty.
type Node struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	URL        string     `json:"url"`
	CreatedAt  time.Time  `json:"created_at"`
	ClusterID  *string    `json:"cluster_id,omitempty"`
	Cluster    string     `json:"cluster,omitempty"` // cluster slug
	Role       string     `json:"role,omitempty"`
	Version    string     `json:"version,omitempty"`
	Alive      *bool      `json:"alive,omitempty"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
}

// NodeList is returned by ListNodes. Data is never null.
type NodeList struct {
	Data []Node `json:"data"`
}

// AddNodeRequest is the input to AddNode.
type AddNodeRequest struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ListNodesRequest filters ListNodes. An empty Cluster means every node.
type ListNodesRequest struct {
	Cluster string `json:"cluster"`
}

// RemoveNodeRequest is the input to RemoveNode. Force passes through to
// AerolVM, which otherwise refuses to delete a node that still owns sandboxes.
//
// There is deliberately no "unregister but leave it in the cluster" option:
// the next sync would find the node among the members and add it back.
type RemoveNodeRequest struct {
	Force bool `json:"force"`
}

// A DNS label: lowercase letters, digits and hyphens, 1–63 characters,
// starting and ending with a letter or digit.
var nodeName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// AddNode registers a node by URL. Names are unique.
func (s *Service) AddNode(ctx context.Context, req AddNodeRequest) (Node, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return Node{}, err
	}
	name := strings.TrimSpace(req.Name)
	if !nodeName.MatchString(name) {
		return Node{}, invalid("name must be 1-63 lowercase letters, digits or hyphens, and start and end with a letter or digit")
	}
	// Names and IDs share one lookup argument (GetNode, RemoveNode), so a
	// name must never parse as a UUID.
	if _, err := uuid.Parse(name); err == nil {
		return Node{}, invalid("name must not be a UUID")
	}
	nodeURL, err := normalizeURL(req.URL)
	if err != nil {
		return Node{}, err
	}

	row, err := s.q.CreateNode(ctx, db.CreateNodeParams{
		ID:        uuid.NewV7().String(),
		Name:      name,
		URL:       nodeURL,
		CreatedAt: s.now(),
	})
	if db.IsUniqueViolation(err) {
		return Node{}, conflict("a node named %q already exists", name)
	}
	if err != nil {
		return Node{}, fmt.Errorf("create node: %w", err)
	}
	return toNode(row), nil
}

// ListNodes returns nodes ordered by cluster slug and then name. Nodes that
// do not belong to a cluster come last.
func (s *Service) ListNodes(ctx context.Context, req ListNodesRequest) (NodeList, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return NodeList{}, err
	}
	var rows []db.Node
	var err error
	if strings.TrimSpace(req.Cluster) != "" {
		var cluster db.Cluster
		cluster, err = s.findCluster(ctx, req.Cluster)
		if err != nil {
			return NodeList{}, err
		}
		rows, err = s.q.ListNodesByCluster(ctx, sql.NullString{String: cluster.ID, Valid: true})
	} else {
		rows, err = s.q.ListNodes(ctx)
	}
	if err != nil {
		return NodeList{}, fmt.Errorf("list nodes: %w", err)
	}
	slugs, err := s.clusterSlugs(ctx)
	if err != nil {
		return NodeList{}, err
	}
	out := NodeList{Data: make([]Node, 0, len(rows))}
	for _, r := range rows {
		out.Data = append(out.Data, toNodeWithSlug(r, slugs))
	}
	sort.SliceStable(out.Data, func(i, j int) bool {
		a, b := out.Data[i], out.Data[j]
		if (a.Cluster == "") != (b.Cluster == "") {
			return a.Cluster != ""
		}
		if a.Cluster != b.Cluster {
			return a.Cluster < b.Cluster
		}
		return a.Name < b.Name
	})
	return out, nil
}

// GetNode returns a node by ID or name.
func (s *Service) GetNode(ctx context.Context, ref string) (Node, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return Node{}, err
	}
	row, err := s.findNode(ctx, ref)
	if err != nil {
		return Node{}, err
	}
	return s.decorateNode(ctx, row)
}

// RemoveNode takes a node out of its cluster and deletes it. A legacy node
// that has no cluster is only deleted.
func (s *Service) RemoveNode(ctx context.Context, ref string, req RemoveNodeRequest) (Node, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return Node{}, err
	}
	row, err := s.findNode(ctx, ref)
	if err != nil {
		return Node{}, err
	}
	node, err := s.decorateNode(ctx, row)
	if err != nil {
		return Node{}, err
	}
	if row.ClusterID.Valid {
		if err := s.removeClusterNode(ctx, row, req); err != nil {
			return Node{}, err
		}
	}
	n, err := s.q.DeleteNode(ctx, row.ID)
	if err != nil {
		return Node{}, fmt.Errorf("delete node: %w", err)
	}
	if n == 0 { // removed by someone else in between
		return Node{}, notFound("node %q not found", ref)
	}
	return node, nil
}

func (s *Service) removeClusterNode(ctx context.Context, row db.Node, req RemoveNodeRequest) error {
	cluster, err := s.q.GetClusterByID(ctx, row.ClusterID.String)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}
	siblings, err := s.q.ListNodesByCluster(ctx, sql.NullString{String: cluster.ID, Valid: true})
	if err != nil {
		return fmt.Errorf("list cluster nodes: %w", err)
	}
	if len(siblings) <= 1 {
		return conflict("node %q is the only node in cluster %q; use `lewbox cluster remove %q` to remove the cluster", row.Name, cluster.Slug, cluster.Slug)
	}

	// Lewbox reaches the cluster through admin_url. Removing the node behind
	// it would leave Lewbox talking to a machine that is no longer a member.
	if row.URL == cluster.AdminURL {
		return conflict("node %q serves cluster %q's admin URL %s; register the cluster through another node first", row.Name, cluster.Slug, cluster.AdminURL)
	}

	client, err := s.clusterClient(cluster)
	if err != nil {
		return err
	}
	members, err := client.Members(ctx)
	if err != nil {
		return unavailable("cluster %q: %s", cluster.Slug, err)
	}
	member := false
	for _, m := range members.Members {
		if m.NodeID == row.Name {
			member = true
			break
		}
	}
	if !member {
		// The cluster already forgot it; only the row is left to remove.
		return nil
	}
	if err := client.DrainNode(ctx, row.Name); err != nil {
		return unavailable("cluster %q: drain %q: %s", cluster.Slug, row.Name, err)
	}
	if err := client.DeleteNode(ctx, row.Name, req.Force); err != nil {
		if apiErr, ok := errors.AsType[*aerolvm.APIError](err); !ok || apiErr.Status != 404 {
			return unavailable("cluster %q: delete %q: %s", cluster.Slug, row.Name, err)
		}
	}
	return nil
}

func (s *Service) findNode(ctx context.Context, ref string) (db.Node, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return db.Node{}, invalid("a node ID or name is required")
	}

	var (
		row db.Node
		err error
	)
	if id, perr := uuid.Parse(ref); perr == nil {
		row, err = s.q.GetNodeByID(ctx, id.String())
	} else {
		row, err = s.q.GetNodeByName(ctx, ref)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return db.Node{}, notFound("node %q not found", ref)
	}
	if err != nil {
		return db.Node{}, fmt.Errorf("get node: %w", err)
	}
	return row, nil
}

func (s *Service) decorateNode(ctx context.Context, r db.Node) (Node, error) {
	n := toNode(r)
	if !r.ClusterID.Valid {
		return n, nil
	}
	cluster, err := s.q.GetClusterByID(ctx, r.ClusterID.String)
	if err != nil {
		return Node{}, fmt.Errorf("get cluster: %w", err)
	}
	n.Cluster = cluster.Slug
	return n, nil
}

// clusterSlugs maps cluster IDs to slugs for node lists.
func (s *Service) clusterSlugs(ctx context.Context) (map[string]string, error) {
	clusters, err := s.q.ListClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	slugs := make(map[string]string, len(clusters))
	for _, c := range clusters {
		slugs[c.ID] = c.Slug
	}
	return slugs, nil
}

// normalizeURL requires an absolute http(s) URL with a host and no query or
// fragment, and drops any trailing slash.
func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", invalid("url is required")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", invalid("url must be an absolute http or https URL, e.g. https://node-1.example.com:8443")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", invalid("url must not contain credentials, a query or a fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

func toNode(r db.Node) Node {
	n := Node{
		ID:        r.ID,
		Name:      r.Name,
		URL:       r.URL,
		CreatedAt: r.CreatedAt.UTC(),
		Role:      r.Role,
		Version:   r.Version,
	}
	if r.ClusterID.Valid {
		id := r.ClusterID.String
		n.ClusterID = &id
	}
	if r.Alive.Valid {
		alive := r.Alive.Int64 != 0
		n.Alive = &alive
	}
	if r.LastSeenAt.Valid {
		last := r.LastSeenAt.Time.UTC()
		n.LastSeenAt = &last
	}
	return n
}

func toNodeWithSlug(r db.Node, slugs map[string]string) Node {
	n := toNode(r)
	if r.ClusterID.Valid {
		n.Cluster = slugs[r.ClusterID.String]
	}
	return n
}
