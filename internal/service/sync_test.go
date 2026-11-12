package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/aerolvm/aerolvmtest"
)

type testClock struct{ at time.Time }

func (c *testClock) advance(d time.Duration) { c.at = c.at.Add(d) }

// syncFixture is one service, one fake cluster and a clock tests control.
type syncFixture struct {
	s     *Service
	clock *testClock
	fake  *aerolvmtest.Server
	org   Organization
}

func newSyncFixture(t *testing.T) syncFixture {
	t.Helper()
	clock := &testClock{at: time.Date(2026, 11, 1, 10, 10, 0, 0, time.UTC)}
	s := newClusterService(t, WithClock(func() time.Time { return clock.at }))
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	addTestCluster(t, s, fake, "eu-west")
	org, err := s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetSubscription(localCtx(), org.Slug, "eu-west", SetSubscriptionRequest{Plan: "free"}); err != nil {
		t.Fatal(err)
	}
	return syncFixture{s: s, clock: clock, fake: fake, org: org}
}

func (f syncFixture) sync(t *testing.T) {
	t.Helper()
	if err := f.s.Sync(localCtx()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

func startedSandbox(id string, cpu float64, memoryMB int64) aerolvm.Sandbox {
	return aerolvm.Sandbox{
		ID:        id,
		Name:      id,
		Image:     "ubuntu:22.04",
		Status:    "started",
		Runtime:   "docker",
		CPU:       cpu,
		MemoryMB:  memoryMB,
		DiskGB:    10,
		CreatedAt: time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC),
	}
}

func TestSyncMirrorsPagedSandboxes(t *testing.T) {
	f := newSyncFixture(t)
	f.fake.SetPageLimit(2)
	for _, id := range []string{"sb-1", "sb-2"} {
		f.fake.AddSandbox(f.org.Team, startedSandbox(id, 1, 512))
	}
	f.fake.AddSandbox("unknown-team", startedSandbox("sb-3", 1, 512))
	f.fake.AddSandbox("", startedSandbox("sb-4", 1, 512))
	f.clock.advance(15 * time.Second)
	f.sync(t)

	list, err := f.s.ListSandboxes(localCtx(), ListSandboxesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 4 {
		t.Fatalf("mirrored %d sandboxes, want 4", len(list.Data))
	}
	byID := map[string]Sandbox{}
	for _, sandbox := range list.Data {
		byID[sandbox.ID] = sandbox
	}
	if byID["sb-1"].Region != "eu-west" || byID["sb-1"].Organization == nil || byID["sb-1"].Organization.Slug != "acme" {
		t.Errorf("attributed sandbox = %+v", byID["sb-1"])
	}
	if byID["sb-3"].Organization != nil {
		t.Errorf("an unknown team was attributed: %+v", byID["sb-3"].Organization)
	}
	if byID["sb-4"].Organization != nil {
		t.Errorf("an unowned sandbox was attributed: %+v", byID["sb-4"].Organization)
	}
}

func TestSyncPartialListKeepsRows(t *testing.T) {
	f := newSyncFixture(t)
	f.fake.AddSandbox(f.org.Team, startedSandbox("sb-1", 1, 512))
	f.fake.AddSandbox(f.org.Team, startedSandbox("sb-2", 1, 512))
	f.clock.advance(15 * time.Second)
	f.sync(t)

	client := aerolvm.New(f.fake.URL(), f.fake.AdminToken, aerolvm.WithRetry(0))
	if err := client.DeleteSandbox(context.Background(), "sb-2"); err != nil {
		t.Fatal(err)
	}
	f.clock.advance(15 * time.Second)
	f.fake.SetSandboxListPartial(true)
	f.sync(t)
	list, err := f.s.ListSandboxes(localCtx(), ListSandboxesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 2 {
		t.Fatalf("a partial list dropped rows: %d", len(list.Data))
	}

	f.clock.advance(15 * time.Second)
	f.fake.SetSandboxListPartial(false)
	f.sync(t)
	list, err = f.s.ListSandboxes(localCtx(), ListSandboxesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || list.Data[0].ID != "sb-1" {
		t.Fatalf("gone sandbox still listed: %+v", list.Data)
	}
}

func TestSyncMetersStartedSandboxes(t *testing.T) {
	f := newSyncFixture(t)
	f.fake.AddSandbox(f.org.Team, startedSandbox("sb-1", 1, 512))
	f.fake.AddSandbox(f.org.Team, startedSandbox("sb-2", 1, 512))
	f.fake.AddSandbox(f.org.Team, func() aerolvm.Sandbox {
		sandbox := startedSandbox("sb-3", 1, 512)
		sandbox.Status = "stopped"
		return sandbox
	}())

	f.clock.advance(15 * time.Second)
	f.sync(t) // records synced_at only
	f.clock.advance(30 * time.Second)
	f.sync(t)
	f.clock.advance(10 * time.Minute)
	f.sync(t) // the gap is capped at 2 × the 15s interval

	report, err := f.s.Usage(localCtx(), UsageRequest{Org: "acme", Granularity: "hour"})
	if err != nil {
		t.Fatal(err)
	}
	// Two started sandboxes, two metered passes of 30s each.
	if report.Totals.SandboxHours != 0.033 || report.Totals.VcpuHours != 0.033 {
		t.Errorf("totals = %+v", report.Totals)
	}
	if report.Totals.MemoryGiBHours != 0.017 {
		t.Errorf("memory = %v", report.Totals.MemoryGiBHours)
	}
	var peak int64
	for _, bucket := range report.Data {
		if bucket.PeakSandboxes > peak {
			peak = bucket.PeakSandboxes
		}
		if bucket.SandboxHours != 0 && !bucket.Start.Equal(time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)) {
			t.Errorf("usage bucketed at %v, want the start of the hour", bucket.Start)
		}
	}
	if peak != 2 {
		t.Errorf("peak = %d, want 2", peak)
	}
}

func TestUsageZeroFillsBuckets(t *testing.T) {
	f := newSyncFixture(t)
	f.fake.AddSandbox(f.org.Team, startedSandbox("sb-1", 2, 1024))
	f.clock.advance(15 * time.Second)
	f.sync(t)
	f.clock.advance(30 * time.Second)
	f.sync(t)

	from := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 11, 1, 23, 0, 0, 0, time.UTC)
	report, err := f.s.Usage(localCtx(), UsageRequest{From: from.Format(time.RFC3339), To: to.Format(time.RFC3339), Org: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Granularity != "day" || len(report.Data) != 3 {
		t.Fatalf("report = %+v", report)
	}
	if report.Data[0].SandboxHours != 0 || report.Data[0].Regions == nil {
		t.Errorf("a zero bucket = %+v", report.Data[0])
	}
	if report.Data[2].SandboxHours == 0 {
		t.Errorf("usage landed in the wrong bucket: %+v", report.Data[2])
	}
	if report.Totals.SandboxHours != 0.008 {
		t.Errorf("totals = %+v", report.Totals)
	}
	// Another organization sees nothing.
	other, err := f.s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "other"})
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := f.s.Usage(localCtx(), UsageRequest{From: from.Format(time.RFC3339), To: to.Format(time.RFC3339), Org: other.Slug})
	if err != nil {
		t.Fatal(err)
	}
	if scoped.Totals.SandboxHours != 0 {
		t.Errorf("another organization's usage = %+v", scoped.Totals)
	}
}

func TestQuotaPerSubscription(t *testing.T) {
	clock := &testClock{at: time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)}
	s := newClusterService(t, WithClock(func() time.Time { return clock.at }))
	first := newClusterFake(t)
	setMembers(first, "node-a")
	addTestCluster(t, s, first, "eu-west")
	second := newClusterFake(t)
	setMembers(second, "node-b")
	addTestCluster(t, s, second, "us-east")
	org, err := s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetSubscription(localCtx(), "acme", "eu-west", SetSubscriptionRequest{Plan: "free"}); err != nil {
		t.Fatal(err)
	}
	clock.advance(15 * time.Second)
	if err := s.Sync(localCtx()); err != nil {
		t.Fatal(err)
	}
	eu := first.QuotaPushes(org.Team)
	if len(eu) != 1 || eu[0].MaxSandboxes != 2 || eu[0].MaxCPU != 2 || eu[0].MaxMemoryMB != 4096 {
		t.Fatalf("eu pushes = %+v", eu)
	}
	// A region the organization never used is left alone.
	if us := second.QuotaPushes(org.Team); len(us) != 0 {
		t.Errorf("unused region pushes = %+v", us)
	}

	// A sandbox in an unsubscribed region gets the blocking quota.
	second.AddSandbox(org.Team, startedSandbox("sb-1", 1, 512))
	clock.advance(15 * time.Second)
	if err := s.Sync(localCtx()); err != nil {
		t.Fatal(err)
	}
	us := second.QuotaPushes(org.Team)
	if len(us) != 1 || us[0].MaxCPU != blockingCPU || us[0].MaxMemoryMB != blockingMemoryMB {
		t.Errorf("unsubscribed region pushes = %+v", us)
	}
	list, err := s.ListSandboxes(localCtx(), ListSandboxesRequest{Org: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || list.Data[0].Status != "stopped" {
		t.Errorf("unsubscribed sandbox = %+v", list.Data)
	}

	// A second pass with nothing changed does not push again.
	if err := s.Sync(localCtx()); err != nil {
		t.Fatal(err)
	}
	if got := len(first.QuotaPushes(org.Team)); got != 1 {
		t.Errorf("unchanged quota pushed %d times", got)
	}

	// An unlimited plan deletes the quota instead.
	if _, err := s.CreatePlan(localCtx(), CreatePlanRequest{Slug: "open", Name: "Open"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetSubscription(localCtx(), "acme", "eu-west", SetSubscriptionRequest{Plan: "open"}); err != nil {
		t.Fatal(err)
	}
	if got := first.QuotaDeletes(org.Team); got != 1 {
		t.Errorf("quota deletes = %d, want 1", got)
	}
}

func TestQuotaBlocksAndDeletes(t *testing.T) {
	f := newSyncFixture(t)
	// A suspended organization gets the blocking quota.
	suspended := orgSuspended
	if _, err := f.s.UpdateOrganization(localCtx(), "acme", UpdateOrganizationRequest{Status: &suspended}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Sync(localCtx()); err != nil {
		t.Fatal(err)
	}
	pushes := f.fake.QuotaPushes(f.org.Team)
	last := pushes[len(pushes)-1]
	if last.MaxCPU != blockingCPU || last.MaxMemoryMB != blockingMemoryMB {
		t.Fatalf("blocking pushes = %+v", pushes)
	}

	// An unlimited plan removes the quota instead.
	open, err := f.s.CreatePlan(localCtx(), CreatePlanRequest{Slug: "open", Name: "Open"})
	if err != nil {
		t.Fatal(err)
	}
	active := orgActive
	if _, err := f.s.UpdateOrganization(localCtx(), "acme", UpdateOrganizationRequest{Status: &active}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SetSubscription(localCtx(), "acme", "eu-west", SetSubscriptionRequest{Plan: open.Slug}); err != nil {
		t.Fatal(err)
	}
	if got := f.fake.QuotaDeletes(f.org.Team); got != 1 {
		t.Errorf("quota deletes = %d, want 1", got)
	}
}

func TestSuspendedOrganizationIsBlockedAndStopped(t *testing.T) {
	f := newSyncFixture(t)
	f.fake.AddSandbox(f.org.Team, startedSandbox("sb-1", 1, 512))
	f.clock.advance(15 * time.Second)
	f.sync(t)

	suspended := "suspended"
	if _, err := f.s.UpdateOrganization(localCtx(), "acme", UpdateOrganizationRequest{Status: &suspended}); err != nil {
		t.Fatal(err)
	}
	f.sync(t)
	pushes := f.fake.QuotaPushes(f.org.Team)
	last := pushes[len(pushes)-1]
	if last.MaxCPU != blockingCPU || last.MaxMemoryMB != blockingMemoryMB {
		t.Errorf("suspended quota = %+v", last)
	}
	list, err := f.s.ListSandboxes(localCtx(), ListSandboxesRequest{Org: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || list.Data[0].Status != "stopped" {
		t.Errorf("suspended sandbox = %+v", list.Data)
	}
}

func TestSandboxActionsAreScoped(t *testing.T) {
	f := newSyncFixture(t)
	createUser(t, f.s, "owner@example.com", false)
	createUser(t, f.s, "viewer@example.com", false)
	createUser(t, f.s, "outsider@example.com", false)
	if _, err := f.s.AddMember(localCtx(), "acme", AddMemberRequest{Email: "viewer@example.com", Role: roleViewer}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.AddMember(localCtx(), "acme", AddMemberRequest{Email: "owner@example.com", Role: roleOwner}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "other", Owner: "outsider@example.com"}); err != nil {
		t.Fatal(err)
	}
	f.fake.AddSandbox(f.org.Team, startedSandbox("sb-1", 1, 512))
	f.clock.advance(15 * time.Second)
	f.sync(t)

	viewer := userCtx(t, f.s, "viewer@example.com")
	if _, err := f.s.StopSandbox(viewer, "sb-1", RegionScope{Org: "acme"}); err == nil {
		t.Error("a viewer stopped a sandbox")
	} else {
		wantCode(t, err, CodeForbidden)
	}

	outsider := userCtx(t, f.s, "outsider@example.com")
	if _, err := f.s.StopSandbox(outsider, "sb-1", RegionScope{Org: "other"}); err == nil {
		t.Error("another organization stopped a sandbox")
	} else {
		wantCode(t, err, CodeNotFound)
	}

	owner := userCtx(t, f.s, "owner@example.com")
	stopped, err := f.s.StopSandbox(owner, "sb-1", RegionScope{Org: "acme", Region: "eu-west"})
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Status != "stopped" {
		t.Errorf("stopped = %+v", stopped)
	}
	if _, err := f.s.DeleteSandbox(owner, "sb-1", RegionScope{Org: "acme"}); err != nil {
		t.Fatal(err)
	}
	list, err := f.s.ListSandboxes(localCtx(), ListSandboxesRequest{Org: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 0 {
		t.Errorf("deleted sandbox still listed: %+v", list.Data)
	}
}

func TestLimitsHidesRegionErrorsFromCustomers(t *testing.T) {
	f := newSyncFixture(t)
	createUser(t, f.s, "owner@example.com", false)
	if _, err := f.s.AddMember(localCtx(), "acme", AddMemberRequest{Email: "owner@example.com", Role: roleOwner}); err != nil {
		t.Fatal(err)
	}
	f.fake.Stop()
	// A changed quota makes the sync record the push failure, which contains
	// the region's URL.
	if _, err := f.s.SetSubscription(localCtx(), "acme", "eu-west", SetSubscriptionRequest{
		Plan:         "free",
		MaxSandboxes: OptionalInt{Set: true, Value: intValue(1)},
	}); err != nil {
		t.Fatal(err)
	}

	admin, err := f.s.Limits(localCtx(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(admin.Regions) != 1 || !strings.Contains(admin.Regions[0].Error, f.fake.URL()) {
		t.Errorf("admin limits = %+v", admin.Regions)
	}
	customer, err := f.s.Limits(userCtx(t, f.s, "owner@example.com"), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(customer.Regions) != 1 || customer.Regions[0].Error == "" || strings.Contains(customer.Regions[0].Error, f.fake.URL()) {
		t.Errorf("customer limits = %+v", customer.Regions)
	}
}

func TestUsagePeakAndTimesAreMilliseconds(t *testing.T) {
	clock := &testClock{at: time.Date(2026, 11, 1, 10, 10, 0, 0, time.UTC)}
	s := newClusterService(t, WithClock(func() time.Time { return clock.at }))
	first := newClusterFake(t)
	setMembers(first, "node-a")
	addTestCluster(t, s, first, "eu-west")
	org, err := s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetSubscription(localCtx(), "acme", "eu-west", SetSubscriptionRequest{Plan: "free"}); err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		first.AddSandbox(org.Team, startedSandbox("eu-"+string(rune('a'+i)), 1, 512))
	}
	clock.advance(15 * time.Second)
	if err := s.Sync(localCtx()); err != nil {
		t.Fatal(err)
	}

	report, err := s.Usage(localCtx(), UsageRequest{
		From: "2026-11-01T10:00:00.123456789Z", To: "2026-11-01T11:00:00.987654321Z", Granularity: "hour", Org: "acme",
	})
	if err != nil {
		t.Fatal(err)
	}
	var peak int64
	for _, bucket := range report.Data {
		if bucket.PeakSandboxes > peak {
			peak = bucket.PeakSandboxes
		}
	}
	if peak != 5 {
		t.Errorf("peak = %d, want 5", peak)
	}
	if report.From.Nanosecond()%int(time.Millisecond) != 0 || report.To.Nanosecond()%int(time.Millisecond) != 0 {
		t.Errorf("from/to = %v, %v; want milliseconds", report.From, report.To)
	}
}

func TestSyncSkipsQuotaPushForAFailedCluster(t *testing.T) {
	f := newSyncFixture(t)
	f.clock.advance(11 * time.Minute) // old enough that a push would refresh
	before := len(f.fake.QuotaPushes(f.org.Team))
	f.fake.FailNext("/v1/cluster/sandbox-index", 500, `{"error":"boom"}`, 1)
	if err := f.s.Sync(localCtx()); err == nil {
		t.Fatal("Sync succeeded with a failed cluster")
	}
	if after := len(f.fake.QuotaPushes(f.org.Team)); after != before {
		t.Errorf("quota pushes = %d, want %d (the failed cluster is skipped)", after, before)
	}
}

func TestSandboxRegionIsScopedToSubscriptions(t *testing.T) {
	f := newSyncFixture(t)
	second := newClusterFake(t)
	setMembers(second, "node-b")
	addTestCluster(t, f.s, second, "us-east")
	f.fake.AddSandbox(f.org.Team, startedSandbox("sb-1", 1, 512))
	f.clock.advance(15 * time.Second)
	f.sync(t)

	_, err := f.s.StopSandbox(localCtx(), "sb-1", RegionScope{Org: "acme", Region: "us-east"})
	se := wantCode(t, err, CodeNotFound)
	if !strings.Contains(se.Message, "region") {
		t.Errorf("message = %q", se.Message)
	}
	_, err = f.s.ListSandboxes(localCtx(), ListSandboxesRequest{Org: "acme", Region: "us-east"})
	wantCode(t, err, CodeNotFound)
}
