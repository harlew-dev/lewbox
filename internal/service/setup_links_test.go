package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// newSetupLinkService is a service with one instance admin and a ready
// cluster, enough to mint setup links.
func newSetupLinkService(t *testing.T, opts ...Option) *Service {
	t.Helper()
	s := newClusterService(t, opts...)
	createUser(t, s, "root@example.com", true)
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	addTestCluster(t, s, fake, "eu-west")
	return s
}

func testSetupLink(t *testing.T, s *Service, req CreateSetupLinkRequest) SetupLink {
	t.Helper()
	link, err := s.CreateSetupLink(localCtx(), req)
	if err != nil {
		t.Fatal(err)
	}
	return link
}

func TestSetupLinksAdminOnly(t *testing.T) {
	s := newSetupLinkService(t)
	createUser(t, s, "ada@example.com", false)
	ada := userCtx(t, s, "ada@example.com")

	if _, err := s.CreateSetupLink(ada, CreateSetupLinkRequest{Plan: "free", Region: "eu-west"}); err != nil {
		wantCode(t, err, CodeForbidden)
	}
	if _, err := s.ListSetupLinks(ada); err != nil {
		wantCode(t, err, CodeForbidden)
	}
	if _, err := s.RevokeSetupLink(ada, "missing"); err != nil {
		wantCode(t, err, CodeForbidden)
	}

	_, err := s.CreateSetupLink(localCtx(), CreateSetupLinkRequest{Plan: "missing", Region: "eu-west"})
	wantCode(t, err, CodeNotFound)
	_, err = s.CreateSetupLink(localCtx(), CreateSetupLinkRequest{Plan: "free", Region: "missing"})
	wantCode(t, err, CodeNotFound)
	_, err = s.CreateSetupLink(localCtx(), CreateSetupLinkRequest{Plan: "free", Region: "eu-west", ExpiresInHours: 721})
	wantCode(t, err, CodeInvalid)
	_, err = s.CreateSetupLink(localCtx(), CreateSetupLinkRequest{Plan: "free", Region: "eu-west", ExpiresInHours: -1})
	wantCode(t, err, CodeInvalid)

	link := testSetupLink(t, s, CreateSetupLinkRequest{
		Plan: "free", Region: "eu-west", ExpiresInHours: 24, Note: "acme",
	})
	if !strings.HasPrefix(link.Token, setupLinkPrefix) || len(link.Token) != len(setupLinkPrefix)+setupLinkChars {
		t.Errorf("token = %q", link.Token)
	}
	if link.Status != setupLinkUnused || link.Plan.Slug != "free" || link.Region.Slug != "eu-west" || link.Note != "acme" {
		t.Errorf("link = %+v", link)
	}
	if link.ClaimedBy != nil || link.Workspace != nil || link.UsedAt != nil {
		t.Errorf("new link has history: %+v", link)
	}

	// Only the hash is stored.
	ctx := context.Background()
	row, err := s.q.GetSetupLinkByTokenHash(ctx, hashSetupToken(link.Token))
	if err != nil {
		t.Fatal(err)
	}
	if row.TokenHash == link.Token || row.TokenHash != hashSetupToken(link.Token) {
		t.Errorf("stored hash = %q", row.TokenHash)
	}
	if _, err := s.q.GetSetupLinkByTokenHash(ctx, link.Token); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("the token itself was found: %v", err)
	}

	list, err := s.ListSetupLinks(localCtx())
	if err != nil || len(list.Data) != 1 || list.Data[0].Token != "" {
		t.Fatalf("list = %+v, %v", list.Data, err)
	}
	data, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"token"`) {
		t.Errorf("list shows a token: %s", data)
	}

	revoked, err := s.RevokeSetupLink(localCtx(), link.ID)
	if err != nil || revoked.ID != link.ID {
		t.Fatalf("revoke = %+v, %v", revoked, err)
	}
	if list, err := s.ListSetupLinks(localCtx()); err != nil || len(list.Data) != 0 {
		t.Errorf("links after revoke = %+v, %v", list.Data, err)
	}
	if _, err := s.RevokeSetupLink(localCtx(), link.ID); err != nil {
		wantCode(t, err, CodeNotFound)
	}
}

func TestSetupLinkSignupClaims(t *testing.T) {
	s := newSetupLinkService(t)
	setSignup(t, s, signupClosed)
	link := testSetupLink(t, s, CreateSetupLinkRequest{Plan: "free", Region: "eu-west"})
	ctx := context.Background()

	if _, _, err := s.Signup(ctx, SignupRequest{Email: "a@example.com", Password: testPassword}); err != nil {
		wantCode(t, err, CodeForbidden)
	}
	_, resp, err := s.Signup(ctx, SignupRequest{Email: "a@example.com", Password: testPassword, SetupToken: link.Token})
	if err != nil {
		t.Fatal(err)
	}
	if resp.User.Email != "a@example.com" {
		t.Errorf("signup user = %+v", resp.User)
	}
	row, err := s.q.GetSetupLinkByTokenHash(ctx, hashSetupToken(link.Token))
	if err != nil {
		t.Fatal(err)
	}
	if !row.ClaimedBy.Valid || row.ClaimedBy.String != resp.User.ID || !row.ClaimedAt.Valid || row.UsedAt.Valid {
		t.Errorf("claimed row = %+v", row)
	}
	links, err := s.ListSetupLinks(localCtx())
	if err != nil || len(links.Data) != 1 || links.Data[0].Status != setupLinkClaimed {
		t.Fatalf("links = %+v, %v", links.Data, err)
	}
	if links.Data[0].ClaimedBy == nil || *links.Data[0].ClaimedBy != "a@example.com" {
		t.Errorf("claimed_by = %v", links.Data[0].ClaimedBy)
	}

	// The same token cannot be claimed twice, and a failed sign-up creates
	// no account.
	_, _, err = s.Signup(ctx, SignupRequest{Email: "b@example.com", Password: testPassword, SetupToken: link.Token})
	wantCode(t, err, CodeInvalid)
	if _, err := s.q.GetUserByEmail(ctx, "b@example.com"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("failed sign-up left an account: %v", err)
	}

	// Another account cannot consume a token claimed by someone else.
	createUser(t, s, "c@example.com", false)
	_, err = s.CreateOrganization(userCtx(t, s, "c@example.com"), CreateOrganizationRequest{Name: "C Labs", SetupToken: link.Token})
	se := wantCode(t, err, CodeInvalid)
	if se.Message != setupLinkInvalidMessage {
		t.Errorf("message = %q", se.Message)
	}
}

func TestSetupLinkCreatesWorkspace(t *testing.T) {
	s := newSetupLinkService(t)
	setSignup(t, s, signupClosed)
	link := testSetupLink(t, s, CreateSetupLinkRequest{Plan: "free", Region: "eu-west"})
	ctx := context.Background()

	if _, _, err := s.Signup(ctx, SignupRequest{Email: "ada@example.com", Password: testPassword, SetupToken: link.Token}); err != nil {
		t.Fatal(err)
	}
	ada := userCtx(t, s, "ada@example.com")
	org, err := s.CreateOrganization(ada, CreateOrganizationRequest{Name: "Ada Labs", SetupToken: link.Token})
	if err != nil {
		t.Fatal(err)
	}
	if org.Status != orgActive || org.Role != roleOwner || org.Region == nil ||
		org.Region.Slug != "eu-west" || org.Region.Plan.Slug != "free" {
		t.Errorf("workspace = %+v", org)
	}
	row, err := s.q.GetSetupLinkByTokenHash(ctx, hashSetupToken(link.Token))
	if err != nil {
		t.Fatal(err)
	}
	if !row.UsedAt.Valid || !row.OrgID.Valid || row.OrgID.String != org.ID {
		t.Errorf("used row = %+v", row)
	}
	links, err := s.ListSetupLinks(localCtx())
	if err != nil || len(links.Data) != 1 || links.Data[0].Status != setupLinkUsed {
		t.Fatalf("links = %+v, %v", links.Data, err)
	}
	if links.Data[0].Workspace == nil || links.Data[0].Workspace.Slug != org.Slug || links.Data[0].UsedAt == nil {
		t.Errorf("used link = %+v", links.Data[0])
	}
	subs, err := s.ListSubscriptions(ada, org.Slug)
	if err != nil || len(subs.Data) != 1 || subs.Data[0].Region.Slug != "eu-west" || subs.Data[0].Plan.Slug != "free" {
		t.Errorf("subscriptions = %+v, %v", subs.Data, err)
	}

	// The public page is gone after use, and the token cannot be reused.
	if _, err := s.SetupLinkInfo(ctx, link.Token); err != nil {
		wantCode(t, err, CodeNotFound)
	}
	_, err = s.CreateOrganization(ada, CreateOrganizationRequest{Slug: "again", Name: "Again", SetupToken: link.Token})
	wantCode(t, err, CodeInvalid)

	// An instance admin with a token gets the link's plan in its region.
	second := testSetupLink(t, s, CreateSetupLinkRequest{Plan: "free", Region: "eu-west"})
	adminOrg, err := s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "from-admin", Name: "From admin", SetupToken: second.Token})
	if err != nil {
		t.Fatal(err)
	}
	if adminOrg.Region == nil || adminOrg.Region.Slug != "eu-west" || adminOrg.Region.Plan.Slug != "free" {
		t.Errorf("admin workspace = %+v", adminOrg)
	}
}

func TestSetupLinkWorkspaceActiveInApprovalMode(t *testing.T) {
	s := newSetupLinkService(t)
	setSignup(t, s, signupApproval)
	link := testSetupLink(t, s, CreateSetupLinkRequest{Plan: "free", Region: "eu-west"})
	ctx := context.Background()

	if _, _, err := s.Signup(ctx, SignupRequest{Email: "ada@example.com", Password: testPassword, SetupToken: link.Token}); err != nil {
		t.Fatal(err)
	}
	org, err := s.CreateOrganization(userCtx(t, s, "ada@example.com"), CreateOrganizationRequest{Name: "Ada Labs", SetupToken: link.Token})
	if err != nil {
		t.Fatal(err)
	}
	if org.Status != orgActive || org.Region == nil || org.Region.Plan.Slug != "free" {
		t.Errorf("workspace = %+v", org)
	}
}

func TestSetupLinkExpiryAndRevoke(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	s := newSetupLinkService(t, WithClock(func() time.Time { return now }))
	setSignup(t, s, signupClosed)
	ctx := context.Background()
	createUser(t, s, "ada@example.com", false)

	link := testSetupLink(t, s, CreateSetupLinkRequest{Plan: "free", Region: "eu-west", ExpiresInHours: 1})
	now = now.Add(2 * time.Hour)
	if links, err := s.ListSetupLinks(localCtx()); err != nil || len(links.Data) != 1 || links.Data[0].Status != setupLinkExpired {
		t.Fatalf("links = %+v, %v", links.Data, err)
	}
	if _, err := s.SetupLinkInfo(ctx, link.Token); err != nil {
		wantCode(t, err, CodeNotFound)
	}
	_, _, err := s.Signup(ctx, SignupRequest{Email: "b@example.com", Password: testPassword, SetupToken: link.Token})
	wantCode(t, err, CodeInvalid)
	_, err = s.CreateOrganization(userCtx(t, s, "ada@example.com"), CreateOrganizationRequest{Name: "Ada Labs", SetupToken: link.Token})
	wantCode(t, err, CodeInvalid)

	// A revoked link's token is invalid too.
	revoked := testSetupLink(t, s, CreateSetupLinkRequest{Plan: "free", Region: "eu-west"})
	if _, err := s.RevokeSetupLink(localCtx(), revoked.ID); err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Signup(ctx, SignupRequest{Email: "c@example.com", Password: testPassword, SetupToken: revoked.Token})
	wantCode(t, err, CodeInvalid)

	if _, err := s.RevokeSetupLink(localCtx(), "missing"); err != nil {
		wantCode(t, err, CodeNotFound)
	}
}

func TestRevokingAUsedSetupLinkConflicts(t *testing.T) {
	s := newSetupLinkService(t)
	setSignup(t, s, signupOpen)
	link := testSetupLink(t, s, CreateSetupLinkRequest{Plan: "free", Region: "eu-west"})

	if _, _, err := s.Signup(context.Background(), SignupRequest{Email: "ada@example.com", Password: testPassword, SetupToken: link.Token}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOrganization(userCtx(t, s, "ada@example.com"), CreateOrganizationRequest{Name: "Ada Labs", SetupToken: link.Token}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RevokeSetupLink(localCtx(), link.ID); err != nil {
		wantCode(t, err, CodeConflict)
	}
}

func TestSetupLinkInfo(t *testing.T) {
	s := newSetupLinkService(t)
	link := testSetupLink(t, s, CreateSetupLinkRequest{Plan: "free", Region: "eu-west"})
	ctx := context.Background()

	info, err := s.SetupLinkInfo(ctx, link.Token)
	if err != nil {
		t.Fatal(err)
	}
	if info.InstanceName != "Lewbox" || info.Plan.Name != "Free" || info.Region.Name != "eu-west" || info.ExpiresAt.IsZero() {
		t.Errorf("info = %+v", info)
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"slug"`) || strings.Contains(string(data), link.Token) {
		t.Errorf("info leaks internals: %s", data)
	}

	for _, token := range []string{"", "not-a-token", setupLinkPrefix + strings.Repeat("a", setupLinkChars)} {
		if _, err := s.SetupLinkInfo(ctx, token); err != nil {
			wantCode(t, err, CodeNotFound)
		}
	}
}
