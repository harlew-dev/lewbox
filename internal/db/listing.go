package db

// The paged list queries. sqlc cannot build a dynamic ORDER BY or an IN list,
// so these are hand-written on purpose (and are not one of the generated
// *.sql.go files). Every filter value is a bound parameter; every ORDER BY
// comes from a fixed map in internal/service and ends in a unique column.

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// listWhere collects a list's conditions and the values bound to them.
type listWhere struct {
	conds []string
	args  []any
}

// Add appends one condition with its placeholders.
func (w *listWhere) Add(cond string, args ...any) {
	w.conds = append(w.conds, cond)
	w.args = append(w.args, args...)
}

// InStrings appends "column IN (?, ?, …)" for values. An empty list adds
// nothing.
func (w *listWhere) InStrings(column string, values []string) {
	if len(values) == 0 {
		return
	}
	w.Add(column+" IN ("+placeholders(len(values))+")", stringsToAny(values)...)
}

// InInts appends "column IN (?, ?, …)" for ints. An empty list adds nothing.
func (w *listWhere) InInts(column string, values []int64) {
	if len(values) == 0 {
		return
	}
	args := make([]any, len(values))
	for i, v := range values {
		args[i] = v
	}
	w.Add(column+" IN ("+placeholders(len(values))+")", args...)
}

func (w *listWhere) sql() string {
	if len(w.conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(w.conds, " AND ")
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func stringsToAny(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// listPage runs the count and the page over one base query. The scope and
// filters live in w, so the count can never see rows the page does not.
func (q *Queries) listPage(ctx context.Context, cols, from string, w *listWhere, order string, limit, offset int, scan func(*sql.Rows) error) (int, error) {
	where := w.sql()
	var total int
	if err := q.db.QueryRowContext(ctx, "SELECT count(*) "+from+where, w.args...).Scan(&total); err != nil {
		return 0, err
	}
	args := append(append([]any{}, w.args...), limit, offset)
	rows, err := q.db.QueryContext(ctx,
		"SELECT "+cols+" "+from+where+" ORDER BY "+order+" LIMIT ? OFFSET ?", args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return 0, err
		}
	}
	return total, rows.Err()
}

// OrganizationListParams filters and pages organizations. ScopeOrgID and
// ScopeUserID narrow the caller's view; the rest are filters.
type OrganizationListParams struct {
	ScopeOrgID  string
	ScopeUserID string
	Statuses    []string
	Regions     []string
	Plans       []string
	Pattern     string
	Order       string
	Limit       int
	Offset      int
}

// OrganizationListRow is an organization with the number of its started
// sandboxes, counted in the same query.
type OrganizationListRow struct {
	Organization
	Running int64
}

// ListOrganizationsPage returns one page of organizations and the total
// matching the same scope and filters.
func (q *Queries) ListOrganizationsPage(ctx context.Context, p OrganizationListParams) ([]OrganizationListRow, int, error) {
	var w listWhere
	if p.ScopeOrgID != "" {
		w.Add("o.id = ?", p.ScopeOrgID)
	}
	if p.ScopeUserID != "" {
		w.Add("EXISTS (SELECT 1 FROM memberships sm WHERE sm.org_id = o.id AND sm.user_id = ?)", p.ScopeUserID)
	}
	w.InStrings("o.status", p.Statuses)
	if len(p.Regions) > 0 {
		w.Add("EXISTS (SELECT 1 FROM subscriptions rs JOIN clusters rc ON rc.id = rs.cluster_id WHERE rs.org_id = o.id AND rc.slug IN ("+placeholders(len(p.Regions))+"))", stringsToAny(p.Regions)...)
	}
	if len(p.Plans) > 0 {
		w.Add("EXISTS (SELECT 1 FROM subscriptions ps JOIN plans pp ON pp.id = ps.plan_id WHERE ps.org_id = o.id AND pp.slug IN ("+placeholders(len(p.Plans))+"))", stringsToAny(p.Plans)...)
	}
	if p.Pattern != "" {
		w.Add(`(lower(o.name) LIKE lower(?) ESCAPE '\' OR lower(o.slug) LIKE lower(?) ESCAPE '\')`, p.Pattern, p.Pattern)
	}
	out := make([]OrganizationListRow, 0)
	total, err := q.listPage(ctx,
		"o.id, o.slug, o.name, o.status, o.created_at, o.team, o.status_reason, COALESCE(sb.running, 0)",
		"FROM organizations o LEFT JOIN (SELECT org_id, COUNT(*) AS running FROM sandboxes WHERE status = 'started' AND gone_at IS NULL GROUP BY org_id) sb ON sb.org_id = o.id",
		&w, p.Order, p.Limit, p.Offset,
		func(rows *sql.Rows) error {
			var row OrganizationListRow
			if err := rows.Scan(&row.ID, &row.Slug, &row.Name, &row.Status, &row.CreatedAt, &row.Team, &row.StatusReason, &row.Running); err != nil {
				return err
			}
			out = append(out, row)
			return nil
		})
	return out, int(total), err
}

// CountStartedSandboxesByOrg counts one organization's started sandboxes in
// the mirror, for a single-organization response.
func (q *Queries) CountStartedSandboxesByOrg(ctx context.Context, orgID string) (int, error) {
	var n int
	err := q.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sandboxes WHERE org_id = ? AND status = 'started' AND gone_at IS NULL", orgID).Scan(&n)
	return n, err
}

// RolesForOrganizations returns one account's role in each organization. An
// empty result means the account is not a member of any of them.
func (q *Queries) RolesForOrganizations(ctx context.Context, userID string, orgIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(orgIDs))
	if userID == "" || len(orgIDs) == 0 {
		return out, nil
	}
	query := "SELECT org_id, role FROM memberships WHERE user_id = ? AND org_id IN (" + placeholders(len(orgIDs)) + ")"
	args := append([]any{userID}, stringsToAny(orgIDs)...)
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var orgID, role string
		if err := rows.Scan(&orgID, &role); err != nil {
			return nil, err
		}
		out[orgID] = role
	}
	return out, rows.Err()
}

