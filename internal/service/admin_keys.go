package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
	"uuid"

	"github.com/harlew-dev/lewbox/internal/db"
)

// AdminKey is an organization's key for the Lewbox API (MVP §2). The token
// itself is shown once, by CreateAdminKey; afterwards only its prefix and
// last four characters identify it.
type AdminKey struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organization_id"`
	Name           string     `json:"name"`
	Prefix         string     `json:"prefix"`
	Last4          string     `json:"last4"`
	Status         string     `json:"status"`               // active | expired | revoked
	CreatedBy      string     `json:"created_by,omitempty"` // the creator's email; empty in local mode
	CreatedAt      time.Time  `json:"created_at"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"` // empty: never
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
}

// NewAdminKey is returned by CreateAdminKey: the key and its token, which is
// never shown again.
type NewAdminKey struct {
	AdminKey
	Token string `json:"token"`
}

// AdminKeyList is returned by ListAdminKeys, newest first. Data is never
// null.
type AdminKeyList struct {
	Data []AdminKey `json:"data"`
}

// CreateAdminKeyRequest is the input to CreateAdminKey. ExpiresIn is a
// duration such as "90d" or "720h"; empty means the key never expires.
type CreateAdminKeyRequest struct {
	Name      string `json:"name"`
	ExpiresIn string `json:"expires_in"`
}

const (
	adminKeyPrefix   = "lba_"
	adminKeyBytes    = 29 // 58 hex characters
	adminKeyChecksum = 6  // base62 characters
	adminKeyLength   = len(adminKeyPrefix) + adminKeyBytes*2 + adminKeyChecksum

	minKeyLifetime = time.Hour
	maxKeyLifetime = 10 * 365 * 24 * time.Hour

	keyStatusActive  = "active"
	keyStatusExpired = "expired"
	keyStatusRevoked = "revoked"
)

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// keyChecksum is the CRC-32 of body in six base62 characters, so a secret
// scanner (or Authenticate) can reject a mistyped or made-up token without a
// database lookup.
func keyChecksum(body string) string {
	n := crc32.ChecksumIEEE([]byte(body))
	out := make([]byte, adminKeyChecksum)
	for i := adminKeyChecksum - 1; i >= 0; i-- {
		out[i] = base62[n%62]
		n /= 62
	}
	return string(out)
}

// newAdminKeyToken returns a token, lba_<58 hex><6 base62 checksum> (MVP §2),
// and the SHA-256 hex digest that is stored.
func newAdminKeyToken() (token, digest string, err error) {
	b := make([]byte, adminKeyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("read token: %w", err)
	}
	body := adminKeyPrefix + hex.EncodeToString(b)
	token = body + keyChecksum(body)
	return token, tokenDigest(token), nil
}

// validAdminKeyToken reports whether token has the shape newAdminKeyToken
// produces and a correct checksum.
func validAdminKeyToken(token string) bool {
	if len(token) != adminKeyLength || !strings.HasPrefix(token, adminKeyPrefix) {
		return false
	}
	body := token[:len(token)-adminKeyChecksum]
	for _, c := range body[len(adminKeyPrefix):] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return token[len(body):] == keyChecksum(body)
}

// parseLifetime reads "90d", "12h" or any Go duration.
func parseLifetime(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	var d time.Duration
	if days, ok := strings.CutSuffix(raw, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, invalid("expires_in must be a duration such as 90d or 720h")
		}
		d = time.Duration(n) * 24 * time.Hour
	} else {
		var err error
		if d, err = time.ParseDuration(raw); err != nil {
			return 0, invalid("expires_in must be a duration such as 90d or 720h")
		}
	}
	if d < minKeyLifetime || d > maxKeyLifetime {
		return 0, invalid("expires_in must be between 1h and 3650d")
	}
	return d, nil
}

// keyName trims and checks a key's name. It is looked up like an ID, so it
// must never parse as a UUID.
func keyName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", invalid("name is required")
	}
	if utf8.RuneCountInString(name) > 64 {
		return "", invalid("name must be at most 64 characters")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", invalid("name must not contain control characters")
		}
	}
	if _, err := uuid.Parse(name); err == nil {
		return "", invalid("name must not be a UUID")
	}
	return name, nil
}

// CreateAdminKey creates a key for the organization and returns its token,
// once. Owners, admins and instance admins can create keys; an admin key
// cannot create another.
func (s *Service) CreateAdminKey(ctx context.Context, org string, req CreateAdminKeyRequest) (NewAdminKey, error) {
	c, err := s.orgFor(ctx, org, accessManage)
	if err != nil {
		return NewAdminKey{}, err
	}
	name, err := keyName(req.Name)
	if err != nil {
		return NewAdminKey{}, err
	}
	now := s.now()
	var expires sql.NullTime
	if strings.TrimSpace(req.ExpiresIn) != "" {
		d, err := parseLifetime(req.ExpiresIn)
		if err != nil {
			return NewAdminKey{}, err
		}
		expires = sql.NullTime{Time: now.Add(d), Valid: true}
	}
	token, digest, err := newAdminKeyToken()
	if err != nil {
		return NewAdminKey{}, err
	}
	var createdBy sql.NullString
	if c.p.Kind == "user" {
		createdBy = sql.NullString{String: c.p.UserID, Valid: true}
	}
	row, err := s.q.CreateAdminKey(ctx, db.CreateAdminKeyParams{
		ID:        uuid.NewV7().String(),
		OrgID:     c.org.ID,
		Name:      name,
		Prefix:    token[:len(adminKeyPrefix)+8],
		KeyHash:   digest,
		Last4:     token[len(token)-4:],
		CreatedBy: createdBy,
		CreatedAt: now,
		ExpiresAt: expires,
	})
	if db.IsUniqueViolation(err) {
		return NewAdminKey{}, conflict("%q already has a key named %q", c.org.Slug, name)
	}
	if err != nil {
		return NewAdminKey{}, fmt.Errorf("create admin key: %w", err)
	}
	return NewAdminKey{AdminKey: toAdminKey(row, c.p.Email, now), Token: token}, nil
}

