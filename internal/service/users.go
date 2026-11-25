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

// User is an account. It never carries the password hash.
type User struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	InstanceAdmin bool      `json:"instance_admin"`
	Disabled      bool      `json:"disabled"`
	CreatedAt     time.Time `json:"created_at"`
}

// UserList is returned by ListUsers. Data is never null.
type UserList struct {
	Data  []User `json:"data"`
	Pages Pages  `json:"pages"`
}

// ListUsersRequest pages and filters accounts. Status is active or disabled;
// Role is admin or user.
type ListUsersRequest struct {
	ListQuery
	Status string `json:"status"`
	Role   string `json:"role"`
}

// userSorts maps a public sort key to SQL written here. status sorts active
// accounts before disabled ones.
var userSorts = mergeSorts(
	sortDirections("email", "lower(email) ASC, id ASC", "lower(email) DESC, id DESC"),
	sortDirections("created", "created_at ASC, id ASC", "created_at DESC, id DESC"),
	sortDirections("status", "disabled ASC, id ASC", "disabled DESC, id DESC"),
)

// CreateUserRequest is the input to CreateUser.
type CreateUserRequest struct {
	Email         string `json:"email"`
	Password      string `json:"password"`
	InstanceAdmin bool   `json:"instance_admin"`
}

// SetPasswordRequest is the input to SetPassword.
// CurrentPassword is the caller's own password: fresh proof that whoever
// holds the session or access token also knows it. Local mode needs none.
type SetPasswordRequest struct {
	Password        string `json:"password"`
	CurrentPassword string `json:"current_password,omitempty"`
}

// LoginRequest is the input to Login.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginResponse is returned by Login. Token is shown only once.
type LoginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      User      `json:"user"`
}

// SessionResponse is returned by WebLogin. The token itself is never in it.
type SessionResponse struct {
	ExpiresAt time.Time `json:"expires_at"`
	User      User      `json:"user"`
	// CSRFToken is set by the HTTP layer for browser sessions: the console
	// sends it back as X-CSRF-Token on every write.
	CSRFToken string `json:"csrf_token,omitempty"`
}

// LogoutResponse is returned by Logout.
type LogoutResponse struct {
	OK bool `json:"ok"`
}

// Me is returned by Me. User is nil for the local principal and for an
// admin key, which has Organization and AdminKey instead. A caller using an
// access token has User and AccessToken. Organizations is
// set for user principals so a client can pick the portal and the current
// organization without another call.
type Me struct {
	User          *User            `json:"user,omitempty"`
	Organization  *Organization    `json:"organization,omitempty"`
	AdminKey      *AdminKey        `json:"admin_key,omitempty"`
	AccessToken   *AccessToken     `json:"access_token,omitempty"`
	Organizations []MeOrganization `json:"organizations,omitzero"`
	Via           string           `json:"via"` // "session", "access_token", "local" or "admin_key"
	// CSRFToken is set by the HTTP layer only for a browser session cookie,
	// so a reloaded console gets its token back. Never set for bearer tokens.
	CSRFToken string `json:"csrf_token,omitempty"`
}

// MeOrganization is one of a user's organizations, as Me lists them.
type MeOrganization struct {
	ID     string `json:"id"`
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

// CreateUser creates an account. Only instance admins can do this.
func (s *Service) CreateUser(ctx context.Context, req CreateUserRequest) (User, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return User{}, err
	}
	nu, err := prepareUser(req)
	if err != nil {
		return User{}, err
	}
	var out User
	err = s.withTx(ctx, func(ctx context.Context) error {
		user, err := s.insertUser(ctx, nu)
		if err != nil {
			return err
		}
		if err := s.record(ctx, Event{
			Name:       "user.created",
			TargetType: "user",
			TargetID:   user.ID,
			TargetName: user.Email,
		}); err != nil {
			return err
		}
		out = user
		return nil
	})
	return out, err
}

// createUser does the work of CreateUser without a permission check, so
// BootstrapAdmin can call it before any account exists.
func (s *Service) createUser(ctx context.Context, req CreateUserRequest) (User, error) {
	nu, err := prepareUser(req)
	if err != nil {
		return User{}, err
	}
	return s.insertUser(ctx, nu)
}