// UserListParams filters and pages accounts. Disabled and InstanceAdmin hold
// the database values the status and role filters map to.
type UserListParams struct {
	Disabled      []int64
	InstanceAdmin []int64
	Pattern       string
	Order         string
	Limit         int
	Offset        int
}

// ListUsersPage returns one page of accounts and the total.
func (q *Queries) ListUsersPage(ctx context.Context, p UserListParams) ([]User, int, error) {
	var w listWhere
	w.InInts("disabled", p.Disabled)
	w.InInts("instance_admin", p.InstanceAdmin)
	if p.Pattern != "" {
		w.Add(`lower(email) LIKE lower(?) ESCAPE '\'`, p.Pattern)
	}
	out := make([]User, 0)
	total, err := q.listPage(ctx,
		"id, email, password_hash, instance_admin, disabled, created_at",
		"FROM users", &w, p.Order, p.Limit, p.Offset,
		func(rows *sql.Rows) error {
			var row User
			if err := rows.Scan(&row.ID, &row.Email, &row.PasswordHash, &row.InstanceAdmin, &row.Disabled, &row.CreatedAt); err != nil {
				return err
			}
			out = append(out, row)
			return nil
		})
	return out, int(total), err
}

// SandboxListParams filters and pages the sandbox mirror. OrgID is the
// caller's scope; OrgIDs and ClusterIDs are resolved filters.
type SandboxListParams struct {
	OrgID      string
	OrgIDs     []string
	ClusterIDs []string
	Statuses   []string
	Pattern    string
	Order      string
	Limit      int
	Offset     int
}