// ListAdminKeys returns the organization's keys, revoked ones included,
// newest first.
func (s *Service) ListAdminKeys(ctx context.Context, org string) (AdminKeyList, error) {
	c, err := s.orgFor(ctx, org, accessKeys)
	if err != nil {
		return AdminKeyList{}, err
	}
	rows, err := s.q.ListAdminKeys(ctx, c.org.ID)
	if err != nil {
		return AdminKeyList{}, fmt.Errorf("list admin keys: %w", err)
	}
	now := s.now()
	out := AdminKeyList{Data: make([]AdminKey, 0, len(rows))}
	for _, r := range rows {
		out.Data = append(out.Data, toAdminKey(r.AdminKey, r.CreatedByEmail.String, now))
	}
	return out, nil
}

// RevokeAdminKey revokes a key by ID or by the name of a live key. It stops
// working at once.
func (s *Service) RevokeAdminKey(ctx context.Context, org, ref string) (AdminKey, error) {
	c, err := s.orgFor(ctx, org, accessManage)
	if err != nil {
		return AdminKey{}, err
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return AdminKey{}, invalid("a key ID or name is required")
	}
	var (
		row   db.AdminKey
		email sql.NullString
	)
	if id, perr := uuid.Parse(ref); perr == nil {
		r, err := s.q.GetAdminKeyByID(ctx, db.GetAdminKeyByIDParams{OrgID: c.org.ID, ID: id.String()})
		row, email = r.AdminKey, r.CreatedByEmail
		err = keyLookupErr(err, ref)
		if err != nil {
			return AdminKey{}, err
		}
	} else {
		r, err := s.q.GetLiveAdminKeyByName(ctx, db.GetLiveAdminKeyByNameParams{OrgID: c.org.ID, Name: ref})
		row, email = r.AdminKey, r.CreatedByEmail
		err = keyLookupErr(err, ref)
		if err != nil {
			return AdminKey{}, err
		}
	}
	if row.RevokedAt.Valid {
		return AdminKey{}, conflict("key %q is already revoked", ref)
	}
	now := s.now()
	n, err := s.q.RevokeAdminKey(ctx, db.RevokeAdminKeyParams{
		RevokedAt: sql.NullTime{Time: now, Valid: true},
		OrgID:     c.org.ID,
		ID:        row.ID,
	})
	if err != nil {
		return AdminKey{}, fmt.Errorf("revoke admin key: %w", err)
	}
	if n == 0 {
		return AdminKey{}, conflict("key %q is already revoked", ref)
	}
	row.RevokedAt = sql.NullTime{Time: now, Valid: true}
	return toAdminKey(row, email.String, now), nil
}

func keyLookupErr(err error, ref string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return notFound("admin key %q not found", ref)
	}
	if err != nil {
		return fmt.Errorf("get admin key: %w", err)
	}
	return nil
}

// authenticateAdminKey resolves an lba_ token to a key principal. Unknown,
// revoked and expired keys look the same.
func (s *Service) authenticateAdminKey(ctx context.Context, token string) (Principal, error) {
	rejected := unauthorized("admin key is invalid, expired or revoked")
	if !validAdminKeyToken(token) {
		return Principal{}, rejected
	}
	row, err := s.q.GetAdminKeyByHash(ctx, tokenDigest(token))
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, rejected
	}
	if err != nil {
		return Principal{}, fmt.Errorf("get admin key: %w", err)
	}
	now := s.now()
	if row.RevokedAt.Valid || (row.ExpiresAt.Valid && !now.Before(row.ExpiresAt.Time)) {
		return Principal{}, rejected
	}
	org, err := s.q.GetOrganizationByID(ctx, row.OrgID)
	if err != nil {
		return Principal{}, fmt.Errorf("get organization: %w", err)
	}
	if org.Status != orgActive {
		return Principal{}, forbidden("organization %q is %s", org.Slug, org.Status)
	}
	// Like sessions, last_used_at is written at most once a minute.
	if !row.LastUsedAt.Valid || now.Sub(row.LastUsedAt.Time) >= time.Minute {
		if err := s.q.TouchAdminKey(ctx, db.TouchAdminKeyParams{
			LastUsedAt: sql.NullTime{Time: now, Valid: true},
			ID:         row.ID,
		}); err != nil {
			return Principal{}, fmt.Errorf("touch admin key: %w", err)
		}
	}
	return Principal{Kind: "key", KeyID: row.ID, OrgID: row.OrgID}, nil
}

func toAdminKey(r db.AdminKey, createdBy string, now time.Time) AdminKey {
	k := AdminKey{
		ID:             r.ID,
		OrganizationID: r.OrgID,
		Name:           r.Name,
		Prefix:         r.Prefix,
		Last4:          r.Last4,
		Status:         keyStatusActive,
		CreatedBy:      createdBy,
		CreatedAt:      r.CreatedAt.UTC(),
		LastUsedAt:     nullTime(r.LastUsedAt),
		ExpiresAt:      nullTime(r.ExpiresAt),
		RevokedAt:      nullTime(r.RevokedAt),
	}
	switch {
	case r.RevokedAt.Valid:
		k.Status = keyStatusRevoked
	case r.ExpiresAt.Valid && !now.Before(r.ExpiresAt.Time):
		k.Status = keyStatusExpired
	}
	return k
}

func nullTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	u := t.Time.UTC()
	return &u
}
