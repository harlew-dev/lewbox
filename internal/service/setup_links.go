package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/harlew-dev/lewbox/internal/db"
)

// SetupLink is a single-use link that lets someone register, create a
// workspace and get a plan in a region (DECISIONS D33). Token is only present
// in the create response.
type SetupLink struct {
	ID        string              `json:"id"`
	Plan      PlanRef             `json:"plan"`
	Region    SetupLinkRegion     `json:"region"`
	Note      string              `json:"note"`
	Status    string              `json:"status"` // unused | claimed | used | expired
	ClaimedBy *string             `json:"claimed_by"`
	Workspace *SetupLinkWorkspace `json:"workspace"`
	CreatedAt time.Time           `json:"created_at"`
	ExpiresAt time.Time           `json:"expires_at"`
	UsedAt    *time.Time          `json:"used_at"`
	Token     string              `json:"token,omitempty"`
}

// SetupLinkRegion names the region a setup link subscribes.
type SetupLinkRegion struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// SetupLinkWorkspace names the workspace a setup link created.
type SetupLinkWorkspace struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// SetupLinkList is returned by ListSetupLinks. Data is never null.
type SetupLinkList struct {
	Data []SetupLink `json:"data"`
}

// CreateSetupLinkRequest is the input to CreateSetupLink. ExpiresInHours
// defaults to 168 and must be 1..720. Plan and Region are a slug or ID.
type CreateSetupLinkRequest struct {
	Plan           string `json:"plan"`
	Region         string `json:"region"`
	ExpiresInHours int    `json:"expires_in_hours"`
	Note           string `json:"note"`
}

// SetupLinkInfo is what the public setup page shows: names only, never slugs
// or who claimed the link.
type SetupLinkInfo struct {
	InstanceName string        `json:"instance_name"`
	Plan         SetupLinkName `json:"plan"`
	Region       SetupLinkName `json:"region"`
	ExpiresAt    time.Time     `json:"expires_at"`
}

// SetupLinkName is a plan or region's display name.
type SetupLinkName struct {
	Name string `json:"name"`
}

const (
	setupLinkUnused  = "unused"
	setupLinkClaimed = "claimed"
	setupLinkUsed    = "used"
	setupLinkExpired = "expired"

	setupLinkPrefix     = "lbs_"
	setupLinkChars      = 32
	setupLinkAlphabet   = "abcdefghijklmnopqrstuvwxyz234567"
	setupLinkNoteMax    = 200
	defaultSetupLinkTTL = 168
	minSetupLinkTTL     = 1
	maxSetupLinkTTL     = 720

	setupLinkInvalidMessage = "this setup link is invalid or has expired"
)

// CreateSetupLink mints a setup link and returns its token once. Instance
// admins only.
func (s *Service) CreateSetupLink(ctx context.Context, req CreateSetupLinkRequest) (SetupLink, error) {
	p, err := requireAdmin(ctx)
	if err != nil {
		return SetupLink{}, err
	}
	plan, err := s.findPlan(ctx, req.Plan)
	if err != nil {
		return SetupLink{}, err
	}
	cluster, err := s.findCluster(ctx, req.Region)
	if err != nil {
		return SetupLink{}, err
	}
	hours := req.ExpiresInHours
	if hours == 0 {
		hours = defaultSetupLinkTTL
	}
	if hours < minSetupLinkTTL || hours > maxSetupLinkTTL {
		return SetupLink{}, invalid("expires_in_hours must be between %d and %d", minSetupLinkTTL, maxSetupLinkTTL)
	}
	note := strings.TrimSpace(req.Note)
	if note != "" {
		if err := displayName(note, "note", setupLinkNoteMax); err != nil {
			return SetupLink{}, err
		}
	}
	token, hash, err := newSetupToken()
	if err != nil {
		return SetupLink{}, err
	}
	now := s.now()
	var createdBy sql.NullString
	if p.Kind == "user" {
		createdBy = sql.NullString{String: p.UserID, Valid: true}
	}
	row, err := s.q.CreateSetupLink(ctx, db.CreateSetupLinkParams{
		ID:        uuid.NewV7().String(),
		TokenHash: hash,
		PlanID:    plan.ID,
		ClusterID: cluster.ID,
		Note:      note,
		CreatedBy: createdBy,
		CreatedAt: now,
		ExpiresAt: now.Add(time.Duration(hours) * time.Hour),
	})
	if err != nil {
		return SetupLink{}, fmt.Errorf("create setup link: %w", err)
	}
	out, err := s.setupLinkResponse(ctx, row)
	if err != nil {
		return SetupLink{}, err
	}
	out.Token = token
	return out, nil
}