// ListSandboxesPage returns one page of mirrored sandboxes and the total.
func (q *Queries) ListSandboxesPage(ctx context.Context, p SandboxListParams) ([]ListSandboxesRow, int, error) {
	var w listWhere
	w.Add("s.gone_at IS NULL")
	if p.OrgID != "" {
		w.Add("s.org_id = ?", p.OrgID)
	}
	w.InStrings("s.org_id", p.OrgIDs)
	w.InStrings("s.cluster_id", p.ClusterIDs)
	w.InStrings("s.status", p.Statuses)
	if p.Pattern != "" {
		w.Add(`lower(s.id) LIKE lower(?) ESCAPE '\'`, p.Pattern)
	}
	out := make([]ListSandboxesRow, 0)
	total, err := q.listPage(ctx,
		"s.cluster_id, s.id, s.org_id, s.team, s.node, s.name, s.image, s.status, s.status_reason, s.runtime, s.cpu, s.memory_mb, s.disk_gb, s.created_at, s.last_seen_at, s.gone_at, c.slug, o.slug, o.name",
		"FROM sandboxes s JOIN clusters c ON c.id = s.cluster_id LEFT JOIN organizations o ON o.id = s.org_id",
		&w, p.Order, p.Limit, p.Offset,
		func(rows *sql.Rows) error {
			var row ListSandboxesRow
			if err := rows.Scan(
				&row.Sandbox.ClusterID, &row.Sandbox.ID, &row.Sandbox.OrgID, &row.Sandbox.Team,
				&row.Sandbox.Node, &row.Sandbox.Name, &row.Sandbox.Image, &row.Sandbox.Status,
				&row.Sandbox.StatusReason, &row.Sandbox.Runtime, &row.Sandbox.Cpu, &row.Sandbox.MemoryMb,
				&row.Sandbox.DiskGb, &row.Sandbox.CreatedAt, &row.Sandbox.LastSeenAt, &row.Sandbox.GoneAt,
				&row.ClusterSlug, &row.OrgSlug, &row.OrgName,
			); err != nil {
				return err
			}
			out = append(out, row)
			return nil
		})
	return out, int(total), err
}

// NodeListParams filters and pages nodes.
type NodeListParams struct {
	ClusterIDs []string
	Roles      []string
	Alive      *bool
	Pattern    string
	Order      string
	Limit      int
	Offset     int
}

// NodeListRow is a node with its cluster's slug.
type NodeListRow struct {
	Node        Node
	ClusterSlug sql.NullString
}

// ListNodesPage returns one page of nodes and the total.
func (q *Queries) ListNodesPage(ctx context.Context, p NodeListParams) ([]NodeListRow, int, error) {
	var w listWhere
	w.InStrings("n.cluster_id", p.ClusterIDs)
	w.InStrings("n.role", p.Roles)
	if p.Alive != nil {
		alive := int64(0)
		if *p.Alive {
			alive = 1
		}
		w.Add("n.alive = ?", alive)
	}
	if p.Pattern != "" {
		w.Add(`(lower(n.name) LIKE lower(?) ESCAPE '\' OR lower(n.url) LIKE lower(?) ESCAPE '\')`, p.Pattern, p.Pattern)
	}
	out := make([]NodeListRow, 0)
	total, err := q.listPage(ctx,
		"n.id, n.name, n.url, n.created_at, n.cluster_id, n.role, n.version, n.alive, n.last_seen_at, n.settings_applied_at, n.settings_storage_profile_id, n.settings_storage_version, n.settings_dns_profile_id, n.settings_dns_version, n.settings_archive_retention, c.slug",
		"FROM nodes n LEFT JOIN clusters c ON c.id = n.cluster_id",
		&w, p.Order, p.Limit, p.Offset,
		func(rows *sql.Rows) error {
			var row NodeListRow
			if err := rows.Scan(
				&row.Node.ID, &row.Node.Name, &row.Node.URL, &row.Node.CreatedAt, &row.Node.ClusterID,
				&row.Node.Role, &row.Node.Version, &row.Node.Alive, &row.Node.LastSeenAt,
				&row.Node.SettingsAppliedAt, &row.Node.SettingsStorageProfileID, &row.Node.SettingsStorageVersion,
				&row.Node.SettingsDnsProfileID, &row.Node.SettingsDnsVersion, &row.Node.SettingsArchiveRetention,
				&row.ClusterSlug,
			); err != nil {
				return err
			}
			out = append(out, row)
			return nil
		})
	return out, int(total), err
}

// MemberListParams filters and pages one organization's members.
type MemberListParams struct {
	OrgID   string
	Roles   []string
	Pattern string
	// HideOperatorEmail keeps a customer's search from probing an instance
	// admin's real email, which the response masks as "operator".
	HideOperatorEmail bool
	Order             string
	Limit             int
	Offset            int
}

