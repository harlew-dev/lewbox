package service

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/aerolvm/aerolvmtest"
	"github.com/harlew-dev/lewbox/internal/db"
)

type sandboxFixture struct {
	s                                            *Service
	fake                                         *aerolvmtest.Server
	org                                          Organization
	root, owner, admin, member, viewer, outsider context.Context
}

func newSandboxFixture(t *testing.T, extra ...Option) sandboxFixture {
	t.Helper()
	s := newClusterService(t, extra...)
	createUser(t, s, "root@example.com", true)
	for _, email := range []string{"owner@example.com", "admin@example.com", "member@example.com", "viewer@example.com", "outsider@example.com"} {
		createUser(t, s, email, false)
	}
	f := sandboxFixture{s: s, root: userCtx(t, s, "root@example.com")}
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
	f.outsider = userCtx(t, s, "outsider@example.com")

	f.fake = newClusterFake(t)
	setMembers(f.fake, "node-a")
	addTestCluster(t, s, f.fake, "eu-west")
	if _, err := s.SetSubscription(f.root, org.Slug, "eu-west", SetSubscriptionRequest{Plan: "free"}); err != nil {
		t.Fatal(err)
	}
	return f
}

func sandboxToken(fake *aerolvmtest.Server, value string) (aerolvm.Token, bool) {
	for _, token := range fake.Tokens() {
		if token.Token == value {
			return token, true
		}
	}
	return aerolvm.Token{}, false
}

func TestCreateSandboxKey(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	f := newSandboxFixture(t, WithClock(func() time.Time { return now }))
	s := f.s

	out, err := s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "laptop", Region: "eu-west", ExpiresIn: "30d"})
	if err != nil {
		t.Fatal(err)
	}
	token, ok := sandboxToken(f.fake, out.Token)
	if !ok {
		t.Fatalf("upstream token %q not found in %+v", out.Token, f.fake.Tokens())
	}
	if token.Role != "runner" || token.Team != f.org.Team || token.Name != "lbx."+out.ID {
		t.Errorf("upstream token = %+v", token)
	}
	if token.ExpiresAt == nil {
		t.Error("upstream token never expires")
	}
	if out.Name != "laptop" || out.CreatedBy != "owner@example.com" || out.ExpiresAt == nil || !out.ExpiresAt.Equal(now.Add(30*24*time.Hour)) {
		t.Errorf("key = %+v", out.SandboxKey)
	}
	if out.TokenHint != "avm_…"+last4(token.Token) || strings.Contains(out.TokenHint, token.Token) {
		t.Errorf("token hint = %q", out.TokenHint)
	}
	if out.Region.Slug != "eu-west" || out.Region.EndpointURL != f.fake.URL() || out.Region.Runtime != "docker" || out.Region.Status != "ready" {
		t.Errorf("region = %+v", out.Region)
	}
	if out.Env["SB_API_URL"] != f.fake.URL() || out.Env["SB_API_TOKEN"] != token.Token {
		t.Errorf("env = %+v", out.Env)
	}
	if out.E2BEnv["E2B_API_URL"] != f.fake.URL()+"/e2b" || out.E2BEnv["E2B_SANDBOX_URL"] != f.fake.URL()+"/e2b/runtime" || out.E2BEnv["E2B_API_KEY"] != token.Token {
		t.Errorf("e2b env = %+v", out.E2BEnv)
	}
	row, err := s.q.GetSandboxKeyByID(context.Background(), db.GetSandboxKeyByIDParams{OrgID: f.org.ID, ID: out.ID})
	if err != nil || row.SandboxKey.UpstreamID != token.ID || row.SandboxKey.Last4 != last4(token.Token) {
		t.Fatalf("stored key = %+v, %v", row, err)
	}

	// A live name is unique; the token minted for the failed second key is gone.
	before := len(f.fake.Tokens())
	_, err = s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "laptop"})
	wantCode(t, err, CodeConflict)
	if len(f.fake.Tokens()) != before {
		t.Errorf("a conflicting key left its token upstream: %+v", f.fake.Tokens())
	}
}

func TestCreateSandboxKeyNeverExpires(t *testing.T) {
	f := newSandboxFixture(t)
	out, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "forever", ExpiresIn: "never"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ExpiresAt != nil || out.Expired {
		t.Errorf("key = %+v", out.SandboxKey)
	}
	token, ok := sandboxToken(f.fake, out.Token)
	if !ok || token.ExpiresAt == nil || !token.ExpiresAt.After(time.Now().Add(30000*24*time.Hour)) {
		t.Errorf("upstream token = %+v", token)
	}
}

