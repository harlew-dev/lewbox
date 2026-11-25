package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

// limitWindowStart is a fixed time inside an aligned hour: 5:06:07 waits
// 53m53s for the 6:00:00 boundary.
var limitWindowStart = time.Date(2026, 11, 11, 5, 6, 7, 0, time.UTC)

const limitWindowRetry = 53*60 + 53

// TestLimitBudgetsMatchVocabulary checks every budget name follows the shared
// <feature>[:<flow>]:<counted-by> vocabulary.
func TestLimitBudgetsMatchVocabulary(t *testing.T) {
	re := regexp.MustCompile(`^[a-z-]+(:[a-z-]+)*:(per-[a-z]+|global)$`)
	for _, name := range rateLimitBudgets {
		if !re.MatchString(name) {
			t.Errorf("budget %q does not match the vocabulary", name)
		}
	}
}

func TestCreateLimitsDefaultsAndZero(t *testing.T) {
	s := newTestService(t)
	if s.workspaceCreates.limit != 5 || s.sandboxKeyCreates.limit != 100 || s.adminKeyCreates.limit != 100 {
		t.Errorf("defaults = %d, %d, %d, want 5, 100, 100",
			s.workspaceCreates.limit, s.sandboxKeyCreates.limit, s.adminKeyCreates.limit)
	}
	for _, l := range []*limiter{s.workspaceCreates, s.sandboxKeyCreates, s.adminKeyCreates} {
		if l.window != time.Hour {
			t.Errorf("window = %v, want 1h", l.window)
		}
	}

	// Zero disables a budget.
	off := newTestServiceWith(t, WithCreateLimits(0, 0, 0))
	ctx := WithPrincipal(context.Background(), Principal{Kind: "user", UserID: "user-1"})
	for i := 0; i < 10; i++ {
		if err := off.allowWorkspaceCreate(ctx, Principal{Kind: "user", UserID: "user-1"}); err != nil {
			t.Fatalf("workspace %d: %v", i, err)
		}
		if err := off.allowSandboxKeyCreate(ctx, "org-1"); err != nil {
			t.Fatalf("sandbox key %d: %v", i, err)
		}
		if err := off.allowAdminKeyCreate(ctx, "org-1"); err != nil {
			t.Fatalf("admin key %d: %v", i, err)
		}
	}
}

func TestWorkspaceCreateLimit(t *testing.T) {
	cur := limitWindowStart
	s := newTestServiceWith(t, WithClock(func() time.Time { return cur }), WithCreateLimits(2, 0, 0))
	createUser(t, s, "root@example.com", true)
	createUser(t, s, "ada@example.com", false)
	ada := userCtx(t, s, "ada@example.com")
	root := userCtx(t, s, "root@example.com")
	setSignup(t, s, signupOpen)

	for i := 0; i < 2; i++ {
		slug := fmt.Sprintf("ada-%d", i)
		if _, err := s.CreateOrganization(ada, CreateOrganizationRequest{Slug: slug, Name: slug}); err != nil {
			t.Fatalf("workspace %d: %v", i, err)
		}
	}
	se := wantCode(t, mustErr(s.CreateOrganization(ada, CreateOrganizationRequest{Slug: "ada-2", Name: "ada-2"})), CodeRateLimited)
	if secs := retryAfterDetail(t, se); secs != limitWindowRetry {
		t.Errorf("retry_after_seconds = %d, want %d", secs, limitWindowRetry)
	}
	if !strings.Contains(se.Message, "too many workspaces created") {
		t.Errorf("message = %q", se.Message)
	}
	if n := countEvents(t, s, "rate_limit.refused"); n != 1 {
		t.Fatalf("refusal events = %d, want 1", n)
	}
	list, err := s.ListEvents(localCtx(), ListEventsRequest{Action: "rate_limit.refused"})
	if err != nil {
		t.Fatal(err)
	}
	if got := list.Data[0]; got.TargetID != budgetWorkspaceCreate {
		t.Errorf("refusal event budget = %q", got.TargetID)
	}

	// Another account has its own budget.
	createUser(t, s, "bob@example.com", false)
	bob := userCtx(t, s, "bob@example.com")
	if _, err := s.CreateOrganization(bob, CreateOrganizationRequest{Slug: "bob", Name: "Bob"}); err != nil {
		t.Errorf("another account was refused: %v", err)
	}

	// Instance admins and local mode are exempt.
	for i := 0; i < 3; i++ {
		slug := fmt.Sprintf("root-%d", i)
		if _, err := s.CreateOrganization(root, CreateOrganizationRequest{Slug: slug, Name: slug}); err != nil {
			t.Errorf("admin workspace %d: %v", i, err)
		}
	}
	if _, err := s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "solo", Name: "Solo"}); err != nil {
		t.Errorf("local workspace: %v", err)
	}

	// Still refused just before the boundary, then allowed in the next hour.
	cur = time.Date(2026, 11, 11, 5, 59, 59, 0, time.UTC)
	wantCode(t, mustErr(s.CreateOrganization(ada, CreateOrganizationRequest{Slug: "ada-late", Name: "late"})), CodeRateLimited)
	if n := countEvents(t, s, "rate_limit.refused"); n != 1 {
		t.Errorf("a second refusal in the same window recorded another event: %d", n)
	}
	cur = time.Date(2026, 11, 11, 6, 0, 0, 0, time.UTC)
	if _, err := s.CreateOrganization(ada, CreateOrganizationRequest{Slug: "ada-3", Name: "ada-3"}); err != nil {
		t.Errorf("the budget did not recover: %v", err)
	}
}

