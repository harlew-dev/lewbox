package service

import (
	"context"
	"strings"
	"testing"
	"time"
)

// orgFixture is an organization with an owner, an admin, a member and a
// viewer, each signed in, plus an instance admin who is not a member.
type orgFixture struct {
	s                                  *Service
	org                                Organization
	root, owner, admin, member, viewer context.Context
}

func userCtx(t *testing.T, s *Service, email string) context.Context {
	t.Helper()
	p, _ := loginUser(t, s, email, testPassword)
	return WithPrincipal(context.Background(), p)
}

func newOrgFixture(t *testing.T, opts ...Option) orgFixture {
	t.Helper()
	s := newTestServiceWith(t, opts...)
	createUser(t, s, "root@example.com", true)
	for _, email := range []string{"owner@example.com", "admin@example.com", "member@example.com", "viewer@example.com", "outsider@example.com"} {
		createUser(t, s, email, false)
	}
	f := orgFixture{s: s, root: userCtx(t, s, "root@example.com")}
	org, err := s.CreateOrganization(f.root, CreateOrganizationRequest{Slug: "acme", Name: "Acme", Owner: "owner@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	f.org = org
	f.owner = userCtx(t, s, "owner@example.com")
	for email, role := range map[string]string{"admin@example.com": roleAdmin, "member@example.com": roleMember, "viewer@example.com": roleViewer} {
		if _, err := s.AddMember(f.owner, "acme", AddMemberRequest{Email: email, Role: role}); err != nil {
			t.Fatalf("add %s: %v", email, err)
		}
	}
	f.admin = userCtx(t, s, "admin@example.com")
	f.member = userCtx(t, s, "member@example.com")
	f.viewer = userCtx(t, s, "viewer@example.com")
	return f
}

func TestCreateOrganization(t *testing.T) {
	s := newTestService(t)
	createUser(t, s, "root@example.com", true)
	createUser(t, s, "user@example.com", false)
	root := userCtx(t, s, "root@example.com")

	// The creator owns it by default.
	org, err := s.CreateOrganization(root, CreateOrganizationRequest{Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if org.Slug != "acme" || org.Name != "acme" || org.Status != "active" || org.Members != 1 || org.Role != roleOwner {
		t.Errorf("org = %+v", org)
	}
	// Local mode has no account to make owner.
	local, err := s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "solo", Name: "Solo"})
	if err != nil || local.Members != 0 {
		t.Errorf("local create = %+v, %v", local, err)
	}

	_, err = s.CreateOrganization(root, CreateOrganizationRequest{Slug: "acme"})
	wantCode(t, err, CodeConflict)
	_, err = s.CreateOrganization(root, CreateOrganizationRequest{Slug: "Not_A_Slug"})
	wantCode(t, err, CodeInvalid)
	_, err = s.CreateOrganization(root, CreateOrganizationRequest{Slug: "0192f0c4a3b27c6d8e9f0a1b2c3d4e5f"})
	wantCode(t, err, CodeInvalid)
	_, err = s.CreateOrganization(root, CreateOrganizationRequest{Slug: "x", Owner: "nobody@example.com"})
	wantCode(t, err, CodeNotFound)
	if _, err := s.GetOrganization(root, "x"); err == nil {
		t.Error("an organization was created for an unknown owner")
	}
	_, err = s.CreateOrganization(userCtx(t, s, "user@example.com"), CreateOrganizationRequest{Slug: "mine"})
	wantCode(t, err, CodeForbidden)
}

func TestOrganizationVisibility(t *testing.T) {
	f := newOrgFixture(t)
	s := f.s
	outsider := userCtx(t, s, "outsider@example.com")
	if _, err := s.CreateOrganization(f.root, CreateOrganizationRequest{Slug: "other", Owner: "outsider@example.com"}); err != nil {
		t.Fatal(err)
	}

	all, err := s.ListOrganizations(f.root, ListOrganizationsRequest{})
	if err != nil || len(all.Data) != 2 {
		t.Fatalf("root list = %+v, %v", all, err)
	}
	mine, err := s.ListOrganizations(f.member, ListOrganizationsRequest{})
	if err != nil || len(mine.Data) != 1 || mine.Data[0].Slug != "acme" || mine.Data[0].Role != roleMember || mine.Data[0].Members != 4 {
		t.Fatalf("member list = %+v, %v", mine, err)
	}
	// Someone else's organization is not found, not forbidden.
	_, err = s.GetOrganization(outsider, "acme")
	wantCode(t, err, CodeNotFound)
	_, err = s.ListMembers(outsider, "acme")
	wantCode(t, err, CodeNotFound)

	// Listing another account's organizations is for instance admins.
	theirs, err := s.ListOrganizations(f.root, ListOrganizationsRequest{User: "viewer@example.com"})
	if err != nil || len(theirs.Data) != 1 || theirs.Data[0].Role != roleViewer {
		t.Fatalf("list for viewer = %+v, %v", theirs, err)
	}
	_, err = s.ListOrganizations(f.member, ListOrganizationsRequest{User: "viewer@example.com"})
	wantCode(t, err, CodeForbidden)
	// An unknown account is a 403 too, not a 404.
	_, err = s.ListOrganizations(f.member, ListOrganizationsRequest{User: "nobody@example.com"})
	wantCode(t, err, CodeForbidden)
	// Asking about yourself by email is allowed.
	if _, err := s.ListOrganizations(f.member, ListOrganizationsRequest{User: "member@example.com"}); err != nil {
		t.Errorf("list own organizations by email: %v", err)
	}

	// No organization named and not an admin key: the caller must choose.
	_, err = s.ListMembers(f.member, "")
	if se := wantCode(t, err, CodeInvalid); !strings.Contains(se.Message, "choose a workspace") {
		t.Errorf("message = %q", se.Message)
	}
}

func TestMemberRoles(t *testing.T) {
	f := newOrgFixture(t)
	s := f.s

	// Members and viewers can read, not change.
	if list, err := s.ListMembers(f.viewer, "acme"); err != nil || len(list.Data) != 4 {
		t.Fatalf("viewer list = %+v, %v", list, err)
	}
	_, err := s.AddMember(f.member, "acme", AddMemberRequest{Email: "outsider@example.com"})
	wantCode(t, err, CodeForbidden)

	// An admin manages members but not owners.
	_, err = s.AddMember(f.admin, "acme", AddMemberRequest{Email: "outsider@example.com", Role: roleOwner})
	wantCode(t, err, CodeForbidden)
	added, err := s.AddMember(f.admin, "acme", AddMemberRequest{Email: "outsider@example.com"})
	if err != nil || added.Role != roleMember || added.Email != "outsider@example.com" {
		t.Fatalf("admin add = %+v, %v", added, err)
	}
	_, err = s.AddMember(f.admin, "acme", AddMemberRequest{Email: "outsider@example.com"})
	wantCode(t, err, CodeConflict)
	_, err = s.SetMemberRole(f.admin, "acme", "owner@example.com", SetMemberRoleRequest{Role: roleMember})
	wantCode(t, err, CodeForbidden)
	_, err = s.SetMemberRole(f.admin, "acme", "member@example.com", SetMemberRoleRequest{Role: roleOwner})
	wantCode(t, err, CodeForbidden)
	_, err = s.SetMemberRole(f.admin, "acme", "member@example.com", SetMemberRoleRequest{Role: "boss"})
	wantCode(t, err, CodeInvalid)
	if m, err := s.SetMemberRole(f.admin, "acme", added.ID, SetMemberRoleRequest{Role: roleViewer}); err != nil || m.Role != roleViewer {
		t.Fatalf("set role by membership id = %+v, %v", m, err)
	}
	_, err = s.RemoveMember(f.admin, "acme", "owner@example.com")
	wantCode(t, err, CodeForbidden)

	// Anyone can leave.
	if _, err := s.RemoveMember(f.viewer, "acme", "viewer@example.com"); err != nil {
		t.Fatalf("viewer leaves: %v", err)
	}
	_, err = s.RemoveMember(f.member, "acme", "admin@example.com")
	wantCode(t, err, CodeForbidden)
	_, err = s.RemoveMember(f.admin, "acme", "nobody@example.com")
	wantCode(t, err, CodeNotFound)

	// A customer cannot add an instance admin, but an instance admin can.
	_, err = s.AddMember(f.owner, "acme", AddMemberRequest{Email: "root@example.com"})
	se := wantCode(t, err, CodeNotFound)
	if se.Message != "no account with that email" {
		t.Errorf("message = %q", se.Message)
	}
	rootUser, err := s.findUser(context.Background(), "root@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddMember(f.root, "acme", AddMemberRequest{Email: "root@example.com"}); err != nil {
		t.Fatalf("instance admin adds an instance admin: %v", err)
	}
	// A customer sees "operator" instead of the operator's address.
	list, err := s.ListMembers(f.owner, "acme")
	if err != nil {
		t.Fatal(err)
	}
	masked := false
	for _, m := range list.Data {
		if m.UserID == rootUser.ID {
			masked = m.Email == "operator"
		}
	}
	if !masked {
		t.Errorf("members = %+v, want the instance admin shown as operator", list.Data)
	}
	list, err = s.ListMembers(f.root, "acme")
	if err != nil {
		t.Fatal(err)
	}
	masked = false
	for _, m := range list.Data {
		if m.UserID == rootUser.ID {
			masked = m.Email == "root@example.com"
		}
	}
	if !masked {
		t.Errorf("instance admin sees its own address: %+v", list.Data)
	}
}

func TestLastOwnerStays(t *testing.T) {
	f := newOrgFixture(t)
	s := f.s

	_, err := s.SetMemberRole(f.owner, "acme", "owner@example.com", SetMemberRoleRequest{Role: roleAdmin})
	wantCode(t, err, CodeConflict)
	_, err = s.RemoveMember(f.owner, "acme", "owner@example.com")
	wantCode(t, err, CodeConflict)
	_, err = s.RemoveMember(f.root, "acme", "owner@example.com")
	wantCode(t, err, CodeConflict)

	// With a second owner, the first can step down.
	if _, err := s.SetMemberRole(f.owner, "acme", "admin@example.com", SetMemberRoleRequest{Role: roleOwner}); err != nil {
		t.Fatal(err)
	}
	if m, err := s.SetMemberRole(f.owner, "acme", "owner@example.com", SetMemberRoleRequest{Role: roleMember}); err != nil || m.Role != roleMember {
		t.Fatalf("step down = %+v, %v", m, err)
	}
}

func TestAdminKeys(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	clock := now
	f := newOrgFixture(t, WithClock(func() time.Time { return clock }))
	s := f.s

	// Members and viewers can't see or make keys; owners and admins can.
	_, err := s.ListAdminKeys(f.member, "acme")
	wantCode(t, err, CodeForbidden)
	_, err = s.CreateAdminKey(f.viewer, "acme", CreateAdminKeyRequest{Name: "ci"})
	wantCode(t, err, CodeForbidden)

	key, err := s.CreateAdminKey(f.admin, "acme", CreateAdminKeyRequest{Name: "backend", ExpiresIn: "30d"})
	if err != nil {
		t.Fatal(err)
	}
	if !validAdminKeyToken(key.Token) || !strings.HasPrefix(key.Token, key.Prefix) || !strings.HasSuffix(key.Token, key.Last4) {
		t.Errorf("token %q, prefix %q, last4 %q", key.Token, key.Prefix, key.Last4)
	}
	if key.Status != keyStatusActive || key.CreatedBy != "admin@example.com" || key.ExpiresAt == nil || !key.ExpiresAt.Equal(now.Add(30*24*time.Hour)) {
		t.Errorf("key = %+v", key.AdminKey)
	}
	_, err = s.CreateAdminKey(f.owner, "acme", CreateAdminKeyRequest{Name: "backend"})
	wantCode(t, err, CodeConflict)
	for _, bad := range []CreateAdminKeyRequest{{Name: ""}, {Name: "x", ExpiresIn: "soon"}, {Name: "x", ExpiresIn: "1m"}, {Name: "0192f0c4-a3b2-7c6d-8e9f-0a1b2c3d4e5f"}} {
		_, err = s.CreateAdminKey(f.owner, "acme", bad)
		wantCode(t, err, CodeInvalid)
	}

	// The key acts for its organization, read-only.
	p, err := s.Authenticate(context.Background(), key.Token)
	if err != nil {
		t.Fatal(err)
	}
	kctx := WithPrincipal(context.Background(), p)
	me, err := s.Me(kctx)
	if err != nil || me.Via != "admin_key" || me.Organization == nil || me.Organization.Slug != "acme" || me.AdminKey == nil || me.AdminKey.Name != "backend" {
		t.Fatalf("me = %+v, %v", me, err)
	}
	if org, err := s.GetOrganization(kctx, ""); err != nil || org.Slug != "acme" {
		t.Fatalf("key's own organization = %+v, %v", org, err)
	}
	if list, err := s.ListMembers(kctx, ""); err != nil || len(list.Data) != 4 {
		t.Fatalf("key lists members = %+v, %v", list, err)
	}
	if keys, err := s.ListAdminKeys(kctx, "acme"); err != nil || len(keys.Data) != 1 || keys.Data[0].LastUsedAt == nil {
		t.Fatalf("key lists keys = %+v, %v", keys, err)
	}
	_, err = s.CreateAdminKey(kctx, "", CreateAdminKeyRequest{Name: "another"})
	wantCode(t, err, CodeForbidden)
	_, err = s.AddMember(kctx, "", AddMemberRequest{Email: "outsider@example.com"})
	wantCode(t, err, CodeForbidden)

	// It is confined to its organization's routes.
	_, err = s.ListClusters(kctx)
	wantCode(t, err, CodeForbidden)
	_, err = s.ListUsers(kctx)
	wantCode(t, err, CodeForbidden)
	_, err = s.ListNodes(kctx, ListNodesRequest{})
	wantCode(t, err, CodeForbidden)
	_, err = s.Logout(kctx)
	wantCode(t, err, CodeInvalid)
	if _, err := s.CreateOrganization(f.root, CreateOrganizationRequest{Slug: "other"}); err != nil {
		t.Fatal(err)
	}
	_, err = s.GetOrganization(kctx, "other")
	wantCode(t, err, CodeNotFound)

	// A tampered token fails its checksum before any lookup.
	tampered := key.Token[:10] + string("0123456789abcdef"[(strings.IndexByte("0123456789abcdef", key.Token[10])+1)%16]) + key.Token[11:]
	_, err = s.Authenticate(context.Background(), tampered)
	wantCode(t, err, CodeUnauthorized)

	// Expired keys stop working and say so in the list.
	clock = now.Add(31 * 24 * time.Hour)
	_, err = s.Authenticate(context.Background(), key.Token)
	wantCode(t, err, CodeUnauthorized)
	keys, err := s.ListAdminKeys(f.owner, "acme")
	if err != nil || keys.Data[0].Status != keyStatusExpired {
		t.Fatalf("expired key = %+v, %v", keys, err)
	}
}

func TestRevokeAndSuspend(t *testing.T) {
	f := newOrgFixture(t)
	s := f.s

	never, err := s.CreateAdminKey(f.owner, "acme", CreateAdminKeyRequest{Name: "never"})
	if err != nil || never.ExpiresAt != nil {
		t.Fatalf("never-expiring key = %+v, %v", never, err)
	}
	if _, err := s.Authenticate(context.Background(), never.Token); err != nil {
		t.Fatal(err)
	}

	// Suspending the organization stops its keys; members can still read.
	suspended := "suspended"
	_, err = s.UpdateOrganization(f.owner, "acme", UpdateOrganizationRequest{Status: &suspended})
	wantCode(t, err, CodeForbidden)
	org, err := s.UpdateOrganization(f.root, "acme", UpdateOrganizationRequest{Status: &suspended})
	if err != nil || org.Status != "suspended" {
		t.Fatalf("suspend = %+v, %v", org, err)
	}
	_, err = s.Authenticate(context.Background(), never.Token)
	wantCode(t, err, CodeForbidden)
	if _, err := s.ListMembers(f.member, "acme"); err != nil {
		t.Errorf("member read while suspended: %v", err)
	}
	_, err = s.CreateAdminKey(f.owner, "acme", CreateAdminKeyRequest{Name: "more"})
	wantCode(t, err, CodeForbidden)
	active := "active"
	if _, err := s.UpdateOrganization(f.root, "acme", UpdateOrganizationRequest{Status: &active}); err != nil {
		t.Fatal(err)
	}

	// Revoke by name; the name is free again afterwards.
	revoked, err := s.RevokeAdminKey(f.admin, "acme", "never")
	if err != nil || revoked.Status != keyStatusRevoked || revoked.RevokedAt == nil {
		t.Fatalf("revoke = %+v, %v", revoked, err)
	}
	_, err = s.Authenticate(context.Background(), never.Token)
	wantCode(t, err, CodeUnauthorized)
	_, err = s.RevokeAdminKey(f.admin, "acme", revoked.ID)
	wantCode(t, err, CodeConflict)
	_, err = s.RevokeAdminKey(f.admin, "acme", "never")
	wantCode(t, err, CodeNotFound)
	if _, err := s.CreateAdminKey(f.owner, "acme", CreateAdminKeyRequest{Name: "never"}); err != nil {
		t.Errorf("reuse a revoked key's name: %v", err)
	}

	// Renaming is for owners and admins.
	name := "Acme Inc."
	_, err = s.UpdateOrganization(f.member, "acme", UpdateOrganizationRequest{Name: &name})
	wantCode(t, err, CodeForbidden)
	if org, err := s.UpdateOrganization(f.admin, "acme", UpdateOrganizationRequest{Name: &name}); err != nil || org.Name != name {
		t.Fatalf("rename = %+v, %v", org, err)
	}
	_, err = s.UpdateOrganization(f.admin, "acme", UpdateOrganizationRequest{})
	wantCode(t, err, CodeInvalid)
}

func TestAdminKeyTokenFormat(t *testing.T) {
	token, digest, err := newAdminKeyToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != adminKeyLength || !strings.HasPrefix(token, "lba_") || digest != tokenDigest(token) {
		t.Errorf("token %q (%d chars)", token, len(token))
	}
	if !validAdminKeyToken(token) {
		t.Error("a fresh token is not valid")
	}
	for _, bad := range []string{"", "lba_", token[:len(token)-1], strings.ToUpper(token), "lbs_" + token[4:], token[:len(token)-1] + "!"} {
		if validAdminKeyToken(bad) {
			t.Errorf("validAdminKeyToken(%q) = true", bad)
		}
	}
}
