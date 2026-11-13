package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/harlew-dev/lewbox/internal/db"
)

func TestAccessTokens(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	clock := now
	f := newOrgFixture(t, WithClock(func() time.Time { return clock }))
	s := f.s

	tok, err := s.CreateAccessToken(f.member, CreateAccessTokenRequest{Name: "laptop", ExpiresIn: "30d"})
	if err != nil {
		t.Fatal(err)
	}
	if !validKeyToken(accessTokenPrefix, tok.Token) || !strings.HasPrefix(tok.Token, tok.Prefix) || !strings.HasSuffix(tok.Token, tok.Last4) {
		t.Errorf("token %q, prefix %q, last4 %q", tok.Token, tok.Prefix, tok.Last4)
	}
	if validAdminKeyToken(tok.Token) {
		t.Error("an access token passes as an admin key")
	}
	if tok.Status != keyStatusActive || tok.ExpiresAt == nil || !tok.ExpiresAt.Equal(now.Add(30*24*time.Hour)) {
		t.Errorf("token = %+v", tok.AccessToken)
	}
	_, err = s.CreateAccessToken(f.member, CreateAccessTokenRequest{Name: "laptop"})
	wantCode(t, err, CodeConflict)
	for _, bad := range []CreateAccessTokenRequest{{Name: ""}, {Name: "x", ExpiresIn: "soon"}, {Name: "x", ExpiresIn: "1m"}, {Name: "0192f0c4-a3b2-7c6d-8e9f-0a1b2c3d4e5f"}} {
		_, err = s.CreateAccessToken(f.member, bad)
		wantCode(t, err, CodeInvalid)
	}
	// Another account's name is free.
	if _, err := s.CreateAccessToken(f.viewer, CreateAccessTokenRequest{Name: "laptop"}); err != nil {
		t.Fatal(err)
	}

	// The token acts as its account, with that account's rights.
	p, err := s.Authenticate(context.Background(), tok.Token)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != "user" || p.Email != "member@example.com" || p.InstanceAdmin || p.TokenID != tok.ID || p.SessionID != "" {
		t.Fatalf("principal = %+v", p)
	}
	tctx := WithPrincipal(context.Background(), p)
	if _, err := s.GetOrganization(tctx, "acme"); err != nil {
		t.Errorf("member's token reads its workspace: %v", err)
	}
	_, err = s.ListClusters(tctx)
	wantCode(t, err, CodeForbidden)
	me, err := s.Me(tctx)
	if err != nil || me.Via != "access_token" || me.User == nil || me.User.Email != "member@example.com" ||
		me.AccessToken == nil || me.AccessToken.ID != tok.ID || len(me.Organizations) != 1 {
		t.Fatalf("me = %+v, %v", me, err)
	}
	_, err = s.Logout(tctx)
	wantCode(t, err, CodeInvalid)

	// A token can't mint another, but can list and revoke.
	_, err = s.CreateAccessToken(tctx, CreateAccessTokenRequest{Name: "more"})
	wantCode(t, err, CodeForbidden)
	list, err := s.ListAccessTokens(tctx, "")
	if err != nil || len(list.Data) != 1 || list.Data[0].ID != tok.ID || list.Data[0].LastUsedAt == nil {
		t.Fatalf("list = %+v, %v", list, err)
	}

	// Plain users can't reach another account's tokens, and aren't told
	// whether it exists.
	_, err = s.ListAccessTokens(f.member, "viewer@example.com")
	wantCode(t, err, CodeForbidden)
	_, err = s.ListAccessTokens(f.member, "nobody@example.com")
	wantCode(t, err, CodeForbidden)
	_, err = s.RevokeAccessToken(f.member, "viewer@example.com", "laptop")
	wantCode(t, err, CodeForbidden)
	// Naming yourself is fine.
	if _, err := s.ListAccessTokens(f.member, "MEMBER@example.com"); err != nil {
		t.Errorf("own tokens by email: %v", err)
	}

	// Expiry.
	clock = now.Add(31 * 24 * time.Hour)
	_, err = s.Authenticate(context.Background(), tok.Token)
	wantCode(t, err, CodeUnauthorized)
	list, _ = s.ListAccessTokens(f.member, "")
	if list.Data[0].Status != keyStatusExpired {
		t.Errorf("status after expiry = %q", list.Data[0].Status)
	}
	clock = now

	// Revoking by name; then the name is free again.
	revoked, err := s.RevokeAccessToken(f.member, "", "laptop")
	if err != nil || revoked.Status != keyStatusRevoked {
		t.Fatalf("revoke = %+v, %v", revoked, err)
	}
	_, err = s.Authenticate(context.Background(), tok.Token)
	wantCode(t, err, CodeUnauthorized)
	_, err = s.RevokeAccessToken(f.member, "", tok.ID)
	wantCode(t, err, CodeConflict)
	_, err = s.RevokeAccessToken(f.member, "", "laptop")
	wantCode(t, err, CodeNotFound)
	if _, err := s.CreateAccessToken(f.member, CreateAccessTokenRequest{Name: "laptop"}); err != nil {
		t.Errorf("reuse a revoked token's name: %v", err)
	}

	// Malformed and made-up tokens.
	for _, bad := range []string{"lbp_", "lbp_" + strings.Repeat("0", 64), tok.Token[:len(tok.Token)-1] + "x"} {
		_, err = s.Authenticate(context.Background(), bad)
		wantCode(t, err, CodeUnauthorized)
	}
}

