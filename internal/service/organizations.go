package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
	"uuid"

	"github.com/harlew-dev/lewbox/internal/db"
)

// Organization is a business using Lewbox (MVP §2). Role is the role of the
// account the list or lookup is about (the caller, or ListOrganizations'
// User) when it is a member. Team and VcpuHours30d are only shown to instance
// admins.
type Organization struct {
	ID           string               `json:"id"`
	Slug         string               `json:"slug"`
	Name         string               `json:"name"`
	Status       string               `json:"status"` // pending | active | suspended
	StatusReason string               `json:"status_reason"`
	Team         string               `json:"team,omitempty"`
	Regions      []OrganizationRegion `json:"regions"`
	Members      int                  `json:"members"`
	Role         string               `json:"role,omitempty"`
	VcpuHours30d *float64             `json:"vcpu_hours_30d,omitempty"`
	CreatedAt    time.Time            `json:"created_at"`
}

// OrganizationRegion is one region an organization is subscribed to.
type OrganizationRegion struct {
	Slug string  `json:"slug"`
	Name string  `json:"name"`
	Plan PlanRef `json:"plan"`
}

// PlanRef names a plan.
type PlanRef struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// Limits are an organization's effective limits. A nil value is unlimited.
type Limits struct {
	Sandboxes *int64 `json:"sandboxes"`
	Vcpus     *int64 `json:"vcpus"`
	MemoryMB  *int64 `json:"memory_mb"`
}

// OrganizationList is returned by ListOrganizations. Data is never null.
type OrganizationList struct {
	Data []Organization `json:"data"`
}

// ListOrganizationsRequest filters ListOrganizations. User (an ID or email)
// lists that account's organizations instead of the caller's view. Status
// keeps only pending, active or suspended organizations.
type ListOrganizationsRequest struct {
	User   string `json:"user"`
	Status string `json:"status"`
}

// CreateOrganizationRequest is the input to CreateOrganization. Owner is an
// account's email or ID; it defaults to the caller's account.
type CreateOrganizationRequest struct {
	Slug  string `json:"slug"`
	Name  string `json:"name"`
	Owner string `json:"owner"`
}

// UpdateOrganizationRequest is the input to UpdateOrganization. Only the
// fields that are set change. Status is for instance admins.
type UpdateOrganizationRequest struct {
	Name   *string `json:"name,omitempty"`
	Status *string `json:"status,omitempty"`
}

// Member is an account's membership in an organization.
type Member struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	UserID         string    `json:"user_id"`
	Email          string    `json:"email"`
	Role           string    `json:"role"`
	CreatedAt      time.Time `json:"created_at"`
}

// MemberList is returned by ListMembers. Data is never null.
type MemberList struct {
	Data []Member `json:"data"`
}

// AddMemberRequest is the input to AddMember. Email is an existing
// account's; Role defaults to "member".
type AddMemberRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// SetMemberRoleRequest is the input to SetMemberRole.
type SetMemberRoleRequest struct {
	Role string `json:"role"`
}

const (
	roleOwner  = "owner"
	roleAdmin  = "admin"
	roleMember = "member"
	roleViewer = "viewer"

	orgPending   = "pending"
	orgActive    = "active"
	orgSuspended = "suspended"

	// statusReasonAdmin is why an instance admin suspended an organization.
	statusReasonAdmin = "admin"
)

func validRole(role string) bool {
	switch role {
	case roleOwner, roleAdmin, roleMember, roleViewer:
		return true
	}
	return false
}

// access is what a caller wants to do with an organization.
type access int

const (
	// accessRead: see the organization and its members. Any member, the
	// organization's admin keys and instance admins.
	accessRead access = iota
	// accessKeys: see the admin keys. Owners, admins, the organization's
	// admin keys and instance admins.
	accessKeys
	// accessUse: create and revoke sandbox keys. Owners, admins, members, the
	// organization's admin keys and instance admins; never a viewer.
	accessUse
	// accessManage: change the organization, its members or its keys.
	// Owners, admins and instance admins; never an admin key, and not while
	// the organization is suspended (except for instance admins).
	accessManage
)

