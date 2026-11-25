package service

import (
	"context"
	"strings"
	"testing"
	"time"
)

func strptr(s string) *string { return &s }

// eventNames returns the event names in a list, newest first.
func eventNames(list EventList) []string {
	out := make([]string, 0, len(list.Data))
	for _, e := range list.Data {
		out = append(out, e.Name)
	}
	return out
}

func TestUpdatePlanRecordsOneEvent(t *testing.T) {
	s := newTestService(t)
	plan, err := s.CreatePlan(localCtx(), CreatePlanRequest{Slug: "starter"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithRequestID(WithClientIP(localCtx(), "203.0.113.7"), "req_test")
	if _, err := s.UpdatePlan(ctx, plan.Slug, UpdatePlanRequest{Name: strptr("Starter plan")}); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListEvents(localCtx(), ListEventsRequest{Action: "plan.updated"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 {
		t.Fatalf("events = %v", eventNames(list))
	}
	e := list.Data[0]
	if e.TargetType != "plan" || e.TargetID != plan.ID || e.TargetName != "starter" {
		t.Errorf("target = %+v", e)
	}
	if e.ActorKind != "local" || e.ActorLabel != "local" {
		t.Errorf("actor = %+v", e)
	}
	if e.IP != "203.0.113.7" || e.RequestID != "req_test" {
		t.Errorf("ip/request = %q %q", e.IP, e.RequestID)
	}
	change, ok := e.Changes["name"]
	if !ok || change.From != "starter" || change.To != "Starter plan" {
		t.Errorf("changes = %+v", e.Changes)
	}
	if _, ok := e.Changes["slug"]; ok {
		t.Errorf("unchanged slug recorded: %+v", e.Changes)
	}
}

func TestUpdatePlanThatChangesNothingRecordsNoEvent(t *testing.T) {
	s := newTestService(t)
	plan, err := s.CreatePlan(localCtx(), CreatePlanRequest{Slug: "starter"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePlan(localCtx(), plan.Slug, UpdatePlanRequest{Name: strptr(plan.Name)}); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListEvents(localCtx(), ListEventsRequest{Action: "plan.updated"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 0 {
		t.Fatalf("events = %v", eventNames(list))
	}
}

func TestRefusedChangeRecordsNoEvent(t *testing.T) {
	s := newTestService(t)
	plan, err := s.CreatePlan(localCtx(), CreatePlanRequest{Slug: "starter"})
	if err != nil {
		t.Fatal(err)
	}
	user := createUser(t, s, "member@example.test", false)
	ctx := WithPrincipal(context.Background(), Principal{Kind: "user", UserID: user.ID, Email: user.Email})
	if _, err := s.UpdatePlan(ctx, plan.Slug, UpdatePlanRequest{Name: strptr("Nope")}); err == nil {
		t.Fatal("a plain user updated a plan")
	}
	list, err := s.ListEvents(localCtx(), ListEventsRequest{Action: "plan.updated"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 0 {
		t.Fatalf("events = %v", eventNames(list))
	}
}

func TestEventInsertFailureRollsTheChangeBack(t *testing.T) {
	s := newTestService(t)
	plan, err := s.CreatePlan(localCtx(), CreatePlanRequest{Slug: "starter"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.database.ExecContext(context.Background(), `DROP TABLE events`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePlan(localCtx(), plan.Slug, UpdatePlanRequest{Name: strptr("Renamed")}); err == nil {
		t.Fatal("update succeeded without an audit table")
	}
	got, err := s.GetPlan(localCtx(), plan.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != plan.Name {
		t.Fatalf("plan name = %q, want %q: the change was not rolled back", got.Name, plan.Name)
	}
}

func TestFailedSignInRecordsEveryAttempt(t *testing.T) {
	s := newTestService(t)
	user := createUser(t, s, "known@example.test", false)
	ctx := WithClientIP(context.Background(), "198.51.100.9")

	if _, err := s.Login(ctx, LoginRequest{Email: user.Email, Password: "wrong-password"}); err == nil {
		t.Fatal("wrong password signed in")
	}
	// An unknown address records the attempt too, and stores no address.
	if _, err := s.Login(ctx, LoginRequest{Email: "nobody@example.test", Password: "wrong-password"}); err == nil {
		t.Fatal("unknown account signed in")
	}

	list, err := s.ListEvents(localCtx(), ListEventsRequest{Action: "session.login_failed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 2 {
		t.Fatalf("events = %v", eventNames(list))
	}
	unknown := list.Data[0]
	if unknown.TargetID != "" || unknown.TargetName != "" {
		t.Errorf("unknown account stored a target: %+v", unknown)
	}
	known := list.Data[1]
	if known.TargetID != user.ID || known.TargetName != user.Email {
		t.Errorf("known account target = %+v", known)
	}
	if unknown.ActorKind != "anonymous" || unknown.ActorLabel != "" {
		t.Errorf("actor = %+v", unknown)
	}
	if unknown.IP != "198.51.100.9" {
		t.Errorf("ip = %q", unknown.IP)
	}
}

func TestFailedFreshProofRecordsAnEvent(t *testing.T) {
	s := newTestService(t)
	user := createUser(t, s, "member@example.test", false)
	ctx := WithPrincipal(context.Background(), Principal{Kind: "user", UserID: user.ID, Email: user.Email})

	if _, err := s.SetPassword(ctx, user.ID, SetPasswordRequest{Password: "new-password-1", CurrentPassword: "wrong"}); err == nil {
		t.Fatal("a wrong current password was accepted")
	}
	if _, err := s.SetPassword(ctx, user.ID, SetPasswordRequest{Password: "new-password-1"}); err == nil {
		t.Fatal("a missing current password was accepted")
	}

	list, err := s.ListEvents(localCtx(), ListEventsRequest{Action: "user.fresh_proof_failed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 2 {
		t.Fatalf("events = %v", eventNames(list))
	}
	for _, e := range list.Data {
		if e.TargetID != user.ID || e.TargetName != user.Email || e.ActorKind != "user" {
			t.Errorf("event = %+v", e)
		}
	}
}

func TestListEventsCursorAndFilters(t *testing.T) {
	s := newTestService(t)
	first := createUser(t, s, "first@example.test", true)
	second := createUser(t, s, "second@example.test", true)
	for _, u := range []User{first, second} {
		ctx := WithPrincipal(context.Background(), Principal{Kind: "user", UserID: u.ID, Email: u.Email, InstanceAdmin: true})
		if _, err := s.CreatePlan(ctx, CreatePlanRequest{Slug: "plan-" + strings.Split(u.Email, "@")[0]}); err != nil {
			t.Fatal(err)
		}
	}

	page, err := s.ListEvents(localCtx(), ListEventsRequest{Action: "plan.created", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 1 || page.Next == nil {
		t.Fatalf("page = %+v", page)
	}
	next, err := s.ListEvents(localCtx(), ListEventsRequest{Action: "plan.created", Limit: 1, Before: *page.Next})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Data) != 1 || next.Next != nil {
		t.Fatalf("second page = %+v", next)
	}
	if next.Data[0].ID == page.Data[0].ID {
		t.Fatal("pages overlap")
	}

	byActor, err := s.ListEvents(localCtx(), ListEventsRequest{Actor: first.Email})
	if err != nil {
		t.Fatal(err)
	}
	if len(byActor.Data) != 1 || byActor.Data[0].TargetName != "plan-first" {
		t.Fatalf("by actor = %+v", byActor.Data)
	}

	unknown, err := s.ListEvents(localCtx(), ListEventsRequest{Actor: "nobody@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if unknown.Data == nil || len(unknown.Data) != 0 {
		t.Fatalf("unknown actor = %+v", unknown)
	}
}

func TestListEventsWorkspaceFilterAndPermissions(t *testing.T) {
	s := newTestService(t)
	org, err := s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	byOrg, err := s.ListEvents(localCtx(), ListEventsRequest{Organization: org.Slug})
	if err != nil {
		t.Fatal(err)
	}
	if len(byOrg.Data) != 1 || byOrg.Data[0].Name != "workspace.created" || byOrg.Data[0].OrganizationID != org.ID {
		t.Fatalf("events = %+v", byOrg.Data)
	}
	if byOrg.Data[0].OrganizationSlug != org.Slug {
		t.Errorf("workspace slug = %q, want %q", byOrg.Data[0].OrganizationSlug, org.Slug)
	}

	user := createUser(t, s, "member@example.test", false)
	ctx := WithPrincipal(context.Background(), Principal{Kind: "user", UserID: user.ID, Email: user.Email})
	if _, err := s.ListEvents(ctx, ListEventsRequest{}); err == nil {
		t.Fatal("a plain user read the audit trail")
	}

	// A cursor from another scope is refused, never used to skip rows.
	plan, err := s.CreatePlan(localCtx(), CreatePlanRequest{Slug: "starter"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListEvents(localCtx(), ListEventsRequest{Before: plan.ID}); err == nil {
		t.Fatal("a target ID was accepted as a cursor")
	}
}

func TestEventsPrunedByRetention(t *testing.T) {
	s := newTestService(t)
	if _, err := s.CreatePlan(localCtx(), CreatePlanRequest{Slug: "starter"}); err != nil {
		t.Fatal(err)
	}
	s.SetAuditRetention(time.Minute)
	if err := s.pruneEvents(localCtx(), s.now().Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListEvents(localCtx(), ListEventsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 0 {
		t.Fatalf("events after prune = %v", eventNames(list))
	}

	// Zero keeps everything.
	s2 := newTestService(t)
	if _, err := s2.CreatePlan(localCtx(), CreatePlanRequest{Slug: "starter"}); err != nil {
		t.Fatal(err)
	}
	s2.SetAuditRetention(0)
	if err := s2.pruneEvents(localCtx(), s2.now().Add(100*365*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	list, err = s2.ListEvents(localCtx(), ListEventsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) == 0 {
		t.Fatal("a zero retention pruned events")
	}
}
