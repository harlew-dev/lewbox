package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/db"
)

const (
	snapshotTokenReuse  = 50 * time.Minute
	snapshotTokenFresh  = 10 * time.Second
	snapshotTokenWait   = 2 * time.Second
	snapshotCacheTTL    = 30 * time.Second
	snapshotNodeTimeout = 5 * time.Second
	snapshotWorkers     = 4
)

// Snapshot is a sandbox snapshot owned by one workspace's AerolVM team.
// Organization and Node are only included for instance admins.
type Snapshot struct {
	Name            string               `json:"name"`
	Region          string               `json:"region"`
	Organization    *SandboxOrganization `json:"organization,omitempty"`
	Node            *string              `json:"node,omitempty"`
	SourceSandboxID string               `json:"source_sandbox_id"`
	Image           string               `json:"image"`
	CreatedAt       time.Time            `json:"created_at"`
	CPU             float64              `json:"cpu"`
	MemoryMB        int64                `json:"memory_mb"`
	DiskGB          int64                `json:"disk_gb"`
	PushState       string               `json:"push_state"`
}

// SnapshotList is returned by ListSnapshots. Data is never null; PartialNodes
// names nodes that did not answer.
type SnapshotList struct {
	Data         []Snapshot `json:"data"`
	PartialNodes []string   `json:"partial_nodes"`
}

// snapshotKey identifies one workspace's snapshots on one cluster.
type snapshotKey struct {
	clusterID string
	team      string
}