// orgCaller is a resolved organization and who is acting on it.
type orgCaller struct {
	org  db.Organization
	p    Principal
	role string // the caller's membership role, "" if none
}

// owner reports whether the caller may hand out or take away the owner role.
func (c orgCaller) owner() bool {
	return c.p.InstanceAdmin || c.role == roleOwner
}

// orgFor resolves ref (an ID or slug; empty means the admin key's own
// organization) and checks that the caller may act on it. An organization
// the caller can't see is reported as not found, so its existence doesn't
// leak.
func (s *Service) orgFor(ctx context.Context, ref string, need access) (orgCaller, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return orgCaller{}, err
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		if p.Kind != "key" {
			return orgCaller{}, invalid("choose a workspace")
		}
		ref = p.OrgID
	}
	org, err := s.findOrganization(ctx, ref)
	if err != nil {
		return orgCaller{}, err
	}
	c := orgCaller{org: org, p: p}

	if p.Kind == "key" {
		if org.ID != p.OrgID {
			return orgCaller{}, notFound("workspace %q not found", ref)
		}
		if need == accessManage {
			return orgCaller{}, forbidden("an admin key cannot change members or keys: sign in as an owner or admin")
		}
		return c, nil
	}

	if p.Kind == "user" {
		m, err := s.q.GetMembership(ctx, db.GetMembershipParams{OrgID: org.ID, UserID: p.UserID})
		switch {
		case err == nil:
			c.role = m.Role
		case !errors.Is(err, sql.ErrNoRows):
			return orgCaller{}, fmt.Errorf("get membership: %w", err)
		}
	}
	if p.InstanceAdmin {
		return c, nil
	}
	if c.role == "" {
		return orgCaller{}, notFound("workspace %q not found", ref)
	}
	if (need == accessKeys || need == accessManage) && c.role != roleOwner && c.role != roleAdmin {
		return orgCaller{}, forbidden("only an owner or admin of %q can do this", org.Slug)
	}
	if need == accessUse && c.role != roleOwner && c.role != roleAdmin && c.role != roleMember {
		return orgCaller{}, forbidden("only an owner, admin or member of %q can do this", org.Slug)
	}
	if need == accessManage && org.Status != orgActive {
		return orgCaller{}, forbidden("workspace %q is %s", org.Slug, org.Status)
	}
	return c, nil
}

// findOrganization looks an organization up by ID or slug.
func (s *Service) findOrganization(ctx context.Context, ref string) (db.Organization, error) {
	var (
		row db.Organization
		err error
	)
	if id, perr := uuid.Parse(ref); perr == nil {
		row, err = s.q.GetOrganizationByID(ctx, id.String())
	} else {
		row, err = s.q.GetOrganizationBySlug(ctx, strings.ToLower(ref))
	}
	if errors.Is(err, sql.ErrNoRows) {
		return db.Organization{}, notFound("workspace %q not found", ref)
	}
	if err != nil {
		return db.Organization{}, fmt.Errorf("get organization: %w", err)
	}
	return row, nil
}

// memberCounts maps organization IDs to their number of members.
func (s *Service) memberCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.q.CountMembersByOrganization(ctx)
	if err != nil {
		return nil, fmt.Errorf("count members: %w", err)
	}
	counts := make(map[string]int, len(rows))
	for _, r := range rows {
		counts[r.OrgID] = int(r.Members)
	}
	return counts, nil
}

func (s *Service) toOrganization(ctx context.Context, row db.Organization, role string, admin bool) (Organization, error) {
	counts, err := s.memberCounts(ctx)
	if err != nil {
		return Organization{}, err
	}
	subs, err := s.subscriptionIndex(ctx)
	if err != nil {
		return Organization{}, err
	}
	var usage map[string]float64
	if admin {
		if usage, err = s.vcpuHoursByOrg(ctx); err != nil {
			return Organization{}, err
		}
	}
	return toOrganization(row, role, counts, subs, usage, admin), nil
}

