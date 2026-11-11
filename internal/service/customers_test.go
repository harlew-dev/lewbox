package service

import (
	"context"
	"database/sql"
	"errors"
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
	if org.Plan == nil || org.Plan.Slug != "free" || org.Limits.Sandboxes == nil || *org.Limits.Sandboxes != 2 {
		t.Errorf("plan and limits = %+v, %+v", org.Plan, org.Limits)
	}
	// Members do not see the team or the raw overrides.
	if org.Team != "" || org.Overrides != nil {
		t.Errorf("member sees admin fields: %+v", org)
	}
	adminOrg, err := s.GetOrganization(localCtx(), org.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(adminOrg.Team, "lbx-") || adminOrg.Overrides == nil {
		t.Errorf("admin organization = %+v", adminOrg)
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

func TestPlanCRUD(t *testing.T) {
	s := newTestService(t)
	createUser(t, s, "root@example.com", true)

	plans, err := s.ListPlans(localCtx())
	if err != nil || len(plans.Data) != 1 {
		t.Fatalf("plans = %+v, %v", plans, err)
	}
	free := plans.Data[0]
	if free.Slug != "free" || !free.Default || free.Organizations != 0 || free.MaxSandboxes == nil || *free.MaxSandboxes != 2 {
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

	// An organization on the plan keeps it alive.
	if _, err := s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "acme", Plan: "pro"}); err != nil {
		t.Fatal(err)
	}
	plans, err = s.ListPlans(localCtx())
	if err != nil {
		t.Fatal(err)
	}
	for _, plan := range plans.Data {
		if plan.Slug == "pro" && plan.Organizations != 1 {
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

func TestOrganizationLimitsAndPlan(t *testing.T) {
	s := newTestService(t)
	createUser(t, s, "root@example.com", true)
	createUser(t, s, "owner@example.com", false)
	ownerCtx := userCtx(t, s, "owner@example.com")

	org, err := s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "acme", Owner: "owner@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if org.Plan == nil || org.Plan.Slug != "free" || org.Limits.Sandboxes == nil || *org.Limits.Sandboxes != 2 {
		t.Fatalf("organization = %+v", org)
	}

	// An override wins; clearing it falls back to the plan.
	updated, err := s.UpdateOrganization(localCtx(), "acme", UpdateOrganizationRequest{
		MaxSandboxes: OptionalInt{Set: true, Value: intValue(10)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Limits.Sandboxes == nil || *updated.Limits.Sandboxes != 10 || updated.Overrides == nil || updated.Overrides.Sandboxes == nil {
		t.Errorf("override = %+v", updated)
	}
	updated, err = s.UpdateOrganization(localCtx(), "acme", UpdateOrganizationRequest{
		MaxSandboxes: OptionalInt{Set: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Limits.Sandboxes == nil || *updated.Limits.Sandboxes != 2 || updated.Overrides.Sandboxes != nil {
		t.Errorf("cleared override = %+v", updated)
	}

	if _, err := s.CreatePlan(localCtx(), CreatePlanRequest{Slug: "pro", MaxSandboxes: intValue(50)}); err != nil {
		t.Fatal(err)
	}
	plan := "pro"
	updated, err = s.UpdateOrganization(localCtx(), "acme", UpdateOrganizationRequest{Plan: &plan})
	if err != nil || updated.Plan == nil || updated.Plan.Slug != "pro" || *updated.Limits.Sandboxes != 50 {
		t.Fatalf("plan change = %+v, %v", updated, err)
	}

	// Owners cannot change the plan, limits or status.
	if _, err := s.UpdateOrganization(ownerCtx, "acme", UpdateOrganizationRequest{Plan: &plan}); err != nil {
		wantCode(t, err, CodeForbidden)
	}
	if _, err := s.UpdateOrganization(ownerCtx, "acme", UpdateOrganizationRequest{
		MaxSandboxes: OptionalInt{Set: true, Value: intValue(99)},
	}); err != nil {
		wantCode(t, err, CodeForbidden)
	}

	suspended := "suspended"
	updated, err = s.UpdateOrganization(localCtx(), "acme", UpdateOrganizationRequest{Status: &suspended})
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

	// Regions are for customers, without the cluster's admin fields.
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	addTestCluster(t, s, fake, "eu-west")
	regions, err := s.ListRegions(plainCtx)
	if err != nil || len(regions.Data) != 1 {
		t.Fatalf("regions = %+v, %v", regions, err)
	}
	r := regions.Data[0]
	if r.Slug != "eu-west" || r.Status != "ready" || r.EndpointURL == "" || r.Runtime != "docker" {
		t.Errorf("region = %+v", r)
	}

	// An organization's API key sees them too.
	org, err := s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "acme"})
	if err != nil {
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
	if regions, err := s.ListRegions(keyCtx); err != nil || len(regions.Data) != 1 {
		t.Errorf("regions with a key = %+v, %v", regions, err)
	}
}