// ListMembersPage returns one page of members and the total.
func (q *Queries) ListMembersPage(ctx context.Context, p MemberListParams) ([]ListMembersRow, int, error) {
	var w listWhere
	w.Add("m.org_id = ?", p.OrgID)
	w.InStrings("m.role", p.Roles)
	if p.Pattern != "" {
		if p.HideOperatorEmail {
			w.Add(`(u.instance_admin = 0 AND lower(u.email) LIKE lower(?) ESCAPE '\')`, p.Pattern)
		} else {
			w.Add(`lower(u.email) LIKE lower(?) ESCAPE '\'`, p.Pattern)
		}
	}
	out := make([]ListMembersRow, 0)
	total, err := q.listPage(ctx,
		"m.id, m.org_id, m.user_id, m.role, m.created_at, u.email, u.instance_admin",
		"FROM memberships m JOIN users u ON u.id = m.user_id",
		&w, p.Order, p.Limit, p.Offset,
		func(rows *sql.Rows) error {
			var row ListMembersRow
			if err := rows.Scan(&row.Membership.ID, &row.Membership.OrgID, &row.Membership.UserID,
				&row.Membership.Role, &row.Membership.CreatedAt, &row.Email, &row.InstanceAdmin); err != nil {
				return err
			}
			out = append(out, row)
			return nil
		})
	return out, int(total), err
}

// keyStatusWhere adds the status filter the three key lists share. Status is
// computed from revoked_at and expires_at, so the conditions are written here
// and bound to now.
func keyStatusWhere(w *listWhere, prefix string, statuses []string, now time.Time) {
	if len(statuses) == 0 {
		return
	}
	conds := make([]string, 0, len(statuses))
	args := make([]any, 0, len(statuses))
	for _, status := range statuses {
		switch status {
		case "active":
			conds = append(conds, "("+prefix+".revoked_at IS NULL AND ("+prefix+".expires_at IS NULL OR "+prefix+".expires_at > ?))")
			args = append(args, now)
		case "expired":
			conds = append(conds, "("+prefix+".revoked_at IS NULL AND "+prefix+".expires_at IS NOT NULL AND "+prefix+".expires_at <= ?)")
			args = append(args, now)
		case "revoked":
			conds = append(conds, prefix+".revoked_at IS NOT NULL")
		}
	}
	if len(conds) > 0 {
		w.Add("("+strings.Join(conds, " OR ")+")", args...)
	}
}

// SandboxKeyListParams filters and pages one organization's sandbox keys.
type SandboxKeyListParams struct {
	OrgID    string
	Statuses []string
	Pattern  string
	Order    string
	Now      time.Time
	Limit    int
	Offset   int
}

// ListSandboxKeysPage returns one page of sandbox keys and the total.
func (q *Queries) ListSandboxKeysPage(ctx context.Context, p SandboxKeyListParams) ([]ListSandboxKeysRow, int, error) {
	var w listWhere
	w.Add("k.org_id = ?", p.OrgID)
	keyStatusWhere(&w, "k", p.Statuses, p.Now)
	if p.Pattern != "" {
		w.Add(`lower(k.name) LIKE lower(?) ESCAPE '\'`, p.Pattern)
	}
	out := make([]ListSandboxKeysRow, 0)
	total, err := q.listPage(ctx,
		"k.id, k.org_id, k.cluster_id, k.name, k.upstream_id, k.last4, k.created_by, k.created_at, k.expires_at, k.revoked_at, u.email, COALESCE(u.instance_admin, 0)",
		"FROM sandbox_keys k LEFT JOIN users u ON u.id = k.created_by",
		&w, p.Order, p.Limit, p.Offset,
		func(rows *sql.Rows) error {
			var row ListSandboxKeysRow
			if err := rows.Scan(
				&row.SandboxKey.ID, &row.SandboxKey.OrgID, &row.SandboxKey.ClusterID, &row.SandboxKey.Name,
				&row.SandboxKey.UpstreamID, &row.SandboxKey.Last4, &row.SandboxKey.CreatedBy,
				&row.SandboxKey.CreatedAt, &row.SandboxKey.ExpiresAt, &row.SandboxKey.RevokedAt,
				&row.CreatedByEmail, &row.CreatedByAdmin,
			); err != nil {
				return err
			}
			out = append(out, row)
			return nil
		})
	return out, int(total), err
}