// newUser is a validated account with its password already hashed.
type newUser struct {
	email         string
	hash          string
	instanceAdmin bool
}

// prepareUser validates an account and hashes its password. The hash is slow
// on purpose, so callers run it before opening a transaction: SQLite holds
// its one write lock for a transaction's whole life.
func prepareUser(req CreateUserRequest) (newUser, error) {
	email, err := normalizeEmail(req.Email)
	if err != nil {
		return newUser{}, err
	}
	if err := validatePassword(req.Password); err != nil {
		return newUser{}, err
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		return newUser{}, err
	}
	return newUser{email: email, hash: hash, instanceAdmin: req.InstanceAdmin}, nil
}

// insertUser stores a prepared account.
func (s *Service) insertUser(ctx context.Context, nu newUser) (User, error) {
	row, err := s.queries(ctx).CreateUser(ctx, db.CreateUserParams{
		ID:            uuid.NewV7().String(),
		Email:         nu.email,
		PasswordHash:  nu.hash,
		InstanceAdmin: boolInt(nu.instanceAdmin),
		Disabled:      0,
		CreatedAt:     s.now(),
	})
	if db.IsUniqueViolation(err) {
		return User{}, conflict("a user with email %q already exists", nu.email)
	}
	if err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return toUser(row), nil
}

// ListUsers returns accounts, ordered by email by default.
func (s *Service) ListUsers(ctx context.Context, req ListUsersRequest) (UserList, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return UserList{}, err
	}
	q, err := parseListQuery(req.ListQuery, userSorts, "email")
	if err != nil {
		return UserList{}, err
	}
	statuses, err := parseList("status", req.Status, []string{"active", "disabled"})
	if err != nil {
		return UserList{}, err
	}
	roles, err := parseList("role", req.Role, []string{"admin", "user"})
	if err != nil {
		return UserList{}, err
	}
	disabled := make([]int64, 0, len(statuses))
	for _, status := range statuses {
		if status == "disabled" {
			disabled = append(disabled, 1)
		} else {
			disabled = append(disabled, 0)
		}
	}
	instanceAdmins := make([]int64, 0, len(roles))
	for _, role := range roles {
		if role == "admin" {
			instanceAdmins = append(instanceAdmins, 1)
		} else {
			instanceAdmins = append(instanceAdmins, 0)
		}
	}
	rows, total, err := s.queries(ctx).ListUsersPage(ctx, db.UserListParams{
		Disabled:      disabled,
		InstanceAdmin: instanceAdmins,
		Pattern:       q.pattern,
		Order:         q.sort,
		Limit:         q.perPage,
		Offset:        (q.page - 1) * q.perPage,
	})
	if err != nil {
		return UserList{}, fmt.Errorf("list users: %w", err)
	}
	out := UserList{
		Data:  make([]User, 0, len(rows)),
		Pages: newPages(q.page, q.perPage, total),
	}
	for _, r := range rows {
		out.Data = append(out.Data, toUser(r))
	}
	return out, nil
}

// GetUser returns an account by ID or email. Admins can read anyone; a plain
// user can read only their own account.
func (s *Service) GetUser(ctx context.Context, ref string) (User, error) {
	p, err := requireUser(ctx)
	if err != nil {
		return User{}, err
	}
	// Check permission before the lookup, so an unknown account is a 403 too.
	ref = strings.TrimSpace(ref)
	if !p.InstanceAdmin && !p.selfRef(ref) {
		return User{}, forbidden("only an instance admin can read another account")
	}
	row, err := s.findUser(ctx, ref)
	if err != nil {
		return User{}, err
	}
	return toUser(row), nil
}

