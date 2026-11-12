package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/db"
)

const (
	snapshotTokenReuse  = 50 * time.Minute
	snapshotCacheTTL    = 30 * time.Second
	snapshotNodeTimeout = 5 * time.Second
	snapshotWorkers     = 4
)

// Snapshot is a sandbox snapshot owned by one workspace's AerolVM team.
type Snapshot struct {
	Name            string    `json:"name"`
	Region          string    `json:"region"`
	SourceSandboxID string    `json:"source_sandbox_id"`
	Image           string    `json:"image"`
	CreatedAt       time.Time `json:"created_at"`
	CPU             float64   `json:"cpu"`
	MemoryMB        int64     `json:"memory_mb"`
	DiskGB          int64     `json:"disk_gb"`
	PushState       string    `json:"push_state"`
}

// SnapshotList is returned by ListSnapshots. Data is never null; PartialNodes
// names nodes that did not answer.
type SnapshotList struct {
	Data         []Snapshot `json:"data"`
	PartialNodes []string   `json:"partial_nodes"`
}

// ListSnapshotsRequest filters snapshots. Org is empty for the whole fleet of
// an instance admin; Region is empty for every subscribed region.
type ListSnapshotsRequest struct {
	Region string `json:"region"`
	Org    string `json:"org"`
}

// SnapshotActionRequest picks the region for a delete and the organization the
// caller acts for.
type SnapshotActionRequest struct {
	Region string `json:"region"`
	Org    string `json:"org"`
}

// snapshotKey identifies one workspace's snapshots on one cluster.
type snapshotKey struct {
	clusterID string
	team      string
}

type snapshotToken struct {
	value   string
	expires time.Time
}

type snapshotCacheEntry struct {
	data    []Snapshot
	partial []string
	expires time.Time
}

// snapshotTarget is one workspace's snapshots in one region.
type snapshotTarget struct {
	org     db.Organization
	cluster db.Cluster
}

// ListSnapshots lists the snapshots of every workspace the caller may see, on
// every node of each region. A node that does not answer is reported in
// PartialNodes.
func (s *Service) ListSnapshots(ctx context.Context, req ListSnapshotsRequest) (SnapshotList, error) {
	_, org, _, err := s.scopeOrg(ctx, req.Org)
	if err != nil {
		return SnapshotList{}, err
	}
	targets, err := s.snapshotTargets(ctx, org, req.Region)
	if err != nil {
		return SnapshotList{}, err
	}
	out := SnapshotList{Data: make([]Snapshot, 0), PartialNodes: make([]string, 0)}
	partial := make(map[string]bool)
	for _, target := range targets {
		data, nodes, err := s.workspaceSnapshots(ctx, target, true)
		if err != nil {
			return SnapshotList{}, err
		}
		out.Data = append(out.Data, data...)
		for _, node := range nodes {
			if !partial[node] {
				partial[node] = true
				out.PartialNodes = append(out.PartialNodes, node)
			}
		}
	}
	sort.Slice(out.Data, func(i, j int) bool {
		a, b := out.Data[i], out.Data[j]
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		if a.Region != b.Region {
			return a.Region < b.Region
		}
		return a.Name < b.Name
	})
	sort.Strings(out.PartialNodes)
	return out, nil
}

