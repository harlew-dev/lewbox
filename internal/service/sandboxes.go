package service

import (
	"context"
	"database/sql"
	"errors"
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
	Member       *MemberRef           `json:"member,omitempty"`
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
// of an instance admin. Member is an email or membership ID.
type ListSandboxesRequest struct {
	Region string `json:"region"`
	Status string `json:"status"`
	Org    string `json:"org"`
	Member string `json:"member"`
}

// SandboxActionRequest picks the cluster for a stop or delete when the
// sandbox ID is not unique, and the organization the caller acts for.
type SandboxActionRequest struct {
	Region string `json:"region"`
	Org    string `json:"org"`
}

// orgScope is the organization a fleet-capable call is about, and the
// caller's place in it.
type orgScope struct {
	p          Principal
	org        *db.Organization
	fleet      bool
	role       string
	membership *db.Membership
}

// scopeOrg resolves the organization a list or usage call is about. An
// instance admin with no organization sees the whole fleet.
func (s *Service) scopeOrg(ctx context.Context, ref string) (orgScope, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return orgScope{}, err
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		if p.InstanceAdmin {
			return orgScope{p: p, fleet: true}, nil
		}
		c, err := s.orgFor(ctx, "", accessRead)
		if err != nil {
			return orgScope{}, err
		}
		return s.scopeFor(ctx, c)
	}
	c, err := s.orgFor(ctx, ref, accessRead)
	if err != nil {
		return orgScope{}, err
	}
	return s.scopeFor(ctx, c)
}

func (s *Service) scopeFor(ctx context.Context, c orgCaller) (orgScope, error) {
	scope := orgScope{p: c.p, org: &c.org, role: c.role}
	if c.p.Kind != "user" {
		return scope, nil
	}
	m, err := s.q.GetMembership(ctx, db.GetMembershipParams{OrgID: c.org.ID, UserID: c.p.UserID})
	switch {
	case err == nil:
		scope.membership = &m
	case !errors.Is(err, sql.ErrNoRows):
		return orgScope{}, fmt.Errorf("get membership: %w", err)
	}
	return scope, nil
}

// limited reports whether the caller sees only their own member's resources.
func (s orgScope) limited() bool {
	return !s.p.InstanceAdmin && s.p.Kind == "user" && s.role != roleOwner && s.role != roleAdmin
}

// ListSandboxes returns the mirrored sandboxes that have not gone away,
// newest first, at most 500.
func (s *Service) ListSandboxes(ctx context.Context, req ListSandboxesRequest) (SandboxList, error) {
	scope, err := s.scopeOrg(ctx, req.Org)
	if err != nil {
		return SandboxList{}, err
	}
	orgID := ""
	if scope.org != nil {
		orgID = scope.org.ID
	}
	clusterID := ""
	if region := strings.TrimSpace(req.Region); region != "" {
		cluster, err := s.findCluster(ctx, region)
		if err != nil {
			return SandboxList{}, err
		}
		clusterID = cluster.ID
	}
	membershipID := ""
	switch {
	case scope.limited():
		if scope.membership != nil {
			membershipID = scope.membership.ID
		} else {
			membershipID = "none"
		}
	case strings.TrimSpace(req.Member) != "":
		if scope.org == nil {
			return SandboxList{}, invalid("choose a workspace to filter by member")
		}
		m, _, err := s.findMember(ctx, *scope.org, req.Member)
		if err != nil {
			return SandboxList{}, err
		}
		membershipID = m.ID
	}
	status := strings.ToLower(strings.TrimSpace(req.Status))
	rows, err := s.q.ListSandboxes(ctx, db.ListSandboxesParams{
		OrgID:        orgID,
		MembershipID: membershipID,
		ClusterID:    clusterID,
		Status:       status,
	})
	if err != nil {
		return SandboxList{}, fmt.Errorf("list sandboxes: %w", err)
	}
	out := SandboxList{Data: make([]Sandbox, 0, len(rows))}
	for _, row := range rows {
		out.Data = append(out.Data, toSandbox(row.Sandbox, row.ClusterSlug, row.OrgSlug, row.OrgName, row.MemberEmail, row.MemberAdmin, scope.p.InstanceAdmin))
	}
	return out, nil
}

// StopSandbox stops a sandbox on its cluster and updates the mirror.
func (s *Service) StopSandbox(ctx context.Context, id string, req SandboxActionRequest) (Sandbox, error) {
	return s.sandboxAction(ctx, id, req, false)
}

// DeleteSandbox deletes a sandbox on its cluster and marks it gone.
func (s *Service) DeleteSandbox(ctx context.Context, id string, req SandboxActionRequest) (Sandbox, error) {
	return s.sandboxAction(ctx, id, req, true)
}