// RemoveUser deletes an account by ID or email and returns what was removed.
// The last enabled instance admin cannot be removed.
func (s *Service) RemoveUser(ctx context.Context, ref string) (User, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return User{}, err
	}
	row, err := s.findUser(ctx, ref)
	if err != nil {
		return User{}, err
	}
	if row.InstanceAdmin != 0 && row.Disabled == 0 {
		n, err := s.queries(ctx).CountEnabledInstanceAdmins(ctx)
		if err != nil {
			return User{}, fmt.Errorf("count admins: %w", err)
		}
		if n <= 1 {
			return User{}, conflict("cannot remove the last enabled instance admin")
		}
	}
	if err := s.revokeCreatedSandboxKeys(ctx, "", row.ID); err != nil {
		return User{}, err
	}
	// The statement re-checks the last-admin rule itself, so a concurrent
	// remove or disable can't slip past the count above.
	var n int64
	err = s.withTx(ctx, func(ctx context.Context) error {
		n, err = s.queries(ctx).DeleteUser(ctx, row.ID)
		if err != nil {
			return fmt.Errorf("delete user: %w", err)
		}
		if n == 0 {
			if _, err := s.queries(ctx).GetUserByID(ctx, row.ID); err == nil {
				return conflict("cannot remove the last enabled instance admin")
			}
			return notFound("user %q not found", ref) // removed in between
		}
		return s.record(ctx, Event{
			Name:       "user.deleted",
			TargetType: "user",
			TargetID:   row.ID,
			TargetName: row.Email,
		})
	})
	if err != nil {
		return User{}, err
	}
	return toUser(row), nil
}

// SetUserDisabled blocks or allows sign-in. Disabling also ends every
// session and revokes every access token, so enabling the account again
// does not bring them back. The last enabled instance admin cannot be disabled.
func (s *Service) SetUserDisabled(ctx context.Context, ref string, disabled bool) (User, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return User{}, err
	}
	row, err := s.findUser(ctx, ref)
	if err != nil {
		return User{}, err
	}
	if disabled && row.InstanceAdmin != 0 && row.Disabled == 0 {
		n, err := s.queries(ctx).CountEnabledInstanceAdmins(ctx)
		if err != nil {
			return User{}, fmt.Errorf("count admins: %w", err)
		}
		if n <= 1 {
			return User{}, conflict("cannot disable the last enabled instance admin")
		}
	}
	if disabled {
		if err := s.revokeCreatedSandboxKeys(ctx, "", row.ID); err != nil {
			return User{}, err
		}
	}
	var updated db.User
	err = s.withTx(ctx, func(ctx context.Context) error {
		var err error
		updated, err = s.queries(ctx).SetUserDisabled(ctx, db.SetUserDisabledParams{
			Disabled: boolInt(disabled),
			ID:       row.ID,
		})
		if errors.Is(err, sql.ErrNoRows) {
			// No row matched: either it was removed in between, or the
			// statement's own last-admin guard held (a concurrent disable got
			// there first).
			if _, gerr := s.queries(ctx).GetUserByID(ctx, row.ID); gerr == nil {
				return conflict("cannot disable the last enabled instance admin")
			}
			return notFound("user %q not found", ref)
		}
		if err != nil {
			return fmt.Errorf("set user disabled: %w", err)
		}
		if disabled {
			if err := s.queries(ctx).DeleteSessionsByUser(ctx, row.ID); err != nil {
				return fmt.Errorf("delete sessions: %w", err)
			}
			if err := s.queries(ctx).RevokeAccessTokensByUser(ctx, db.RevokeAccessTokensByUserParams{
				RevokedAt: sql.NullTime{Time: s.now(), Valid: true},
				UserID:    row.ID,
			}); err != nil {
				return fmt.Errorf("revoke access tokens: %w", err)
			}
		}
		name := "user.restored"
		if disabled {
			name = "user.suspended"
		}
		return s.record(ctx, Event{
			Name:       name,
			TargetType: "user",
			TargetID:   row.ID,
			TargetName: row.Email,
			Changes:    map[string]Change{"disabled": {From: !disabled, To: disabled}},
		})
	})
	if err != nil {
		return User{}, err
	}
	return toUser(updated), nil
}

