package service

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSetMemberShares(t *testing.T) {
	f := newSandboxFixture(t)
	out, err := f.s.SetMemberShares(f.owner, "acme", "member@example.com", "eu-west", SetMemberSharesRequest{MaxSandboxes: intValue(1)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Email != "member@example.com" || out.Share.Sandboxes == nil || *out.Share.Sandboxes != 1 ||
		out.Explicit.Sandboxes == nil || *out.Explicit.Sandboxes != 1 || out.Blocked {
		t.Errorf("share = %+v", out)
	}
	pushes := f.fake.QuotaPushes(teamOf(t, f.s, "acme", "member@example.com"))
	if len(pushes) == 0 || pushes[len(pushes)-1].MaxSandboxes != 1 {
		t.Errorf("member quota = %+v", pushes)
	}

	// Someone else's explicit share may not push the total over the plan.
	_, err = f.s.SetMemberShares(f.owner, "acme", "admin@example.com", "eu-west", SetMemberSharesRequest{MaxSandboxes: intValue(2)})
	se := wantCode(t, err, CodeConflict)
	if !strings.Contains(se.Message, "shares add up to 3 sandboxes") || !strings.Contains(se.Message, "allows 2 sandboxes") {
		t.Errorf("message = %q", se.Message)
	}

	if _, err := f.s.SetMemberShares(f.owner, "acme", "viewer@example.com", "eu-west", SetMemberSharesRequest{MaxSandboxes: intValue(1)}); err != nil {
		wantCode(t, err, CodeInvalid)
	}
	if _, err := f.s.SetMemberShares(f.member, "acme", "admin@example.com", "eu-west", SetMemberSharesRequest{MaxSandboxes: intValue(1)}); err != nil {
		wantCode(t, err, CodeForbidden)
	}
}

func TestSetMemberSharesMemoryConflict(t *testing.T) {
	f := newSandboxFixture(t)
	if _, err := f.s.SetMemberShares(f.owner, "acme", "member@example.com", "eu-west", SetMemberSharesRequest{MaxMemoryMB: intValue(3072)}); err != nil {
		t.Fatal(err)
	}
	_, err := f.s.SetMemberShares(f.owner, "acme", "admin@example.com", "eu-west", SetMemberSharesRequest{MaxMemoryMB: intValue(2048)})
	se := wantCode(t, err, CodeConflict)
	if !strings.Contains(se.Message, "shares add up to 5 GB") || !strings.Contains(se.Message, "allows 4 GB") {
		t.Errorf("message = %q", se.Message)
	}
}

func TestMemberShareBlocksKeys(t *testing.T) {
	f := newSandboxFixture(t)
	// Two sandboxes between three non-viewers leaves the third with none.
	if _, err := f.s.CreateSandboxKey(f.member, "acme", CreateSandboxKeyRequest{Name: "mine"}); err != nil {
		se := wantCode(t, err, CodePrecondition)
		if !strings.Contains(se.Message, "no share of the plan in eu-west") {
			t.Errorf("message = %q", se.Message)
		}
	} else {
		t.Fatal("a member with no share created a key")
	}
	if _, err := f.s.SetMemberShares(f.owner, "acme", "member@example.com", "eu-west", SetMemberSharesRequest{MaxSandboxes: intValue(1)}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateSandboxKey(f.member, "acme", CreateSandboxKeyRequest{Name: "mine"}); err != nil {
		t.Fatalf("key after a share: %v", err)
	}
}

func TestKeyOwnership(t *testing.T) {
	f := newSandboxFixture(t)
	f.sandboxShares(t)

	if _, err := f.s.CreateSandboxKey(f.root, "acme", CreateSandboxKeyRequest{Name: "ops"}); err != nil {
		se := wantCode(t, err, CodeInvalid)
		if !strings.Contains(se.Message, "member") {
			t.Errorf("message = %q", se.Message)
		}
	} else {
		t.Fatal("an instance admin created a key without naming a member")
	}
	out, err := f.s.CreateSandboxKey(f.root, "acme", CreateSandboxKeyRequest{Name: "ops", Member: "member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Member == nil || out.Member.Email != "member@example.com" {
		t.Errorf("key member = %+v", out.Member)
	}
	key, ok := sandboxToken(f.fake, out.Token)
	if !ok || key.Team != teamOf(t, f.s, "acme", "member@example.com") {
		t.Errorf("upstream team = %+v", key)
	}

	if _, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "owner", Member: "member@example.com"}); err != nil {
		wantCode(t, err, CodeInvalid)
	}

	adminKey, err := f.s.CreateAdminKey(f.owner, "acme", CreateAdminKeyRequest{Name: "backend"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.s.Authenticate(context.Background(), adminKey.Token)
	if err != nil {
		t.Fatal(err)
	}
	keyCtx := WithPrincipal(context.Background(), p)
	if _, err := f.s.CreateSandboxKey(keyCtx, "", CreateSandboxKeyRequest{Name: "key"}); err != nil {
		wantCode(t, err, CodeInvalid)
	}
	if _, err := f.s.CreateSandboxKey(keyCtx, "", CreateSandboxKeyRequest{Name: "key", Member: "member@example.com"}); err != nil {
		t.Fatalf("API key key: %v", err)
	}
}

// sandboxShares gives every non-viewer room in the free plan, so key tests
// are not about shares.
func (f sandboxFixture) sandboxShares(t *testing.T) {
	t.Helper()
	if _, err := f.s.SetSubscription(f.root, "acme", "eu-west", SetSubscriptionRequest{
		Plan:         "free",
		MaxSandboxes: OptionalInt{Set: true, Value: intValue(3)},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMemberRemovalRevokesBlocksAndStops(t *testing.T) {
	f := newSandboxFixture(t)
	f.sandboxShares(t)
	out, err := f.s.CreateSandboxKey(f.member, "acme", CreateSandboxKeyRequest{Name: "mine", Region: "eu-west"})
	if err != nil {
		t.Fatal(err)
	}
	upstream, ok := sandboxToken(f.fake, out.Token)
	if !ok {
		t.Fatal("upstream token not found")
	}
	memberTeam := teamOf(t, f.s, "acme", "member@example.com")
	f.fake.AddSandbox(memberTeam, startedSandbox("sb-1", 1, 512))
	if err := f.s.Sync(localCtx()); err != nil {
		t.Fatal(err)
	}

	if _, err := f.s.RemoveMember(f.owner, "acme", "member@example.com"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range f.fake.Revoked() {
		found = found || id == upstream.ID
	}
	if !found {
		t.Errorf("revoked = %v, want %s", f.fake.Revoked(), upstream.ID)
	}
	pushes := f.fake.QuotaPushes(memberTeam)
	if len(pushes) == 0 || pushes[len(pushes)-1].MaxCPU != blockingCPU {
		t.Errorf("member quota after removal = %+v", pushes)
	}
	list, err := f.s.ListSandboxes(localCtx(), ListSandboxesRequest{Org: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || list.Data[0].Status != "stopped" {
		t.Errorf("sandbox after removal = %+v", list.Data)
	}
	if members, err := f.s.ListMembers(localCtx(), "acme"); err != nil {
		t.Fatal(err)
	} else {
		for _, m := range members.Data {
			if m.Email == "member@example.com" {
				t.Error("the member is still listed")
			}
		}
	}
}

func TestMemberRemovalFailsClosed(t *testing.T) {
	f := newSandboxFixture(t)
	f.sandboxShares(t)
	if _, err := f.s.CreateSandboxKey(f.member, "acme", CreateSandboxKeyRequest{Name: "mine", Region: "eu-west"}); err != nil {
		t.Fatal(err)
	}
	f.fake.Stop()
	if _, err := f.s.RemoveMember(f.owner, "acme", "member@example.com"); err != nil {
		wantCode(t, err, CodeUnavailable)
	} else {
		t.Fatal("a failed revoke removed the member")
	}
	members, err := f.s.ListMembers(localCtx(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range members.Data {
		found = found || m.Email == "member@example.com"
	}
	if !found {
		t.Error("the member was removed anyway")
	}
}

func TestMemberScalingWhenThePlanShrinks(t *testing.T) {
	f := newSandboxFixture(t)
	if _, err := f.s.CreatePlan(localCtx(), CreatePlanRequest{Slug: "wide", Name: "Wide", MaxSandboxes: intValue(4)}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SetSubscription(f.root, "acme", "eu-west", SetSubscriptionRequest{Plan: "wide"}); err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"member@example.com", "admin@example.com"} {
		if _, err := f.s.SetMemberShares(f.owner, "acme", email, "eu-west", SetMemberSharesRequest{MaxSandboxes: intValue(2)}); err != nil {
			t.Fatal(err)
		}
	}
	// The operator shrinks the plan below the explicit shares; they scale down
	// and everyone is blocked.
	if _, err := f.s.SetSubscription(f.root, "acme", "eu-west", SetSubscriptionRequest{
		Plan:         "wide",
		MaxSandboxes: OptionalInt{Set: true, Value: intValue(1)},
	}); err != nil {
		t.Fatal(err)
	}
	report, err := f.s.Limits(localCtx(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	blocked := 0
	for _, m := range report.Regions[0].Members {
		if m.Blocked {
			blocked++
		}
		if m.Email == "member@example.com" && (m.Share.Sandboxes == nil || *m.Share.Sandboxes != 0) {
			t.Errorf("scaled share = %+v", m)
		}
	}
	if blocked != 4 {
		t.Errorf("blocked = %d, members = %+v", blocked, report.Regions[0].Members)
	}
	// Lowering an explicit share is allowed even while the total is still
	// over, so an owner can dig out of a plan that shrank.
	if _, err := f.s.SetMemberShares(f.owner, "acme", "member@example.com", "eu-west", SetMemberSharesRequest{MaxSandboxes: intValue(1)}); err != nil {
		t.Fatalf("reducing a share: %v", err)
	}
}

func TestUsageAndSandboxesAreScopedToMembers(t *testing.T) {
	f := newSyncFixture(t)
	createUser(t, f.s, "member@example.com", false)
	if _, err := f.s.AddMember(localCtx(), "acme", AddMemberRequest{Email: "member@example.com", Role: roleMember}); err != nil {
		t.Fatal(err)
	}
	memberTeam := teamOf(t, f.s, "acme", "member@example.com")
	f.fake.AddSandbox(f.team, startedSandbox("sb-owner", 1, 512))
	f.fake.AddSandbox(memberTeam, startedSandbox("sb-member", 1, 512))
	f.clock.advance(15 * time.Second)
	f.sync(t)
	f.clock.advance(30 * time.Second)
	f.sync(t)

	list, err := f.s.ListSandboxes(localCtx(), ListSandboxesRequest{Org: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, sandbox := range list.Data {
		if sandbox.Member == nil {
			t.Errorf("sandbox %s has no member", sandbox.ID)
			continue
		}
		seen[sandbox.ID] = sandbox.Member.Email
	}
	if seen["sb-owner"] != "owner@example.com" || seen["sb-member"] != "member@example.com" {
		t.Errorf("members = %v", seen)
	}
	memberCtx := userCtx(t, f.s, "member@example.com")
	scoped, err := f.s.ListSandboxes(memberCtx, ListSandboxesRequest{Org: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped.Data) != 1 || scoped.Data[0].ID != "sb-member" {
		t.Errorf("member sandboxes = %+v", scoped.Data)
	}

	usage, err := f.s.Usage(userCtx(t, f.s, "owner@example.com"), UsageRequest{Org: "acme", Granularity: "hour"})
	if err != nil {
		t.Fatal(err)
	}
	last := usage.Data[len(usage.Data)-1]
	if usage.Totals.SandboxHours == 0 || len(last.Members) != 2 || last.SandboxHours == 0 {
		t.Errorf("owner usage totals = %+v, last bucket = %+v", usage.Totals, last)
	}
	memberUsage, err := f.s.Usage(memberCtx, UsageRequest{Org: "acme", Granularity: "hour"})
	if err != nil {
		t.Fatal(err)
	}
	memberLast := memberUsage.Data[len(memberUsage.Data)-1]
	if memberUsage.Totals.SandboxHours != 0.008 || len(memberLast.Members) != 0 || memberLast.SandboxHours != 0.008 {
		t.Errorf("member usage totals = %+v, last bucket = %+v", memberUsage.Totals, memberLast)
	}
	filtered, err := f.s.Usage(userCtx(t, f.s, "owner@example.com"), UsageRequest{Org: "acme", Granularity: "hour", Member: "member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Totals.SandboxHours != memberUsage.Totals.SandboxHours {
		t.Errorf("filtered usage = %+v", filtered.Totals)
	}
}

func TestMemberTeamAttribution(t *testing.T) {
	f := newSyncFixture(t)
	createUser(t, f.s, "member@example.com", false)
	if _, err := f.s.AddMember(localCtx(), "acme", AddMemberRequest{Email: "member@example.com", Role: roleMember}); err != nil {
		t.Fatal(err)
	}
	memberTeam := teamOf(t, f.s, "acme", "member@example.com")
	f.fake.AddSandbox(memberTeam, startedSandbox("sb-1", 1, 512))
	f.clock.advance(15 * time.Second)
	f.sync(t)
	list, err := f.s.ListSandboxes(localCtx(), ListSandboxesRequest{Org: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || list.Data[0].Organization == nil || list.Data[0].Organization.Slug != "acme" ||
		list.Data[0].Member == nil || list.Data[0].Member.Email != "member@example.com" {
		t.Errorf("attributed = %+v", list.Data)
	}
}
