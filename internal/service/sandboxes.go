package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/harlew-dev/lewbox/internal/db"
)

// Sandbox is one sandbox in the mirror. Organization and Node are only
// included for instance admins.
type Sandbox struct {
	ID           string               `json:"id"`
	Region       string               `json:"region"`
	Organization *SandboxOrganization `json:"organization,omitempty"`
	Name         string               `json:"name"`
	Image        string               `json:"image"`
	Status       string               `json:"status"`
	StatusReason string               `json:"status_reason"`
	Runtime      string               `json:"runtime"`
	CPU          float64              `json:"cpu"`
	MemoryMB     int64                `json:"memory_mb"`
	DiskGB       int64                `json:"disk_gb"`
	Node         *string              `json:"node,omitempty"`
	CreatedAt    time.Time            `json:"created_at"`
	LastSeenAt   time.Time            `json:"last_seen_at"`
}

// SandboxOrganization names the organization a sandbox belongs to.
type SandboxOrganization struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// SandboxList is returned by ListSandboxes. Data is never null.
type SandboxList struct {
	Data []Sandbox `json:"data"`
}

// ListSandboxesRequest filters the mirror. Org is empty for the whole fleet
// of an instance admin.
type ListSandboxesRequest struct {
	Region string `json:"region"`
	Status string `json:"status"`
	Org    string `json:"org"`
}

// RegionScope picks the region and the workspace a fleet-capable route acts
// on. An empty Org is the whole fleet of an instance admin.
type RegionScope struct {
	Region string `json:"region"`
	Org    string `json:"org"`
}

// scopeOrg resolves the organization a list or usage call is about. nil means
// an instance admin with no organization chosen, who sees the whole fleet.
func (s *Service) scopeOrg(ctx context.Context, ref string) (*db.Organization, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		if p.InstanceAdmin {
			return nil, nil
		}
		c, err := s.orgFor(ctx, "", accessRead)
		if err != nil {
			return nil, err
		}
		return &c.org, nil
	}
	c, err := s.orgFor(ctx, ref, accessRead)
	if err != nil {
		return nil, err
	}
	return &c.org, nil
}

// regionFilter resolves a region argument: within the workspace's
// subscriptions when one is chosen, otherwise any cluster.
func (s *Service) regionFilter(ctx context.Context, org *db.Organization, ref string) (*db.Cluster, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, nil
	}
	if org == nil {
		cluster, err := s.findCluster(ctx, ref)
		if err != nil {
			return nil, err
		}
		return &cluster, nil
	}
	subs, err := s.subscriptionsByCluster(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	for _, row := range rows {
		if row.ID != ref && row.Slug != ref {
			continue
		}
		if _, ok := subs[row.ID]; !ok {
			break
		}
		cluster := row
		return &cluster, nil
	}
	return nil, notFound("region %q not found", ref)
}

// ListSandboxes returns the mirrored sandboxes that have not gone away,
// newest first, at most 500.
func (s *Service) ListSandboxes(ctx context.Context, req ListSandboxesRequest) (SandboxList, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return SandboxList{}, err
	}
	org, err := s.scopeOrg(ctx, req.Org)
	if err != nil {
		return SandboxList{}, err
	}
	orgID := ""
	if org != nil {
		orgID = org.ID
	}
	cluster, err := s.regionFilter(ctx, org, req.Region)
	if err != nil {
		return SandboxList{}, err
	}
	clusterID := ""
	if cluster != nil {
		clusterID = cluster.ID
	}
	status := strings.ToLower(strings.TrimSpace(req.Status))
	rows, err := s.q.ListSandboxes(ctx, db.ListSandboxesParams{
		OrgID:     orgID,
		ClusterID: clusterID,
		Status:    status,
	})
	if err != nil {
		return SandboxList{}, fmt.Errorf("list sandboxes: %w", err)
	}
	out := SandboxList{Data: make([]Sandbox, 0, len(rows))}
	for _, row := range rows {
		out.Data = append(out.Data, toSandbox(row.Sandbox, row.ClusterSlug, row.OrgSlug, row.OrgName, p.InstanceAdmin))
	}
	return out, nil
}

// StopSandbox stops a sandbox on its cluster and updates the mirror.
func (s *Service) StopSandbox(ctx context.Context, id string, req RegionScope) (Sandbox, error) {
	return s.sandboxAction(ctx, id, req, false)
}