// AdminKeyListParams filters and pages one organization's admin keys.
type AdminKeyListParams struct {
	OrgID    string
	Statuses []string
	Pattern  string
	Order    string
	Now      time.Time
	Limit    int
	Offset   int
}

// ListAdminKeysPage returns one page of admin keys and the total.
func (q *Queries) ListAdminKeysPage(ctx context.Context, p AdminKeyListParams) ([]ListAdminKeysRow, int, error) {
	var w listWhere
	w.Add("k.org_id = ?", p.OrgID)
	keyStatusWhere(&w, "k", p.Statuses, p.Now)
	if p.Pattern != "" {
		w.Add(`(lower(k.name) LIKE lower(?) ESCAPE '\' OR lower(k.prefix) LIKE lower(?) ESCAPE '\')`, p.Pattern, p.Pattern)
	}
	out := make([]ListAdminKeysRow, 0)
	total, err := q.listPage(ctx,
		"k.id, k.org_id, k.name, k.prefix, k.key_hash, k.last4, k.created_by, k.created_at, k.last_used_at, k.expires_at, k.revoked_at, u.email",
		"FROM admin_keys k LEFT JOIN users u ON u.id = k.created_by",
		&w, p.Order, p.Limit, p.Offset,
		func(rows *sql.Rows) error {
			var row ListAdminKeysRow
			if err := rows.Scan(
				&row.AdminKey.ID, &row.AdminKey.OrgID, &row.AdminKey.Name, &row.AdminKey.Prefix,
				&row.AdminKey.KeyHash, &row.AdminKey.Last4, &row.AdminKey.CreatedBy, &row.AdminKey.CreatedAt,
				&row.AdminKey.LastUsedAt, &row.AdminKey.ExpiresAt, &row.AdminKey.RevokedAt,
				&row.CreatedByEmail,
			); err != nil {
				return err
			}
			out = append(out, row)
			return nil
		})
	return out, int(total), err
}

// AccessTokenListParams filters and pages one account's access tokens.
type AccessTokenListParams struct {
	UserID   string
	Statuses []string
	Pattern  string
	Order    string
	Now      time.Time
	Limit    int
	Offset   int
}

// ListAccessTokensPage returns one page of access tokens and the total.
func (q *Queries) ListAccessTokensPage(ctx context.Context, p AccessTokenListParams) ([]AccessToken, int, error) {
	var w listWhere
	w.Add("t.user_id = ?", p.UserID)
	keyStatusWhere(&w, "t", p.Statuses, p.Now)
	if p.Pattern != "" {
		w.Add(`(lower(t.name) LIKE lower(?) ESCAPE '\' OR lower(t.prefix) LIKE lower(?) ESCAPE '\')`, p.Pattern, p.Pattern)
	}
	out := make([]AccessToken, 0)
	total, err := q.listPage(ctx,
		"t.id, t.user_id, t.name, t.prefix, t.token_hash, t.last4, t.created_at, t.last_used_at, t.expires_at, t.revoked_at",
		"FROM access_tokens t", &w, p.Order, p.Limit, p.Offset,
		func(rows *sql.Rows) error {
			var row AccessToken
			if err := rows.Scan(&row.ID, &row.UserID, &row.Name, &row.Prefix, &row.TokenHash,
				&row.Last4, &row.CreatedAt, &row.LastUsedAt, &row.ExpiresAt, &row.RevokedAt); err != nil {
				return err
			}
			out = append(out, row)
			return nil
		})
	return out, int(total), err
}

// SetupLinkListParams filters and pages setup links. PlanIDs and ClusterIDs
// are resolved from the public plan and region filters.
type SetupLinkListParams struct {
	Statuses   []string
	PlanIDs    []string
	ClusterIDs []string
	Pattern    string
	Order      string
	Now        time.Time
	Limit      int
	Offset     int
}

