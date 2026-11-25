package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
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

// Snapshot is a sandbox snapshot or an archived sandbox, owned by one
// workspace's AerolVM team. Kind tells them apart; an archive is a snapshot of
// a stopped sandbox that lives in a cluster's bucket. Organization and Node
// are only included for instance admins, and an archive has no node.
type Snapshot struct {
	ID                    string               `json:"id"`
	Kind                  string               `json:"kind"`
	Name                  string               `json:"name"`
	Region                string               `json:"region"`
	Organization          *SandboxOrganization `json:"organization,omitempty"`
	Node                  *string              `json:"node,omitempty"`
	SandboxName           string               `json:"sandbox_name"`
	SourceSandboxID       string               `json:"source_sandbox_id"`
	Image                 string               `json:"image"`
	Runtime               string               `json:"runtime"`
	CreatedAt             time.Time            `json:"created_at"`
	ArchivedAt            *time.Time           `json:"archived_at"`
	CPU                   float64              `json:"cpu"`
	MemoryMB              int64                `json:"memory_mb"`
	DiskGB                int64                `json:"disk_gb"`
	Tags                  map[string]string    `json:"tags"`
	Status                string               `json:"status"`
	Error                 string               `json:"error"`
	RegistryRef           string               `json:"registry_ref"`
	LastRestoredSandboxID string               `json:"last_restored_sandbox_id"`
	LastRestoredAt        *time.Time           `json:"last_restored_at"`
}

// Snapshot kinds. An archive is a snapshot of a stopped sandbox, kept in a
// cluster's bucket rather than on a node.
const (
	snapshotKindSnapshot = "snapshot"
	snapshotKindArchive  = "archive"
)

// Snapshot statuses use AerolVM's vocabulary for both kinds.
const (
	snapshotStatusPending = "pending"
	snapshotStatusPushing = "pushing"
	snapshotStatusActive  = "active"
	snapshotStatusError   = "error"
)

// SnapshotList is returned by ListSnapshots. Data is never null; PartialNodes
// names nodes that did not answer. Summary is present only when the caller
// asks for it.
type SnapshotList struct {
	Data         []Snapshot       `json:"data"`
	Pages        Pages            `json:"pages"`
	Summary      *SnapshotSummary `json:"summary,omitempty"`
	PartialNodes []string         `json:"partial_nodes"`
}

// SnapshotSummary totals the whole filtered set, before paging.
type SnapshotSummary struct {
	Count       int                        `json:"count"`
	DiskGB      int64                      `json:"disk_gb"`
	MemoryMB    int64                      `json:"memory_mb"`
	Failed      int                        `json:"failed"`
	Workspaces  int                        `json:"workspaces"`
	ByNode      []SnapshotSummaryNode      `json:"by_node"`
	ByWorkspace []SnapshotSummaryWorkspace `json:"by_workspace"`
}

// SnapshotSummaryNode is one region, node and kind in the summary. Node is
// nil for an archive, and for any caller who may not see nodes.
type SnapshotSummaryNode struct {
	Region string  `json:"region"`
	Node   *string `json:"node"`
	Kind   string  `json:"kind"`
	Count  int     `json:"count"`
	DiskGB int64   `json:"disk_gb"`
}

// SnapshotSummaryWorkspace is one workspace in the summary.
type SnapshotSummaryWorkspace struct {
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Count  int    `json:"count"`
	DiskGB int64  `json:"disk_gb"`
}

// ListSnapshotsRequest pages and filters snapshots. Region, Organization,
// Kind and Status are comma-separated multi-value filters; Organization is
// for instance admins. Summary is "include" to add the totals.
type ListSnapshotsRequest struct {
	ListQuery
	Region       string `json:"region"`
	Org          string `json:"org"`
	Organization string `json:"organization"`
	Kind         string `json:"kind"`
	Status       string `json:"status"`
	Summary      string `json:"summary"`
}