func TestSandboxKeyRegionDefault(t *testing.T) {
	f := newSandboxFixture(t)

	out, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "default"})
	if err != nil || out.Region.Slug != "eu-west" {
		t.Fatalf("default region key = %+v, %v", out, err)
	}

	second := newClusterFake(t)
	setMembers(second, "node-b")
	addTestCluster(t, f.s, second, "us-east")
	// A region without a subscription is refused, and does not become the
	// default.
	_, err = f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "two", Region: "us-east"})
	se := wantCode(t, err, CodePrecondition)
	if !strings.Contains(se.Message, "no plan in region") {
		t.Errorf("message = %q", se.Message)
	}
	if out, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "again", Region: "eu-west"}); err != nil || out.Region.Slug != "eu-west" {
		t.Errorf("default region key = %+v, %v", out, err)
	}

	// With two subscribed regions the caller must choose.
	if _, err := f.s.SetSubscription(f.root, "acme", "us-east", SetSubscriptionRequest{Plan: "free"}); err != nil {
		t.Fatal(err)
	}
	_, err = f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "two"})
	se = wantCode(t, err, CodeInvalid)
	if !strings.Contains(se.Message, "--region") {
		t.Errorf("message = %q", se.Message)
	}
	if out, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "two", Region: "us-east"}); err != nil || out.Region.Slug != "us-east" {
		t.Errorf("chosen region key = %+v, %v", out, err)
	}
}

func TestSandboxKeyLifetimeCap(t *testing.T) {
	f := newSandboxFixture(t)
	f.fake.SetTokenTTLCap(365 * 24 * time.Hour)
	before := len(f.fake.Tokens())
	_, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "forever", ExpiresIn: "never"})
	se := wantCode(t, err, CodeInvalid)
	if !strings.Contains(se.Message, "SB_TOKEN_MAX_TTL") {
		t.Errorf("message = %q", se.Message)
	}
	if len(f.fake.Tokens()) != before {
		t.Error("a refused lifetime left a token upstream")
	}
}

func TestSandboxKeyOrganizationStatus(t *testing.T) {
	f := newSandboxFixture(t)

	suspended := orgSuspended
	if _, err := f.s.UpdateOrganization(f.root, "acme", UpdateOrganizationRequest{Status: &suspended}); err != nil {
		t.Fatal(err)
	}
	before := len(f.fake.Tokens())
	_, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "blocked"})
	se := wantCode(t, err, CodePrecondition)
	if !strings.Contains(se.Message, "suspended") {
		t.Errorf("message = %q", se.Message)
	}
	if len(f.fake.Tokens()) != before {
		t.Error("a suspended organization minted a token")
	}

	// A pending sign-up gets its own organization.
	setSignup(t, f.s, signupApproval)
	token, _, err := f.s.Signup(context.Background(), SignupRequest{Email: "waiting@example.com", Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.s.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	pendingCtx := WithPrincipal(context.Background(), p)
	orgs, err := f.s.ListOrganizations(pendingCtx, ListOrganizationsRequest{})
	if err != nil || len(orgs.Data) != 1 {
		t.Fatalf("pending organizations = %+v, %v", orgs, err)
	}
	_, err = f.s.CreateSandboxKey(pendingCtx, orgs.Data[0].Slug, CreateSandboxKeyRequest{Name: "waiting"})
	se = wantCode(t, err, CodePrecondition)
	if !strings.Contains(se.Message, "waiting for approval") {
		t.Errorf("message = %q", se.Message)
	}
}

func TestSandboxKeyAccess(t *testing.T) {
	f := newSandboxFixture(t)
	s := f.s

	if _, err := s.ListSandboxKeys(f.viewer, "acme"); err != nil {
		t.Errorf("viewer list: %v", err)
	}
	_, err := s.CreateSandboxKey(f.viewer, "acme", CreateSandboxKeyRequest{Name: "no"})
	wantCode(t, err, CodeForbidden)
	_, err = s.RevokeSandboxKey(f.viewer, "acme", "no")
	wantCode(t, err, CodeForbidden)

	// A member creates its own key and may not revoke someone else's.
	if _, err := s.CreateSandboxKey(f.member, "acme", CreateSandboxKeyRequest{Name: "mine"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "shared"}); err != nil {
		t.Fatal(err)
	}
	_, err = s.RevokeSandboxKey(f.member, "acme", "shared")
	wantCode(t, err, CodeForbidden)
	if _, err := s.RevokeSandboxKey(f.member, "acme", "mine"); err != nil {
		t.Fatalf("member revokes its own key: %v", err)
	}

	// The organization's API key uses the same routes.
	key, err := s.CreateAdminKey(f.owner, "acme", CreateAdminKeyRequest{Name: "backend"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Authenticate(context.Background(), key.Token)
	if err != nil {
		t.Fatal(err)
	}
	keyCtx := WithPrincipal(context.Background(), p)
	if _, err := s.CreateSandboxKey(keyCtx, "", CreateSandboxKeyRequest{Name: "agent"}); err != nil {
		t.Fatalf("admin key creates a sandbox key: %v", err)
	}
	if list, err := s.ListSandboxKeys(keyCtx, ""); err != nil || len(list.Data) != 3 {
		t.Fatalf("admin key lists sandbox keys = %+v, %v", list, err)
	}
	if _, err := s.RevokeSandboxKey(keyCtx, "", "shared"); err != nil {
		t.Fatalf("admin key revokes a sandbox key: %v", err)
	}
}

func TestSandboxKeyListOrder(t *testing.T) {
	f := newSandboxFixture(t)
	for _, name := range []string{"first", "second"} {
		if _, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.s.RevokeSandboxKey(f.owner, "acme", "first"); err != nil {
		t.Fatal(err)
	}
	list, err := f.s.ListSandboxKeys(f.viewer, "acme")
	if err != nil || len(list.Data) != 2 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if list.Data[0].Name != "second" || list.Data[0].RevokedAt != nil || list.Data[1].Name != "first" || list.Data[1].RevokedAt == nil {
		t.Errorf("order = %+v", list.Data)
	}
}

func TestSandboxKeyUpstreamFailure(t *testing.T) {
	f := newSandboxFixture(t)
	f.fake.FailNext("/v1/tokens", 500, `{"error":"boom"}`, 1)
	before := len(f.fake.Tokens())
	_, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "laptop"})
	wantCode(t, err, CodeUnavailable)
	if len(f.fake.Tokens()) != before {
		t.Errorf("failed mint left a token: %+v", f.fake.Tokens())
	}
	list, err := f.s.ListSandboxKeys(f.owner, "acme")
	if err != nil || len(list.Data) != 0 {
		t.Errorf("list after failure = %+v, %v", list, err)
	}
}

func TestSandboxKeyRevokeFailureKeepsItLive(t *testing.T) {
	f := newSandboxFixture(t)
	if _, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "laptop"}); err != nil {
		t.Fatal(err)
	}
	f.fake.Stop()
	_, err := f.s.RevokeSandboxKey(f.owner, "acme", "laptop")
	wantCode(t, err, CodeUnavailable)
	list, err := f.s.ListSandboxKeys(f.owner, "acme")
	if err != nil || len(list.Data) != 1 || list.Data[0].RevokedAt != nil {
		t.Fatalf("key after failed revoke = %+v, %v", list, err)
	}
}

