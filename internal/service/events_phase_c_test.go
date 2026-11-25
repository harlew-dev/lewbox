package service

import (
	"context"
	"testing"
	"time"
)

func countEvents(t *testing.T, s *Service, action string) int {
	t.Helper()
	list, err := s.ListEvents(localCtx(), ListEventsRequest{Action: action})
	if err != nil {
		t.Fatalf("list events %s: %v", action, err)
	}
	return len(list.Data)
}

func TestSandboxEvents(t *testing.T) {
	f := newSyncFixture(t)
	f.fake.AddSandbox(f.org.Team, startedSandbox("sb-1", 1, 512))
	f.clock.advance(15 * time.Second)
	f.sync(t)

	if _, err := f.s.StopSandbox(localCtx(), "sb-1", RegionScope{Org: "acme"}); err != nil {
		t.Fatal(err)
	}
	stopped, ok := findEvent(t, f.s, "sandbox.stopped")
	if !ok || stopped.TargetID != "sb-1" || stopped.TargetName != "sb-1" {
		t.Fatalf("sandbox.stopped = %+v", stopped)
	}
	if stopped.OrganizationID != f.org.ID || stopped.OrganizationSlug != "acme" {
		t.Errorf("sandbox.stopped workspace = %q %q", stopped.OrganizationID, stopped.OrganizationSlug)
	}

	if _, err := f.s.DeleteSandbox(localCtx(), "sb-1", RegionScope{Org: "acme"}); err != nil {
		t.Fatal(err)
	}
	if deleted, ok := findEvent(t, f.s, "sandbox.deleted"); !ok || deleted.TargetID != "sb-1" {
		t.Errorf("sandbox.deleted = %+v", deleted)
	}
}

func TestSnapshotDeleteEvent(t *testing.T) {
	f := newSandboxFixture(t)
	pointAtFake(t, f)
	f.fake.AddSnapshot(f.org.Team, testSnapshot("ckpt-a"))

	if _, err := f.s.DeleteSnapshot(f.owner, "ckpt-a", RegionScope{Org: "acme", Region: "eu-west"}); err != nil {
		t.Fatal(err)
	}
	event, ok := findEvent(t, f.s, "snapshot.deleted")
	if !ok || event.TargetID != "ckpt-a" || event.OrganizationID != f.org.ID || event.OrganizationSlug != "acme" {
		t.Fatalf("snapshot.deleted = %+v", event)
	}
}

func TestArchiveEvents(t *testing.T) {
	f := newArchiveFixture(t)
	row := f.archivedRow(t)

	if _, err := f.s.restoreArchive(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	restored, ok := findEvent(t, f.s, "archive.restored")
	if !ok || restored.TargetID != row.ID || restored.TargetName != row.SandboxName {
		t.Fatalf("archive.restored = %+v", restored)
	}
	if restored.OrganizationID != f.org.ID {
		t.Errorf("archive.restored workspace = %q", restored.OrganizationID)
	}
	if change, ok := restored.Changes["sandbox"]; !ok || change.To == "" {
		t.Errorf("archive.restored changes = %+v", restored.Changes)
	}

	if err := f.s.deleteArchive(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	if deleted, ok := findEvent(t, f.s, "archive.deleted"); !ok || deleted.TargetID != row.ID {
		t.Errorf("archive.deleted = %+v", deleted)
	}
}

func TestLimiterReportsOnlyTheFirstRefusal(t *testing.T) {
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	l := newLoginLimiter(1, 0)
	if _, ok, first := l.allowIP(now, "192.0.2.1"); !ok || first {
		t.Fatal("the first attempt was refused or reported as a refusal")
	}
	if _, ok, first := l.allowIP(now, "192.0.2.1"); ok || !first {
		t.Fatal("the first refusal was not reported")
	}
	if _, ok, first := l.allowIP(now, "192.0.2.1"); ok || first {
		t.Fatal("a later refusal was reported again")
	}
	// A new aligned window reports its own first refusal.
	next := now.Add(time.Minute)
	if _, ok, _ := l.allowIP(next, "192.0.2.1"); !ok {
		t.Fatal("the new window's first attempt was refused")
	}
	if _, ok, first := l.allowIP(next, "192.0.2.1"); ok || !first {
		t.Fatal("the new window did not report its first refusal")
	}
}

func TestAPIBudgetRefusalRecordedOnce(t *testing.T) {
	clock := &testClock{at: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)}
	s := newTestServiceWith(t, WithClock(func() time.Time { return clock.at }), WithRequestBudget(1))
	ctx := WithClientIP(WithPrincipal(context.Background(), Principal{Kind: "user", UserID: "user-1"}), "203.0.113.9")

	if err := s.AllowRequest(ctx); err != nil {
		t.Fatal(err)
	}
	wantCode(t, s.AllowRequest(ctx), CodeRateLimited)
	wantCode(t, s.AllowRequest(ctx), CodeRateLimited)
	if n := countEvents(t, s, "rate_limit.refused"); n != 1 {
		t.Fatalf("events after a flood = %d, want 1", n)
	}
	list, err := s.ListEvents(localCtx(), ListEventsRequest{Action: "rate_limit.refused"})
	if err != nil {
		t.Fatal(err)
	}
	if got := list.Data[0]; got.TargetID != "api:per-user" || got.IP != "203.0.113.9" || got.ActorID != "user-1" {
		t.Errorf("refusal event = %+v", got)
	}

	// The next aligned window records its own first refusal.
	clock.advance(time.Minute)
	if err := s.AllowRequest(ctx); err != nil {
		t.Fatal(err)
	}
	wantCode(t, s.AllowRequest(ctx), CodeRateLimited)
	if n := countEvents(t, s, "rate_limit.refused"); n != 2 {
		t.Fatalf("events after a new window = %d, want 2", n)
	}
}

func TestLoginRefusalRecordedWithoutTheTypedEmail(t *testing.T) {
	s := newTestServiceWith(t, WithLoginLimits(1, 0))
	createUser(t, s, "member@example.test", false)
	ctx := WithClientIP(context.Background(), "198.51.100.9")

	// The first attempt spends the IP budget and fails on the password; the
	// next is refused by the limiter.
	if _, err := s.Login(ctx, LoginRequest{Email: "member@example.test", Password: "wrong-password"}); err == nil {
		t.Fatal("a wrong password signed in")
	}
	wantCode(t, func() error {
		_, err := s.Login(ctx, LoginRequest{Email: "member@example.test", Password: "wrong-password"})
		return err
	}(), CodeRateLimited)
	wantCode(t, func() error {
		_, err := s.Login(ctx, LoginRequest{Email: "member@example.test", Password: "wrong-password"})
		return err
	}(), CodeRateLimited)

	if n := countEvents(t, s, "rate_limit.refused"); n != 1 {
		t.Fatalf("rate-limit events = %d, want 1", n)
	}
	list, err := s.ListEvents(localCtx(), ListEventsRequest{Action: "rate_limit.refused"})
	if err != nil {
		t.Fatal(err)
	}
	got := list.Data[0]
	if got.TargetID != "auth:per-ip" || got.IP != "198.51.100.9" {
		t.Errorf("refusal event = %+v", got)
	}
}