// snapshotSorts names the sort keys the merged list supports. The values are
// the keys themselves because sorting and paging happen in Go.
var snapshotSorts = map[string]string{
	"created":  "created",
	"-created": "-created",
	"name":     "name",
	"-name":    "-name",
	"size":     "size",
	"-size":    "-size",
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
// PartialNodes. Filtering, sorting and paging happen here, over the cached
// merged list.
func (s *Service) ListSnapshots(ctx context.Context, req ListSnapshotsRequest) (SnapshotList, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return SnapshotList{}, err
	}
	q, err := parseListQuery(req.ListQuery, snapshotSorts, "-created")
	if err != nil {
		return SnapshotList{}, err
	}
	kinds, err := parseList("kind", req.Kind, []string{snapshotKindSnapshot, snapshotKindArchive})
	if err != nil {
		return SnapshotList{}, err
	}
	statuses, err := parseList("status", req.Status, []string{snapshotStatusPending, snapshotStatusPushing, snapshotStatusActive, snapshotStatusError})
	if err != nil {
		return SnapshotList{}, err
	}
	includeSummary := false
	switch req.Summary {
	case "":
	case "include":
		includeSummary = true
	default:
		return SnapshotList{}, badRequestField("summary", "summary must be include")
	}
	org, err := s.scopeOrg(ctx, req.Org)
	if err != nil {
		return SnapshotList{}, err
	}
	regions, err := parseList("region", req.Region, nil)
	if err != nil {
		return SnapshotList{}, err
	}
	regionIDs, err := s.regionIDsForFilter(ctx, org, regions, "region")
	if err != nil {
		return SnapshotList{}, err
	}
	regionSet := refSet(regionIDs)
	orgs, err := parseList("organization", req.Organization, nil)
	if err != nil {
		return SnapshotList{}, err
	}
	orgIDs, err := s.organizationIDsForFilter(ctx, orgs, "organization")
	if err != nil {
		return SnapshotList{}, err
	}
	orgSet := refSet(orgIDs)

	targets, err := s.snapshotTargetsFor(ctx, org, regionSet, orgSet)
	if err != nil {
		return SnapshotList{}, err
	}
	out := SnapshotList{Data: make([]Snapshot, 0), PartialNodes: make([]string, 0)}
	// Archives come from the database, so they are never partial and are
	// listed even when a region's nodes do not answer.
	archives, err := s.listArchiveSnapshots(ctx, org, regionSet, orgSet, p.InstanceAdmin)
	if err != nil {
		return SnapshotList{}, err
	}
	out.Data = append(out.Data, archives...)
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

	// Filters apply to the merged list, archives included.
	kept := out.Data[:0]
	for _, snapshot := range out.Data {
		if len(kinds) > 0 && !slices.Contains(kinds, snapshot.Kind) {
			continue
		}
		if len(statuses) > 0 && !slices.Contains(statuses, snapshot.Status) {
			continue
		}
		if q.q != "" && !strings.Contains(strings.ToLower(snapshot.Name), strings.ToLower(q.q)) {
			continue
		}
		kept = append(kept, snapshot)
	}
	// The summary covers the whole filtered set, before paging.
	if includeSummary {
		out.Summary = snapshotSummary(kept, org, p.InstanceAdmin)
	}
	sortSnapshots(kept, q.sortKey)
	out.Data, out.Pages = pageSlice(q, kept)
	sort.Strings(out.PartialNodes)
	return out, nil
}