// DeleteSandbox deletes a sandbox on its cluster and marks it gone.
func (s *Service) DeleteSandbox(ctx context.Context, id string, req RegionScope) (Sandbox, error) {
	return s.sandboxAction(ctx, id, req, true)
}

func (s *Service) sandboxAction(ctx context.Context, id string, req RegionScope, remove bool) (Sandbox, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return Sandbox{}, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Sandbox{}, invalid("a sandbox ID is required")
	}
	var org *db.Organization
	if strings.TrimSpace(req.Org) != "" || !p.InstanceAdmin {
		c, err := s.orgFor(ctx, req.Org, accessUse)
		if err != nil {
			return Sandbox{}, err
		}
		org = &c.org
	}
	cluster, err := s.regionFilter(ctx, org, req.Region)
	if err != nil {
		return Sandbox{}, err
	}
	rows, err := s.q.ListSandboxesByID(ctx, id)
	if err != nil {
		return Sandbox{}, fmt.Errorf("get sandbox: %w", err)
	}
	filtered := rows[:0]
	for _, row := range rows {
		if cluster != nil && row.Sandbox.ClusterID != cluster.ID {
			continue
		}
		if org != nil && (!row.Sandbox.OrgID.Valid || row.Sandbox.OrgID.String != org.ID) {
			continue
		}
		filtered = append(filtered, row)
	}
	rows = filtered
	switch len(rows) {
	case 0:
		return Sandbox{}, notFound("sandbox %q not found", id)
	case 1:
	default:
		return Sandbox{}, invalid("sandbox %q is in more than one region; choose a region", id)
	}
	row := rows[0]

	home, err := s.findCluster(ctx, row.Sandbox.ClusterID)
	if err != nil {
		return Sandbox{}, err
	}
	client, err := s.clusterClient(home)
	if err != nil {
		return Sandbox{}, err
	}
	// callCtx outlives the caller, for the call and for recording what it
	// did: a stop that finished on the cluster is recorded even if the
	// caller went away.
	callCtx, cancel := lifecycleContext(ctx)
	defer cancel()
	if remove {
		if err := client.DeleteSandbox(callCtx, id); err != nil {
			return Sandbox{}, s.regionUnavailable(ctx, home.Slug, err)
		}
		if err := s.q.MarkSandboxGone(callCtx, db.MarkSandboxGoneParams{
			GoneAt:    sql.NullTime{Time: s.now(), Valid: true},
			ClusterID: home.ID,
			ID:        id,
		}); err != nil {
			return Sandbox{}, fmt.Errorf("mark sandbox gone: %w", err)
		}
		row.Sandbox.Status = "destroyed"
	} else {
		if err := client.StopSandbox(callCtx, id); err != nil {
			return Sandbox{}, s.regionUnavailable(ctx, home.Slug, err)
		}
		if err := s.q.UpdateSandboxStatus(callCtx, db.UpdateSandboxStatusParams{
			Status:    "stopped",
			ClusterID: home.ID,
			ID:        id,
		}); err != nil {
			return Sandbox{}, fmt.Errorf("update sandbox: %w", err)
		}
		row.Sandbox.Status = "stopped"
		row.Sandbox.StatusReason = ""
	}
	return toSandbox(row.Sandbox, row.ClusterSlug, row.OrgSlug, row.OrgName, p.InstanceAdmin), nil
}

func toSandbox(row db.Sandbox, region string, orgSlug, orgName sql.NullString, admin bool) Sandbox {
	sandbox := Sandbox{
		ID:           row.ID,
		Region:       region,
		Name:         row.Name,
		Image:        row.Image,
		Status:       row.Status,
		StatusReason: row.StatusReason,
		Runtime:      row.Runtime,
		CPU:          row.Cpu,
		MemoryMB:     row.MemoryMb,
		DiskGB:       row.DiskGb,
		CreatedAt:    row.CreatedAt.UTC(),
		LastSeenAt:   row.LastSeenAt.UTC(),
	}
	if admin {
		node := row.Node
		sandbox.Node = &node
		if orgSlug.Valid {
			sandbox.Organization = &SandboxOrganization{Slug: orgSlug.String, Name: orgName.String}
		}
	}
	return sandbox
}