// vcpuHoursByOrg sums each organization's metered vCPUs over the last 30 days.
func (s *Service) vcpuHoursByOrg(ctx context.Context) (map[string]float64, error) {
	rows, err := s.q.SumVcpuSecondsByOrg(ctx, s.now().Add(-30*24*time.Hour))
	if err != nil {
		return nil, fmt.Errorf("sum vcpu seconds: %w", err)
	}
	out := make(map[string]float64, len(rows))
	for _, row := range rows {
		out[row.OrgID] = row.VcpuSeconds / 3600
	}
	return out, nil
}

// subscriptionIndex is every subscription with the plans and clusters it
// names, for turning organizations and subscriptions into responses.
type subscriptionIndex struct {
	byOrg    map[string][]db.Subscription
	plans    map[string]db.Plan
	clusters map[string]db.Cluster
}

func (s *Service) subscriptionIndex(ctx context.Context) (subscriptionIndex, error) {
	subs, err := s.q.ListAllSubscriptions(ctx)
	if err != nil {
		return subscriptionIndex{}, fmt.Errorf("list subscriptions: %w", err)
	}
	plans, err := s.planRows(ctx)
	if err != nil {
		return subscriptionIndex{}, err
	}
	clusters, err := s.clustersByID(ctx)
	if err != nil {
		return subscriptionIndex{}, err
	}
	x := subscriptionIndex{
		byOrg:    make(map[string][]db.Subscription, len(subs)),
		plans:    plans,
		clusters: clusters,
	}
	for _, sub := range subs {
		x.byOrg[sub.OrgID] = append(x.byOrg[sub.OrgID], sub)
	}
	return x, nil
}