func TestAccessTokensForInstanceAdmins(t *testing.T) {
	f := newOrgFixture(t)
	s := f.s

	// An instance admin's token manages the instance.
	tok, err := s.CreateAccessToken(f.root, CreateAccessTokenRequest{Name: "ci", ExpiresIn: "90d"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Authenticate(context.Background(), tok.Token)
	if err != nil || !p.InstanceAdmin {
		t.Fatalf("principal = %+v, %v", p, err)
	}
	tctx := WithPrincipal(context.Background(), p)
	if _, err := s.ListClusters(tctx); err != nil {
		t.Errorf("clusters with an admin's token: %v", err)
	}
	if _, err := s.ListUsers(tctx); err != nil {
		t.Errorf("users with an admin's token: %v", err)
	}

	// Local mode has no account, so it names one. Admins can manage anyone's.
	_, err = s.CreateAccessToken(localCtx(), CreateAccessTokenRequest{Name: "ops"})
	wantCode(t, err, CodeInvalid)
	theirs, err := s.CreateAccessToken(localCtx(), CreateAccessTokenRequest{Name: "ops", User: "owner@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.ListAccessTokens(f.root, "owner@example.com")
	if err != nil || len(list.Data) != 1 || list.Data[0].UserID != theirs.UserID {
		t.Fatalf("admin lists another's tokens = %+v, %v", list, err)
	}
	_, err = s.ListAccessTokens(f.root, "nobody@example.com")
	wantCode(t, err, CodeNotFound)
	if _, err := s.RevokeAccessToken(f.root, "owner@example.com", "ops"); err != nil {
		t.Errorf("admin revokes another's token: %v", err)
	}

	other, err := s.CreateUser(f.root, CreateUserRequest{Email: "ops@example.com", Password: testPassword, InstanceAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	opsTok, err := s.CreateAccessToken(localCtx(), CreateAccessTokenRequest{Name: "ci", User: other.Email})
	if err != nil {
		t.Fatal(err)
	}

	// Disabling an account revokes its tokens for good; removing it deletes them.
	if _, err := s.SetUserDisabled(f.root, other.Email, true); err != nil {
		t.Fatal(err)
	}
	_, err = s.Authenticate(context.Background(), opsTok.Token)
	wantCode(t, err, CodeUnauthorized)
	_, err = s.CreateAccessToken(localCtx(), CreateAccessTokenRequest{Name: "again", User: other.Email})
	wantCode(t, err, CodeConflict)
	if _, err := s.SetUserDisabled(f.root, other.Email, false); err != nil {
		t.Fatal(err)
	}
	_, err = s.Authenticate(context.Background(), opsTok.Token)
	wantCode(t, err, CodeUnauthorized)
	list, _ = s.ListAccessTokens(f.root, other.Email)
	if len(list.Data) != 1 || list.Data[0].Status != keyStatusRevoked {
		t.Errorf("tokens after disable = %+v", list.Data)
	}
	if _, err := s.RemoveUser(f.root, other.Email); err != nil {
		t.Fatal(err)
	}
	_, err = s.Authenticate(context.Background(), opsTok.Token)
	wantCode(t, err, CodeUnauthorized)
}

// A token's admin rights are the account's, read on every request: losing
// them takes them from the token at once.
func TestAccessTokenFollowsAdminFlag(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(ctx, filepath.Join(t.TempDir(), "lewbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	s := New(db.New(conn))
	ops, err := s.CreateUser(localCtx(), CreateUserRequest{Email: "ops@example.com", Password: testPassword, InstanceAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := s.CreateAccessToken(localCtx(), CreateAccessTokenRequest{Name: "ci", User: ops.Email})
	if err != nil {
		t.Fatal(err)
	}
	if p, err := s.Authenticate(ctx, tok.Token); err != nil || !p.InstanceAdmin {
		t.Fatalf("admin's token = %+v, %v", p, err)
	}
	if _, err := conn.ExecContext(ctx, "UPDATE users SET instance_admin = 0 WHERE id = ?", ops.ID); err != nil {
		t.Fatal(err)
	}
	p, err := s.Authenticate(ctx, tok.Token)
	if err != nil || p.InstanceAdmin {
		t.Fatalf("demoted admin's token = %+v, %v", p, err)
	}
	_, err = s.ListClusters(WithPrincipal(ctx, p))
	wantCode(t, err, CodeForbidden)
}