// SetPassword changes an account's password. Admins can change anyone's; a
// plain user can change only their own. The caller's session survives when it
// is the account's own session.
func (s *Service) SetPassword(ctx context.Context, ref string, req SetPasswordRequest) (User, error) {
	p, err := requireUser(ctx)
	if err != nil {
		return User{}, err
	}
	// Refuse before the lookup, so the answer never reveals whether another
	// email has an account.
	if !p.InstanceAdmin && !p.selfRef(strings.TrimSpace(ref)) {
		return User{}, forbidden("only an instance admin can change another account's password")
	}
	row, err := s.findUser(ctx, ref)
	if err != nil {
		return User{}, err
	}
	if !p.InstanceAdmin && p.UserID != row.ID {
		return User{}, forbidden("only an instance admin can change another account's password")
	}
	if err := validatePassword(req.Password); err != nil {
		return User{}, err
	}
	if err := s.requireFreshProof(ctx, p, req.CurrentPassword); err != nil {
		return User{}, err
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		return User{}, err
	}
	err = s.withTx(ctx, func(ctx context.Context) error {
		if err := s.queries(ctx).SetUserPassword(ctx, db.SetUserPasswordParams{PasswordHash: hash, ID: row.ID}); err != nil {
			return fmt.Errorf("set password: %w", err)
		}
		if p.Kind == "user" && p.UserID == row.ID && p.SessionID != "" {
			err = s.queries(ctx).DeleteOtherSessionsByUser(ctx, db.DeleteOtherSessionsByUserParams{
				UserID: row.ID,
				ID:     p.SessionID,
			})
		} else {
			err = s.queries(ctx).DeleteSessionsByUser(ctx, row.ID)
		}
		if err != nil {
			return fmt.Errorf("delete sessions: %w", err)
		}
		return s.record(ctx, Event{
			Name:       "user.password_changed",
			TargetType: "user",
			TargetID:   row.ID,
			TargetName: row.Email,
		})
	})
	if err != nil {
		return User{}, err
	}
	return toUser(row), nil
}

// Login checks an email and password and starts a session.
func (s *Service) Login(ctx context.Context, req LoginRequest) (LoginResponse, error) {
	token, expires, user, err := s.signIn(ctx, req, "cli", sessionLifetime)
	if err != nil {
		return LoginResponse{}, err
	}
	return LoginResponse{Token: token, ExpiresAt: expires, User: toUser(user)}, nil
}

// WebLogin checks an email and password and starts a browser session. It
// returns the session token separately so the HTTP layer can put it in an
// HttpOnly cookie; the token must never appear in a JSON body.
func (s *Service) WebLogin(ctx context.Context, req LoginRequest) (token string, resp SessionResponse, err error) {
	token, expires, user, err := s.signIn(ctx, req, "web", webSessionLifetime)
	if err != nil {
		return "", SessionResponse{}, err
	}
	return token, SessionResponse{ExpiresAt: expires, User: toUser(user)}, nil
}

// signIn checks an email and password and creates a session of kind with the
// given lifetime. Login and WebLogin share it so the credential checks,
// messages, timing and rate limits cannot drift apart.
func (s *Service) signIn(ctx context.Context, req LoginRequest, kind string, lifetime time.Duration) (token string, expires time.Time, user db.User, err error) {
	email := strings.ToLower(strings.TrimSpace(req.Email))
	now := s.now()

	// Both budgets are checked before the password hash, so a flood costs
	// little CPU. The IP budget counts every attempt; the email budget counts
	// only failures (a success clears it).
	if retry, ok, first := s.limiter.allowIP(now, clientIPFrom(ctx)); !ok {
		if first {
			s.auditRateLimit(ctx, budgetAuthPerIP)
		}
		return "", time.Time{}, db.User{}, loginRateLimited(retry)
	}
	if retry, ok, first := s.limiter.allowEmail(now, email); !ok {
		if first {
			s.auditRateLimit(ctx, budgetAuthPerEmail)
		}
		return "", time.Time{}, db.User{}, loginRateLimited(retry)
	}

	row, err := s.queries(ctx).GetUserByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		// Burn the same work an existing account would, so timing does not
		// reveal which emails exist.
		_, _ = hashPassword(req.Password)
		// Every failed attempt is recorded, known account or not, so the
		// trail itself cannot reveal which emails exist.
		if err := s.recordLoginFailure(ctx, nil); err != nil {
			return "", time.Time{}, db.User{}, err
		}
		return "", time.Time{}, db.User{}, unauthorized("email or password is wrong")
	}
	if err != nil {
		return "", time.Time{}, db.User{}, fmt.Errorf("get user: %w", err)
	}
	ok, err := verifyPassword(row.PasswordHash, req.Password)
	if err != nil {
		return "", time.Time{}, db.User{}, fmt.Errorf("verify password: %w", err)
	}
	if !ok || row.Disabled != 0 {
		if err := s.recordLoginFailure(ctx, &row); err != nil {
			return "", time.Time{}, db.User{}, err
		}
		return "", time.Time{}, db.User{}, unauthorized("email or password is wrong")
	}
	s.rehashPassword(ctx, row, req.Password)

	token, expires, err = s.startSession(ctx, now, row.ID, kind, lifetime)
	if err != nil {
		return "", time.Time{}, db.User{}, err
	}
	s.limiter.clearEmail(email)
	return token, expires, row, nil
}

