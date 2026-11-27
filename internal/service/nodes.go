package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
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
	// Settings is what the node reported it applied, and whether that is
	// behind its cluster's profiles. Null for a node in no cluster.
	Settings *NodeSettingsState `json:"settings"`
}

// NodeList is returned by ListNodes. Data is never null.
type NodeList struct {
	Data  []Node `json:"data"`
	Pages Pages  `json:"pages"`
}

// AddNodeRequest is the input to AddNode.
type AddNodeRequest struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ListNodesRequest pages and filters ListNodes. Cluster and Role are
// comma-separated multi-value filters; Alive is true or false.
type ListNodesRequest struct {
	ListQuery
	Cluster string `json:"cluster"`
	Role    string `json:"role"`
	Alive   string `json:"alive"`
}

// nodeRoles are AerolVM's member roles.
var nodeRoles = []string{"server", "worker", "ingress", "mixed"}

// nodeSorts maps a public sort key to SQL written here. Nodes in no cluster
// sort last, as they always have.
var nodeSorts = mergeSorts(
	sortDirections("cluster",
		"(c.slug IS NULL) ASC, lower(c.slug) ASC, lower(n.name) ASC, n.id ASC",
		"(c.slug IS NULL) ASC, lower(c.slug) DESC, lower(n.name) DESC, n.id DESC"),
	sortDirections("name", "lower(n.name) ASC, n.id ASC", "lower(n.name) DESC, n.id DESC"),
)

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
		return Node{}, invalidField("name", "name must be 1-63 lowercase letters, digits or hyphens, and start and end with a letter or digit")
	}
	// Names and IDs share one lookup argument (GetNode, RemoveNode), so a
	// name must never parse as a UUID.
	if _, err := uuid.Parse(name); err == nil {
		return Node{}, invalidField("name", "name must not be a UUID")
	}
	nodeURL, err := normalizeURL(req.URL)
	if err != nil {
		return Node{}, err
	}

	var row db.Node
	err = s.withTx(ctx, func(ctx context.Context) error {
		var err error
		row, err = s.queries(ctx).CreateNode(ctx, db.CreateNodeParams{
			ID:        uuid.NewV7().String(),
			Name:      name,
			URL:       nodeURL,
			CreatedAt: s.now(),
		})
		if db.IsUniqueViolation(err) {
			return conflict("a node named %q already exists", name)
		}
		if err != nil {
			return fmt.Errorf("create node: %w", err)
		}
		return s.record(ctx, Event{
			Name:       "node.created",
			TargetType: "node",
			TargetID:   row.ID,
			TargetName: row.Name,
		})
	})
	if err != nil {
		return Node{}, err
	}
	return toNode(row), nil
}

// ListNodes returns nodes ordered by cluster slug and then name by default.
// Nodes that do not belong to a cluster come last.
func (s *Service) ListNodes(ctx context.Context, req ListNodesRequest) (NodeList, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return NodeList{}, err
	}
	q, err := parseListQuery(req.ListQuery, nodeSorts, "cluster")
	if err != nil {
		return NodeList{}, err
	}
	clustersFilter, err := parseList("cluster", req.Cluster, nil)
	if err != nil {
		return NodeList{}, err
	}
	clusterIDs, err := s.clusterIDsForFilter(ctx, clustersFilter, "cluster")
	if err != nil {
		return NodeList{}, err
	}
	roles, err := parseList("role", req.Role, nodeRoles)
	if err != nil {
		return NodeList{}, err
	}
	var alive *bool
	switch req.Alive {
	case "":
	case "true":
		v := true
		alive = &v
	case "false":
		v := false
		alive = &v
	default:
		return NodeList{}, badRequestField("alive", "alive must be true or false")
	}
	rows, total, err := s.queries(ctx).ListNodesPage(ctx, db.NodeListParams{
		ClusterIDs: clusterIDs,
		Roles:      roles,
		Alive:      alive,
		Pattern:    q.pattern,
		Order:      q.sort,
		Limit:      q.perPage,
		Offset:     (q.page - 1) * q.perPage,
	})
	if err != nil {
		return NodeList{}, fmt.Errorf("list nodes: %w", err)
	}
	clusters, err := s.clustersByID(ctx)
	if err != nil {
		return NodeList{}, err
	}
	out := NodeList{
		Data:  make([]Node, 0, len(rows)),
		Pages: newPages(q.page, q.perPage, total),
	}
	for _, row := range rows {
		n := toNode(row.Node)
		if row.ClusterSlug.Valid {
			n.Cluster = row.ClusterSlug.String
		}
		if row.Node.ClusterID.Valid {
			if cluster, ok := clusters[row.Node.ClusterID.String]; ok {
				if err := s.applyNodeOutdated(ctx, &n, row.Node, cluster); err != nil {
					return NodeList{}, err
				}
			}
		}
		out.Data = append(out.Data, n)
	}
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
	err = s.withTx(ctx, func(ctx context.Context) error {
		n, err := s.queries(ctx).DeleteNode(ctx, row.ID)
		if err != nil {
			return fmt.Errorf("delete node: %w", err)
		}
		if n == 0 { // removed by someone else in between
			return notFound("node %q not found", ref)
		}
		return s.record(ctx, Event{
			Name:       "node.deleted",
			TargetType: "node",
			TargetID:   row.ID,
			TargetName: row.Name,
		})
	})
	if err != nil {
		return Node{}, err
	}
	return node, nil
}

