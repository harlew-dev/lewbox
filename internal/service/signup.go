package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SignupRequest is the input to Signup. SetupToken is the token of an unused
// setup link, which allows sign-up in any mode and is claimed here.
type SignupRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	SetupToken string `json:"setup_token"`
}

// Signup creates an account and signs it in as a browser session. The account
// has no workspace; its owner creates one with CreateOrganization. Any failure
// after the account is created deletes it, so the email can be tried again.
func (s *Service) Signup(ctx context.Context, req SignupRequest) (token string, resp SessionResponse, err error) {
	settings, err := s.getSettings(ctx)
	if err != nil {
		return "", SessionResponse{}, err
	}
	setupToken := req.SetupToken
	if setupToken != "" {
		// A valid setup link allows sign-up in any mode; the conditional
		// update after the account exists is the real guard.
		link, err := s.findSetupLink(ctx, setupToken)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return "", SessionResponse{}, setupLinkInvalid()
		case err != nil:
			return "", SessionResponse{}, err
		case link.UsedAt.Valid, link.ClaimedBy.Valid, !link.ExpiresAt.After(s.now()):
			return "", SessionResponse{}, setupLinkInvalid()
		}
	} else if settings.Signup == signupClosed {
		return "", SessionResponse{}, forbidden("sign-up is closed on this instance")
	}

	now := s.now()
	if retry, ok, first := s.signupLimiter.allowIP(now, clientIPFrom(ctx)); !ok {
		if first {
			s.auditRateLimit(ctx, budgetSignupPerIP)
		}
		return "", SessionResponse{}, signupRateLimited(retry)
	}

	nu, err := prepareUser(CreateUserRequest{Email: req.Email, Password: req.Password})
	if err != nil {
		return "", SessionResponse{}, err
	}
	var created User
	err = s.withTx(ctx, func(ctx context.Context) error {
		user, err := s.insertUser(ctx, nu)
		if err != nil {
			if se, ok := errors.AsType[*Error](err); ok && se.Code == CodeConflict {
				return conflict("an account with this email already exists; sign in instead")
			}
			return err
		}
		created = user
		if setupToken != "" {
			if _, err := s.claimSetupLink(ctx, setupToken, user.ID, now); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return setupLinkInvalid()
				}
				return fmt.Errorf("claim setup link: %w", err)
			}
		}
		var expires time.Time
		token, expires, err = s.startSession(ctx, now, user.ID, "web", webSessionLifetime)
		if err != nil {
			return err
		}
		if err := s.record(ctx, Event{
			Name:       "user.created",
			ActorKind:  "user",
			ActorID:    user.ID,
			ActorLabel: user.Email,
			TargetType: "user",
			TargetID:   user.ID,
			TargetName: user.Email,
		}); err != nil {
			return err
		}
		resp = SessionResponse{ExpiresAt: expires, User: created}
		return nil
	})
	if err != nil {
		return "", SessionResponse{}, err
	}
	return token, resp, nil
}
