package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
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
	ID           string              `json:"id"`
	Slug         string              `json:"slug"`
	Name         string              `json:"name"`
	Status       string              `json:"status"` // pending | active | suspended
	StatusReason string              `json:"status_reason"`
	Team         string              `json:"team,omitempty"`
	Region       *OrganizationRegion `json:"region"`
	Members      int                 `json:"members"`
	Role         string              `json:"role,omitempty"`
	VcpuHours30d *float64            `json:"vcpu_hours_30d,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
}

// OrganizationRegion is the region an organization is subscribed to.
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

// CreateOrganizationRequest is the input to CreateOrganization. Slug is
// generated from the name when it is empty. Owner is an account's email or ID;
// it defaults to the caller's account and only instance admins may set it.
// SetupToken consumes a setup link, which gives the workspace that link's plan
// in its region and is required when sign-up is closed.
type CreateOrganizationRequest struct {
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Owner      string `json:"owner"`
	SetupToken string `json:"setup_token"`
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

	// maxOwnedWorkspaces caps the workspaces one account may own.
	maxOwnedWorkspaces = 10
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

// region is an organization's subscribed region, or nil when it has none.
func (x subscriptionIndex) region(orgID string) *OrganizationRegion {
	subs := x.byOrg[orgID]
	if len(subs) == 0 {
		return nil
	}
	sub := subs[0]
	cluster, ok := x.clusters[sub.ClusterID]
	if !ok {
		return nil
	}
	plan, ok := x.plans[sub.PlanID]
	if !ok {
		return nil
	}
	return &OrganizationRegion{
		Slug: cluster.Slug,
		Name: cluster.Name,
		Plan: PlanRef{Slug: plan.Slug, Name: plan.Name},
	}
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
		Region:       subs.region(row.ID),
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

// CreateOrganization creates an organization with its first owner. Instance
// admins may choose the owner and get no default subscription. Anyone else
// becomes the owner, subject to the instance's sign-up mode.
func (s *Service) CreateOrganization(ctx context.Context, req CreateOrganizationRequest) (Organization, error) {
	p, err := requireUser(ctx)
	if err != nil {
		return Organization{}, err
	}
	settings, err := s.getSettings(ctx)
	if err != nil {
		return Organization{}, err
	}

	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	name := strings.TrimSpace(req.Name)
	if slug != "" {
		if !nodeName.MatchString(slug) {
			return Organization{}, invalid("slug must be 1-63 lowercase letters, digits or hyphens, and start and end with a letter or digit")
		}
		if _, err := uuid.Parse(slug); err == nil {
			return Organization{}, invalid("slug must not be a UUID")
		}
		if name, err = orgName(req.Name, slug); err != nil {
			return Organization{}, err
		}
	} else if name == "" {
		return Organization{}, invalid("name is required when slug is empty")
	} else if _, err := orgName(name, ""); err != nil {
		return Organization{}, err
	}

	// The owner is checked before anything is written.
	setupToken := strings.TrimSpace(req.SetupToken)
	status := orgActive
	ownerID := ""
	if p.InstanceAdmin {
		if ref := strings.TrimSpace(req.Owner); ref != "" {
			owner, err := s.findUser(ctx, ref)
			if err != nil {
				return Organization{}, err
			}
			ownerID = owner.ID
		} else if p.Kind == "user" {
			ownerID = p.UserID
		}
	} else {
		if strings.TrimSpace(req.Owner) != "" {
			return Organization{}, forbidden("only an instance admin can choose a workspace's owner")
		}
		// A valid setup token will allow this in any mode and force an
		// active workspace.
		if setupToken == "" {
			if status, err = organizationStatus(settings.Signup); err != nil {
				return Organization{}, err
			}
		}
		ownerID = p.UserID
		owned, err := s.q.CountOwnedOrganizations(ctx, ownerID)
		if err != nil {
			return Organization{}, fmt.Errorf("count owned organizations: %w", err)
		}
		if owned >= maxOwnedWorkspaces {
			return Organization{}, precondition(nil, "you can own at most %d workspaces", maxOwnedWorkspaces)
		}
	}

	// A setup link allows a workspace in any mode and forces it active.
	var (
		link        db.SetupLink
		linkClaimed bool
	)
	if setupToken != "" {
		existing, err := s.findSetupLink(ctx, setupToken)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return Organization{}, setupLinkInvalid()
		case err != nil:
			return Organization{}, err
		case existing.UsedAt.Valid, !existing.ExpiresAt.After(s.now()):
			return Organization{}, setupLinkInvalid()
		case existing.ClaimedBy.Valid && ownerID != "" && existing.ClaimedBy.String != ownerID:
			return Organization{}, setupLinkInvalid()
		}
		// Consume before writing anything: the statement lets only one
		// caller win.
		now := s.now()
		link, err = s.q.ConsumeSetupLink(ctx, db.ConsumeSetupLinkParams{
			UsedAt:    sql.NullTime{Time: now, Valid: true},
			UserID:    sql.NullString{String: ownerID, Valid: ownerID != ""},
			ClaimedAt: sql.NullTime{Time: now, Valid: true},
			TokenHash: hashSetupToken(setupToken),
			Now:       now,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return Organization{}, setupLinkInvalid()
		}
		if err != nil {
			return Organization{}, fmt.Errorf("consume setup link: %w", err)
		}
		linkClaimed = !existing.ClaimedBy.Valid
		status = orgActive
	}

	var row db.Organization
	// Undo every write if anything after the link or organization row fails.
	committed := false
	defer func() {
		if committed {
			return
		}
		cleanup := context.WithoutCancel(ctx)
		if row.ID != "" {
			_ = s.q.DeleteOrganization(cleanup, row.ID)
		}
		if link.ID != "" {
			if linkClaimed {
				_ = s.q.UnclaimSetupLink(cleanup, link.ID)
			} else {
				_ = s.q.ReleaseSetupLink(cleanup, link.ID)
			}
		}
	}()

	if slug == "" {
		row, err = s.createGeneratedOrganization(ctx, name, name, status)
		if err != nil {
			return Organization{}, err
		}
	} else {
		id := uuid.NewV7().String()
		row, err = s.q.CreateOrganization(ctx, db.CreateOrganizationParams{
			ID:        id,
			Slug:      slug,
			Name:      name,
			Status:    status,
			CreatedAt: s.now(),
			Team:      sql.NullString{String: teamForID(id), Valid: true},
		})
		if db.IsUniqueViolation(err) {
			return Organization{}, conflict("an organization with slug %q already exists", slug)
		}
		if err != nil {
			return Organization{}, fmt.Errorf("create organization: %w", err)
		}
	}
	role := ""
	if ownerID != "" {
		membershipID := uuid.NewV7().String()
		if p.InstanceAdmin {
			_, err = s.q.CreateMembership(ctx, db.CreateMembershipParams{
				ID:        membershipID,
				OrgID:     row.ID,
				UserID:    ownerID,
				Role:      roleOwner,
				CreatedAt: s.now(),
			})
		} else {
			// The cap is in the statement, so two concurrent creates cannot
			// both pass it.
			_, err = s.q.CreateOwnerMembership(ctx, db.CreateOwnerMembershipParams{
				ID:        membershipID,
				OrgID:     row.ID,
				UserID:    ownerID,
				CreatedAt: s.now(),
				MaxOwned:  maxOwnedWorkspaces,
			})
			if errors.Is(err, sql.ErrNoRows) {
				return Organization{}, precondition(nil, "you can own at most %d workspaces", maxOwnedWorkspaces)
			}
		}
		if err != nil {
			return Organization{}, fmt.Errorf("add owner: %w", err)
		}
		if p.Kind == "user" && ownerID == p.UserID {
			role = roleOwner
		}
	}
	switch {
	case link.ID != "":
		if err := s.subscribeSetupLink(ctx, row, link); err != nil {
			return Organization{}, err
		}
	case !p.InstanceAdmin:
		if err := s.subscribeDefaults(ctx, row, settings); err != nil {
			return Organization{}, err
		}
	}
	if link.ID != "" {
		if err := s.q.SetSetupLinkOrg(ctx, db.SetSetupLinkOrgParams{
			OrgID: sql.NullString{String: row.ID, Valid: true},
			ID:    link.ID,
		}); err != nil {
			return Organization{}, fmt.Errorf("set setup link workspace: %w", err)
		}
	}
	out, err := s.toOrganization(ctx, row, role, p.InstanceAdmin)
	if err != nil {
		return Organization{}, err
	}
	committed = true
	return out, nil
}

// organizationStatus is the status a workspace created by its owner gets. A
// valid setup token will allow it in any mode.
func organizationStatus(mode string) (string, error) {
	switch mode {
	case signupOpen:
		return orgActive, nil
	case signupApproval:
		return orgPending, nil
	default:
		return "", forbidden("creating workspaces is closed on this instance; ask your provider for a setup link")
	}
}

// teamForID is an organization's AerolVM team: unique, valid, and never
// reused because IDs never are.
func teamForID(id string) string {
	return "lbx-" + strings.ReplaceAll(id, "-", "")
}

const (
	orgSlugSuffixLength = 6
	orgSlugBaseMax      = 40
	orgSlugAttempts     = 5
	orgSlugBase32       = "abcdefghijklmnopqrstuvwxyz234567"
)

// orgSlugSuffix returns the random tail of a generated slug. It is a variable
// so tests can force a collision.
var orgSlugSuffix = func() (string, error) {
	b := make([]byte, orgSlugSuffixLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read slug suffix: %w", err)
	}
	for i := range b {
		b[i] = orgSlugBase32[int(b[i])%len(orgSlugBase32)]
	}
	return string(b), nil
}

// createGeneratedOrganization picks an unused slug from base and creates the
// organization.
func (s *Service) createGeneratedOrganization(ctx context.Context, base, name, status string) (db.Organization, error) {
	var err error
	for range orgSlugAttempts {
		suffix, serr := orgSlugSuffix()
		if serr != nil {
			return db.Organization{}, serr
		}
		slug := generatedOrgSlug(base, suffix)
		if !nodeName.MatchString(slug) {
			return db.Organization{}, fmt.Errorf("generated slug %q is not valid", slug)
		}
		id := uuid.NewV7().String()
		row, cerr := s.q.CreateOrganization(ctx, db.CreateOrganizationParams{
			ID:        id,
			Slug:      slug,
			Name:      name,
			Status:    status,
			CreatedAt: s.now(),
			Team:      sql.NullString{String: teamForID(id), Valid: true},
		})
		if db.IsUniqueViolation(cerr) {
			err = cerr
			continue
		}
		if cerr != nil {
			return db.Organization{}, fmt.Errorf("create organization: %w", cerr)
		}
		return row, nil
	}
	return db.Organization{}, fmt.Errorf("no unique organization slug after %d attempts: %w", orgSlugAttempts, err)
}

// generatedOrgSlug is base reduced to a DNS label, trimmed to 40 characters,
// plus "-" and the random suffix.
func generatedOrgSlug(base, suffix string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	label := strings.Trim(b.String(), "-")
	if len(label) > orgSlugBaseMax {
		label = strings.Trim(label[:orgSlugBaseMax], "-")
	}
	if label == "" {
		label = "org"
	}
	return label + "-" + suffix
}

// subscribeSetupLink gives a workspace the plan and region its setup link
// names, pushing the quota before it returns.
func (s *Service) subscribeSetupLink(ctx context.Context, org db.Organization, link db.SetupLink) error {
	plan, err := s.q.GetPlanByID(ctx, link.PlanID)
	if err != nil {
		return fmt.Errorf("get setup link plan: %w", err)
	}
	cluster, err := s.q.GetClusterByID(ctx, link.ClusterID)
	if err != nil {
		return fmt.Errorf("get setup link region: %w", err)
	}
	sub, err := s.q.CreateSubscription(ctx, db.CreateSubscriptionParams{
		OrgID:     org.ID,
		ClusterID: cluster.ID,
		PlanID:    plan.ID,
		CreatedAt: s.now(),
	})
	if err != nil {
		return fmt.Errorf("subscribe %q: %w", cluster.Slug, err)
	}
	s.pushSubscriptionQuota(ctx, org, cluster, plan, sub)
	return nil
}

// subscribeDefaults gives a new workspace the default plan in the default
// region when both still exist, pushing the quota before it returns. With no
// default plan or region, it gets none.
func (s *Service) subscribeDefaults(ctx context.Context, org db.Organization, settings Settings) error {
	if settings.DefaultPlan == nil || settings.DefaultRegion == nil {
		return nil
	}
	plan, err := s.findPlan(ctx, *settings.DefaultPlan)
	if err != nil {
		if se, ok := errors.AsType[*Error](err); ok && se.Code == CodeNotFound {
			return nil
		}
		return err
	}
	cluster, err := s.q.GetClusterBySlug(ctx, *settings.DefaultRegion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get cluster %q: %w", *settings.DefaultRegion, err)
	}
	sub, err := s.q.CreateSubscription(ctx, db.CreateSubscriptionParams{
		OrgID:     org.ID,
		ClusterID: cluster.ID,
		PlanID:    plan.ID,
		CreatedAt: s.now(),
	})
	if err != nil {
		return fmt.Errorf("subscribe %q: %w", cluster.Slug, err)
	}
	s.pushSubscriptionQuota(ctx, org, cluster, plan, sub)
	return nil
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
