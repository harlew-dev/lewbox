package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/harlew-dev/lewbox/internal/db"
)

// AccessToken is an account's long-lived token for the Lewbox API (D38). It
// acts as that account, with the account's current rights: an instance
// admin's token can manage clusters and nodes. The token itself is shown
// once, by CreateAccessToken; afterwards its prefix and last four characters
// identify it.
type AccessToken struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Last4      string     `json:"last4"`
	Status     string     `json:"status"` // active | expired | revoked
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"` // empty: never
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// NewAccessToken is returned by CreateAccessToken: the token's record and
// the token, which is never shown again.
type NewAccessToken struct {
	AccessToken
	Token string `json:"token"`
}

// AccessTokenList is returned by ListAccessTokens, newest first. Data is
// never null.
type AccessTokenList struct {
	Data []AccessToken `json:"data"`
}

// CreateAccessTokenRequest is the input to CreateAccessToken. User names the
// account (ID or email); empty means the caller's own. Only instance admins
// can name another account, and local mode, which has no account, must name
// one. ExpiresIn is a duration such as "90d" or "720h"; empty means the token
// never expires.
type CreateAccessTokenRequest struct {
	Name      string `json:"name"`
	ExpiresIn string `json:"expires_in"`
	User      string `json:"user,omitempty"`
}

const accessTokenPrefix = "lbp_"

// tokenOwner resolves the account whose access tokens a call manages. An
// empty ref is the caller's own account. A plain user can only name
// themselves, and is told so before the lookup, so the answer never reveals
// whether another email has an account.
func (s *Service) tokenOwner(ctx context.Context, p Principal, ref string) (db.User, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		if p.Kind != "user" {
			return db.User{}, invalid("local mode has no account of its own: name one with --user")
		}
		row, err := s.q.GetUserByID(ctx, p.UserID)
		if errors.Is(err, sql.ErrNoRows) {
			return db.User{}, unauthorized("session expired or invalid: lewbox login")
		}
		if err != nil {
			return db.User{}, fmt.Errorf("get user: %w", err)
		}
		return row, nil
	}
	if !p.InstanceAdmin && !p.selfRef(ref) {
		return db.User{}, forbidden("only an instance admin can manage another account's access tokens")
	}
	return s.findUser(ctx, ref)
}

// CreateAccessToken creates an access token and returns it, once. A token
// cannot create another: a leaked token must not be able to outlive its own
// expiry or revocation.
func (s *Service) CreateAccessToken(ctx context.Context, req CreateAccessTokenRequest) (NewAccessToken, error) {
	p, err := requireUser(ctx)
	if err != nil {
		return NewAccessToken{}, err
	}
	if p.TokenID != "" {
		return NewAccessToken{}, forbidden("an access token cannot create another: sign in with a password (lewbox login)")
	}
	owner, err := s.tokenOwner(ctx, p, req.User)
	if err != nil {
		return NewAccessToken{}, err
	}
	if owner.Disabled != 0 {
		return NewAccessToken{}, conflict("account %q is disabled", owner.Email)
	}
	name, err := keyName(req.Name)
	if err != nil {
		return NewAccessToken{}, err
	}
	now := s.now()
	var expires sql.NullTime
	if strings.TrimSpace(req.ExpiresIn) != "" {
		d, err := parseLifetime(req.ExpiresIn)
		if err != nil {
			return NewAccessToken{}, err
		}
		expires = sql.NullTime{Time: now.Add(d), Valid: true}
	}
	token, digest, err := newKeyToken(accessTokenPrefix)
	if err != nil {
		return NewAccessToken{}, err
	}
	row, err := s.q.CreateAccessToken(ctx, db.CreateAccessTokenParams{
		ID:        uuid.NewV7().String(),
		UserID:    owner.ID,
		Name:      name,
		Prefix:    token[:len(accessTokenPrefix)+8],
		TokenHash: digest,
		Last4:     token[len(token)-4:],
		CreatedAt: now,
		ExpiresAt: expires,
	})
	if db.IsUniqueViolation(err) {
		return NewAccessToken{}, conflict("%s already has an access token named %q", owner.Email, name)
	}
	if err != nil {
		return NewAccessToken{}, fmt.Errorf("create access token: %w", err)
	}
	return NewAccessToken{AccessToken: toAccessToken(row, now), Token: token}, nil
}

// ListAccessTokens returns an account's access tokens, revoked ones
// included, newest first. user is as for CreateAccessTokenRequest.User.
func (s *Service) ListAccessTokens(ctx context.Context, user string) (AccessTokenList, error) {
	p, err := requireUser(ctx)
	if err != nil {
		return AccessTokenList{}, err
	}
	owner, err := s.tokenOwner(ctx, p, user)
	if err != nil {
		return AccessTokenList{}, err
	}
	rows, err := s.q.ListAccessTokens(ctx, owner.ID)
	if err != nil {
		return AccessTokenList{}, fmt.Errorf("list access tokens: %w", err)
	}
	now := s.now()
	out := AccessTokenList{Data: make([]AccessToken, 0, len(rows))}
	for _, r := range rows {
		out.Data = append(out.Data, toAccessToken(r, now))
	}
	return out, nil
}