func (s *Service) sandboxAction(ctx context.Context, id string, req SandboxActionRequest, remove bool) (Sandbox, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return Sandbox{}, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Sandbox{}, invalid("a sandbox ID is required")
	}
	rows, err := s.q.ListSandboxesByID(ctx, id)
	if err != nil {
		return Sandbox{}, fmt.Errorf("get sandbox: %w", err)
	}
	if region := strings.TrimSpace(req.Region); region != "" {
		cluster, err := s.findCluster(ctx, region)
		if err != nil {
			return Sandbox{}, err
		}
		filtered := rows[:0]
		for _, row := range rows {
			if row.Sandbox.ClusterID == cluster.ID {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	switch len(rows) {
	case 0:
		return Sandbox{}, notFound("sandbox %q not found", id)
	case 1:
	default:
		return Sandbox{}, invalid("sandbox %q is in more than one region: pass region", id)
	}
	row := rows[0]

	if req.Org != "" {
		c, err := s.orgFor(ctx, req.Org, accessUse)
		if err != nil {
			return Sandbox{}, err
		}
		if !row.Sandbox.OrgID.Valid || row.Sandbox.OrgID.String != c.org.ID {
			return Sandbox{}, notFound("sandbox %q not found", id)
		}
		if err := s.checkSandboxOwnership(ctx, row.Sandbox, c, p); err != nil {
			return Sandbox{}, err
		}
	} else if !p.InstanceAdmin {
		c, err := s.orgFor(ctx, "", accessUse)
		if err != nil {
			return Sandbox{}, err
		}
		if !row.Sandbox.OrgID.Valid || row.Sandbox.OrgID.String != c.org.ID {
			return Sandbox{}, notFound("sandbox %q not found", id)
		}
		if err := s.checkSandboxOwnership(ctx, row.Sandbox, c, p); err != nil {
			return Sandbox{}, err
		}
	}

	cluster, err := s.findCluster(ctx, row.Sandbox.ClusterID)
	if err != nil {
		return Sandbox{}, err
	}
	client, err := s.clusterClient(cluster)
	if err != nil {
		return Sandbox{}, err
	}
	if remove {
		if err := client.DeleteSandbox(ctx, id); err != nil {
			return Sandbox{}, s.regionUnavailable(ctx, cluster.Slug, err)
		}
		if err := s.q.MarkSandboxGone(ctx, db.MarkSandboxGoneParams{
			GoneAt:    sql.NullTime{Time: s.now(), Valid: true},
			ClusterID: cluster.ID,
			ID:        id,
		}); err != nil {
			return Sandbox{}, fmt.Errorf("mark sandbox gone: %w", err)
		}
		row.Sandbox.Status = "destroyed"
	} else {
		if err := client.StopSandbox(ctx, id); err != nil {
			return Sandbox{}, s.regionUnavailable(ctx, cluster.Slug, err)
		}
		if err := s.q.UpdateSandboxStatus(ctx, db.UpdateSandboxStatusParams{
			Status:    "stopped",
			ClusterID: cluster.ID,
			ID:        id,
		}); err != nil {
			return Sandbox{}, fmt.Errorf("update sandbox: %w", err)
		}
		row.Sandbox.Status = "stopped"
		row.Sandbox.StatusReason = ""
	}
	return toSandbox(row.Sandbox, row.ClusterSlug, row.OrgSlug, row.OrgName, row.MemberEmail, row.MemberAdmin, p.InstanceAdmin), nil
}

// checkSandboxOwnership refuses a plain member touch on another member's
// sandbox. Owners, admins, API keys and instance admins may touch any sandbox
// in the workspace.
func (s *Service) checkSandboxOwnership(ctx context.Context, sandbox db.Sandbox, c orgCaller, p Principal) error {
	if p.Kind != "user" || c.role != roleMember {
		return nil
	}
	m, err := s.q.GetMembership(ctx, db.GetMembershipParams{OrgID: c.org.ID, UserID: p.UserID})
	if err != nil || !sandbox.MembershipID.Valid || sandbox.MembershipID.String != m.ID {
		return notFound("sandbox %q not found", sandbox.ID)
	}
	return nil
}

func toSandbox(row db.Sandbox, region string, orgSlug, orgName sql.NullString, memberEmail sql.NullString, memberAdmin int64, admin bool) Sandbox {
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
	if memberEmail.Valid {
		email := memberEmail.String
		if memberAdmin != 0 && !admin {
			email = "operator"
		}
		sandbox.Member = &MemberRef{Email: email}
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