// regions lists an organization's subscribed regions, by slug.
func (x subscriptionIndex) regions(orgID string) []OrganizationRegion {
	subs := x.byOrg[orgID]
	out := make([]OrganizationRegion, 0, len(subs))
	for _, sub := range subs {
		cluster, ok := x.clusters[sub.ClusterID]
		if !ok {
			continue
		}
		plan, ok := x.plans[sub.PlanID]
		if !ok {
			continue
		}
		out = append(out, OrganizationRegion{
			Slug: cluster.Slug,
			Name: cluster.Name,
			Plan: PlanRef{Slug: plan.Slug, Name: plan.Name},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

// effectiveLimits is one subscription's limits: an override if it is set,
// otherwise the plan's value. A nil value is unlimited.
func effectiveLimits(plan db.Plan, maxSandboxes, maxVcpus, maxMemoryMB sql.NullInt64) Limits {
	limits := Limits{
		Sandboxes: nullInt(plan.MaxSandboxes),
		Vcpus:     nullInt(plan.MaxVcpus),
		MemoryMB:  nullInt(plan.MaxMemoryMb),
	}
	if maxSandboxes.Valid {
		v := maxSandboxes.Int64
		limits.Sandboxes = &v
	}
	if maxVcpus.Valid {
		v := maxVcpus.Int64
		limits.Vcpus = &v
	}
	if maxMemoryMB.Valid {
		v := maxMemoryMB.Int64
		limits.MemoryMB = &v
	}
	return limits
}

// toOrganization renders an organization. admin decides whether to include
// the fields only instance admins see: team and recent vCPU-hours.
func toOrganization(row db.Organization, role string, counts map[string]int, subs subscriptionIndex, usage map[string]float64, admin bool) Organization {
	org := Organization{
		ID:           row.ID,
		Slug:         row.Slug,
		Name:         row.Name,
		Status:       row.Status,
		StatusReason: row.StatusReason,
		Regions:      subs.regions(row.ID),
		Members:      counts[row.ID],
		Role:         role,
		CreatedAt:    row.CreatedAt.UTC(),
	}
	if row.Team.Valid {
		org.Team = row.Team.String
	}
	if admin {
		hours := usage[row.ID]
		org.VcpuHours30d = &hours
	} else {
		org.Team = ""
	}
	return org
}

// ListOrganizations returns the organizations the caller can see: all of
// them for an instance admin, the caller's own for anyone else, and its own
// for an admin key. With User set (instance admins, or yourself) it lists
// that account's organizations with its role in each.
func (s *Service) ListOrganizations(ctx context.Context, req ListOrganizationsRequest) (OrganizationList, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return OrganizationList{}, err
	}
	status := strings.ToLower(strings.TrimSpace(req.Status))
	switch status {
	case "", orgPending, orgActive, orgSuspended:
	default:
		return OrganizationList{}, invalid("status must be pending, active or suspended")
	}
	counts, err := s.memberCounts(ctx)
	if err != nil {
		return OrganizationList{}, err
	}
	subs, err := s.subscriptionIndex(ctx)
	if err != nil {
		return OrganizationList{}, err
	}
	out := OrganizationList{Data: []Organization{}}
	keep := func(org Organization) bool {
		return status == "" || org.Status == status
	}
	var usage map[string]float64
	if p.InstanceAdmin {
		if usage, err = s.vcpuHoursByOrg(ctx); err != nil {
			return OrganizationList{}, err
		}
	}

	if p.Kind == "key" {
		if strings.TrimSpace(req.User) != "" {
			return OrganizationList{}, forbidden("an admin key cannot list another account's workspaces")
		}
		row, err := s.q.GetOrganizationByID(ctx, p.OrgID)
		if err != nil {
			return OrganizationList{}, fmt.Errorf("get organization: %w", err)
		}
		if org := toOrganization(row, "", counts, subs, nil, false); keep(org) {
			out.Data = append(out.Data, org)
		}
		return out, nil
	}

	if ref := strings.TrimSpace(req.User); ref != "" {
		if !p.InstanceAdmin && !p.selfRef(ref) {
			return OrganizationList{}, forbidden("only an instance admin can list another account's workspaces")
		}
		user, err := s.findUser(ctx, ref)
		if err != nil {
			return OrganizationList{}, err
		}
		return s.organizationsOf(ctx, user.ID, counts, subs, usage, p.InstanceAdmin, keep)
	}

	if !p.InstanceAdmin {
		return s.organizationsOf(ctx, p.UserID, counts, subs, nil, false, keep)
	}
	rows, err := s.q.ListOrganizations(ctx)
	if err != nil {
		return OrganizationList{}, fmt.Errorf("list organizations: %w", err)
	}
	roles := map[string]string{}
	if p.Kind == "user" {
		mine, err := s.q.ListOrganizationsForUser(ctx, p.UserID)
		if err != nil {
			return OrganizationList{}, fmt.Errorf("list organizations: %w", err)
		}
		for _, m := range mine {
			roles[m.Organization.ID] = m.Role
		}
	}
	for _, r := range rows {
		if org := toOrganization(r, roles[r.ID], counts, subs, usage, true); keep(org) {
			out.Data = append(out.Data, org)
		}
	}
	return out, nil
}

func (s *Service) organizationsOf(ctx context.Context, userID string, counts map[string]int, subs subscriptionIndex, usage map[string]float64, admin bool, keep func(Organization) bool) (OrganizationList, error) {
	rows, err := s.q.ListOrganizationsForUser(ctx, userID)
	if err != nil {
		return OrganizationList{}, fmt.Errorf("list organizations: %w", err)
	}
	out := OrganizationList{Data: make([]Organization, 0, len(rows))}
	for _, r := range rows {
		if org := toOrganization(r.Organization, r.Role, counts, subs, usage, admin); keep(org) {
			out.Data = append(out.Data, org)
		}
	}
	return out, nil
}

// CreateOrganization creates an organization with its first owner. Only
// instance admins can do this.
func (s *Service) CreateOrganization(ctx context.Context, req CreateOrganizationRequest) (Organization, error) {
	p, err := requireAdmin(ctx)
	if err != nil {
		return Organization{}, err
	}
	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	if !nodeName.MatchString(slug) {
		return Organization{}, invalid("slug must be 1-63 lowercase letters, digits or hyphens, and start and end with a letter or digit")
	}
	if _, err := uuid.Parse(slug); err == nil {
		return Organization{}, invalid("slug must not be a UUID")
	}
	name, err := orgName(req.Name, slug)
	if err != nil {
		return Organization{}, err
	}

	// The owner is checked before anything is written.
	ownerID := ""
	if ref := strings.TrimSpace(req.Owner); ref != "" {
		owner, err := s.findUser(ctx, ref)
		if err != nil {
			return Organization{}, err
		}
		ownerID = owner.ID
	} else if p.Kind == "user" {
		ownerID = p.UserID
	}

	id := uuid.NewV7().String()
	row, err := s.q.CreateOrganization(ctx, db.CreateOrganizationParams{
		ID:        id,
		Slug:      slug,
		Name:      name,
		Status:    orgActive,
		CreatedAt: s.now(),
		Team:      sql.NullString{String: teamForID(id), Valid: true},
	})
	if db.IsUniqueViolation(err) {
		return Organization{}, conflict("an organization with slug %q already exists", slug)
	}
	if err != nil {
		return Organization{}, fmt.Errorf("create organization: %w", err)
	}
	role := ""
	if ownerID != "" {
		if _, err := s.q.CreateMembership(ctx, db.CreateMembershipParams{
			ID:        uuid.NewV7().String(),
			OrgID:     row.ID,
			UserID:    ownerID,
			Role:      roleOwner,
			CreatedAt: s.now(),
		}); err != nil {
			// Don't leave an organization nobody owns because of a race
			// with the owner's removal.
			_ = s.q.DeleteOrganization(ctx, row.ID)
			return Organization{}, fmt.Errorf("add owner: %w", err)
		}
		if p.Kind == "user" && ownerID == p.UserID {
			role = roleOwner
		}
	}
	return s.toOrganization(ctx, row, role, p.InstanceAdmin)
}

// teamForID is an organization's AerolVM team: unique, valid, and never
// reused because IDs never are.
func teamForID(id string) string {
	return "lbx-" + strings.ReplaceAll(id, "-", "")
}

// orgName trims and checks a display name; empty means the slug.
func orgName(raw, slug string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return slug, nil
	}
	if err := displayName(name, "name", 100); err != nil {
		return "", err
	}
	return name, nil
}

// displayName rejects characters that would corrupt a terminal line: control
// characters and the bidi overrides and isolates.
func displayName(name, field string, max int) error {
	if utf8.RuneCountInString(name) > max {
		return invalid("%s must be at most %d characters", field, max)
	}
	for _, r := range name {
		if unicode.IsControl(r) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			return invalid("%s must not contain control characters", field)
		}
	}
	return nil
}

// GetOrganization returns an organization by ID or slug, or the admin key's
// own organization when ref is empty.
func (s *Service) GetOrganization(ctx context.Context, ref string) (Organization, error) {
	c, err := s.orgFor(ctx, ref, accessRead)
	if err != nil {
		return Organization{}, err
	}
	return s.toOrganization(ctx, c.org, c.role, c.p.InstanceAdmin)
}

// UpdateOrganization renames an organization (owners, admins, instance
// admins), or changes its status (instance admins only). A suspended or
// pending organization's admin keys stop working, and its members can only
// read it.
func (s *Service) UpdateOrganization(ctx context.Context, ref string, req UpdateOrganizationRequest) (Organization, error) {
	if req.Name == nil && req.Status == nil {
		return Organization{}, invalid("nothing to change: set name or status")
	}
	// orgFor lets instance admins act on a suspended organization, so they
	// can restore it.
	c, err := s.orgFor(ctx, ref, accessManage)
	if err != nil {
		return Organization{}, err
	}
	adminOnly := req.Status != nil
	if adminOnly && !c.p.InstanceAdmin {
		return Organization{}, forbidden("only an instance admin can change an organization's status")
	}
	// Validate every field before the first write, so a failed request never
	// half-applies.
	row := c.org
	name := row.Name
	if req.Name != nil {
		if name, err = orgName(*req.Name, row.Slug); err != nil {
			return Organization{}, err
		}
	}
	status := ""
	if req.Status != nil {
		status = strings.ToLower(strings.TrimSpace(*req.Status))
		switch status {
		case orgActive, orgSuspended:
		default:
			return Organization{}, invalid("status must be active or suspended")
		}
	}

	if req.Name != nil {
		row, err = s.q.SetOrganizationName(ctx, db.SetOrganizationNameParams{Name: name, ID: row.ID})
		if err != nil {
			return Organization{}, fmt.Errorf("rename organization: %w", err)
		}
	}
	if req.Status != nil {
		reason := ""
		if status == orgSuspended {
			reason = statusReasonAdmin
		}
		row, err = s.q.SetOrganizationStatus(ctx, db.SetOrganizationStatusParams{
			Status:       status,
			StatusReason: reason,
			ID:           row.ID,
		})
		if err != nil {
			return Organization{}, fmt.Errorf("set organization status: %w", err)
		}
	}
	if adminOnly {
		if err := s.syncOrganizationQuotas(ctx, row, nil); err != nil {
			s.log.Warn("quota sync failed", "organization", row.Slug, "err", err)
		}
	}
	return s.toOrganization(ctx, row, c.role, c.p.InstanceAdmin)
}

// ListMembers returns an organization's members, ordered by email.
func (s *Service) ListMembers(ctx context.Context, org string) (MemberList, error) {
	c, err := s.orgFor(ctx, org, accessRead)
	if err != nil {
		return MemberList{}, err
	}
	rows, err := s.q.ListMembers(ctx, c.org.ID)
	if err != nil {
		return MemberList{}, fmt.Errorf("list members: %w", err)
	}
	out := MemberList{Data: make([]Member, 0, len(rows))}
	for _, r := range rows {
		email := r.Email
		if !c.p.InstanceAdmin && r.InstanceAdmin != 0 {
			email = "operator"
		}
		out.Data = append(out.Data, toMember(r.Membership, email))
	}
	return out, nil
}

// AddMember gives an existing account a role in the organization. Only an
// owner (or an instance admin) can add another owner.
func (s *Service) AddMember(ctx context.Context, org string, req AddMemberRequest) (Member, error) {
	c, err := s.orgFor(ctx, org, accessManage)
	if err != nil {
		return Member{}, err
	}
	role := strings.ToLower(strings.TrimSpace(req.Role))
	if role == "" {
		role = roleMember
	}
	if !validRole(role) {
		return Member{}, invalid("role must be owner, admin, member or viewer")
	}
	if role == roleOwner && !c.owner() {
		return Member{}, forbidden("only an owner can add another owner")
	}
	if strings.TrimSpace(req.Email) == "" {
		return Member{}, invalid("email is required")
	}
	user, err := s.findUser(ctx, req.Email)
	if err != nil {
		return Member{}, err
	}
	// A customer cannot learn that an account is an operator's.
	if !c.p.InstanceAdmin && user.InstanceAdmin != 0 {
		return Member{}, notFound("no account with that email")
	}
	row, err := s.q.CreateMembership(ctx, db.CreateMembershipParams{
		ID:        uuid.NewV7().String(),
		OrgID:     c.org.ID,
		UserID:    user.ID,
		Role:      role,
		CreatedAt: s.now(),
	})
	if db.IsUniqueViolation(err) {
		return Member{}, conflict("%s is already a member of %q", user.Email, c.org.Slug)
	}
	if err != nil {
		return Member{}, fmt.Errorf("add member: %w", err)
	}
	return toMember(row, user.Email), nil
}

// SetMemberRole changes a member's role. Only an owner (or an instance
// admin) can make or unmake an owner, and the last owner always stays.
func (s *Service) SetMemberRole(ctx context.Context, org, ref string, req SetMemberRoleRequest) (Member, error) {
	c, err := s.orgFor(ctx, org, accessManage)
	if err != nil {
		return Member{}, err
	}
	role := strings.ToLower(strings.TrimSpace(req.Role))
	if !validRole(role) {
		return Member{}, invalid("role must be owner, admin, member or viewer")
	}
	m, email, err := s.findMember(ctx, c.org, ref)
	if err != nil {
		return Member{}, err
	}
	if email, err = s.memberEmail(ctx, m.UserID, email, c.p.InstanceAdmin); err != nil {
		return Member{}, err
	}
	if (role == roleOwner || m.Role == roleOwner) && !c.owner() {
		return Member{}, forbidden("only an owner can make or change an owner")
	}
	if m.Role == role {
		return toMember(m, email), nil
	}
	if role == roleViewer {
		if err := s.revokeCreatedSandboxKeys(ctx, c.org.ID, m.UserID); err != nil {
			return Member{}, err
		}
	}
	updated, err := s.q.SetMembershipRole(ctx, db.SetMembershipRoleParams{Role: role, ID: m.ID})
	if errors.Is(err, sql.ErrNoRows) {
		return Member{}, conflict("%q needs at least one owner", c.org.Slug)
	}
	if err != nil {
		return Member{}, fmt.Errorf("set role: %w", err)
	}
	return toMember(updated, email), nil
}

// RemoveMember takes an account out of the organization. Owners and admins
// can remove members (only an owner can remove an owner), anyone can leave,
// and the last owner always stays.
func (s *Service) RemoveMember(ctx context.Context, org, ref string) (Member, error) {
	c, err := s.orgFor(ctx, org, accessRead)
	if err != nil {
		return Member{}, err
	}
	m, email, err := s.findMember(ctx, c.org, ref)
	if err != nil {
		return Member{}, err
	}
	if email, err = s.memberEmail(ctx, m.UserID, email, c.p.InstanceAdmin); err != nil {
		return Member{}, err
	}
	self := c.p.Kind == "user" && m.UserID == c.p.UserID
	if !self {
		if _, err := s.orgFor(ctx, c.org.ID, accessManage); err != nil {
			return Member{}, err
		}
		if m.Role == roleOwner && !c.owner() {
			return Member{}, forbidden("only an owner can remove an owner")
		}
	}
	if err := s.revokeCreatedSandboxKeys(ctx, c.org.ID, m.UserID); err != nil {
		return Member{}, err
	}
	n, err := s.q.DeleteMembership(ctx, m.ID)
	if err != nil {
		return Member{}, fmt.Errorf("remove member: %w", err)
	}
	if n == 0 {
		if _, gerr := s.q.GetMembershipByID(ctx, db.GetMembershipByIDParams{OrgID: c.org.ID, ID: m.ID}); gerr == nil {
			return Member{}, conflict("%q needs at least one owner", c.org.Slug)
		}
		return Member{}, notFound("%q is not a member of %q", ref, c.org.Slug)
	}
	return toMember(m, email), nil
}

// findMember looks a member up by membership ID, account ID or email.
func (s *Service) findMember(ctx context.Context, org db.Organization, ref string) (db.Membership, string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return db.Membership{}, "", invalid("a member's email or ID is required")
	}
	if id, err := uuid.Parse(ref); err == nil {
		m, err := s.q.GetMembershipByID(ctx, db.GetMembershipByIDParams{OrgID: org.ID, ID: id.String()})
		if err == nil {
			user, err := s.q.GetUserByID(ctx, m.UserID)
			if err != nil {
				return db.Membership{}, "", fmt.Errorf("get user: %w", err)
			}
			return m, user.Email, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return db.Membership{}, "", fmt.Errorf("get membership: %w", err)
		}
	}
	user, err := s.findUser(ctx, ref)
	if err != nil {
		if se, ok := errors.AsType[*Error](err); ok && se.Code == CodeNotFound {
			return db.Membership{}, "", notFound("%q is not a member of %q", ref, org.Slug)
		}
		return db.Membership{}, "", err
	}
	m, err := s.q.GetMembership(ctx, db.GetMembershipParams{OrgID: org.ID, UserID: user.ID})
	if errors.Is(err, sql.ErrNoRows) {
		return db.Membership{}, "", notFound("%q is not a member of %q", ref, org.Slug)
	}
	if err != nil {
		return db.Membership{}, "", fmt.Errorf("get membership: %w", err)
	}
	return m, user.Email, nil
}

func toMember(m db.Membership, email string) Member {
	return Member{
		ID:             m.ID,
		OrganizationID: m.OrgID,
		UserID:         m.UserID,
		Email:          email,
		Role:           m.Role,
		CreatedAt:      m.CreatedAt.UTC(),
	}
}

// memberEmail hides an instance admin's address from customers.
func (s *Service) memberEmail(ctx context.Context, userID, email string, admin bool) (string, error) {
	if admin {
		return email, nil
	}
	user, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("get user: %w", err)
	}
	if user.InstanceAdmin != 0 {
		return "operator", nil
	}
	return email, nil
}
