package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestWebLogin(t *testing.T) {
	base := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	cur := base
	s := newTestServiceWith(t, WithClock(func() time.Time { return cur }))
	u := createUser(t, s, "ada@example.com", false)

	token, resp, err := s.WebLogin(context.Background(), LoginRequest{Email: "  ADA@example.com ", Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	if !validSessionToken(token) {
		t.Errorf("web login token %q is not valid", token)
	}
	if resp.User.ID != u.ID || resp.User.Email != u.Email {
		t.Errorf("web login user = %+v", resp.User)
	}
	if d := resp.ExpiresAt.Sub(resp.User.CreatedAt); d != webSessionLifetime {
		t.Errorf("web session lasts %v, want %v", d, webSessionLifetime)
	}

	row, err := s.q.GetSessionByTokenHash(context.Background(), tokenDigest(token))
	if err != nil {
		t.Fatal(err)
	}
	if row.Kind != "web" {
		t.Errorf("web session kind = %q", row.Kind)
	}
	if !row.ExpiresAt.Equal(base.Add(webSessionLifetime)) {
		t.Errorf("web session expires at %v", row.ExpiresAt)
	}

	// CLI sessions keep their kind and 30-day lifetime.
	login, err := s.Login(context.Background(), LoginRequest{Email: u.Email, Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	row, err = s.q.GetSessionByTokenHash(context.Background(), tokenDigest(login.Token))
	if err != nil {
		t.Fatal(err)
	}
	if row.Kind != "cli" {
		t.Errorf("cli session kind = %q", row.Kind)
	}
	if d := row.ExpiresAt.Sub(row.CreatedAt); d != sessionLifetime {
		t.Errorf("cli session lasts %v, want %v", d, sessionLifetime)
	}

	// WebLogin shares Login's credential errors and messages.
	_, _, err = s.WebLogin(context.Background(), LoginRequest{Email: u.Email, Password: testPassword + "x"})
	se := wantCode(t, err, CodeUnauthorized)
	if se.Message != "email or password is wrong" {
		t.Errorf("web login message = %q", se.Message)
	}
}

func TestWebSessionIdleTimeout(t *testing.T) {
	base := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	cur := base
	s := newTestServiceWith(t, WithClock(func() time.Time { return cur }))
	u := createUser(t, s, "ada@example.com", false)

	token, _, err := s.WebLogin(context.Background(), LoginRequest{Email: u.Email, Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}

	// Activity refreshes the idle clock.
	cur = base.Add(11 * time.Hour)
	if _, err := s.Authenticate(context.Background(), token); err != nil {
		t.Fatalf("authenticate after 11h: %v", err)
	}
	cur = base.Add(22 * time.Hour)
	if _, err := s.Authenticate(context.Background(), token); err != nil {
		t.Fatalf("authenticate after another 11h: %v", err)
	}

	// Exactly one idle timeout after the last use, the session is gone.
	cur = base.Add(22*time.Hour + webSessionIdle)
	_, err = s.Authenticate(context.Background(), token)
	wantCode(t, err, CodeUnauthorized)
	if _, err := s.q.GetSessionByTokenHash(context.Background(), tokenDigest(token)); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("idle session was not deleted: %v", err)
	}
}

func TestWebSessionAbsoluteLifetime(t *testing.T) {
	base := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	cur := base
	s := newTestServiceWith(t, WithClock(func() time.Time { return cur }))
	u := createUser(t, s, "ada@example.com", false)

	token, _, err := s.WebLogin(context.Background(), LoginRequest{Email: u.Email, Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}

	// Keep the session active so only the absolute lifetime can end it.
	for at := base.Add(11 * time.Hour); at.Before(base.Add(webSessionLifetime)); at = at.Add(11 * time.Hour) {
		cur = at
		if _, err := s.Authenticate(context.Background(), token); err != nil {
			t.Fatalf("authenticate at %v: %v", at.Sub(base), err)
		}
	}
	cur = base.Add(webSessionLifetime)
	_, err = s.Authenticate(context.Background(), token)
	wantCode(t, err, CodeUnauthorized)
}

func TestCLISessionIgnoresWebIdle(t *testing.T) {
	base := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	cur := base
	s := newTestServiceWith(t, WithClock(func() time.Time { return cur }))
	u := createUser(t, s, "ada@example.com", false)

	login, err := s.Login(context.Background(), LoginRequest{Email: u.Email, Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	cur = base.Add(webSessionIdle + time.Hour)
	if _, err := s.Authenticate(context.Background(), login.Token); err != nil {
		t.Fatalf("cli session ended after the web idle timeout: %v", err)
	}
	cur = base.Add(sessionLifetime)
	_, err = s.Authenticate(context.Background(), login.Token)
	wantCode(t, err, CodeUnauthorized)
}

func TestLoginRateLimitPerIP(t *testing.T) {
	base := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	cur := base
	s := newTestServiceWith(t, WithClock(func() time.Time { return cur }), WithLoginLimits(2, 0))
	u := createUser(t, s, "ada@example.com", false)
	ctx := WithClientIP(context.Background(), "192.0.2.1")
	wrong := LoginRequest{Email: u.Email, Password: testPassword + "x"}

	for i := 0; i < 2; i++ {
		_, err := s.Login(ctx, wrong)
		wantCode(t, err, CodeUnauthorized)
	}
	_, err := s.Login(ctx, wrong)
	se := wantCode(t, err, CodeRateLimited)
	if secs := retryAfterDetail(t, se); secs != 60 {
		t.Errorf("retry_after_seconds = %d, want 60", secs)
	}
	if se.Message != "too many sign-in attempts; try again in 60 seconds" {
		t.Errorf("message = %q", se.Message)
	}

	// Every attempt counts, including one with the right password.
	_, err = s.Login(ctx, LoginRequest{Email: u.Email, Password: testPassword})
	wantCode(t, err, CodeRateLimited)

	// Another address has its own budget.
	_, err = s.Login(WithClientIP(context.Background(), "198.51.100.7"), wrong)
	wantCode(t, err, CodeUnauthorized)
	// Without an address in the context, the IP budget is skipped entirely.
	_, err = s.Login(context.Background(), wrong)
	wantCode(t, err, CodeUnauthorized)

	// A new window allows the account again.
	cur = base.Add(loginWindow)
	if _, err := s.Login(ctx, LoginRequest{Email: u.Email, Password: testPassword}); err != nil {
		t.Fatalf("login after the window: %v", err)
	}
}

func TestLoginRateLimitPerEmail(t *testing.T) {
	base := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	cur := base
	s := newTestServiceWith(t, WithClock(func() time.Time { return cur }), WithLoginLimits(0, 2))
	u := createUser(t, s, "ada@example.com", false)

	// A success is never counted.
	if _, err := s.Login(context.Background(), LoginRequest{Email: u.Email, Password: testPassword}); err != nil {
		t.Fatal(err)
	}

	// Failures against different spellings of the same email share a budget.
	for _, email := range []string{" ADA@example.com ", "ada@EXAMPLE.com"} {
		_, err := s.Login(context.Background(), LoginRequest{Email: email, Password: testPassword + "x"})
		wantCode(t, err, CodeUnauthorized)
	}
	_, err := s.Login(context.Background(), LoginRequest{Email: u.Email, Password: testPassword + "x"})
	se := wantCode(t, err, CodeRateLimited)
	if secs := retryAfterDetail(t, se); secs != 60 {
		t.Errorf("retry_after_seconds = %d, want 60", secs)
	}

	// The budget is per email, so another account can still try.
	if _, err := s.Login(context.Background(), LoginRequest{Email: "bob@example.com", Password: testPassword}); err != nil {
		wantCode(t, err, CodeUnauthorized)
	}

	// After the window the email may try again.
	cur = base.Add(loginWindow)
	if _, err := s.Login(context.Background(), LoginRequest{Email: u.Email, Password: testPassword}); err != nil {
		t.Fatalf("login after the window: %v", err)
	}
}

func TestLoginLimiterMemoryCap(t *testing.T) {
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	l := newLoginLimiter(0, 5)
	for i := 0; i < loginLimiterMaxKeys; i++ {
		email := fmt.Sprintf("user%d@example.com", i)
		if _, ok := l.allowEmail(now, email); !ok {
			t.Fatalf("allowEmail(%q) rejected before the cap", email)
		}
	}
	if _, ok := l.allowEmail(now, "one-more@example.com"); ok {
		t.Error("a new key was accepted when the map was full")
	}

	// Once old windows expire, the map is pruned and new keys fit again.
	now = now.Add(loginWindow)
	if _, ok := l.allowEmail(now, "one-more@example.com"); !ok {
		t.Error("a new key was rejected after pruning")
	}
}

func retryAfterDetail(t *testing.T, se *Error) int {
	t.Helper()
	details, ok := se.Details.(map[string]any)
	if !ok {
		t.Fatalf("details = %#v, want a map", se.Details)
	}
	seconds, ok := details["retry_after_seconds"].(int)
	if !ok || seconds < 1 {
		t.Fatalf("retry_after_seconds = %#v, want a positive int", details["retry_after_seconds"])
	}
	return seconds
}

// Attempts that are still in flight hold their slot, so concurrent guesses
// against one account can't all pass the check while the hash runs.
func TestLoginLimiterEmailReservesSlots(t *testing.T) {
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	l := newLoginLimiter(0, 2)
	for i := range 2 {
		if _, ok := l.allowEmail(now, "a@example.com"); !ok {
			t.Fatalf("attempt %d rejected", i+1)
		}
	}
	if _, ok := l.allowEmail(now, "a@example.com"); ok {
		t.Fatal("third concurrent attempt was allowed")
	}
	l.clearEmail("a@example.com") // a success frees the budget
	if _, ok := l.allowEmail(now, "a@example.com"); !ok {
		t.Fatal("attempt after a success was rejected")
	}
}