// rehashPassword stores a new hash for a password that has just been
// verified, when the stored one used fewer iterations than today's. It is
// best effort: a failure leaves the old hash, which still works.
func (s *Service) rehashPassword(ctx context.Context, row db.User, password string) {
	if !passwordNeedsRehash(row.PasswordHash) {
		return
	}
	hash, err := hashPassword(password)
	if err == nil {
		_, err = s.queries(ctx).RehashUserPassword(ctx, db.RehashUserPasswordParams{
			NewHash: hash,
			ID:      row.ID,
			OldHash: row.PasswordHash,
		})
	}
	if err != nil && s.log != nil {
		s.log.Warn("rehash password", "user_id", row.ID, "err", err)
	}
}

// requireFreshProof checks the caller's current password before a sensitive
// action (changing a password, creating an access token or an admin key), so
// a stolen session or access token alone cannot do it. Local mode owns the
// instance and needs no proof; an admin key has no password and is refused.
// Wrong guesses share the account's sign-in failure budget, so this is not a
// second way to guess the password.
func (s *Service) requireFreshProof(ctx context.Context, p Principal, password string) error {
	switch p.Kind {
	case "local":
		return nil
	case "user":
	default:
		return forbidden("this needs a signed-in account and its password")
	}
	if password == "" {
		return s.freshProofFailed(ctx, p, invalidField("current_password", "enter your current password to confirm"))
	}
	row, err := s.queries(ctx).GetUserByID(ctx, p.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return unauthorized("session expired or invalid: lewbox login")
	}
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if retry, ok, first := s.limiter.allowEmail(s.now(), row.Email); !ok {
		if first {
			s.auditRateLimit(ctx, budgetFreshProofPerEmail)
		}
		return loginRateLimited(retry)
	}
	ok, err := verifyPassword(row.PasswordHash, password)
	if err != nil {
		return fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		p.Email = row.Email
		return s.freshProofFailed(ctx, p, invalidField("current_password", "current password is wrong"))
	}
	return nil
}

// freshProofFailed records a wrong or missing fresh-proof password and returns
// cause. An attempt that cannot be audited does not proceed.
func (s *Service) freshProofFailed(ctx context.Context, p Principal, cause error) error {
	if err := s.record(ctx, Event{
		Name:       "user.fresh_proof_failed",
		TargetType: "user",
		TargetID:   p.UserID,
		TargetName: p.Email,
	}); err != nil {
		return err
	}
	return cause
}

// recordLoginFailure records a failed sign-in. row is nil when no account has
// the email that was typed, so the address is not stored.
func (s *Service) recordLoginFailure(ctx context.Context, row *db.User) error {
	e := Event{Name: "session.login_failed", TargetType: "user"}
	if row != nil {
		e.TargetID = row.ID
		e.TargetName = row.Email
	}
	return s.record(ctx, e)
}

