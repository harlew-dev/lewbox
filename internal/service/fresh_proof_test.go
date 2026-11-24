package service

import (
	"context"
	"testing"
)

// Sensitive actions need the caller's current password, whatever proved the
// session; local mode needs none and an admin key is refused.
func TestFreshProof(t *testing.T) {
	f := newOrgFixture(t)
	s := f.s

	wantField := func(t *testing.T, err error) {
		t.Helper()
		se := wantCode(t, err, CodeInvalid)
		if d, ok := se.Details.(FieldDetails); !ok || d.Field != "current_password" {
			t.Fatalf("details = %#v, want field current_password", se.Details)
		}
	}

	// Missing and wrong proof are refused before anything is written.
	_, err := s.CreateAccessToken(f.member, CreateAccessTokenRequest{Name: "laptop"})
	wantField(t, err)
	_, err = s.CreateAccessToken(f.member, CreateAccessTokenRequest{Name: "laptop", CurrentPassword: "not-the-password"})
	wantField(t, err)
	_, err = s.CreateAdminKey(f.owner, "acme", CreateAdminKeyRequest{Name: "ci"})
	wantField(t, err)
	_, err = s.CreateAdminKey(f.owner, "acme", CreateAdminKeyRequest{Name: "ci", CurrentPassword: "not-the-password"})
	wantField(t, err)
	_, err = s.SetPassword(f.member, "member@example.com", SetPasswordRequest{Password: "a-brand-new-password"})
	wantField(t, err)
	if list, err := s.ListAccessTokens(f.member, ""); err != nil || len(list.Data) != 0 {
		t.Fatalf("tokens after refused creates = %+v, %v", list, err)
	}
	if list, err := s.ListAdminKeys(f.owner, "acme"); err != nil || len(list.Data) != 0 {
		t.Fatalf("keys after refused creates = %+v, %v", list, err)
	}
	if _, _, err := s.WebLogin(context.Background(), LoginRequest{Email: "member@example.com", Password: testPassword}); err != nil {
		t.Fatalf("old password stopped working after a refused change: %v", err)
	}

	// The right password works.
	if _, err := s.CreateAccessToken(f.member, CreateAccessTokenRequest{Name: "laptop", CurrentPassword: testPassword}); err != nil {
		t.Fatal(err)
	}
	key, err := s.CreateAdminKey(f.owner, "acme", CreateAdminKeyRequest{Name: "ci", CurrentPassword: testPassword})
	if err != nil {
		t.Fatal(err)
	}

	// An instance admin resetting someone else proves with their own
	// password, not the target's.
	_, err = s.SetPassword(f.root, "viewer@example.com", SetPasswordRequest{Password: "a-brand-new-password", CurrentPassword: "not-the-password"})
	wantField(t, err)
	if _, err := s.SetPassword(f.root, "viewer@example.com", SetPasswordRequest{Password: "a-brand-new-password", CurrentPassword: testPassword}); err != nil {
		t.Fatal(err)
	}

	// Local mode needs no proof.
	if _, err := s.CreateAccessToken(localCtx(), CreateAccessTokenRequest{Name: "ops", User: "outsider@example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAdminKey(localCtx(), "acme", CreateAdminKeyRequest{Name: "ops"}); err != nil {
		t.Fatal(err)
	}

	// An admin key has no password to prove with.
	kp, err := s.Authenticate(context.Background(), key.Token)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateAdminKey(WithPrincipal(context.Background(), kp), "", CreateAdminKeyRequest{Name: "another", CurrentPassword: testPassword})
	wantCode(t, err, CodeForbidden)
}

// Wrong proof spends the account's sign-in failure budget, so it is not a
// second, unlimited way to guess the password.
func TestFreshProofSharesSignInBudget(t *testing.T) {
	f := newOrgFixture(t)
	s := f.s
	for range defaultEmailFailuresPerMinute {
		_, err := s.CreateAccessToken(f.member, CreateAccessTokenRequest{Name: "laptop", CurrentPassword: "not-the-password"})
		wantCode(t, err, CodeInvalid)
	}
	_, err := s.CreateAccessToken(f.member, CreateAccessTokenRequest{Name: "laptop", CurrentPassword: testPassword})
	wantCode(t, err, CodeRateLimited)
	_, err = s.Login(context.Background(), LoginRequest{Email: "member@example.com", Password: testPassword})
	wantCode(t, err, CodeRateLimited)
}

// A plain account asking to change another account's password is refused
// with the same answer whether that account exists or not.
func TestSetPasswordDoesNotRevealAccounts(t *testing.T) {
	f := newOrgFixture(t)
	req := SetPasswordRequest{Password: "a-brand-new-password", CurrentPassword: testPassword}
	_, existing := f.s.SetPassword(f.member, "viewer@example.com", req)
	_, missing := f.s.SetPassword(f.member, "nobody@example.com", req)
	a, b := wantCode(t, existing, CodeForbidden), wantCode(t, missing, CodeForbidden)
	if a.Message != b.Message {
		t.Fatalf("messages differ: %q vs %q", a.Message, b.Message)
	}
}

// Signing in upgrades a hash made with fewer iterations than today's.
func TestSignInRehashesOldPasswords(t *testing.T) {
	s := newTestService(t)
	saved := passwordIterations
	passwordIterations = 500
	u := createUser(t, s, "ada@example.com", false)
	passwordIterations = saved

	before, err := s.q.GetUserByID(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !passwordNeedsRehash(before.PasswordHash) {
		t.Fatalf("hash %q should need a rehash", before.PasswordHash)
	}
	loginUser(t, s, u.Email, testPassword)
	after, err := s.q.GetUserByID(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.PasswordHash == before.PasswordHash || passwordNeedsRehash(after.PasswordHash) {
		t.Fatalf("hash not upgraded: %q", after.PasswordHash)
	}
	loginUser(t, s, u.Email, testPassword)
}