// RevokeAccessToken revokes an account's token by ID or by the name of a
// live token. It stops working at once. A token may revoke itself.
func (s *Service) RevokeAccessToken(ctx context.Context, user, ref string) (AccessToken, error) {
	p, err := requireUser(ctx)
	if err != nil {
		return AccessToken{}, err
	}
	owner, err := s.tokenOwner(ctx, p, user)
	if err != nil {
		return AccessToken{}, err
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return AccessToken{}, invalid("an access token ID or name is required")
	}
	var row db.AccessToken
	if id, perr := uuid.Parse(ref); perr == nil {
		row, err = s.q.GetAccessTokenByID(ctx, db.GetAccessTokenByIDParams{UserID: owner.ID, ID: id.String()})
	} else {
		row, err = s.q.GetLiveAccessTokenByName(ctx, db.GetLiveAccessTokenByNameParams{UserID: owner.ID, Name: ref})
	}
	if errors.Is(err, sql.ErrNoRows) {
		return AccessToken{}, notFound("access token %q not found", ref)
	}
	if err != nil {
		return AccessToken{}, fmt.Errorf("get access token: %w", err)
	}
	if row.RevokedAt.Valid {
		return AccessToken{}, conflict("access token %q is already revoked", ref)
	}
	now := s.now()
	n, err := s.q.RevokeAccessToken(ctx, db.RevokeAccessTokenParams{
		RevokedAt: sql.NullTime{Time: now, Valid: true},
		UserID:    owner.ID,
		ID:        row.ID,
	})
	if err != nil {
		return AccessToken{}, fmt.Errorf("revoke access token: %w", err)
	}
	if n == 0 {
		return AccessToken{}, conflict("access token %q is already revoked", ref)
	}
	row.RevokedAt = sql.NullTime{Time: now, Valid: true}
	return toAccessToken(row, now), nil
}

// authenticateAccessToken resolves an lbp_ token to its account's principal.
// Unknown, revoked and expired tokens, and tokens of disabled accounts, look
// the same. The account's admin flag is read now, so a token loses admin
// rights with its account.
func (s *Service) authenticateAccessToken(ctx context.Context, token string) (Principal, error) {
	rejected := unauthorized("access token is invalid, expired or revoked")
	if !validKeyToken(accessTokenPrefix, token) {
		return Principal{}, rejected
	}
	row, err := s.q.GetAccessTokenByHash(ctx, tokenDigest(token))
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, rejected
	}
	if err != nil {
		return Principal{}, fmt.Errorf("get access token: %w", err)
	}
	now := s.now()
	if row.RevokedAt.Valid || (row.ExpiresAt.Valid && !now.Before(row.ExpiresAt.Time)) {
		return Principal{}, rejected
	}
	user, err := s.q.GetUserByID(ctx, row.UserID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && user.Disabled != 0) {
		return Principal{}, rejected
	}
	if err != nil {
		return Principal{}, fmt.Errorf("get user: %w", err)
	}
	// Like sessions, last_used_at is written at most once a minute.
	if !row.LastUsedAt.Valid || now.Sub(row.LastUsedAt.Time) >= time.Minute {
		if err := s.q.TouchAccessToken(ctx, db.TouchAccessTokenParams{
			LastUsedAt: sql.NullTime{Time: now, Valid: true},
			ID:         row.ID,
		}); err != nil {
			return Principal{}, fmt.Errorf("touch access token: %w", err)
		}
	}
	return Principal{
		Kind:          "user",
		UserID:        user.ID,
		Email:         user.Email,
		InstanceAdmin: user.InstanceAdmin != 0,
		TokenID:       row.ID,
	}, nil
}

func toAccessToken(r db.AccessToken, now time.Time) AccessToken {
	t := AccessToken{
		ID:         r.ID,
		UserID:     r.UserID,
		Name:       r.Name,
		Prefix:     r.Prefix,
		Last4:      r.Last4,
		Status:     keyStatusActive,
		CreatedAt:  r.CreatedAt.UTC(),
		LastUsedAt: nullTime(r.LastUsedAt),
		ExpiresAt:  nullTime(r.ExpiresAt),
		RevokedAt:  nullTime(r.RevokedAt),
	}
	switch {
	case r.RevokedAt.Valid:
		t.Status = keyStatusRevoked
	case r.ExpiresAt.Valid && !now.Before(r.ExpiresAt.Time):
		t.Status = keyStatusExpired
	}
	return t
}