// ListSetupLinksPage returns one page of setup links and the total.
func (q *Queries) ListSetupLinksPage(ctx context.Context, p SetupLinkListParams) ([]SetupLink, int, error) {
	var w listWhere
	if len(p.Statuses) > 0 {
		conds := make([]string, 0, len(p.Statuses))
		args := make([]any, 0, len(p.Statuses))
		for _, status := range p.Statuses {
			switch status {
			case "used":
				conds = append(conds, "l.used_at IS NOT NULL")
			case "expired":
				conds = append(conds, "(l.used_at IS NULL AND l.expires_at <= ?)")
				args = append(args, p.Now)
			case "claimed":
				conds = append(conds, "(l.used_at IS NULL AND l.expires_at > ? AND l.claimed_by IS NOT NULL)")
				args = append(args, p.Now)
			case "unused":
				conds = append(conds, "(l.used_at IS NULL AND l.expires_at > ? AND l.claimed_by IS NULL)")
				args = append(args, p.Now)
			}
		}
		w.Add("("+strings.Join(conds, " OR ")+")", args...)
	}
	w.InStrings("l.plan_id", p.PlanIDs)
	w.InStrings("l.cluster_id", p.ClusterIDs)
	if p.Pattern != "" {
		w.Add(`lower(l.note) LIKE lower(?) ESCAPE '\'`, p.Pattern)
	}
	out := make([]SetupLink, 0)
	total, err := q.listPage(ctx,
		"l.id, l.token_hash, l.plan_id, l.cluster_id, l.note, l.created_by, l.created_at, l.expires_at, l.claimed_by, l.claimed_at, l.used_at, l.org_id",
		"FROM setup_links l", &w, p.Order, p.Limit, p.Offset,
		func(rows *sql.Rows) error {
			var row SetupLink
			if err := rows.Scan(&row.ID, &row.TokenHash, &row.PlanID, &row.ClusterID, &row.Note,
				&row.CreatedBy, &row.CreatedAt, &row.ExpiresAt, &row.ClaimedBy, &row.ClaimedAt,
				&row.UsedAt, &row.OrgID); err != nil {
				return err
			}
			out = append(out, row)
			return nil
		})
	return out, int(total), err
}

// EventListParams filters one cursor page of the audit trail. The three
// multi-value filters hold resolved values; the order stays newest first.
type EventListParams struct {
	ActorIDs        []string
	OrganizationIDs []string
	Names           []string
	FromTime        time.Time
	ToTime          time.Time
	BeforeID        string
	BeforeTime      time.Time
	RowLimit        int
}

// ListEventsFiltered returns one cursor page of events, newest first.
func (q *Queries) ListEventsFiltered(ctx context.Context, p EventListParams) ([]ListEventsRow, error) {
	var w listWhere
	w.InStrings("e.actor_id", p.ActorIDs)
	w.InStrings("e.organization_id", p.OrganizationIDs)
	w.InStrings("e.name", p.Names)
	if !p.FromTime.IsZero() {
		w.Add("e.created_at >= ?", p.FromTime)
	}
	if !p.ToTime.IsZero() {
		w.Add("e.created_at < ?", p.ToTime)
	}
	if p.BeforeID != "" {
		w.Add("(e.created_at, e.id) < (?, ?)", p.BeforeTime, p.BeforeID)
	}
	args := append(append([]any{}, w.args...), p.RowLimit)
	rows, err := q.db.QueryContext(ctx,
		"SELECT e.id, e.name, e.actor_kind, e.actor_id, e.actor_label, e.target_type, e.target_id, e.target_name, e.organization_id, e.changes, e.ip, e.request_id, e.created_at, o.slug"+
			" FROM events e LEFT JOIN organizations o ON o.id = e.organization_id"+w.sql()+
			" ORDER BY e.created_at DESC, e.id DESC LIMIT ?", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ListEventsRow, 0)
	for rows.Next() {
		var row ListEventsRow
		if err := rows.Scan(&row.Event.ID, &row.Event.Name, &row.Event.ActorKind, &row.Event.ActorID,
			&row.Event.ActorLabel, &row.Event.TargetType, &row.Event.TargetID, &row.Event.TargetName,
			&row.Event.OrganizationID, &row.Event.Changes, &row.Event.Ip, &row.Event.RequestID,
			&row.Event.CreatedAt, &row.OrganizationSlug); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
