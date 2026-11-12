package service

import (
	"context"
	"testing"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
)

// pointAtFake makes the fixture's single node answer at the fake's address,
// so snapshot listing can reach it.
func pointAtFake(t *testing.T, f sandboxFixture) {
	t.Helper()
	member := testMember(f.fake, "node-a", "docker", true)
	member.APIURL = f.fake.URL()
	f.fake.SetMembers([]aerolvm.Member{member})
	if _, err := f.s.CheckCluster(f.root, "eu-west"); err != nil {
		t.Fatal(err)
	}
}

func testSnapshot(name string) aerolvm.Snapshot {
	return aerolvm.Snapshot{
		Name:            name,
		Image:           "ubuntu:22.04",
		SourceSandboxID: "sb-1",
		CPU:             1,
		MemoryMB:        512,
		DiskGB:          10,
		PushState:       "active",
		CreatedAt:       time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC),
	}
}

func TestListSnapshotsScoping(t *testing.T) {
	f := newSandboxFixture(t)
	pointAtFake(t, f)
	f.fake.AddSnapshot(f.org.Team, testSnapshot("ckpt-a"))

	other, err := f.s.CreateOrganization(f.root, CreateOrganizationRequest{Slug: "other", Owner: "outsider@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SetSubscription(f.root, other.Slug, "eu-west", SetSubscriptionRequest{Plan: "free"}); err != nil {
		t.Fatal(err)
	}
	f.fake.AddSnapshot(other.Team, testSnapshot("ckpt-b"))

	for name, ctx := range map[string]context.Context{
		"owner":  f.owner,
		"admin":  f.admin,
		"member": f.member,
		"viewer": f.viewer,
	} {
		list, err := f.s.ListSnapshots(ctx, ListSnapshotsRequest{Org: "acme"})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(list.Data) != 1 || list.Data[0].Name != "ckpt-a" || list.Data[0].Region != "eu-west" ||
			list.Data[0].SourceSandboxID != "sb-1" || list.Data[0].PushState != "active" {
			t.Errorf("%s sees %+v", name, list.Data)
		}
	}

	if _, err := f.s.ListSnapshots(f.outsider, ListSnapshotsRequest{Org: "acme"}); err != nil {
		wantCode(t, err, CodeNotFound)
	}
	list, err := f.s.ListSnapshots(f.outsider, ListSnapshotsRequest{Org: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || list.Data[0].Name != "ckpt-b" {
		t.Errorf("outsider sees %+v", list.Data)
	}

	// An API key sees its own workspace only.
	key, err := f.s.CreateAdminKey(f.owner, "acme", CreateAdminKeyRequest{Name: "backend"})
	if err != nil {
		t.Fatal(err)
	}
	keyCtx := context.Background()
	if p, err := f.s.Authenticate(keyCtx, key.Token); err != nil {
		t.Fatal(err)
	} else {
		keyCtx = WithPrincipal(keyCtx, p)
	}
	list, err = f.s.ListSnapshots(keyCtx, ListSnapshotsRequest{})
	if err != nil || len(list.Data) != 1 || list.Data[0].Name != "ckpt-a" {
		t.Errorf("key sees %+v, %v", list.Data, err)
	}
}

func TestListSnapshotsMergesNodesAndCaches(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	s := newClusterService(t, WithClock(func() time.Time { return now }))
	createUser(t, s, "root@example.com", true)
	root := userCtx(t, s, "root@example.com")

	first := newClusterFake(t)
	second := newClusterFake(t)
	first.SetMembers([]aerolvm.Member{
		{NodeID: "node-a", NodeName: "node-a", APIURL: first.URL(), Alive: true, Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}}},
		{NodeID: "node-b", NodeName: "node-b", APIURL: second.URL(), Alive: true, Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}}},
	})
	addTestCluster(t, s, first, "eu-west")
	org, err := s.CreateOrganization(root, CreateOrganizationRequest{Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetSubscription(root, org.Slug, "eu-west", SetSubscriptionRequest{Plan: "free"}); err != nil {
		t.Fatal(err)
	}
	first.AddSnapshot(org.Team, testSnapshot("ckpt-a"))

	list, err := s.ListSnapshots(root, ListSnapshotsRequest{Org: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || list.Data[0].Name != "ckpt-a" {
		t.Fatalf("first list = %+v", list.Data)
	}
	if len(list.PartialNodes) != 1 || list.PartialNodes[0] != "node-b" {
		t.Errorf("partial nodes = %v", list.PartialNodes)
	}
	if len(first.Tokens()) != 1 {
		t.Fatalf("tokens = %+v", first.Tokens())
	}

	// The token is reused once both nodes know it, and the 30 s cache keeps
	// the old list until it expires.
	for _, token := range first.Tokens() {
		second.AcceptToken(token)
	}
	second.AddSnapshot(org.Team, testSnapshot("ckpt-b"))
	list, err = s.ListSnapshots(root, ListSnapshotsRequest{Org: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || list.Data[0].Name != "ckpt-a" || len(list.PartialNodes) != 1 {
		t.Errorf("cached list = %+v", list)
	}

	now = now.Add(31 * time.Second)
	list, err = s.ListSnapshots(root, ListSnapshotsRequest{Org: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 2 || len(list.PartialNodes) != 0 {
		t.Errorf("merged list = %+v", list)
	}
	if len(first.Tokens()) != 1 {
		t.Errorf("a second list minted another token: %+v", first.Tokens())
	}

	// After the reuse window the viewer token is minted again.
	now = now.Add(51 * time.Minute)
	list, err = s.ListSnapshots(root, ListSnapshotsRequest{Org: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Tokens()) != 2 {
		t.Errorf("tokens after the reuse window = %+v", first.Tokens())
	}
	if tokens := first.Tokens(); tokens[1].Role != "viewer" || tokens[1].Team != org.Team {
		t.Errorf("viewer token = %+v", tokens[1])
	}
}

func TestDeleteSnapshotOnAnotherNode(t *testing.T) {
	s := newClusterService(t)
	createUser(t, s, "root@example.com", true)
	root := userCtx(t, s, "root@example.com")

	first := newClusterFake(t)
	second := newClusterFake(t)
	members := []aerolvm.Member{
		{NodeID: "node-a", NodeName: "node-a", APIURL: first.URL(), Alive: true, Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}}},
		{NodeID: "node-b", NodeName: "node-b", APIURL: second.URL(), Alive: true, Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}}},
	}
	first.SetMembers(members)
	second.SetMembers(members)
	addTestCluster(t, s, first, "eu-west")
	org, err := s.CreateOrganization(root, CreateOrganizationRequest{Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetSubscription(root, org.Slug, "eu-west", SetSubscriptionRequest{Plan: "free"}); err != nil {
		t.Fatal(err)
	}
	second.AddSnapshot(org.Team, testSnapshot("ckpt-b"))

	// The viewer token is minted on the admin node; let the other node see it.
	if _, err := s.ListSnapshots(root, ListSnapshotsRequest{Org: "acme"}); err != nil {
		t.Fatal(err)
	}
	for _, token := range first.Tokens() {
		second.AcceptToken(token)
	}

	out, err := s.DeleteSnapshot(root, "ckpt-b", SnapshotActionRequest{Org: "acme", Region: "eu-west"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != "ckpt-b" || out.Region != "eu-west" {
		t.Errorf("deleted snapshot = %+v", out)
	}
	if len(second.Snapshots(org.Team)) != 0 {
		t.Errorf("snapshot left on node-b = %+v", second.Snapshots(org.Team))
	}
	if len(second.Revoked()) == 0 {
		t.Error("the runner token was not revoked on the node that held the snapshot")
	}
}

func TestDeleteSnapshot(t *testing.T) {
	f := newSandboxFixture(t)
	pointAtFake(t, f)
	f.fake.AddSnapshot(f.org.Team, testSnapshot("ckpt-a"))

	if _, err := f.s.DeleteSnapshot(f.viewer, "ckpt-a", SnapshotActionRequest{Org: "acme", Region: "eu-west"}); err != nil {
		wantCode(t, err, CodeForbidden)
	}
	if _, err := f.s.DeleteSnapshot(f.outsider, "ckpt-a", SnapshotActionRequest{Org: "acme", Region: "eu-west"}); err != nil {
		wantCode(t, err, CodeNotFound)
	}

	out, err := f.s.DeleteSnapshot(f.member, "ckpt-a", SnapshotActionRequest{Org: "acme", Region: "eu-west"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != "ckpt-a" || out.Region != "eu-west" || out.SourceSandboxID != "sb-1" {
		t.Errorf("deleted snapshot = %+v", out)
	}
	if len(f.fake.Snapshots(f.org.Team)) != 0 {
		t.Errorf("snapshots left = %+v", f.fake.Snapshots(f.org.Team))
	}
	revoked := f.fake.Revoked()
	if len(revoked) == 0 {
		t.Fatal("the temporary token was not revoked")
	}
	runner := false
	for _, token := range f.fake.Tokens() {
		if token.Role == "runner" {
			runner = true
		}
	}
	if runner {
		t.Errorf("the runner token is still listed: %+v", f.fake.Tokens())
	}

	list, err := f.s.ListSnapshots(f.owner, ListSnapshotsRequest{Org: "acme"})
	if err != nil || len(list.Data) != 0 {
		t.Errorf("list after delete = %+v, %v", list.Data, err)
	}
	if _, err := f.s.DeleteSnapshot(f.owner, "ckpt-a", SnapshotActionRequest{Org: "acme", Region: "eu-west"}); err != nil {
		wantCode(t, err, CodeNotFound)
	}
}