type snapshotToken struct {
	value   string
	minted  time.Time
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
func (s *Service) ListSnapshots(ctx context.Context, req RegionScope) (SnapshotList, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return SnapshotList{}, err
	}
	org, err := s.scopeOrg(ctx, req.Org)
	if err != nil {
		return SnapshotList{}, err
	}
	targets, err := s.snapshotTargets(ctx, org, req.Region)
	if err != nil {
		return SnapshotList{}, err
	}
	out := SnapshotList{Data: make([]Snapshot, 0), PartialNodes: make([]string, 0)}
	partial := make(map[string]bool)
	addPartial := func(name string) {
		if !partial[name] {
			partial[name] = true
			out.PartialNodes = append(out.PartialNodes, name)
		}
	}
	for _, target := range targets {
		data, nodes, err := s.workspaceSnapshots(ctx, target)
		if err != nil {
			// An unreachable region is partial, never a failed list.
			if regionNodes, nerr := s.snapshotNodes(ctx, target.cluster); nerr == nil {
				for _, node := range regionNodes {
					addPartial(node.name)
				}
			} else {
				addPartial(target.cluster.Slug)
			}
			continue
		}
		for _, snapshot := range data {
			out.Data = append(out.Data, forPrincipal(snapshot, target.org, p.InstanceAdmin))
		}
		for _, node := range nodes {
			addPartial(node)
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
func (s *Service) DeleteSnapshot(ctx context.Context, name string, req RegionScope) (Snapshot, error) {
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
		node     snapshotNode
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
		return Snapshot{}, invalid("snapshot %q exists in more than one region or workspace; choose a region", name)
	}
	found := matches[0]

	// Snapshots are node-local, so mint the token on the member that holds
	// this one and delete it there.
	adminToken, err := s.clusterToken(found.target.cluster)
	if err != nil {
		return Snapshot{}, err
	}
	admin := s.aerolvmClient(found.node.url, adminToken)
	team := found.target.org.Team.String
	mintCtx, cancel := context.WithTimeout(ctx, requestCallTimeout)
	token, err := mintSandboxToken(mintCtx, admin, aerolvm.CreateTokenRequest{
		Name:      tempTokenName("del", found.target.org.ID, s.now()),
		Role:      "runner",
		Team:      team,
		ExpiresIn: "1h",
	})
	cancel()
	if err != nil {
		return Snapshot{}, s.regionUnavailable(ctx, found.target.cluster.Slug, err)
	}
	if token.Token == "" {
		return Snapshot{}, unavailable("region %q returned no token", found.target.cluster.Slug)
	}
	deleteCtx, cancelDelete := context.WithTimeout(ctx, requestCallTimeout)
	deleteErr := s.aerolvmClient(found.node.url, token.Token).DeleteSnapshot(deleteCtx, name)
	cancelDelete()
	cleanup, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), requestCallTimeout)
	revokeErr := admin.RevokeToken(cleanup, token.ID)
	cancelCleanup()
	if revokeErr != nil {
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
	return forPrincipal(found.snapshot, found.target.org, p.InstanceAdmin), nil
}

// snapshotTargets lists the workspaces and regions the caller may read. With
// no organization chosen, an instance admin sees every workspace.
func (s *Service) snapshotTargets(ctx context.Context, org *db.Organization, region string) ([]snapshotTarget, error) {
	clusters, err := s.q.ListClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	filter, err := s.regionFilter(ctx, org, region)
	if err != nil {
		return nil, err
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

// findSnapshotOnNodes returns one snapshot and the node that holds it. A node
// that failed to answer is reported as a region error, never as "not found".
func (s *Service) findSnapshotOnNodes(ctx context.Context, target snapshotTarget, name string) (Snapshot, snapshotNode, bool, error) {
	token, err := s.snapshotToken(ctx, target.cluster, target.org)
	if err != nil {
		return Snapshot{}, snapshotNode{}, false, err
	}
	nodes, err := s.snapshotNodes(ctx, target.cluster)
	if err != nil {
		return Snapshot{}, snapshotNode{}, false, err
	}
	var firstErr error
	for _, node := range nodes {
		snapshots, err := s.listNodeSnapshots(ctx, node.url, token)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, snapshot := range snapshots {
			if snapshot.Name == name {
				return toSnapshot(snapshot, target.cluster.Slug, node.name), node, true, nil
			}
		}
	}
	if firstErr != nil {
		return Snapshot{}, snapshotNode{}, false, s.regionUnavailable(ctx, target.cluster.Slug, firstErr)
	}
	return Snapshot{}, snapshotNode{}, false, nil
}

// listNodeSnapshots reads one node's snapshots. A freshly minted token may not
// work on the other nodes yet, so a 401 retries once after a short wait.
func (s *Service) listNodeSnapshots(ctx context.Context, url string, token snapshotToken) ([]aerolvm.Snapshot, error) {
	list := func() ([]aerolvm.Snapshot, error) {
		reqCtx, cancel := context.WithTimeout(ctx, snapshotNodeTimeout)
		defer cancel()
		return s.aerolvmClient(url, token.value).ListSnapshots(reqCtx)
	}
	snapshots, err := list()
	if err == nil || !isUnauthorized(err) || s.now().Sub(token.minted) >= snapshotTokenFresh {
		return snapshots, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(snapshotTokenWait):
	}
	return list()
}

func isUnauthorized(err error) bool {
	apiErr, ok := errors.AsType[*aerolvm.APIError](err)
	return ok && apiErr.Status == http.StatusUnauthorized
}

// workspaceSnapshots reads one workspace's snapshots from every node of a
// cluster. The merged result is cached for 30 s per workspace and region.
func (s *Service) workspaceSnapshots(ctx context.Context, target snapshotTarget) ([]Snapshot, []string, error) {
	key := snapshotKey{target.cluster.ID, target.org.Team.String}
	now := s.now()
	s.snapshotMu.Lock()
	entry, ok := s.snapshotCache[key]
	s.snapshotMu.Unlock()
	if ok && now.Before(entry.expires) {
		return append([]Snapshot(nil), entry.data...), append([]string(nil), entry.partial...), nil
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
			snapshots, err := s.listNodeSnapshots(ctx, n.url, token)
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
				data = append(data, toSnapshot(snapshot, target.cluster.Slug, n.name))
			}
		}(n)
	}
	wg.Wait()
	s.snapshotMu.Lock()
	s.snapshotCache[key] = snapshotCacheEntry{
		data:    append([]Snapshot(nil), data...),
		partial: append([]string(nil), partial...),
		expires: s.now().Add(snapshotCacheTTL),
	}
	s.snapshotMu.Unlock()
	return data, partial, nil
}

// snapshotToken returns a cached viewer token for a workspace's team, minting
// one for an hour when the cache has no live entry.
func (s *Service) snapshotToken(ctx context.Context, cluster db.Cluster, org db.Organization) (snapshotToken, error) {
	team := org.Team.String
	key := snapshotKey{cluster.ID, team}

	s.snapshotMu.Lock()
	if cached, ok := s.snapshotTokens[key]; ok && s.now().Before(cached.expires) {
		s.snapshotMu.Unlock()
		return cached, nil
	}
	s.snapshotMu.Unlock()

	client, err := s.clusterClient(cluster)
	if err != nil {
		return snapshotToken{}, err
	}
	mintCtx, cancel := context.WithTimeout(ctx, requestCallTimeout)
	token, err := mintSandboxToken(mintCtx, client, aerolvm.CreateTokenRequest{
		Name:      tempTokenName("view", org.ID, s.now()),
		Role:      "viewer",
		Team:      team,
		ExpiresIn: "1h",
	})
	cancel()
	if err != nil {
		return snapshotToken{}, s.regionUnavailable(ctx, cluster.Slug, err)
	}
	if token.Token == "" {
		return snapshotToken{}, unavailable("region %q returned no snapshot token", cluster.Slug)
	}
	now := s.now()
	minted := snapshotToken{value: token.Token, minted: now, expires: now.Add(snapshotTokenReuse)}
	s.snapshotMu.Lock()
	// A concurrent request may have stored one already; keep it and let this
	// one expire unused.
	if cached, ok := s.snapshotTokens[key]; ok && now.Before(cached.expires) {
		minted = cached
	} else {
		s.snapshotTokens[key] = minted
	}
	s.snapshotMu.Unlock()
	return minted, nil
}

// toSnapshot converts one node's snapshot. node is the member that holds it;
// forPrincipal decides whether the caller sees it.
func toSnapshot(snapshot aerolvm.Snapshot, region, node string) Snapshot {
	return Snapshot{
		Name:            snapshot.Name,
		Region:          region,
		Node:            &node,
		SourceSandboxID: snapshot.SourceSandboxID,
		Image:           snapshot.Image,
		CreatedAt:       snapshot.CreatedAt.UTC().Truncate(time.Millisecond),
		CPU:             snapshot.CPU,
		MemoryMB:        snapshot.MemoryMB,
		DiskGB:          snapshot.DiskGB,
		PushState:       snapshot.PushState,
	}
}

// forPrincipal names the owning workspace and the node for an instance admin,
// and hides the node from everyone else.
func forPrincipal(snapshot Snapshot, org db.Organization, admin bool) Snapshot {
	if !admin {
		snapshot.Node = nil
		snapshot.Organization = nil
		return snapshot
	}
	if snapshot.Node != nil {
		node := *snapshot.Node
		snapshot.Node = &node
	}
	snapshot.Organization = &SandboxOrganization{Slug: org.Slug, Name: org.Name}
	return snapshot
}

// tempTokenName names a short-lived token Lewbox mints for a workspace:
// "lbx.view.<32 hex>.<ms base36>", well inside AerolVM's 64-character limit.
func tempTokenName(kind, orgID string, now time.Time) string {
	return "lbx." + kind + "." + strings.ReplaceAll(orgID, "-", "") + "." + strconv.FormatInt(now.UnixMilli(), 36)
}
