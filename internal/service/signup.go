package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	defer func() {
		if committed {
			return
		}
		_, _ = s.q.DeleteUser(context.WithoutCancel(ctx), user.ID)
	}()

	if setupToken != "" {
		if _, err := s.claimSetupLink(ctx, setupToken, user.ID, now); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return "", SessionResponse{}, setupLinkInvalid()
			}
			return "", SessionResponse{}, fmt.Errorf("claim setup link: %w", err)
		}
	}

	token, expires, err := s.startSession(ctx, now, user.ID, "web", webSessionLifetime)
	if err != nil {
		return "", SessionResponse{}, err
	}
	committed = true
	return token, SessionResponse{ExpiresAt: expires, User: user}, nil
}
