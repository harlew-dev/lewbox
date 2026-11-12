package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"uuid"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/db"
)

// SandboxKey is a customer's runner token on one cluster. Lewbox stores only
// its ID upstream and the last four characters of the token.
type SandboxKey struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Region    Region     `json:"region"`
	TokenHint string     `json:"token_hint"`
	CreatedBy string     `json:"created_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	Expired   bool       `json:"expired"`
}

// NewSandboxKey is returned by CreateSandboxKey: the key, its token and the
// environment variables a sandbox client pastes.
type NewSandboxKey struct {
	SandboxKey
	Token  string            `json:"token"`
	Env    map[string]string `json:"env"`
	E2BEnv map[string]string `json:"e2b_env,omitempty"`
}

// SandboxKeyList is returned by ListSandboxKeys. Data is never null.
type SandboxKeyList struct {
	Data []SandboxKey `json:"data"`
}

// CreateSandboxKeyRequest is the input to CreateSandboxKey. Region defaults to
// the only ready region. ExpiresIn defaults to 90d; "never" means no expiry.
type CreateSandboxKeyRequest struct {
	Name      string `json:"name"`
	Region    string `json:"region"`
	ExpiresIn string `json:"expires_in"`
}

const (
	defaultSandboxKeyLifetime = 90 * 24 * time.Hour
	neverSandboxKeyLifetime   = "36500d"
)

// sandboxKeyID returns a new key's ID. Tests replace it to know the upstream
// token name before the key exists.
var sandboxKeyID = func() string { return uuid.NewV7().String() }

// CreateSandboxKey mints a runner token for the organization's team and
// returns it once. An owner, admin or member may create one; a viewer cannot.
func (s *Service) CreateSandboxKey(ctx context.Context, org string, req CreateSandboxKeyRequest) (NewSandboxKey, error) {
	c, err := s.orgFor(ctx, org, accessUse)
	if err != nil {
		return NewSandboxKey{}, err
	}
	if err := sandboxOrgReady(c.org.Status); err != nil {
		return NewSandboxKey{}, err
	}
	name, err := keyName(req.Name)
	if err != nil {
		return NewSandboxKey{}, err
	}
	ttl, upstreamTTL, requested, err := sandboxLifetime(req.ExpiresIn)
	if err != nil {
		return NewSandboxKey{}, err
	}
	cluster, err := s.sandboxRegion(ctx, req.Region)
	if err != nil {
		return NewSandboxKey{}, err
	}
	if !c.org.Team.Valid || c.org.Team.String == "" {
		return NewSandboxKey{}, fmt.Errorf("organization %q has no team", c.org.Slug)
	}
	if _, err := s.q.GetLiveSandboxKeyByName(ctx, db.GetLiveSandboxKeyByNameParams{OrgID: c.org.ID, Name: name}); err == nil {
		return NewSandboxKey{}, conflict("%q already has a sandbox key named %q", c.org.Slug, name)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return NewSandboxKey{}, fmt.Errorf("get sandbox key: %w", err)
	}

	client, err := s.clusterClient(cluster)
	if err != nil {
		return NewSandboxKey{}, err
	}
	keyID := sandboxKeyID()
	upstreamName := "lbx." + keyID
	token, err := mintSandboxToken(ctx, client, aerolvm.CreateTokenRequest{
		Name:      upstreamName,
		Role:      "runner",
		Team:      c.org.Team.String,
		ExpiresIn: upstreamTTL,
	})
	if err != nil {
		return NewSandboxKey{}, s.sandboxTokenErr(ctx, cluster.Slug, requested, err)
	}
	if token.Token == "" {
		return NewSandboxKey{}, unavailable("cluster %q returned no token", cluster.Slug)
	}

	now := s.now()
	var expires sql.NullTime
	if ttl > 0 {
		expires = sql.NullTime{Time: now.Add(ttl), Valid: true}
	}
	var createdBy sql.NullString
	if c.p.Kind == "user" {
		createdBy = sql.NullString{String: c.p.UserID, Valid: true}
	}
	row, err := s.q.CreateSandboxKey(ctx, db.CreateSandboxKeyParams{
		ID:         keyID,
		OrgID:      c.org.ID,
		ClusterID:  cluster.ID,
		Name:       name,
		UpstreamID: token.ID,
		Last4:      last4(token.Token),
		CreatedBy:  createdBy,
		CreatedAt:  now,
		ExpiresAt:  expires,
	})
	if err != nil {
		_ = client.RevokeToken(context.WithoutCancel(ctx), token.ID)
		if db.IsUniqueViolation(err) {
			return NewSandboxKey{}, conflict("%q already has a sandbox key named %q", c.org.Slug, name)
		}
		return NewSandboxKey{}, fmt.Errorf("create sandbox key: %w", err)
	}

	key := toSandboxKey(row, c.p.Email, c.p.InstanceAdmin, cluster, now, c.p.InstanceAdmin)
	out := NewSandboxKey{
		SandboxKey: key,
		Token:      token.Token,
		Env: map[string]string{
			"SB_API_URL":   key.Region.EndpointURL,
			"SB_API_TOKEN": token.Token,
		},
	}
	if cluster.Runtime == "docker" {
		base := strings.TrimRight(key.Region.EndpointURL, "/") + "/e2b"
		out.E2BEnv = map[string]string{
			"E2B_API_URL":     base,
			"E2B_SANDBOX_URL": base + "/runtime",
			"E2B_API_KEY":     token.Token,
		}
	}
	return out, nil
}

// ListSandboxKeys returns the organization's keys, live ones first and newest
// first.
func (s *Service) ListSandboxKeys(ctx context.Context, org string) (SandboxKeyList, error) {
	c, err := s.orgFor(ctx, org, accessRead)
	if err != nil {
		return SandboxKeyList{}, err
	}
	rows, err := s.q.ListSandboxKeys(ctx, c.org.ID)
	if err != nil {
		return SandboxKeyList{}, fmt.Errorf("list sandbox keys: %w", err)
	}
	regions, err := s.clustersByID(ctx)
	if err != nil {
		return SandboxKeyList{}, err
	}
	now := s.now()
	out := SandboxKeyList{Data: make([]SandboxKey, 0, len(rows))}
	for _, r := range rows {
		cluster := regions[r.SandboxKey.ClusterID]
		out.Data = append(out.Data, toSandboxKey(r.SandboxKey, r.CreatedByEmail.String, r.CreatedByAdmin != 0, cluster, now, c.p.InstanceAdmin))
	}
	return out, nil
}

// RevokeSandboxKey revokes the upstream token, then marks the key revoked. A
// member may revoke only keys they created.
func (s *Service) RevokeSandboxKey(ctx context.Context, org, ref string) (SandboxKey, error) {
	c, err := s.orgFor(ctx, org, accessUse)
	if err != nil {
		return SandboxKey{}, err
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return SandboxKey{}, invalid("a key ID or name is required")
	}
	var row db.SandboxKey
	var email sql.NullString
	var createdByAdmin bool
	if id, perr := uuid.Parse(ref); perr == nil {
		r, err := s.q.GetSandboxKeyByID(ctx, db.GetSandboxKeyByIDParams{OrgID: c.org.ID, ID: id.String()})
		row, email, createdByAdmin = r.SandboxKey, r.CreatedByEmail, r.CreatedByAdmin != 0
		if err := sandboxKeyLookupErr(err, ref); err != nil {
			return SandboxKey{}, err
		}
	} else {
		r, err := s.q.GetLiveSandboxKeyByName(ctx, db.GetLiveSandboxKeyByNameParams{OrgID: c.org.ID, Name: ref})
		row, email, createdByAdmin = r.SandboxKey, r.CreatedByEmail, r.CreatedByAdmin != 0
		if err := sandboxKeyLookupErr(err, ref); err != nil {
			return SandboxKey{}, err
		}
	}
	if row.RevokedAt.Valid {
		return SandboxKey{}, conflict("sandbox key %q is already revoked", ref)
	}
	if c.role == roleMember && !c.p.InstanceAdmin {
		if !row.CreatedBy.Valid || row.CreatedBy.String != c.p.UserID {
			return SandboxKey{}, forbidden("only the member who created a key can revoke it; ask an owner or admin")
		}
	}
	cluster, err := s.findCluster(ctx, row.ClusterID)
	if err != nil {
		return SandboxKey{}, err
	}
	client, err := s.clusterClient(cluster)
	if err != nil {
		return SandboxKey{}, err
	}
	if err := client.RevokeToken(ctx, row.UpstreamID); err != nil {
		return SandboxKey{}, s.regionUnavailable(ctx, cluster.Slug, err)
	}
	now := s.now()
	n, err := s.q.RevokeSandboxKey(ctx, db.RevokeSandboxKeyParams{
		RevokedAt: sql.NullTime{Time: now, Valid: true},
		OrgID:     c.org.ID,
		ID:        row.ID,
	})
	if err != nil {
		return SandboxKey{}, fmt.Errorf("revoke sandbox key: %w", err)
	}
	if n == 0 {
		return SandboxKey{}, conflict("sandbox key %q is already revoked", ref)
	}
	row.RevokedAt = sql.NullTime{Time: now, Valid: true}
	return toSandboxKey(row, email.String, createdByAdmin, cluster, now, c.p.InstanceAdmin), nil
}

// sandboxOrgReady refuses keys for an organization that is not active.
func sandboxOrgReady(status string) error {
	switch status {
	case orgActive:
		return nil
	case orgPending:
		return precondition(nil, "your account is waiting for approval")
	case orgSuspended:
		return precondition(nil, "your organization is suspended")
	default:
		return precondition(nil, "organization is %s", status)
	}
}

// sandboxLifetime returns the stored lifetime (zero for never), the value
// sent upstream and what the customer asked for. Upstream gets whole hours
// rounded up, never the raw string.
func sandboxLifetime(raw string) (time.Duration, string, time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultSandboxKeyLifetime, upstreamLifetime(defaultSandboxKeyLifetime), defaultSandboxKeyLifetime, nil
	}
	if strings.EqualFold(raw, "never") {
		requested := 36500 * 24 * time.Hour
		return 0, neverSandboxKeyLifetime, requested, nil
	}
	d, err := parseLifetime(raw)
	if err != nil {
		return 0, "", 0, err
	}
	return d, upstreamLifetime(d), d, nil
}

func upstreamLifetime(d time.Duration) string {
	return fmt.Sprintf("%dh", (d+time.Hour-1)/time.Hour)
}

// sandboxRegion finds the cluster behind ref. An empty ref means the only
// ready region.
func (s *Service) sandboxRegion(ctx context.Context, ref string) (db.Cluster, error) {
	rows, err := s.q.ListClusters(ctx)
	if err != nil {
		return db.Cluster{}, fmt.Errorf("list clusters: %w", err)
	}
	ready := make([]db.Cluster, 0, len(rows))
	for _, row := range rows {
		if regionReady(row.Status) {
			ready = append(ready, row)
		}
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		switch len(ready) {
		case 0:
			return db.Cluster{}, precondition(nil, "no region is available: ask the operator to add a ready cluster")
		case 1:
			return ready[0], nil
		default:
			return db.Cluster{}, invalid("choose a region: pass --region <slug>")
		}
	}
	for _, row := range rows {
		if row.ID != ref && row.Slug != ref {
			continue
		}
		if !regionReady(row.Status) {
			return db.Cluster{}, precondition(nil, "region %q is %s", row.Slug, row.Status)
		}
		return row, nil
	}
	return db.Cluster{}, notFound("region %q not found", ref)
}

func regionReady(status string) bool {
	return status == "ready" || status == "degraded"
}

// clustersByID maps cluster IDs to rows, for rendering the region of a key.
func (s *Service) clustersByID(ctx context.Context) (map[string]db.Cluster, error) {
	rows, err := s.q.ListClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	clusters := make(map[string]db.Cluster, len(rows))
	for _, row := range rows {
		clusters[row.ID] = row
	}
	return clusters, nil
}

// mintSandboxToken mints a runner token, recovering from the 409 a retried 503
// can leave behind: drop our own token with that name, then mint once more.
func mintSandboxToken(ctx context.Context, client *aerolvm.Client, req aerolvm.CreateTokenRequest) (aerolvm.Token, error) {
	token, err := client.CreateToken(ctx, req)
	if err == nil {
		return token, nil
	}
	if apiErr, ok := errors.AsType[*aerolvm.APIError](err); !ok || apiErr.Status != http.StatusConflict {
		return aerolvm.Token{}, err
	}
	tokens, err := client.ListTokens(ctx)
	if err != nil {
		return aerolvm.Token{}, err
	}
	for _, t := range tokens {
		if t.Name != req.Name || t.Team != req.Team {
			continue
		}
		if err := client.RevokeToken(ctx, t.ID); err != nil {
			return aerolvm.Token{}, err
		}
	}
	return client.CreateToken(ctx, req)
}

// sandboxTokenErr turns an upstream token failure into the error the customer
// sees. A lifetime over 365 days that the cluster refuses is its cap, not a
// bad request.
func (s *Service) sandboxTokenErr(ctx context.Context, slug string, requested time.Duration, err error) error {
	if apiErr, ok := errors.AsType[*aerolvm.APIError](err); ok && apiErr.Status == http.StatusBadRequest && requested > 365*24*time.Hour {
		return invalid("cluster %q refused the token lifetime: %s; raise SB_TOKEN_MAX_TTL on every node so keys can last", slug, apiErr.Message)
	}
	return s.regionUnavailable(ctx, slug, err)
}

// regionUnavailable reports a cluster failure without exposing its URL.
// AerolVM's own 4xx text is safe to show; instance admins get the detail.
func (s *Service) regionUnavailable(ctx context.Context, slug string, err error) error {
	s.log.Warn("region call failed", "region", slug, "err", err)
	if apiErr, ok := errors.AsType[*aerolvm.APIError](err); ok && apiErr.Status >= 400 && apiErr.Status < 500 {
		return unavailable("region %q: %s", slug, apiErr.Message)
	}
	if p, ok := PrincipalFrom(ctx); ok && p.InstanceAdmin {
		return unavailable("region %q: %s", slug, err)
	}
	return unavailable("region %q is not reachable right now; try again shortly", slug)
}

func sandboxKeyLookupErr(err error, ref string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return notFound("sandbox key %q not found", ref)
	}
	if err != nil {
		return fmt.Errorf("get sandbox key: %w", err)
	}
	return nil
}

func toSandboxKey(r db.SandboxKey, createdBy string, createdByAdmin bool, cluster db.Cluster, now time.Time, callerAdmin bool) SandboxKey {
	if createdByAdmin && !callerAdmin {
		createdBy = "operator"
	}
	return SandboxKey{
		ID:        r.ID,
		Name:      r.Name,
		Region:    toRegion(cluster),
		TokenHint: "avm_…" + r.Last4,
		CreatedBy: createdBy,
		CreatedAt: r.CreatedAt.UTC(),
		ExpiresAt: nullTime(r.ExpiresAt),
		RevokedAt: nullTime(r.RevokedAt),
		Expired:   r.ExpiresAt.Valid && !now.Before(r.ExpiresAt.Time),
	}
}