func TestSandboxKeyRecoversFrom503Then409(t *testing.T) {
	original := sandboxKeyID
	t.Cleanup(func() { sandboxKeyID = original })
	const id = "0192f0c4-a3b2-7c6d-8e9f-0a1b2c3d4e5f"
	sandboxKeyID = func() string { return id }

	f := newSandboxFixture(t, WithAerolVMOptions(
		aerolvm.WithRetry(time.Second),
		aerolvm.WithSleep(func(time.Duration) {}),
	))
	client := aerolvm.New(f.fake.URL(), f.fake.AdminToken, aerolvm.WithRetry(0))
	seeded, err := client.CreateToken(context.Background(), aerolvm.CreateTokenRequest{
		Name: "lbx." + id, Role: "runner", Team: f.org.Team, ExpiresIn: "90d",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.fake.FailNext("/v1/tokens", 503, `{"error":"no raft leader"}`, 1)

	out, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "laptop"})
	if err != nil {
		t.Fatalf("create after 503 then 409: %v", err)
	}
	found := false
	for _, revoked := range f.fake.Revoked() {
		if revoked == seeded.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("revoked = %v, want the seeded %s", f.fake.Revoked(), seeded.ID)
	}
	if out.Token == seeded.Token {
		t.Error("the stale token was returned")
	}
	if tokens := f.fake.Tokens(); len(tokens) != 1 || tokens[0].Token != out.Token {
		t.Errorf("tokens after recovery = %+v", tokens)
	}
}

func TestRemoveClusterRevokesSandboxKeys(t *testing.T) {
	f := newSandboxFixture(t)
	out, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "laptop"})
	if err != nil {
		t.Fatal(err)
	}
	upstream, ok := sandboxToken(f.fake, out.Token)
	if !ok {
		t.Fatal("upstream token not found")
	}
	if _, err := f.s.RemoveCluster(f.root, "eu-west", false); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, revoked := range f.fake.Revoked() {
		if revoked == upstream.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("revoked = %v, want %s", f.fake.Revoked(), upstream.ID)
	}
	list, err := f.s.ListSandboxKeys(f.owner, "acme")
	if err != nil || len(list.Data) != 0 {
		t.Errorf("keys after cluster removal = %+v, %v", list, err)
	}
}