// snapshotSummary totals the filtered snapshots. Every list is non-nil, so
// the JSON carries [] rather than null.
func snapshotSummary(data []Snapshot, scope *db.Organization, admin bool) *SnapshotSummary {
	summary := &SnapshotSummary{
		Count:       len(data),
		ByNode:      []SnapshotSummaryNode{},
		ByWorkspace: []SnapshotSummaryWorkspace{},
	}
	type nodeKey struct {
		region string
		node   string
		kind   string
	}
	nodes := make(map[nodeKey]*SnapshotSummaryNode)
	workspaces := make(map[string]*SnapshotSummaryWorkspace)
	for _, snapshot := range data {
		summary.DiskGB += snapshot.DiskGB
		summary.MemoryMB += snapshot.MemoryMB
		if snapshot.Status == snapshotStatusError {
			summary.Failed++
		}
		node := ""
		if snapshot.Node != nil {
			node = *snapshot.Node
		}
		key := nodeKey{region: snapshot.Region, node: node, kind: snapshot.Kind}
		entry, ok := nodes[key]
		if !ok {
			entry = &SnapshotSummaryNode{Region: snapshot.Region, Kind: snapshot.Kind}
			if snapshot.Node != nil {
				value := *snapshot.Node
				entry.Node = &value
			}
			nodes[key] = entry
		}
		entry.Count++
		entry.DiskGB += snapshot.DiskGB

		owner := ownerFor(snapshot, scope, admin)
		if owner == nil {
			continue
		}
		ws, ok := workspaces[owner.Slug]
		if !ok {
			ws = &SnapshotSummaryWorkspace{Slug: owner.Slug, Name: owner.Name}
			workspaces[owner.Slug] = ws
		}
		ws.Count++
		ws.DiskGB += snapshot.DiskGB
	}
	for _, entry := range nodes {
		summary.ByNode = append(summary.ByNode, *entry)
	}
	sort.Slice(summary.ByNode, func(i, j int) bool {
		a, b := summary.ByNode[i], summary.ByNode[j]
		if a.DiskGB != b.DiskGB {
			return a.DiskGB > b.DiskGB
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.Region != b.Region {
			return a.Region < b.Region
		}
		if (a.Node == nil) != (b.Node == nil) {
			return a.Node != nil
		}
		if a.Node != nil && *a.Node != *b.Node {
			return *a.Node < *b.Node
		}
		return a.Kind < b.Kind
	})
	for _, entry := range workspaces {
		summary.ByWorkspace = append(summary.ByWorkspace, *entry)
	}
	sort.Slice(summary.ByWorkspace, func(i, j int) bool {
		a, b := summary.ByWorkspace[i], summary.ByWorkspace[j]
		if a.DiskGB != b.DiskGB {
			return a.DiskGB > b.DiskGB
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Slug < b.Slug
	})
	summary.Workspaces = len(workspaces)
	if !admin && len(data) > 0 {
		summary.Workspaces = 1
	}
	return summary
}

// ownerFor names a snapshot's workspace for the summary. A customer may not
// see the owner on a row, so their scoped workspace stands in for it.
func ownerFor(snapshot Snapshot, scope *db.Organization, admin bool) *SandboxOrganization {
	if admin {
		return snapshot.Organization
	}
	if scope == nil {
		return nil
	}
	return &SandboxOrganization{Slug: scope.Slug, Name: scope.Name}
}

// refSet turns a resolved filter list into a set, or nil when the filter is
// not applied.
func refSet(ids []string) map[string]bool {
	if len(ids) == 0 {
		return nil
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

// sortSnapshots orders the merged list by a validated key. The tiebreak is
// total (name, region, owner, kind, status), so no row repeats or goes
// missing across pages.
func sortSnapshots(data []Snapshot, key string) {
	desc := strings.HasPrefix(key, "-")
	metric := strings.TrimPrefix(key, "-")
	sort.SliceStable(data, func(i, j int) bool {
		a, b := data[i], data[j]
		cmp := 0
		switch metric {
		case "name":
			cmp = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case "size":
			switch {
			case a.DiskGB < b.DiskGB:
				cmp = -1
			case a.DiskGB > b.DiskGB:
				cmp = 1
			}
		default:
			cmp = a.CreatedAt.Compare(b.CreatedAt)
		}
		if cmp == 0 {
			if c := strings.Compare(a.Name, b.Name); c != 0 {
				return c < 0
			}
			if c := strings.Compare(a.Region, b.Region); c != 0 {
				return c < 0
			}
			if c := strings.Compare(snapshotOrgSlug(a), snapshotOrgSlug(b)); c != 0 {
				return c < 0
			}
			if c := strings.Compare(a.Kind, b.Kind); c != 0 {
				return c < 0
			}
			return a.Status < b.Status
		}
		if desc {
			return cmp > 0
		}
		return cmp < 0
	})
}

// snapshotOrgSlug is the owner's slug, for the total sort order.
func snapshotOrgSlug(s Snapshot) string {
	if s.Organization == nil {
		return ""
	}
	return s.Organization.Slug
}

// listArchiveSnapshots reads the live archives the caller may see and returns
// them as snapshot rows of kind archive. Archives come from the database, so
// they never make a list partial.
func (s *Service) listArchiveSnapshots(ctx context.Context, org *db.Organization, regionSet, orgSet map[string]bool, admin bool) ([]Snapshot, error) {
	orgID := ""
	if org != nil {
		orgID = org.ID
	}
	rows, err := s.queries(ctx).ListArchives(ctx, db.ListArchivesParams{OrgID: orgID, ClusterID: ""})
	if err != nil {
		return nil, fmt.Errorf("list archives: %w", err)
	}
	archives := make([]Snapshot, 0, len(rows))
	for _, row := range rows {
		if regionSet != nil && !regionSet[row.ClusterID] {
			continue
		}
		if orgSet != nil && !orgSet[row.OrgID] {
			continue
		}
		var owner *SandboxOrganization
		if admin {
			owner = &SandboxOrganization{Slug: row.Org, Name: row.OrgName}
		}
		archives = append(archives, archiveSnapshot(db.Archive{
			ID:                    row.ID,
			OrgID:                 row.OrgID,
			ClusterID:             row.ClusterID,
			SandboxID:             row.SandboxID,
			SandboxName:           row.SandboxName,
			Image:                 row.Image,
			Runtime:               row.Runtime,
			Cpu:                   row.Cpu,
			MemoryMb:              row.MemoryMb,
			DiskGb:                row.DiskGb,
			Tags:                  row.Tags,
			SnapshotName:          row.SnapshotName,
			RegistryRef:           row.RegistryRef,
			Status:                row.Status,
			Error:                 row.Error,
			Attempts:              row.Attempts,
			NextAttemptAt:         row.NextAttemptAt,
			PushingSince:          row.PushingSince,
			CreatedAt:             row.CreatedAt,
			ArchivedAt:            row.ArchivedAt,
			DeletedAt:             row.DeletedAt,
			LastRestoredSandboxID: row.LastRestoredSandboxID,
			LastRestoredAt:        row.LastRestoredAt,
		}, row.Region, owner))
	}
	return archives, nil
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
		return Snapshot{}, badRequest("a snapshot name is required")
	}
	if isArchiveSnapshot(name) {
		return Snapshot{}, invalidField("snapshot", "%s is an archived sandbox: delete it with the archive API", name)
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
		return Snapshot{}, invalidField("region", "snapshot %q exists in more than one region or workspace; choose a region", name)
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
	if err := s.record(ctx, Event{
		Name:           "snapshot.deleted",
		TargetType:     "snapshot",
		TargetID:       name,
		TargetName:     name,
		OrganizationID: found.target.org.ID,
	}); err != nil {
		return Snapshot{}, err
	}
	return forPrincipal(found.snapshot, found.target.org, p.InstanceAdmin), nil
}

// snapshotTargets lists the workspaces and regions a single-region command
// may read. With no organization chosen, an instance admin sees every
// workspace.
func (s *Service) snapshotTargets(ctx context.Context, org *db.Organization, region string) ([]snapshotTarget, error) {
	filter, err := s.regionFilter(ctx, org, region)
	if err != nil {
		return nil, err
	}
	var regionSet map[string]bool
	if filter != nil {
		regionSet = map[string]bool{filter.ID: true}
	}
	return s.snapshotTargetsFor(ctx, org, regionSet, nil)
}

// snapshotTargetsFor lists the workspaces and regions a list may read,
// narrowed by the resolved region and workspace filters.
func (s *Service) snapshotTargetsFor(ctx context.Context, org *db.Organization, regionSet, orgSet map[string]bool) ([]snapshotTarget, error) {
	clusters, err := s.queries(ctx).ListClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	var orgs []db.Organization
	if org != nil {
		orgs = append(orgs, *org)
	} else {
		orgs, err = s.queries(ctx).ListOrganizations(ctx)
		if err != nil {
			return nil, fmt.Errorf("list organizations: %w", err)
		}
	}
	targets := make([]snapshotTarget, 0)
	for _, row := range orgs {
		if orgSet != nil && !orgSet[row.ID] {
			continue
		}
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
			if regionSet != nil && !regionSet[cluster.ID] {
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
	rows, err := s.queries(ctx).ListNodesByCluster(ctx, sql.NullString{String: cluster.ID, Valid: true})
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
				// Archives are listed and deleted through the archive API,
				// not as customer snapshots.
				if seen[snapshot.Name] || isArchiveSnapshot(snapshot.Name) {
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
	return s.teamToken(ctx, cluster, org, "view", "viewer", s.snapshotTokens)
}

// archiveToken returns a cached runner token for a workspace's team, so the
// archiver can read, snapshot and delete that workspace's sandboxes only.
func (s *Service) archiveToken(ctx context.Context, cluster db.Cluster, org db.Organization) (snapshotToken, error) {
	return s.teamToken(ctx, cluster, org, "arch", "runner", s.archiveTokens)
}

// teamToken returns a cached token for a workspace's team, minting one for an
// hour when cache has no live entry.
func (s *Service) teamToken(ctx context.Context, cluster db.Cluster, org db.Organization, kind, role string, cache map[snapshotKey]snapshotToken) (snapshotToken, error) {
	team := org.Team.String
	key := snapshotKey{cluster.ID, team}

	s.snapshotMu.Lock()
	if cached, ok := cache[key]; ok && s.now().Before(cached.expires) {
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
		Name:      tempTokenName(kind, org.ID, s.now()),
		Role:      role,
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
	if cached, ok := cache[key]; ok && now.Before(cached.expires) {
		minted = cached
	} else {
		cache[key] = minted
	}
	s.snapshotMu.Unlock()
	return minted, nil
}

// isArchiveSnapshot names the snapshots the archiver takes for stopped
// sandboxes. They belong to the archive API, not to the customer's snapshots.
func isArchiveSnapshot(name string) bool {
	return strings.HasPrefix(name, "archive-")
}

// toSnapshot converts one node's snapshot. node is the member that holds it;
// forPrincipal decides whether the caller sees it.
func toSnapshot(snapshot aerolvm.Snapshot, region, node string) Snapshot {
	return Snapshot{
		Kind:            snapshotKindSnapshot,
		Name:            snapshot.Name,
		Region:          region,
		Node:            &node,
		SourceSandboxID: snapshot.SourceSandboxID,
		Image:           snapshot.Image,
		CreatedAt:       snapshot.CreatedAt.UTC().Truncate(time.Millisecond),
		CPU:             snapshot.CPU,
		MemoryMB:        snapshot.MemoryMB,
		DiskGB:          snapshot.DiskGB,
		Tags:            map[string]string{},
		Status:          snapshot.PushState,
		Error:           snapshot.PushError,
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
// "lbx.view.<32 hex>.<ms base36>.<8 hex>", inside AerolVM's 64-character
// limit. The random part keeps two mints in the same millisecond apart: on a
// clash mintSandboxToken revokes the token holding the name, which another
// caller may be using.
func tempTokenName(kind, orgID string, now time.Time) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return "lbx." + kind + "." + strings.ReplaceAll(orgID, "-", "") + "." + strconv.FormatInt(now.UnixMilli(), 36) + "." + hex.EncodeToString(b[:])
}