// DeleteSnapshot deletes one snapshot with a temporary runner token bound to
// the workspace's team, so it can only hit the customer's own snapshot.
func (s *Service) DeleteSnapshot(ctx context.Context, name string, req SnapshotActionRequest) (Snapshot, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Snapshot{}, invalid("a snapshot name is required")
	}
	var org *db.Organization
	if strings.TrimSpace(req.Org) != "" || !p.InstanceAdmin {
		c, err := s.orgFor(ctx, req.Org, accessUse)
		if err != nil {
			return Snapshot{}, err
		}
		org = &c.org
	}
	targets, err := s.snapshotTargets(ctx, org, req.Region)
	if err != nil {
		return Snapshot{}, err
	}
	type match struct {
		target   snapshotTarget
		snapshot Snapshot
		node     string
	}
	matches := make([]match, 0, 1)
	for _, target := range targets {
		snapshot, node, ok, err := s.findSnapshotOnNodes(ctx, target, name)
		if err != nil {
			return Snapshot{}, err
		}
		if ok {
			matches = append(matches, match{target: target, snapshot: snapshot, node: node})
		}
	}
	switch len(matches) {
	case 0:
		return Snapshot{}, notFound("snapshot %q not found", name)
	case 1:
	default:
		return Snapshot{}, invalid("snapshot %q exists in more than one region or workspace: pass region", name)
	}
	found := matches[0]

	// Snapshots are node-local, so mint the token on the member that holds
	// this one and delete it there.
	adminToken, err := s.clusterToken(found.target.cluster)
	if err != nil {
		return Snapshot{}, err
	}
	admin := s.aerolvmClient(found.node, adminToken)
	team := found.target.org.Team.String
	token, err := mintSandboxToken(ctx, admin, aerolvm.CreateTokenRequest{
		Name:      fmt.Sprintf("lbx.del.%s.%d", found.target.org.ID, s.now().UnixNano()),
		Role:      "runner",
		Team:      team,
		ExpiresIn: "1h",
	})
	if err != nil {
		return Snapshot{}, s.regionUnavailable(ctx, found.target.cluster.Slug, err)
	}
	if token.Token == "" {
		return Snapshot{}, unavailable("region %q returned no token", found.target.cluster.Slug)
	}
	deleteErr := s.aerolvmClient(found.node, token.Token).DeleteSnapshot(ctx, name)
	if revokeErr := admin.RevokeToken(context.WithoutCancel(ctx), token.ID); revokeErr != nil {
		s.log.Warn("revoke snapshot token failed", "region", found.target.cluster.Slug, "err", revokeErr)
	}
	if deleteErr != nil {
		if apiErr, ok := errors.AsType[*aerolvm.APIError](deleteErr); ok && apiErr.Status == http.StatusNotFound {
			return Snapshot{}, notFound("snapshot %q not found", name)
		}
		return Snapshot{}, s.regionUnavailable(ctx, found.target.cluster.Slug, deleteErr)
	}
	s.snapshotMu.Lock()
	delete(s.snapshotCache, snapshotKey{found.target.cluster.ID, team})
	s.snapshotMu.Unlock()
	return found.snapshot, nil
}

// snapshotTargets lists the workspaces and regions the caller may read. With
// no organization chosen, an instance admin sees every workspace.
func (s *Service) snapshotTargets(ctx context.Context, org *db.Organization, region string) ([]snapshotTarget, error) {
	clusters, err := s.q.ListClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	var filter *db.Cluster
	if region = strings.TrimSpace(region); region != "" {
		cluster, err := s.findCluster(ctx, region)
		if err != nil {
			return nil, err
		}
		filter = &cluster
	}
	var orgs []db.Organization
	if org != nil {
		orgs = append(orgs, *org)
	} else {
		orgs, err = s.q.ListOrganizations(ctx)
		if err != nil {
			return nil, fmt.Errorf("list organizations: %w", err)
		}
	}
	targets := make([]snapshotTarget, 0)
	for _, row := range orgs {
		if !row.Team.Valid || row.Team.String == "" {
			continue
		}
		subs, err := s.subscriptionsByCluster(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		for _, cluster := range clusters {
			if _, ok := subs[cluster.ID]; !ok {
				continue
			}
			if filter != nil && cluster.ID != filter.ID {
				continue
			}
			targets = append(targets, snapshotTarget{org: row, cluster: cluster})
		}
	}
	return targets, nil
}

// snapshotNode is one member's address, for reaching node-local snapshots.
type snapshotNode struct {
	name string
	url  string
}

// snapshotNodes lists the addresses snapshots can live at, from the members
// Lewbox mirrored.
func (s *Service) snapshotNodes(ctx context.Context, cluster db.Cluster) ([]snapshotNode, error) {
	rows, err := s.q.ListNodesByCluster(ctx, sql.NullString{String: cluster.ID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list cluster nodes: %w", err)
	}
	nodes := make([]snapshotNode, 0, len(rows))
	for _, row := range rows {
		if row.URL != "" {
			nodes = append(nodes, snapshotNode{name: row.Name, url: row.URL})
		}
	}
	if len(nodes) == 0 {
		nodes = append(nodes, snapshotNode{name: cluster.Slug, url: cluster.AdminURL})
	}
	return nodes, nil
}

// findSnapshotOnNodes returns one snapshot and the node that holds it.
func (s *Service) findSnapshotOnNodes(ctx context.Context, target snapshotTarget, name string) (Snapshot, string, bool, error) {
	token, err := s.snapshotToken(ctx, target.cluster, target.org)
	if err != nil {
		return Snapshot{}, "", false, err
	}
	nodes, err := s.snapshotNodes(ctx, target.cluster)
	if err != nil {
		return Snapshot{}, "", false, err
	}
	for _, node := range nodes {
		reqCtx, cancel := context.WithTimeout(ctx, snapshotNodeTimeout)
		snapshots, err := s.aerolvmClient(node.url, token).ListSnapshots(reqCtx)
		cancel()
		if err != nil {
			continue
		}
		for _, snapshot := range snapshots {
			if snapshot.Name == name {
				return toSnapshot(snapshot, target.cluster.Slug), node.url, true, nil
			}
		}
	}
	return Snapshot{}, "", false, nil
}

// workspaceSnapshots reads one workspace's snapshots from every node of a
// cluster. The merged result is cached for 30 s per workspace and region.
func (s *Service) workspaceSnapshots(ctx context.Context, target snapshotTarget, cached bool) ([]Snapshot, []string, error) {
	key := snapshotKey{target.cluster.ID, target.org.Team.String}
	now := s.now()
	if cached {
		s.snapshotMu.Lock()
		entry, ok := s.snapshotCache[key]
		s.snapshotMu.Unlock()
		if ok && now.Before(entry.expires) {
			return append([]Snapshot(nil), entry.data...), append([]string(nil), entry.partial...), nil
		}
	}
	token, err := s.snapshotToken(ctx, target.cluster, target.org)
	if err != nil {
		return nil, nil, err
	}
	nodes, err := s.snapshotNodes(ctx, target.cluster)
	if err != nil {
		return nil, nil, err
	}

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		sem     = make(chan struct{}, snapshotWorkers)
		data    = make([]Snapshot, 0)
		seen    = make(map[string]bool)
		partial []string
	)
	for _, n := range nodes {
		wg.Add(1)
		go func(n snapshotNode) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			reqCtx, cancel := context.WithTimeout(ctx, snapshotNodeTimeout)
			defer cancel()
			snapshots, err := s.aerolvmClient(n.url, token).ListSnapshots(reqCtx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				partial = append(partial, n.name)
				return
			}
			for _, snapshot := range snapshots {
				if seen[snapshot.Name] {
					continue
				}
				seen[snapshot.Name] = true
				data = append(data, toSnapshot(snapshot, target.cluster.Slug))
			}
		}(n)
	}
	wg.Wait()
	if cached {
		s.snapshotMu.Lock()
		s.snapshotCache[key] = snapshotCacheEntry{
			data:    append([]Snapshot(nil), data...),
			partial: append([]string(nil), partial...),
			expires: s.now().Add(snapshotCacheTTL),
		}
		s.snapshotMu.Unlock()
	}
	return data, partial, nil
}