// ListSetupLinks returns every setup link, newest first, never its token.
// Instance admins only.
func (s *Service) ListSetupLinks(ctx context.Context) (SetupLinkList, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return SetupLinkList{}, err
	}
	rows, err := s.q.ListSetupLinks(ctx)
	if err != nil {
		return SetupLinkList{}, fmt.Errorf("list setup links: %w", err)
	}
	out := SetupLinkList{Data: make([]SetupLink, 0, len(rows))}
	for _, row := range rows {
		link, err := s.setupLinkResponse(ctx, row)
		if err != nil {
			return SetupLinkList{}, err
		}
		out.Data = append(out.Data, link)
	}
	return out, nil
}

// RevokeSetupLink deletes an unused setup link and returns it. A used link
// cannot be revoked; it stays for the record. Instance admins only.
func (s *Service) RevokeSetupLink(ctx context.Context, id string) (SetupLink, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return SetupLink{}, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return SetupLink{}, invalid("a setup link ID is required")
	}
	row, err := s.q.GetSetupLinkByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return SetupLink{}, notFound("setup link %q not found", id)
	}
	if err != nil {
		return SetupLink{}, fmt.Errorf("get setup link: %w", err)
	}
	out, err := s.setupLinkResponse(ctx, row)
	if err != nil {
		return SetupLink{}, err
	}
	n, err := s.q.DeleteSetupLink(ctx, id)
	if err != nil {
		return SetupLink{}, fmt.Errorf("delete setup link: %w", err)
	}
	if n == 0 {
		return SetupLink{}, conflict("setup link %q has already been used", id)
	}
	return out, nil
}

// SetupLinkInfo describes an unused, unexpired setup link for the public page.
// It is HTTP-only, like sign-up.
func (s *Service) SetupLinkInfo(ctx context.Context, token string) (SetupLinkInfo, error) {
	row, err := s.findSetupLink(ctx, token)
	if errors.Is(err, sql.ErrNoRows) {
		return SetupLinkInfo{}, setupLinkNotFound()
	}
	if err != nil {
		return SetupLinkInfo{}, err
	}
	if row.UsedAt.Valid || !row.ExpiresAt.After(s.now()) {
		return SetupLinkInfo{}, setupLinkNotFound()
	}
	settings, err := s.getSettings(ctx)
	if err != nil {
		return SetupLinkInfo{}, err
	}
	plan, err := s.findPlan(ctx, row.PlanID)
	if err != nil {
		return SetupLinkInfo{}, err
	}
	cluster, err := s.findCluster(ctx, row.ClusterID)
	if err != nil {
		return SetupLinkInfo{}, err
	}
	return SetupLinkInfo{
		InstanceName: settings.InstanceName,
		Plan:         SetupLinkName{Name: plan.Name},
		Region:       SetupLinkName{Name: cluster.Name},
		ExpiresAt:    row.ExpiresAt.UTC(),
	}, nil
}