func TestSandboxKeyCreateLimit(t *testing.T) {
	cur := limitWindowStart
	f := newSandboxFixture(t, WithClock(func() time.Time { return cur }), WithCreateLimits(0, 2, 0))
	for i := 0; i < 2; i++ {
		name := fmt.Sprintf("key-%d", i)
		if _, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: name}); err != nil {
			t.Fatalf("key %d: %v", i, err)
		}
	}
	before := len(f.fake.Tokens())
	se := wantCode(t, mustErr(f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "third"})), CodeRateLimited)
	if secs := retryAfterDetail(t, se); secs != limitWindowRetry {
		t.Errorf("retry_after_seconds = %d, want %d", secs, limitWindowRetry)
	}
	if !strings.Contains(se.Message, "too many sandbox keys created") {
		t.Errorf("message = %q", se.Message)
	}
	if len(f.fake.Tokens()) != before {
		t.Error("a refused key reached the region")
	}
	if n := countEvents(t, f.s, "rate_limit.refused"); n != 1 {
		t.Errorf("refusal events = %d, want 1", n)
	}

	// Instance admins are not exempt from this one.
	wantCode(t, mustErr(f.s.CreateSandboxKey(f.root, "acme", CreateSandboxKeyRequest{Name: "root"})), CodeRateLimited)

	// Another workspace has its own budget.
	other, err := f.s.CreateOrganization(f.root, CreateOrganizationRequest{Slug: "other", Name: "Other", Owner: "outsider@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SetSubscription(f.root, other.Slug, "eu-west", SetSubscriptionRequest{Plan: "free"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateSandboxKey(f.root, other.Slug, CreateSandboxKeyRequest{Name: "elsewhere"}); err != nil {
		t.Errorf("another workspace was refused: %v", err)
	}

	cur = time.Date(2026, 11, 11, 6, 0, 0, 0, time.UTC)
	if _, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "next-hour"}); err != nil {
		t.Errorf("the budget did not recover: %v", err)
	}
}

func TestAdminKeyCreateLimit(t *testing.T) {
	cur := limitWindowStart
	f := newOrgFixture(t, WithClock(func() time.Time { return cur }), WithCreateLimits(0, 0, 2))
	for i := 0; i < 2; i++ {
		name := fmt.Sprintf("key-%d", i)
		if _, err := f.s.CreateAdminKey(f.owner, "acme", CreateAdminKeyRequest{Name: name, CurrentPassword: testPassword}); err != nil {
			t.Fatalf("key %d: %v", i, err)
		}
	}
	se := wantCode(t, mustErr(f.s.CreateAdminKey(f.owner, "acme", CreateAdminKeyRequest{Name: "third", CurrentPassword: testPassword})), CodeRateLimited)
	if secs := retryAfterDetail(t, se); secs != limitWindowRetry {
		t.Errorf("retry_after_seconds = %d, want %d", secs, limitWindowRetry)
	}
	if !strings.Contains(se.Message, "too many admin keys created") {
		t.Errorf("message = %q", se.Message)
	}
	if n := countEvents(t, f.s, "rate_limit.refused"); n != 1 {
		t.Errorf("refusal events = %d, want 1", n)
	}

	// Instance admins are not exempt, and another workspace has its own budget.
	wantCode(t, mustErr(f.s.CreateAdminKey(f.root, "acme", CreateAdminKeyRequest{Name: "admin", CurrentPassword: testPassword})), CodeRateLimited)
	other, err := f.s.CreateOrganization(f.root, CreateOrganizationRequest{Slug: "other", Name: "Other", Owner: "admin@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateAdminKey(f.admin, other.Slug, CreateAdminKeyRequest{Name: "elsewhere", CurrentPassword: testPassword}); err != nil {
		t.Errorf("another workspace was refused: %v", err)
	}

	cur = time.Date(2026, 11, 11, 6, 0, 0, 0, time.UTC)
	if _, err := f.s.CreateAdminKey(f.owner, "acme", CreateAdminKeyRequest{Name: "next-hour", CurrentPassword: testPassword}); err != nil {
		t.Errorf("the budget did not recover: %v", err)
	}
}

// mustErr is a helper for the create calls whose success is not expected; the
// caller passes the result's error to wantCode.
func mustErr[T any](_ T, err error) error { return err }