// snapshotToken returns a cached viewer token for a workspace's team, minting
// one for an hour when the cache has no live entry. Nothing is stored.
func (s *Service) snapshotToken(ctx context.Context, cluster db.Cluster, org db.Organization) (string, error) {
	team := org.Team.String
	key := snapshotKey{cluster.ID, team}

	s.snapshotMu.Lock()
	if cached, ok := s.snapshotTokens[key]; ok && s.now().Before(cached.expires) {
		s.snapshotMu.Unlock()
		return cached.value, nil
	}
	client, err := s.clusterClient(cluster)
	if err != nil {
		s.snapshotMu.Unlock()
		return "", err
	}
	token, err := mintSandboxToken(ctx, client, aerolvm.CreateTokenRequest{
		Name:      fmt.Sprintf("lbx.view.%s.%d", org.ID, s.now().UnixNano()),
		Role:      "viewer",
		Team:      team,
		ExpiresIn: "1h",
	})
	if err != nil {
		s.snapshotMu.Unlock()
		return "", s.regionUnavailable(ctx, cluster.Slug, err)
	}
	if token.Token == "" {
		s.snapshotMu.Unlock()
		return "", unavailable("region %q returned no snapshot token", cluster.Slug)
	}
	s.snapshotTokens[key] = snapshotToken{value: token.Token, expires: s.now().Add(snapshotTokenReuse)}
	s.snapshotMu.Unlock()
	return token.Token, nil
}

func toSnapshot(snapshot aerolvm.Snapshot, region string) Snapshot {
	return Snapshot{
		Name:            snapshot.Name,
		Region:          region,
		SourceSandboxID: snapshot.SourceSandboxID,
		Image:           snapshot.Image,
		CreatedAt:       snapshot.CreatedAt.UTC().Truncate(time.Millisecond),
		CPU:             snapshot.CPU,
		MemoryMB:        snapshot.MemoryMB,
		DiskGB:          snapshot.DiskGB,
		PushState:       snapshot.PushState,
	}
}