// setupLinkResponse turns a stored link into its API shape.
func (s *Service) setupLinkResponse(ctx context.Context, row db.SetupLink) (SetupLink, error) {
	plan, err := s.findPlan(ctx, row.PlanID)
	if err != nil {
		return SetupLink{}, err
	}
	cluster, err := s.findCluster(ctx, row.ClusterID)
	if err != nil {
		return SetupLink{}, err
	}
	out := SetupLink{
		ID:        row.ID,
		Plan:      PlanRef{Slug: plan.Slug, Name: plan.Name},
		Region:    SetupLinkRegion{Slug: cluster.Slug, Name: cluster.Name},
		Note:      row.Note,
		Status:    setupLinkStatus(row, s.now()),
		CreatedAt: row.CreatedAt.UTC(),
		ExpiresAt: row.ExpiresAt.UTC(),
	}
	if row.ClaimedBy.Valid {
		user, err := s.q.GetUserByID(ctx, row.ClaimedBy.String)
		switch {
		case err == nil:
			email := user.Email
			out.ClaimedBy = &email
		case errors.Is(err, sql.ErrNoRows):
		default:
			return SetupLink{}, fmt.Errorf("get user: %w", err)
		}
	}
	if row.OrgID.Valid {
		org, err := s.q.GetOrganizationByID(ctx, row.OrgID.String)
		switch {
		case err == nil:
			out.Workspace = &SetupLinkWorkspace{Slug: org.Slug, Name: org.Name}
		case errors.Is(err, sql.ErrNoRows):
		default:
			return SetupLink{}, fmt.Errorf("get organization: %w", err)
		}
	}
	if row.UsedAt.Valid {
		used := row.UsedAt.Time.UTC()
		out.UsedAt = &used
	}
	return out, nil
}

// setupLinkStatus is computed, in order: used, expired, claimed, unused.
func setupLinkStatus(row db.SetupLink, now time.Time) string {
	switch {
	case row.UsedAt.Valid:
		return setupLinkUsed
	case !row.ExpiresAt.After(now):
		return setupLinkExpired
	case row.ClaimedBy.Valid:
		return setupLinkClaimed
	default:
		return setupLinkUnused
	}
}

// findSetupLink looks a token up by its hash. sql.ErrNoRows means it is
// unknown or revoked.
func (s *Service) findSetupLink(ctx context.Context, token string) (db.SetupLink, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return db.SetupLink{}, sql.ErrNoRows
	}
	row, err := s.q.GetSetupLinkByTokenHash(ctx, hashSetupToken(token))
	if errors.Is(err, sql.ErrNoRows) {
		return db.SetupLink{}, sql.ErrNoRows
	}
	if err != nil {
		return db.SetupLink{}, fmt.Errorf("get setup link: %w", err)
	}
	return row, nil
}

// claimSetupLink claims an unclaimed, unused, unexpired link for a new
// account. No row means the claim failed.
func (s *Service) claimSetupLink(ctx context.Context, token, userID string, now time.Time) (db.SetupLink, error) {
	return s.q.ClaimSetupLink(ctx, db.ClaimSetupLinkParams{
		UserID:    sql.NullString{String: userID, Valid: true},
		ClaimedAt: sql.NullTime{Time: now, Valid: true},
		TokenHash: hashSetupToken(token),
		Now:       now,
	})
}

// setupLinkInvalid is the 400 for a token that cannot be used.
func setupLinkInvalid() error { return invalid(setupLinkInvalidMessage) }

// setupLinkNotFound is the 404 for the public page.
func setupLinkNotFound() error { return notFound(setupLinkInvalidMessage) }

// hashSetupToken is the hex SHA-256 stored for a token.
func hashSetupToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newSetupToken returns a token and its storage hash. Only the hash is kept.
func newSetupToken() (token, hash string, err error) {
	b := make([]byte, setupLinkChars)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("read setup token: %w", err)
	}
	for i := range b {
		b[i] = setupLinkAlphabet[int(b[i])%len(setupLinkAlphabet)]
	}
	token = setupLinkPrefix + string(b)
	return token, hashSetupToken(token), nil
}
