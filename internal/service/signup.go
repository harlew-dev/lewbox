package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"uuid"

	"github.com/harlew-dev/lewbox/internal/db"
)

// SignupRequest is the input to Signup. Organization is a display name and
// defaults to "<local part>'s organization".
type SignupRequest struct {
	Email        string `json:"email"`
	Password     string `json:"password"`
	Organization string `json:"organization"`
}

const (
	signupSuffixLength = 6
	signupSlugBaseMax  = 40
	signupAttempts     = 5
	signupBase32       = "abcdefghijklmnopqrstuvwxyz234567"
)

// signupSuffix returns the random tail of a new organization's slug. It is a
// variable so tests can force a collision.
var signupSuffix = func() (string, error) {
	b := make([]byte, signupSuffixLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read slug suffix: %w", err)
	}
	for i := range b {
		b[i] = signupBase32[int(b[i])%len(signupBase32)]
	}
	return string(b), nil
}

// Signup creates an account with an organization of its own and signs it in
// as a browser session. Any failure after the account is created deletes
// what was created, so the email can be tried again.
func (s *Service) Signup(ctx context.Context, req SignupRequest) (token string, resp SessionResponse, err error) {
	settings, err := s.getSettings(ctx)
	if err != nil {
		return "", SessionResponse{}, err
	}
	if settings.Signup == signupClosed {
		return "", SessionResponse{}, forbidden("sign-up is closed on this instance")
	}

	now := s.now()
	if retry, ok := s.signupLimiter.allowIP(now, clientIPFrom(ctx)); !ok {
		return "", SessionResponse{}, signupRateLimited(retry)
	}

	user, err := s.createUser(ctx, CreateUserRequest{Email: req.Email, Password: req.Password})
	if err != nil {
		if se, ok := errors.AsType[*Error](err); ok && se.Code == CodeConflict {
			return "", SessionResponse{}, conflict("an account with this email already exists; sign in instead")
		}
		return "", SessionResponse{}, err
	}

	// Roll back on anything after this point. The cleanup must run even if
	// the request's context was canceled.
	committed := false
	var orgID string
	defer func() {
		if committed {
			return
		}
		cleanup := context.WithoutCancel(ctx)
		if orgID != "" {
			_ = s.q.DeleteOrganization(cleanup, orgID)
		}
		_, _ = s.q.DeleteUser(cleanup, user.ID)
	}()

	local := emailLocal(user.Email)
	name, err := signupOrganizationName(req.Organization, local)
	if err != nil {
		return "", SessionResponse{}, err
	}
	status := orgActive
	if settings.Signup == signupApproval {
		status = orgPending
	}
	org, err := s.createSignupOrganization(ctx, local, name, status)
	if err != nil {
		return "", SessionResponse{}, err
	}
	orgID = org.ID
	if err := s.subscribeDefaults(ctx, org, settings); err != nil {
		return "", SessionResponse{}, err
	}

	if _, err := s.q.CreateMembership(ctx, db.CreateMembershipParams{
		ID:        uuid.NewV7().String(),
		OrgID:     org.ID,
		UserID:    user.ID,
		Role:      roleOwner,
		CreatedAt: now,
	}); err != nil {
		return "", SessionResponse{}, fmt.Errorf("add owner: %w", err)
	}

	token, expires, err := s.startSession(ctx, now, user.ID, "web", webSessionLifetime)
	if err != nil {
		return "", SessionResponse{}, err
	}
	committed = true
	return token, SessionResponse{ExpiresAt: expires, User: user}, nil
}

// createSignupOrganization picks an unused slug and creates the organization.
func (s *Service) createSignupOrganization(ctx context.Context, local, name, status string) (db.Organization, error) {
	var err error
	for range signupAttempts {
		suffix, serr := signupSuffix()
		if serr != nil {
			return db.Organization{}, serr
		}
		slug := signupSlug(local, suffix)
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
	return db.Organization{}, fmt.Errorf("no unique organization slug after %d attempts: %w", signupAttempts, err)
}

// subscribeDefaults gives a new organization the default plan in each default
// region that still exists, pushing each quota before it returns. With no
// default plan or regions, it gets none.
func (s *Service) subscribeDefaults(ctx context.Context, org db.Organization, settings Settings) error {
	if settings.DefaultPlan == nil || len(settings.DefaultRegions) == 0 {
		return nil
	}
	plan, err := s.findPlan(ctx, *settings.DefaultPlan)
	if err != nil {
		if se, ok := errors.AsType[*Error](err); ok && se.Code == CodeNotFound {
			return nil
		}
		return err
	}
	for _, slug := range settings.DefaultRegions {
		cluster, err := s.q.GetClusterBySlug(ctx, slug)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("get cluster %q: %w", slug, err)
		}
		sub, err := s.q.CreateSubscription(ctx, db.CreateSubscriptionParams{
			OrgID:     org.ID,
			ClusterID: cluster.ID,
			PlanID:    plan.ID,
			CreatedAt: s.now(),
		})
		if err != nil {
			return fmt.Errorf("subscribe %q: %w", slug, err)
		}
		s.pushSubscriptionQuota(ctx, org, cluster, plan, sub)
	}
	return nil
}

// signupOrganizationName defaults the display name from the email.
func signupOrganizationName(raw, local string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		name = local + "'s organization"
	}
	return orgName(name, "")
}

func emailLocal(email string) string {
	if at := strings.IndexByte(email, '@'); at > 0 {
		return email[:at]
	}
	return email
}

// signupSlug is the email's local part reduced to a DNS label, trimmed to 40
// characters, plus "-" and the random suffix.
func signupSlug(local, suffix string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(local) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	base := strings.Trim(b.String(), "-")
	if len(base) > signupSlugBaseMax {
		base = strings.Trim(base[:signupSlugBaseMax], "-")
	}
	if base == "" {
		base = "org"
	}
	return base + "-" + suffix
}