func (s *Service) removeClusterNode(ctx context.Context, row db.Node, req RemoveNodeRequest) error {
	cluster, err := s.queries(ctx).GetClusterByID(ctx, row.ClusterID.String)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}
	siblings, err := s.queries(ctx).ListNodesByCluster(ctx, sql.NullString{String: cluster.ID, Valid: true})
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
	var member *aerolvm.Member
	for i := range members.Members {
		if members.Members[i].NodeID == row.Name {
			member = &members.Members[i]
			break
		}
	}
	if member == nil {
		// The cluster already forgot it; only the row is left to remove.
		return nil
	}
	leader := ""
	for _, m := range members.Members {
		if m.Leader {
			leader = m.NodeID
			break
		}
	}
	if leader == "" && hasVoter(members.Members) {
		// This AerolVM annotates voters but names no leader: a removal now
		// could not be proved to leave one behind.
		return unavailable("cluster %q: no Raft leader is reported; try again when the cluster has one", cluster.Slug)
	}
	if leader == row.Name {
		if err := transferLeadershipOff(ctx, client, cluster.Slug, row.Name, members.Members); err != nil {
			return err
		}
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

// leaderHandoffWait bounds how long RemoveNode waits for the member list to
// name the new leader after a transfer.
const leaderHandoffWait = 5 * time.Second

// hasVoter reports whether any member carries AerolVM's voter annotation. A
// cluster that annotates nobody predates those fields, so the check that needs
// them is skipped rather than refusing every removal there.
func hasVoter(members []aerolvm.Member) bool {
	for _, m := range members {
		if m.Voter {
			return true
		}
	}
	return false
}

// transferLeadershipOff moves Raft leadership off nodeID before it is removed,
// and confirms the new leader before anything is changed.
//
// AerolVM v0.7.x deletes a Raft server even when that server is the leader.
// The leader commits the smaller configuration with its own vote and shuts its
// Raft down; in a two-voter cluster the survivor then elects itself, but its
// gossip view can re-add the deleted node as a voter before it sees the deny
// record. The survivor then needs a vote from a node whose Raft is gone, so it
// has no leader and every write answers 503 (see §5 of
// docs/design/aerolvm-integration.md). Removing a follower avoids that path.
func transferLeadershipOff(ctx context.Context, client *aerolvm.Client, slug, nodeID string, members []aerolvm.Member) error {
	target := ""
	for _, m := range members {
		if m.NodeID != nodeID && m.Alive && m.Voter {
			target = m.NodeID
			break
		}
	}
	if target == "" {
		return conflict("node %q is the Raft leader of cluster %q and no other voter is alive to take over; add a server node before removing it", nodeID, slug)
	}
	_, err := client.TransferLeadership(ctx, target)
	if err != nil {
		return unavailable("cluster %q: move Raft leadership from %q to %q: %s", slug, nodeID, target, err)
	}
	deadline := time.Now().Add(leaderHandoffWait)
	for {
		got, err := client.Members(ctx)
		if err != nil {
			return unavailable("cluster %q: confirm the new Raft leader: %s", slug, err)
		}
		leader := ""
		for _, m := range got.Members {
			if m.Leader {
				leader = m.NodeID
			}
		}
		// Any leader other than the node being removed is safe: AerolVM
		// may pick another voter than the one asked for.
		if leader != "" && leader != nodeID {
			return nil
		}
		if time.Now().After(deadline) {
			if leader == "" {
				return unavailable("cluster %q: no Raft leader was reported after moving leadership off %q", slug, nodeID)
			}
			return unavailable("cluster %q: %q still reports itself as Raft leader after the transfer", slug, nodeID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (s *Service) findNode(ctx context.Context, ref string) (db.Node, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return db.Node{}, badRequest("a node ID or name is required")
	}

	var (
		row db.Node
		err error
	)
	if id, perr := uuid.Parse(ref); perr == nil {
		row, err = s.queries(ctx).GetNodeByID(ctx, id.String())
	} else {
		row, err = s.queries(ctx).GetNodeByName(ctx, ref)
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
	cluster, err := s.queries(ctx).GetClusterByID(ctx, r.ClusterID.String)
	if err != nil {
		return Node{}, fmt.Errorf("get cluster: %w", err)
	}
	n.Cluster = cluster.Slug
	if err := s.applyNodeOutdated(ctx, &n, r, cluster); err != nil {
		return Node{}, err
	}
	return n, nil
}

// normalizeURL requires an absolute http(s) URL with a host and no query or
// fragment, and drops any trailing slash.
func normalizeURL(raw string) (string, error) {
	return normalizeURLField(raw, "url")
}

// normalizeURLField is normalizeURL for a named request field.
func normalizeURLField(raw, field string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", invalidField(field, "%s is required", field)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", invalidField(field, "%s must be an absolute http or https URL, e.g. https://node-1.example.com:8443", field)
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", invalidField(field, "%s must not contain credentials, a query or a fragment", field)
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
	n.Settings = nodeSettingsState(r)
	return n
}
