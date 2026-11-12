package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func intValue(n int64) *int64 { return &n }

func setSignup(t *testing.T, s *Service, mode string) {
	t.Helper()
	if _, err := s.UpdateSettings(localCtx(), UpdateSettingsRequest{Signup: &mode}); err != nil {
		t.Fatal(err)
	}
}

func TestSignupModes(t *testing.T) {
	s := newTestService(t)
	createUser(t, s, "root@example.com", true)

	// The migration seeds "closed".
	if _, _, err := s.Signup(context.Background(), SignupRequest{Email: "ada@example.com", Password: testPassword}); err != nil {
		wantCode(t, err, CodeForbidden)
	}

	setSignup(t, s, signupOpen)
	token, resp, err := s.Signup(context.Background(), SignupRequest{Email: "ada@example.com", Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	if !validSessionToken(token) || resp.User.Email != "ada@example.com" || resp.User.InstanceAdmin {
		t.Fatalf("signup = %q, %+v", token, resp)
	}
	p, err := s.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	userCtx := WithPrincipal(context.Background(), p)
	orgs, err := s.ListOrganizations(userCtx, ListOrganizationsRequest{})
	if err != nil || len(orgs.Data) != 1 {
		t.Fatalf("signup organizations = %+v, %v", orgs, err)
	}
	org := orgs.Data[0]
	suffix, ok := strings.CutPrefix(org.Slug, "ada-")
	if !ok || len(suffix) != signupSuffixLength ||
		org.Status != orgActive || org.Role != roleOwner {
		t.Errorf("signup organization = %+v", org)
	}
	// No default regions are set, so the new organization has no plan yet.
	if len(org.Regions) != 0 {
		t.Errorf("plan and regions = %+v", org.Regions)
	}

	// Approval mode creates a pending organization its owner cannot manage.
	setSignup(t, s, signupApproval)
	_, bobResp, err := s.Signup(context.Background(), SignupRequest{Email: "bob@example.com", Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	bobPrincipal, err := s.Authenticate(context.Background(), tokenOf(t, s, bobResp.User.Email))
	if err != nil {
		t.Fatal(err)
	}
	bobCtx := WithPrincipal(context.Background(), bobPrincipal)
	pending, err := s.ListOrganizations(bobCtx, ListOrganizationsRequest{})
	if err != nil || len(pending.Data) != 1 || pending.Data[0].Status != orgPending {
		t.Fatalf("pending organization = %+v, %v", pending, err)
	}
	if _, err := s.CreateAdminKey(bobCtx, pending.Data[0].Slug, CreateAdminKeyRequest{Name: "ci"}); err != nil {
		wantCode(t, err, CodeForbidden)
	}
	active := orgActive
	if approved, err := s.UpdateOrganization(localCtx(), pending.Data[0].Slug, UpdateOrganizationRequest{Status: &active}); err != nil || approved.Status != orgActive {
		t.Fatalf("approve = %+v, %v", approved, err)
	}

	setSignup(t, s, signupClosed)
	_, _, err = s.Signup(context.Background(), SignupRequest{Email: "eve@example.com", Password: testPassword})
	wantCode(t, err, CodeForbidden)
}

// tokenOf signs an account in and returns its session token.
func tokenOf(t *testing.T, s *Service, email string) string {
	t.Helper()
	resp, err := s.Login(context.Background(), LoginRequest{Email: email, Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	return resp.Token
}

func TestSignupSlugCollisions(t *testing.T) {
	s := newTestService(t)
	createUser(t, s, "root@example.com", true)
	setSignup(t, s, signupOpen)

	original := signupSuffix
	t.Cleanup(func() { signupSuffix = original })
	calls := 0
	signupSuffix = func() (string, error) {
		calls++
		if calls <= 2 {
			return "aaaaaa", nil
		}
		return "bbbbbb", nil
	}

	if _, _, err := s.Signup(context.Background(), SignupRequest{Email: "ada@one.test", Password: testPassword}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Signup(context.Background(), SignupRequest{Email: "ada@two.test", Password: testPassword}); err != nil {
		t.Fatal(err)
	}
	orgs, err := s.ListOrganizations(localCtx(), ListOrganizationsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	slugs := []string{}
	for _, org := range orgs.Data {
		slugs = append(slugs, org.Slug)
	}
	if len(slugs) != 2 || slugs[0] != "ada-aaaaaa" || slugs[1] != "ada-bbbbbb" {
		t.Errorf("slugs = %v", slugs)
	}
}

func TestSignupRollback(t *testing.T) {
	s := newTestService(t)
	createUser(t, s, "root@example.com", true)
	setSignup(t, s, signupOpen)

	original := signupSuffix
	t.Cleanup(func() { signupSuffix = original })
	signupSuffix = func() (string, error) { return "zzzzzz", nil }

	if _, _, err := s.Signup(context.Background(), SignupRequest{Email: "ada@one.test", Password: testPassword}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Signup(context.Background(), SignupRequest{Email: "ada@two.test", Password: testPassword}); err == nil {
		t.Fatal("a duplicate slug did not fail")
	}
	// The account and organization were rolled back, so the email is free.
	if _, err := s.q.GetUserByEmail(context.Background(), "ada@two.test"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("rolled-back user: %v", err)
	}
	signupSuffix = original
	if _, _, err := s.Signup(context.Background(), SignupRequest{Email: "ada@two.test", Password: testPassword}); err != nil {
		t.Errorf("the email could not be retried: %v", err)
	}
}

func TestSignupTakenEmail(t *testing.T) {
	s := newTestService(t)
	createUser(t, s, "root@example.com", true)
	createUser(t, s, "ada@example.com", false)
	setSignup(t, s, signupOpen)

	_, _, err := s.Signup(context.Background(), SignupRequest{Email: " Ada@Example.com ", Password: testPassword})
	se := wantCode(t, err, CodeConflict)
	if !strings.Contains(se.Message, "sign in instead") {
		t.Errorf("message = %q", se.Message)
	}
}

func TestSignupSubscribesDefaults(t *testing.T) {
	s := newClusterService(t)
	createUser(t, s, "root@example.com", true)
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	addTestCluster(t, s, fake, "eu-west")
	setSignup(t, s, signupOpen)

	plan := "free"
	regions := []string{"eu-west"}
	if _, err := s.UpdateSettings(localCtx(), UpdateSettingsRequest{DefaultPlan: &plan, DefaultRegions: &regions}); err != nil {
		t.Fatal(err)
	}
	token, _, err := s.Signup(context.Background(), SignupRequest{Email: "ada@example.com", Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	userCtx := WithPrincipal(context.Background(), p)
	orgs, err := s.ListOrganizations(userCtx, ListOrganizationsRequest{})
	if err != nil || len(orgs.Data) != 1 {
		t.Fatalf("signup organizations = %+v, %v", orgs, err)
	}
	org := orgs.Data[0]
	if len(org.Regions) != 1 || org.Regions[0].Slug != "eu-west" || org.Regions[0].Plan.Slug != "free" {
		t.Errorf("signup regions = %+v", org.Regions)
	}

	// With no default regions, the next customer has none.
	none := []string{}
	if _, err := s.UpdateSettings(localCtx(), UpdateSettingsRequest{DefaultRegions: &none}); err != nil {
		t.Fatal(err)
	}
	token, _, err = s.Signup(context.Background(), SignupRequest{Email: "bob@example.com", Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	orgs, err = s.ListOrganizations(WithPrincipal(context.Background(), p), ListOrganizationsRequest{})
	if err != nil || len(orgs.Data) != 1 || len(orgs.Data[0].Regions) != 0 {
		t.Errorf("second signup = %+v, %v", orgs, err)
	}
}

func TestPlanCRUD(t *testing.T) {
	s := newClusterService(t)
	createUser(t, s, "root@example.com", true)
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	addTestCluster(t, s, fake, "eu-west")

	plans, err := s.ListPlans(localCtx())
	if err != nil || len(plans.Data) != 1 {
		t.Fatalf("plans = %+v, %v", plans, err)
	}
	free := plans.Data[0]
	if free.Slug != "free" || !free.Default || free.Subscriptions != 0 || free.MaxSandboxes == nil || *free.MaxSandboxes != 2 {
		t.Errorf("free plan = %+v", free)
	}

	pro, err := s.CreatePlan(localCtx(), CreatePlanRequest{
		Slug:         "pro",
		Name:         "Pro",
		MaxSandboxes: intValue(10),
		MaxVcpus:     intValue(8),
		MaxMemoryMB:  intValue(16384),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetPlan(localCtx(), pro.ID); err != nil || got.Slug != "pro" {
		t.Fatalf("get by id = %+v, %v", got, err)
	}
	if _, err := s.CreatePlan(localCtx(), CreatePlanRequest{Slug: "pro"}); err != nil {
		wantCode(t, err, CodeConflict)
	}
	for _, bad := range []string{"", "Not_A_Slug", "0192f0c4-a3b2-7c6d-8e9f-0a1b2c3d4e5f"} {
		if _, err := s.CreatePlan(localCtx(), CreatePlanRequest{Slug: bad}); err != nil {
			wantCode(t, err, CodeInvalid)
		}
	}

	name := "Pro Plus"
	got, err := s.UpdatePlan(localCtx(), "pro", UpdatePlanRequest{
		Name:     &name,
		MaxVcpus: OptionalInt{Set: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Pro Plus" || got.MaxVcpus != nil || got.MaxSandboxes == nil || *got.MaxSandboxes != 10 {
		t.Errorf("updated plan = %+v", got)
	}
	if _, err := s.UpdatePlan(localCtx(), "pro", UpdatePlanRequest{}); err != nil {
		wantCode(t, err, CodeInvalid)
	}

	// A subscription on the plan keeps it alive.
	if _, err := s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "acme"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetSubscription(localCtx(), "acme", "eu-west", SetSubscriptionRequest{Plan: "pro"}); err != nil {
		t.Fatal(err)
	}
	plans, err = s.ListPlans(localCtx())
	if err != nil {
		t.Fatal(err)
	}
	for _, plan := range plans.Data {
		if plan.Slug == "pro" && plan.Subscriptions != 1 {
			t.Errorf("pro plan = %+v", plan)
		}
	}
	if _, err := s.DeletePlan(localCtx(), "pro"); err != nil {
		wantCode(t, err, CodeConflict)
	}
	if _, err := s.DeletePlan(localCtx(), "free"); err != nil {
		wantCode(t, err, CodeConflict)
	}
	if _, err := s.CreatePlan(localCtx(), CreatePlanRequest{Slug: "temp"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeletePlan(localCtx(), "temp"); err != nil {
		t.Errorf("delete unused plan: %v", err)
	}
}

func TestSubscriptionLimitsAndStatus(t *testing.T) {
	s := newClusterService(t)
	createUser(t, s, "root@example.com", true)
	createUser(t, s, "owner@example.com", false)
	ownerCtx := userCtx(t, s, "owner@example.com")
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	addTestCluster(t, s, fake, "eu-west")

	org, err := s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "acme", Owner: "owner@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if len(org.Regions) != 0 {
		t.Fatalf("organization = %+v", org)
	}

	// The operator assigns a plan and an override; clearing the override
	// falls back to the plan.
	sub, err := s.SetSubscription(localCtx(), "acme", "eu-west", SetSubscriptionRequest{
		Plan:         "free",
		MaxSandboxes: OptionalInt{Set: true, Value: intValue(10)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Plan.Slug != "free" || sub.Limits.Sandboxes == nil || *sub.Limits.Sandboxes != 10 || sub.Overrides == nil || sub.Overrides.Sandboxes == nil {
		t.Errorf("override = %+v", sub)
	}
	sub, err = s.SetSubscription(localCtx(), "acme", "eu-west", SetSubscriptionRequest{
		Plan:         "free",
		MaxSandboxes: OptionalInt{Set: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Limits.Sandboxes == nil || *sub.Limits.Sandboxes != 2 || sub.Overrides.Sandboxes != nil {
		t.Errorf("cleared override = %+v", sub)
	}
	if org, err := s.GetOrganization(localCtx(), "acme"); err != nil || len(org.Regions) != 1 || org.Regions[0].Plan.Slug != "free" {
		t.Fatalf("organization regions = %+v, %v", org, err)
	}

	if _, err := s.CreatePlan(localCtx(), CreatePlanRequest{Slug: "pro", MaxSandboxes: intValue(50)}); err != nil {
		t.Fatal(err)
	}
	sub, err = s.SetSubscription(localCtx(), "acme", "eu-west", SetSubscriptionRequest{Plan: "pro"})
	if err != nil || sub.Plan.Slug != "pro" || *sub.Limits.Sandboxes != 50 {
		t.Fatalf("plan change = %+v, %v", sub, err)
	}

	// Owners cannot change the plan in a region or the status.
	if _, err := s.SetSubscription(ownerCtx, "acme", "eu-west", SetSubscriptionRequest{Plan: "free"}); err != nil {
		wantCode(t, err, CodeForbidden)
	}
	if _, err := s.DeleteSubscription(ownerCtx, "acme", "eu-west", false); err != nil {
		wantCode(t, err, CodeForbidden)
	}

	report, err := s.Limits(localCtx(), "acme")
	if err != nil || len(report.Regions) != 1 || report.Regions[0].Plan.Slug != "pro" || *report.Regions[0].Limits.Sandboxes != 50 {
		t.Fatalf("limits = %+v, %v", report, err)
	}

	suspended := "suspended"
	updated, err := s.UpdateOrganization(localCtx(), "acme", UpdateOrganizationRequest{Status: &suspended})
	if err != nil || updated.Status != orgSuspended || updated.StatusReason != statusReasonAdmin {
		t.Fatalf("suspend = %+v, %v", updated, err)
	}
	active := "active"
	if updated, err = s.UpdateOrganization(localCtx(), "acme", UpdateOrganizationRequest{Status: &active}); err != nil || updated.StatusReason != "" {
		t.Fatalf("restore = %+v, %v", updated, err)
	}

	// Listing by status finds it.
	if list, err := s.ListOrganizations(localCtx(), ListOrganizationsRequest{Status: orgActive}); err != nil || len(list.Data) != 1 {
		t.Errorf("active list = %+v, %v", list, err)
	}
	if list, err := s.ListOrganizations(localCtx(), ListOrganizationsRequest{Status: orgSuspended}); err != nil || len(list.Data) != 0 {
		t.Errorf("suspended list = %+v, %v", list, err)
	}
	if _, err := s.ListOrganizations(localCtx(), ListOrganizationsRequest{Status: "gone"}); err != nil {
		wantCode(t, err, CodeInvalid)
	}
}

func TestCustomerRoutesNeedAnInstanceAdmin(t *testing.T) {
	s := newClusterService(t)
	createUser(t, s, "root@example.com", true)
	createUser(t, s, "plain@example.com", false)
	plainCtx := userCtx(t, s, "plain@example.com")

	for name, call := range map[string]func() error{
		"list clusters":   func() error { _, err := s.ListClusters(plainCtx); return err },
		"get cluster":     func() error { _, err := s.GetCluster(plainCtx, "eu-west"); return err },
		"resolve cluster": func() error { _, err := s.ResolveCluster(plainCtx, "eu-west"); return err },
		"list nodes":      func() error { _, err := s.ListNodes(plainCtx, ListNodesRequest{}); return err },
		"get node":        func() error { _, err := s.GetNode(plainCtx, "node-a"); return err },
		"list plans":      func() error { _, err := s.ListPlans(plainCtx); return err },
		"get plan":        func() error { _, err := s.GetPlan(plainCtx, "free"); return err },
		"create plan": func() error {
			_, err := s.CreatePlan(plainCtx, CreatePlanRequest{Slug: "x"})
			return err
		},
		"update plan": func() error {
			name := "x"
			_, err := s.UpdatePlan(plainCtx, "free", UpdatePlanRequest{Name: &name})
			return err
		},
		"delete plan":  func() error { _, err := s.DeletePlan(plainCtx, "free"); return err },
		"get settings": func() error { _, err := s.GetSettings(plainCtx); return err },
		"set settings": func() error {
			mode := signupOpen
			_, err := s.UpdateSettings(plainCtx, UpdateSettingsRequest{Signup: &mode})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			wantCode(t, call(), CodeForbidden)
		})
	}

	// Regions are scoped to an organization's subscriptions.
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	addTestCluster(t, s, fake, "eu-west")
	regions, err := s.ListRegions(plainCtx, "")
	if err != nil || len(regions.Data) != 0 {
		t.Fatalf("regions without an organization = %+v, %v", regions, err)
	}

	// An organization's API key sees its subscribed regions, with the plan.
	org, err := s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetSubscription(localCtx(), org.Slug, "eu-west", SetSubscriptionRequest{Plan: "free"}); err != nil {
		t.Fatal(err)
	}
	key, err := s.CreateAdminKey(localCtx(), org.Slug, CreateAdminKeyRequest{Name: "backend"})
	if err != nil {
		t.Fatal(err)
	}
	keyPrincipal, err := s.Authenticate(context.Background(), key.Token)
	if err != nil {
		t.Fatal(err)
	}
	keyCtx := WithPrincipal(context.Background(), keyPrincipal)
	if regions, err := s.ListRegions(keyCtx, ""); err != nil || len(regions.Data) != 1 {
		t.Errorf("regions with a key = %+v, %v", regions, err)
	} else if regions.Data[0].Plan == nil || regions.Data[0].Plan.Slug != "free" || regions.Data[0].Limits == nil {
		t.Errorf("region plan = %+v", regions.Data[0])
	}
}

func TestSignupHasItsOwnRateLimit(t *testing.T) {
	s := newTestServiceWith(t, WithLoginLimits(1, 0))
	createUser(t, s, "root@example.com", true)
	setSignup(t, s, signupOpen)
	ctx := WithClientIP(context.Background(), "203.0.113.7")

	for i := 0; i < signupIPAttemptsPerHour; i++ {
		email := fmt.Sprintf("user%d@example.com", i)
		if _, _, err := s.Signup(ctx, SignupRequest{Email: email, Password: testPassword}); err != nil {
			t.Fatalf("signup %d: %v", i, err)
		}
	}
	_, _, err := s.Signup(ctx, SignupRequest{Email: "one-too-many@example.com", Password: testPassword})
	se := wantCode(t, err, CodeRateLimited)
	if !strings.Contains(se.Message, "sign-up") {
		t.Errorf("message = %q", se.Message)
	}

	// Sign-up spam did not spend the sign-in budget.
	if _, err := s.Login(ctx, LoginRequest{Email: "user0@example.com", Password: testPassword}); err != nil {
		t.Errorf("sign-in after sign-up spam: %v", err)
	}
	// A failed sign-in did not spend the sign-up budget.
	spent := WithClientIP(context.Background(), "203.0.113.8")
	if _, err := s.Login(spent, LoginRequest{Email: "root@example.com", Password: "wrong-password-1"}); err != nil {
		wantCode(t, err, CodeUnauthorized)
	}
	if _, _, err := s.Signup(spent, SignupRequest{Email: "elsewhere@example.com", Password: testPassword}); err != nil {
		t.Errorf("signup after a failed sign-in: %v", err)
	}
}

func TestLimitsArePositive(t *testing.T) {
	s := newClusterService(t)
	createUser(t, s, "root@example.com", true)
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	addTestCluster(t, s, fake, "eu-west")
	zero, negative := int64(0), int64(-1)

	for _, n := range []*int64{&zero, &negative} {
		_, err := s.CreatePlan(localCtx(), CreatePlanRequest{Slug: "bad", MaxSandboxes: n})
		se := wantCode(t, err, CodeInvalid)
		if !strings.Contains(se.Message, "at least 1") || !strings.Contains(se.Message, "unlimited") {
			t.Errorf("plan message = %q", se.Message)
		}
	}
	if _, err := s.CreatePlan(localCtx(), CreatePlanRequest{Slug: "pro"}); err != nil {
		t.Fatal(err)
	}
	for _, n := range []*int64{&zero, &negative} {
		_, err := s.UpdatePlan(localCtx(), "pro", UpdatePlanRequest{MaxVcpus: OptionalInt{Set: true, Value: n}})
		se := wantCode(t, err, CodeInvalid)
		if !strings.Contains(se.Message, "at least 1") {
			t.Errorf("update plan message = %q", se.Message)
		}
	}
	if _, err := s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "acme"}); err != nil {
		t.Fatal(err)
	}
	for _, n := range []*int64{&zero, &negative} {
		_, err := s.SetSubscription(localCtx(), "acme", "eu-west", SetSubscriptionRequest{
			Plan:         "pro",
			MaxSandboxes: OptionalInt{Set: true, Value: n},
		})
		se := wantCode(t, err, CodeInvalid)
		if !strings.Contains(se.Message, "at least 1") || !strings.Contains(se.Message, "plan's limit") {
			t.Errorf("subscription message = %q", se.Message)
		}
	}
}

func TestDefaultPlanStoredAsID(t *testing.T) {
	s := newTestService(t)
	createUser(t, s, "root@example.com", true)
	pro, err := s.CreatePlan(localCtx(), CreatePlanRequest{Slug: "pro", Name: "Pro"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateSettings(localCtx(), UpdateSettingsRequest{DefaultPlan: &pro.Slug}); err != nil {
		t.Fatal(err)
	}

	var stored string
	rows, err := s.q.ListSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Key == "default_plan" {
			if err := json.Unmarshal([]byte(row.Value), &stored); err != nil {
				t.Fatal(err)
			}
		}
	}
	if stored != pro.ID {
		t.Errorf("stored default_plan = %q, want the plan ID %q", stored, pro.ID)
	}

	// Renaming the slug keeps it the default; no setting rewrite is needed.
	slug := "paid"
	updated, err := s.UpdatePlan(localCtx(), pro.ID, UpdatePlanRequest{Slug: &slug})
	if err != nil || !updated.Default {
		t.Fatalf("renamed plan = %+v, %v", updated, err)
	}
	settings, err := s.GetSettings(localCtx())
	if err != nil || settings.DefaultPlan == nil || *settings.DefaultPlan != "paid" {
		t.Errorf("settings default plan = %+v, %v", settings, err)
	}
}

func TestUnknownSignupModeFailsClosed(t *testing.T) {
	s := newTestService(t)
	createUser(t, s, "root@example.com", true)
	if err := s.setSetting(context.Background(), "signup", "wide-open"); err != nil {
		t.Fatal(err)
	}
	instance, err := s.Instance(context.Background())
	if err != nil || instance.Signup != signupClosed {
		t.Errorf("instance = %+v, %v", instance, err)
	}
	if _, _, err := s.Signup(context.Background(), SignupRequest{Email: "ada@example.com", Password: testPassword}); err != nil {
		wantCode(t, err, CodeForbidden)
	}
}

func TestUpdateSettingsValidatesBeforeWriting(t *testing.T) {
	s := newTestService(t)
	createUser(t, s, "root@example.com", true)
	name := "Original"
	if _, err := s.UpdateSettings(localCtx(), UpdateSettingsRequest{InstanceName: &name}); err != nil {
		t.Fatal(err)
	}
	changed, bad := "Changed", "sometimes"
	_, err := s.UpdateSettings(localCtx(), UpdateSettingsRequest{InstanceName: &changed, Signup: &bad})
	wantCode(t, err, CodeInvalid)
	_, err = s.UpdateSettings(localCtx(), UpdateSettingsRequest{InstanceName: &changed, DefaultPlan: strPtr("missing")})
	wantCode(t, err, CodeNotFound)
	settings, err := s.GetSettings(localCtx())
	if err != nil || settings.InstanceName != "Original" {
		t.Errorf("settings after a failed patch = %+v, %v", settings, err)
	}
}

func TestUpdateOrganizationValidatesBeforeWriting(t *testing.T) {
	s := newTestService(t)
	createUser(t, s, "root@example.com", true)
	if _, err := s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "acme", Name: "Acme"}); err != nil {
		t.Fatal(err)
	}
	name := "Changed"
	_, err := s.UpdateOrganization(localCtx(), "acme", UpdateOrganizationRequest{Name: &name, Status: strPtr("gone")})
	wantCode(t, err, CodeInvalid)
	org, err := s.GetOrganization(localCtx(), "acme")
	if err != nil || org.Name != "Acme" {
		t.Errorf("organization after a failed patch = %+v, %v", org, err)
	}
}

func TestDisplayNamesRejectControlCharacters(t *testing.T) {
	s := newTestService(t)
	createUser(t, s, "root@example.com", true)
	for _, name := range []string{"Acme\nInc", "Acme\u202eInc", "Acme\u2066Inc"} {
		_, err := s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "acme", Name: name})
		wantCode(t, err, CodeInvalid)
		_, err = s.CreatePlan(localCtx(), CreatePlanRequest{Slug: "pro", Name: name})
		wantCode(t, err, CodeInvalid)
		_, err = s.UpdateSettings(localCtx(), UpdateSettingsRequest{InstanceName: &name})
		wantCode(t, err, CodeInvalid)
	}
}

func TestOptionalIntJSON(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want string
	}{
		{UpdateOrganizationRequest{}, `{}`},
		{SetSubscriptionRequest{Plan: "pro", MaxSandboxes: OptionalInt{Set: true}}, `{"plan":"pro","max_sandboxes":null}`},
		{UpdatePlanRequest{MaxVcpus: OptionalInt{Set: true, Value: intValue(5)}}, `{"max_vcpus":5}`},
		{UpdatePlanRequest{Name: strPtr("Pro")}, `{"name":"Pro"}`},
	} {
		data, err := json.Marshal(tc.in)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != tc.want {
			t.Errorf("%T = %s, want %s", tc.in, data, tc.want)
		}
	}
}

func strPtr(s string) *string { return &s }

func TestDeletePlanGuardsAreInTheStatement(t *testing.T) {
	s := newClusterService(t)
	createUser(t, s, "root@example.com", true)
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	addTestCluster(t, s, fake, "eu-west")
	free, err := s.findPlan(localCtx(), "free")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.q.DeletePlan(context.Background(), free.ID); err != nil || n != 0 {
		t.Errorf("deleting the default plan: n=%d err=%v", n, err)
	}
	pro, err := s.CreatePlan(localCtx(), CreatePlanRequest{Slug: "pro"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "acme"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetSubscription(localCtx(), "acme", "eu-west", SetSubscriptionRequest{Plan: "pro"}); err != nil {
		t.Fatal(err)
	}
	if n, err := s.q.DeletePlan(context.Background(), pro.ID); err != nil || n != 0 {
		t.Errorf("deleting a used plan: n=%d err=%v", n, err)
	}
}