// startSession creates a session of kind for an account and returns its token
// and expiry. The token itself is never stored.
func (s *Service) startSession(ctx context.Context, now time.Time, userID, kind string, lifetime time.Duration) (token string, expires time.Time, err error) {
	token, digest, err := newSessionToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires = now.Add(lifetime)
	if err := s.queries(ctx).CreateSession(ctx, db.CreateSessionParams{
		ID:         uuid.NewV7().String(),
		UserID:     userID,
		TokenHash:  digest,
		CreatedAt:  now,
		ExpiresAt:  expires,
		LastUsedAt: now,
		Kind:       kind,
	}); err != nil {
		return "", time.Time{}, fmt.Errorf("create session: %w", err)
	}
	return token, expires, nil
}

// Logout ends the caller's session.
func (s *Service) Logout(ctx context.Context) (LogoutResponse, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return LogoutResponse{}, err
	}
	if p.Kind == "key" {
		return LogoutResponse{}, badRequest("an admin key has no session: revoke it with lewbox admin-key revoke")
	}
	if p.TokenID != "" {
		return LogoutResponse{}, badRequest("an access token has no session: revoke it with lewbox token revoke")
	}
	if p.Kind != "user" {
		return LogoutResponse{}, badRequest("local mode has no session")
	}
	if p.SessionID != "" {
		if _, err := s.queries(ctx).DeleteSession(ctx, p.SessionID); err != nil {
			return LogoutResponse{}, fmt.Errorf("delete session: %w", err)
		}
	}
	return LogoutResponse{OK: true}, nil
}

// Me describes the caller.
func (s *Service) Me(ctx context.Context) (Me, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return Me{}, err
	}
	if p.Kind == "key" {
		return s.meForKey(ctx, p)
	}
	if p.Kind != "user" {
		return Me{Via: "local"}, nil
	}
	row, err := s.queries(ctx).GetUserByID(ctx, p.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return Me{}, unauthorized("session expired or invalid: lewbox login")
	}
	if err != nil {
		return Me{}, fmt.Errorf("get user: %w", err)
	}
	u := toUser(row)
	rows, err := s.queries(ctx).ListOrganizationsForUser(ctx, p.UserID)
	if err != nil {
		return Me{}, fmt.Errorf("list organizations: %w", err)
	}
	orgs := make([]MeOrganization, 0, len(rows))
	for _, r := range rows {
		orgs = append(orgs, MeOrganization{
			ID:     r.Organization.ID,
			Slug:   r.Organization.Slug,
			Name:   r.Organization.Name,
			Role:   r.Role,
			Status: r.Organization.Status,
		})
	}
	if p.TokenID != "" {
		row, err := s.queries(ctx).GetAccessTokenByID(ctx, db.GetAccessTokenByIDParams{UserID: p.UserID, ID: p.TokenID})
		if err != nil {
			return Me{}, fmt.Errorf("get access token: %w", err)
		}
		t := toAccessToken(row, s.now())
		return Me{User: &u, AccessToken: &t, Organizations: orgs, Via: "access_token"}, nil
	}
	return Me{User: &u, Organizations: orgs, Via: "session"}, nil
}