func TestSandboxKeyAnotherOrganization(t *testing.T) {
	f := newSandboxFixture(t)
	if _, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "laptop"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateOrganization(f.root, CreateOrganizationRequest{Slug: "other", Owner: "outsider@example.com"}); err != nil {
		t.Fatal(err)
	}
	_, err := f.s.ListSandboxKeys(f.outsider, "acme")
	wantCode(t, err, CodeNotFound)
	_, err = f.s.RevokeSandboxKey(f.outsider, "acme", "laptop")
	wantCode(t, err, CodeNotFound)
}

func TestSandboxKeyErrorsDoNotLeakTheRegionURL(t *testing.T) {
	f := newSandboxFixture(t)
	f.fake.Stop()
	_, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "laptop"})
	se := wantCode(t, err, CodeUnavailable)
	if strings.Contains(se.Message, f.fake.URL()) || !strings.Contains(se.Message, "region") {
		t.Errorf("customer message = %q", se.Message)
	}

	// Instance admins get the detail.
	_, err = f.s.CreateSandboxKey(f.root, "acme", CreateSandboxKeyRequest{Name: "laptop"})
	se = wantCode(t, err, CodeUnavailable)
	if !strings.Contains(se.Message, "eu-west") {
		t.Errorf("admin message = %q", se.Message)
	}
}

func TestSandboxKeyKeepsAerolVM4xxText(t *testing.T) {
	f := newSandboxFixture(t)
	f.fake.FailNext("/v1/tokens", http.StatusTooManyRequests, `{"error":"slow down"}`, 1)
	_, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "laptop", ExpiresIn: "30d"})
	se := wantCode(t, err, CodeUnavailable)
	if !strings.Contains(se.Message, "slow down") || strings.Contains(se.Message, "SB_TOKEN_MAX_TTL") {
		t.Errorf("message = %q", se.Message)
	}
	if strings.Contains(se.Message, f.fake.URL()) {
		t.Errorf("message contains the region URL: %q", se.Message)
	}
}

func TestSandboxLifetimeIsCanonical(t *testing.T) {
	for _, tc := range []struct {
		raw       string
		stored    time.Duration
		upstream  string
		requested time.Duration
	}{
		{"", defaultSandboxKeyLifetime, "2160h", defaultSandboxKeyLifetime},
		{"90m", 90 * time.Minute, "2h", 90 * time.Minute},
		{"30d", 30 * 24 * time.Hour, "720h", 30 * 24 * time.Hour},
		{"never", 0, "36500d", 36500 * 24 * time.Hour},
	} {
		stored, upstream, requested, err := sandboxLifetime(tc.raw)
		if err != nil {
			t.Fatalf("%q: %v", tc.raw, err)
		}
		if stored != tc.stored || upstream != tc.upstream || requested != tc.requested {
			t.Errorf("%q = %v, %q, %v; want %v, %q, %v", tc.raw, stored, upstream, requested, tc.stored, tc.upstream, tc.requested)
		}
	}
}

func TestRemoveClusterFailsClosedOnLiveKeys(t *testing.T) {
	f := newSandboxFixture(t)
	if _, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "laptop"}); err != nil {
		t.Fatal(err)
	}
	f.fake.Stop()
	_, err := f.s.RemoveCluster(f.root, "eu-west", false)
	se := wantCode(t, err, CodeUnavailable)
	if !strings.Contains(se.Message, "could not revoke") || !strings.Contains(se.Message, "force") {
		t.Errorf("message = %q", se.Message)
	}
	if _, err := f.s.GetCluster(f.root, "eu-west"); err != nil {
		t.Errorf("the cluster was removed anyway: %v", err)
	}
	if _, err := f.s.RemoveCluster(f.root, "eu-west", true); err != nil {
		t.Errorf("forced removal: %v", err)
	}
}

func TestSandboxKeyCreatorIsHidden(t *testing.T) {
	f := newSandboxFixture(t)
	out, err := f.s.CreateSandboxKey(f.root, "acme", CreateSandboxKeyRequest{Name: "ops"})
	if err != nil {
		t.Fatal(err)
	}
	if out.CreatedBy != "root@example.com" {
		t.Errorf("instance admin sees its own address: %q", out.CreatedBy)
	}
	if _, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "mine"}); err != nil {
		t.Fatal(err)
	}
	list, err := f.s.ListSandboxKeys(f.owner, "acme")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range list.Data {
		want := "owner@example.com"
		if key.Name == "ops" {
			want = "operator"
		}
		if key.CreatedBy != want {
			t.Errorf("key %q created_by = %q, want %q", key.Name, key.CreatedBy, want)
		}
	}
}