// Authenticate resolves a bearer token to a principal. The HTTP middleware
// calls it; expired and disabled accounts look the same as unknown tokens.
func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	if strings.HasPrefix(token, adminKeyPrefix) {
		return s.authenticateAdminKey(ctx, token)
	}
	if strings.HasPrefix(token, accessTokenPrefix) {
		return s.authenticateAccessToken(ctx, token)
	}
	if !validSessionToken(token) {
		return Principal{}, unauthorized("session expired or invalid: lewbox login")
	}
	row, err := s.queries(ctx).GetSessionByTokenHash(ctx, tokenDigest(token))
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, unauthorized("session expired or invalid: lewbox login")
	}
	if err != nil {
		return Principal{}, fmt.Errorf("get session: %w", err)
	}
	now := s.now()
	if !now.Before(row.ExpiresAt) {
		_, _ = s.queries(ctx).DeleteSession(ctx, row.ID)
		return Principal{}, unauthorized("session expired or invalid: lewbox login")
	}
	// Browser sessions also end after a quiet spell. last_used_at is only
	// written once a minute, which is well inside the idle timeout.
	if row.Kind == "web" && now.Sub(row.LastUsedAt) >= webSessionIdle {
		_, _ = s.queries(ctx).DeleteSession(ctx, row.ID)
		return Principal{}, unauthorized("session expired or invalid: lewbox login")
	}
	user, err := s.queries(ctx).GetUserByID(ctx, row.UserID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && user.Disabled != 0) {
		_, _ = s.queries(ctx).DeleteSession(ctx, row.ID)
		return Principal{}, unauthorized("session expired or invalid: lewbox login")
	}
	if err != nil {
		return Principal{}, fmt.Errorf("get user: %w", err)
	}
	// last_used_at is a coarse signal, so write it at most once a minute.
	if now.Sub(row.LastUsedAt) >= time.Minute {
		if err := s.queries(ctx).TouchSession(ctx, db.TouchSessionParams{LastUsedAt: now, ID: row.ID}); err != nil {
			return Principal{}, fmt.Errorf("touch session: %w", err)
		}
	}
	return Principal{
		Kind:          "user",
		UserID:        user.ID,
		Email:         user.Email,
		InstanceAdmin: user.InstanceAdmin != 0,
		SessionID:     row.ID,
	}, nil
}

// meForKey describes an admin key and its organization.
func (s *Service) meForKey(ctx context.Context, p Principal) (Me, error) {
	org, err := s.GetOrganization(ctx, p.OrgID)
	if err != nil {
		return Me{}, err
	}
	row, err := s.queries(ctx).GetAdminKeyByID(ctx, db.GetAdminKeyByIDParams{OrgID: p.OrgID, ID: p.KeyID})
	if err != nil {
		return Me{}, keyLookupErr(err, p.KeyID)
	}
	key := toAdminKey(row.AdminKey, row.CreatedByEmail.String, s.now())
	return Me{Organization: &org, AdminKey: &key, Via: "admin_key"}, nil
}

// BootstrapAdmin creates the first instance admin when the users table is
// empty. It reports whether an account was created.
func (s *Service) BootstrapAdmin(ctx context.Context, email, password string) (bool, error) {
	n, err := s.queries(ctx).CountUsers(ctx)
	if err != nil {
		return false, fmt.Errorf("count users: %w", err)
	}
	if n > 0 {
		return false, nil
	}
	ctx = WithPrincipal(ctx, LocalPrincipal())
	if _, err := s.createUser(ctx, CreateUserRequest{
		Email:         email,
		Password:      password,
		InstanceAdmin: true,
	}); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) findUser(ctx context.Context, ref string) (db.User, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return db.User{}, badRequest("a user ID or email is required")
	}

	var (
		row db.User
		err error
	)
	if id, perr := uuid.Parse(ref); perr == nil {
		row, err = s.queries(ctx).GetUserByID(ctx, id.String())
	} else {
		row, err = s.queries(ctx).GetUserByEmail(ctx, strings.ToLower(ref))
	}
	if errors.Is(err, sql.ErrNoRows) {
		return db.User{}, notFound("user %q not found", ref)
	}
	if err != nil {
		return db.User{}, fmt.Errorf("get user: %w", err)
	}
	return row, nil
}

// normalizeEmail trims, lowercases and validates an email address.
func normalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" {
		return "", invalidField("email", "email is required")
	}
	if len(email) > 254 {
		return "", invalidField("email", "email must be at most 254 characters")
	}
	at := strings.IndexByte(email, '@')
	if at <= 0 || at != strings.LastIndexByte(email, '@') || at == len(email)-1 {
		return "", invalidField("email", "email must contain one @ with text on both sides")
	}
	// Email and ID share one lookup argument, so an email must never parse
	// as a UUID.
	if _, err := uuid.Parse(email); err == nil {
		return "", invalidField("email", "email must not be a UUID")
	}
	return email, nil
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func toUser(r db.User) User {
	return User{
		ID:            r.ID,
		Email:         r.Email,
		InstanceAdmin: r.InstanceAdmin != 0,
		Disabled:      r.Disabled != 0,
		CreatedAt:     r.CreatedAt.UTC(),
	}
}
