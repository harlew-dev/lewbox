package service

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/aerolvm/aerolvmtest"
	"github.com/harlew-dev/lewbox/internal/db"
	"github.com/harlew-dev/lewbox/internal/objstore"
	"github.com/harlew-dev/lewbox/internal/objstore/objstoretest"
	"github.com/harlew-dev/lewbox/internal/secrets"
)

// archiveFixture is a service, a fake cluster, a fake bucket and a clock.
type archiveFixture struct {
	t      *testing.T
	s      *Service
	conn   *sql.DB
	clock  *testClock
	fake   *aerolvmtest.Server
	bucket *objstoretest.Fake
	org    Organization
	// cluster is the stored row, including the archive settings.
	cluster ClusterDetail
	profile StorageProfile
}

func newArchiveFixture(t *testing.T) *archiveFixture {
	t.Helper()
	ctx := context.Background()
	clock := &testClock{at: time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)}
	conn, err := db.Open(ctx, filepath.Join(t.TempDir(), "lewbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	keyring, err := secrets.LoadOrCreate(filepath.Join(t.TempDir(), "keyring.key"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(db.New(conn),
		WithKeyring(keyring),
		WithAerolVMOptions(aerolvm.WithRetry(0)),
		WithClock(func() time.Time { return clock.at }),
	)
	bucket := objstoretest.New(t)
	bucket.AddBucket("assets")
	profile, err := s.CreateStorageProfile(localCtx(), CreateStorageProfileRequest{
		Name:            "bucket",
		Endpoint:        bucket.URL,
		Region:          "us-east-1",
		Bucket:          "assets",
		AccessKeyID:     bucket.AccessKeyID,
		SecretAccessKey: storageTestSecret,
	})
	if err != nil {
		t.Fatalf("create storage profile: %v", err)
	}

	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	cluster := addTestCluster(t, s, fake, "eu-west")
	org, err := s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetSubscription(localCtx(), org.Slug, "eu-west", SetSubscriptionRequest{Plan: "free"}); err != nil {
		t.Fatal(err)
	}
	f := &archiveFixture{t: t, s: s, conn: conn, clock: clock, fake: fake, bucket: bucket, org: org, cluster: cluster, profile: profile}
	f.setCluster("archive", true)
	return f
}

// setCluster sets stop_policy and whether the cluster uses the profile.
func (f *archiveFixture) setCluster(stopPolicy string, withProfile bool) {
	f.t.Helper()
	var profile any
	if withProfile {
		profile = f.profile.ID
	}
	if _, err := f.conn.ExecContext(context.Background(),
		`UPDATE clusters SET stop_policy = ?, storage_profile_id = ? WHERE id = ?`,
		stopPolicy, profile, f.cluster.ID); err != nil {
		f.t.Fatal(err)
	}
}

func (f *archiveFixture) sync(t *testing.T) {
	t.Helper()
	if err := f.s.Sync(localCtx()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

func (f *archiveFixture) pass(t *testing.T) {
	t.Helper()
	if err := f.s.ArchivePass(localCtx()); err != nil {
		t.Fatalf("ArchivePass: %v", err)
	}
}

func (f *archiveFixture) advance(d time.Duration) { f.clock.advance(d) }

// addStopped adds a stopped sandbox to the fake and mirrors it.
func (f *archiveFixture) addStopped(t *testing.T, id string) aerolvm.Sandbox {
	t.Helper()
	tags := map[string]string{"app": "demo"}
	sandbox := aerolvm.Sandbox{
		ID:        id,
		Name:      "box",
		Image:     "ubuntu:22.04",
		Status:    "stopped",
		Runtime:   "docker",
		CPU:       2,
		MemoryMB:  1024,
		DiskGB:    20,
		Tags:      tags,
		CreatedAt: time.Date(2026, 11, 12, 11, 0, 0, 0, time.UTC),
	}
	f.fake.AddSandbox(f.org.Team, sandbox)
	f.sync(t)
	return sandbox
}

// row returns the sandbox's live archive row.
func (f *archiveFixture) row(t *testing.T, sandboxID string) db.Archive {
	t.Helper()
	rows, err := f.s.q.ListArchivesBySandbox(context.Background(), db.ListArchivesBySandboxParams{
		ClusterID: f.cluster.ID,
		SandboxID: sandboxID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatalf("no archive row for sandbox %q", sandboxID)
	}
	return rows[0]
}

// archiveRef is the registry ref a real cluster would report for snapshot.
func (f *archiveFixture) archiveRef(name string) string {
	return archiveRegistryHost + "/cluster/" + f.cluster.ID + "/snapshots/tenant-x/" + name + ":latest"
}

// bucketPrefix is where the registry stores one snapshot's repository.
func (f *archiveFixture) bucketPrefix(repo string) string {
	return "clusters/" + strings.ToLower(f.cluster.ID) + "/docker/registry/v2/repositories/" + repo + "/"
}

func (f *archiveFixture) put(t *testing.T, key string) {
	t.Helper()
	client, err := objstore.New(objstore.Config{
		Endpoint:        f.bucket.URL,
		Region:          "us-east-1",
		Bucket:          "assets",
		AccessKeyID:     f.bucket.AccessKeyID,
		SecretAccessKey: storageTestSecret,
		PathStyle:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Put(context.Background(), key, []byte("layer")); err != nil {
		t.Fatal(err)
	}
}

func (f *archiveFixture) deleteSandbox(t *testing.T, id string) {
	t.Helper()
	client := aerolvm.New(f.fake.URL(), f.fake.AdminToken, aerolvm.WithRetry(0))
	if err := client.DeleteSandbox(context.Background(), id); err != nil {
		t.Fatal(err)
	}
}

func TestArchivePassArchivesStoppedSandbox(t *testing.T) {
	f := newArchiveFixture(t)
	f.addStopped(t, "sb-1")

	f.pass(t)
	row := f.row(t, "sb-1")
	if row.Status != archivePushing || row.SandboxName != "box" || row.Image != "ubuntu:22.04" ||
		row.Runtime != "docker" || row.Cpu != 2 || row.MemoryMb != 1024 || row.DiskGb != 20 {
		t.Fatalf("after the first pass = %+v", row)
	}
	if row.Tags != `{"app":"demo"}` || row.SnapshotName != "archive-sb-1" || !row.PushingSince.Valid {
		t.Errorf("after the first pass = %+v", row)
	}
	if _, ok := f.fake.Sandbox("sb-1"); !ok {
		t.Error("the sandbox was deleted before its push finished")
	}
	snapshots := f.fake.Snapshots(f.org.Team)
	if len(snapshots) != 1 || snapshots[0].Name != "archive-sb-1" || snapshots[0].SourceSandboxID != "sb-1" {
		t.Fatalf("snapshots = %+v", snapshots)
	}
	tokens := f.fake.Tokens()
	if len(tokens) != 1 || tokens[0].Role != "runner" || tokens[0].Team != f.org.Team ||
		!strings.HasPrefix(tokens[0].Name, "lbx.arch.") {
		t.Errorf("tokens = %+v", tokens)
	}

	// The push is still pending: the row waits.
	f.advance(5 * time.Second)
	f.pass(t)
	if row = f.row(t, "sb-1"); row.Status != archivePushing {
		t.Fatalf("while pushing = %+v", row)
	}
	if _, ok := f.fake.Sandbox("sb-1"); !ok {
		t.Error("the sandbox was deleted before the push was active")
	}

	ref := f.archiveRef("archive-sb-1")
	f.fake.SetSnapshotPush(f.org.Team, "archive-sb-1", "active", ref)
	f.advance(5 * time.Second)
	f.pass(t)

	row = f.row(t, "sb-1")
	if row.Status != archiveArchived || row.RegistryRef != ref || !row.ArchivedAt.Valid || row.Error != "" {
		t.Fatalf("archived = %+v", row)
	}
	if _, ok := f.fake.Sandbox("sb-1"); ok {
		t.Error("the archived sandbox is still in the fake")
	}
}

func TestArchivePassLeavesOtherSandboxesAlone(t *testing.T) {
	t.Run("keep cluster", func(t *testing.T) {
		f := newArchiveFixture(t)
		f.setCluster("keep", true)
		f.addStopped(t, "sb-1")
		f.pass(t)
		if rows, err := f.s.q.ListArchivesBySandbox(context.Background(), db.ListArchivesBySandboxParams{ClusterID: f.cluster.ID, SandboxID: "sb-1"}); err != nil || len(rows) != 0 {
			t.Errorf("keep cluster archived %+v, %v", rows, err)
		}
	})
	t.Run("no storage profile", func(t *testing.T) {
		f := newArchiveFixture(t)
		f.setCluster("archive", false)
		f.addStopped(t, "sb-1")
		f.pass(t)
		if rows, err := f.s.q.ListArchivesBySandbox(context.Background(), db.ListArchivesBySandboxParams{ClusterID: f.cluster.ID, SandboxID: "sb-1"}); err != nil || len(rows) != 0 {
			t.Errorf("unprofiled cluster archived %+v, %v", rows, err)
		}
	})
	t.Run("running sandbox", func(t *testing.T) {
		f := newArchiveFixture(t)
		f.fake.AddSandbox(f.org.Team, startedSandbox("sb-1", 1, 512))
		f.sync(t)
		f.pass(t)
		if rows, err := f.s.q.ListArchivesBySandbox(context.Background(), db.ListArchivesBySandboxParams{ClusterID: f.cluster.ID, SandboxID: "sb-1"}); err != nil || len(rows) != 0 {
			t.Errorf("running sandbox archived %+v, %v", rows, err)
		}
	})
	t.Run("sandbox without a workspace", func(t *testing.T) {
		f := newArchiveFixture(t)
		f.fake.AddSandbox("unknown-team", func() aerolvm.Sandbox {
			sandbox := startedSandbox("sb-1", 1, 512)
			sandbox.Status = "stopped"
			return sandbox
		}())
		f.sync(t)
		f.pass(t)
		if rows, err := f.s.q.ListArchivesBySandbox(context.Background(), db.ListArchivesBySandboxParams{ClusterID: f.cluster.ID, SandboxID: "sb-1"}); err != nil || len(rows) != 0 {
			t.Errorf("unowned sandbox archived %+v, %v", rows, err)
		}
	})
}

func TestArchiveDiscoveryInsertsOnce(t *testing.T) {
	f := newArchiveFixture(t)
	f.addStopped(t, "sb-1")

	f.pass(t)
	f.advance(5 * time.Second)
	f.pass(t)

	rows, err := f.s.q.ListArchivesBySandbox(context.Background(), db.ListArchivesBySandboxParams{
		ClusterID: f.cluster.ID,
		SandboxID: "sb-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("two passes made %d archive rows: %+v", len(rows), rows)
	}
}

func TestArchiveCancelsWhenSandboxStartsAgain(t *testing.T) {
	f := newArchiveFixture(t)
	f.addStopped(t, "sb-1")
	f.pass(t)

	ref := f.archiveRef("archive-sb-1")
	f.fake.SetSnapshotPush(f.org.Team, "archive-sb-1", "active", ref)
	repoPrefix := f.bucketPrefix("cluster/" + f.cluster.ID + "/snapshots/tenant-x/archive-sb-1")
	otherPrefix := f.bucketPrefix("cluster/" + f.cluster.ID + "/snapshots/tenant-x/other")
	f.put(t, repoPrefix+"manifests/latest/link")
	f.put(t, repoPrefix+"blobs/sha256/deadbeef")
	f.put(t, otherPrefix+"manifests/latest/link")

	f.fake.SetSandboxStatus("sb-1", "started")
	f.advance(5 * time.Second)
	f.pass(t)

	row := f.row(t, "sb-1")
	if row.Status != archiveCancelled || !strings.Contains(row.Error, "started again") {
		t.Fatalf("cancelled = %+v", row)
	}
	if snapshots := f.fake.Snapshots(f.org.Team); len(snapshots) != 0 {
		t.Errorf("the snapshot survived the cancellation: %+v", snapshots)
	}
	objects := f.bucket.Objects("assets")
	for key := range objects {
		if strings.HasPrefix(key, repoPrefix) {
			t.Errorf("the cancelled archive's repository survived: %s", key)
		}
	}
	if _, ok := objects[otherPrefix+"manifests/latest/link"]; !ok {
		t.Error("another snapshot's repository was deleted")
	}
}

func TestArchiveCancelsWhenSandboxIsGone(t *testing.T) {
	f := newArchiveFixture(t)
	f.addStopped(t, "sb-1")
	f.deleteSandbox(t, "sb-1")

	f.pass(t)
	row := f.row(t, "sb-1")
	if row.Status != archiveCancelled || !strings.Contains(row.Error, "gone") {
		t.Fatalf("cancelled = %+v", row)
	}
}

func TestArchivePushTimeoutFailsAndRetries(t *testing.T) {
	f := newArchiveFixture(t)
	f.addStopped(t, "sb-1")
	f.pass(t)

	f.advance(31 * time.Minute)
	f.pass(t)
	row := f.row(t, "sb-1")
	if row.Status != archiveFailed || row.Attempts != 1 || !row.NextAttemptAt.Valid {
		t.Fatalf("stuck push = %+v", row)
	}
	if row.PushingSince.Valid {
		t.Errorf("pushing_since was kept after the push failed: %+v", row)
	}

	// Not due yet: a minute after the 10-minute retry delay it still waits.
	f.advance(9 * time.Minute)
	f.pass(t)
	if row = f.row(t, "sb-1"); row.Status != archiveFailed || row.Attempts != 1 {
		t.Fatalf("retried too early = %+v", row)
	}

	// Due: the push finished in the meantime, so the retry archives it.
	f.fake.SetSnapshotPush(f.org.Team, "archive-sb-1", "active", f.archiveRef("archive-sb-1"))
	f.advance(2 * time.Minute)
	f.pass(t)
	if row = f.row(t, "sb-1"); row.Status != archiveArchived || row.Attempts != 1 {
		t.Fatalf("after the retry = %+v", row)
	}
}

func TestArchivePassDefersOnAerolVMError(t *testing.T) {
	f := newArchiveFixture(t)
	f.addStopped(t, "sb-1")
	f.fake.FailNext("/v1/sandboxes/sb-1", 503, `{"error":"unavailable"}`, 1)

	f.pass(t)
	row := f.row(t, "sb-1")
	if row.Status != archivePending || row.Error == "" || !row.NextAttemptAt.Valid {
		t.Fatalf("after a 503 = %+v", row)
	}
	if !row.NextAttemptAt.Time.After(f.clock.at) {
		t.Errorf("next attempt = %v, want after %v", row.NextAttemptAt.Time, f.clock.at)
	}

	// It is not due before the minute is over.
	f.pass(t)
	if row = f.row(t, "sb-1"); row.Status != archivePending {
		t.Fatalf("retried before it was due: %+v", row)
	}

	f.advance(61 * time.Second)
	f.pass(t)
	row = f.row(t, "sb-1")
	if row.Status != archivePushing || row.Error != "" {
		t.Fatalf("after the retry = %+v", row)
	}
}

func (f *archiveFixture) archivedRow(t *testing.T) db.Archive {
	t.Helper()
	f.addStopped(t, "sb-1")
	f.pass(t)
	f.fake.SetSnapshotPush(f.org.Team, "archive-sb-1", "active", f.archiveRef("archive-sb-1"))
	f.advance(5 * time.Second)
	f.pass(t)
	row := f.row(t, "sb-1")
	if row.Status != archiveArchived {
		t.Fatalf("archive = %+v", row)
	}
	return row
}

func TestRestoreArchive(t *testing.T) {
	f := newArchiveFixture(t)
	row := f.archivedRow(t)

	sandbox, err := f.s.restoreArchive(context.Background(), row)
	if err != nil {
		t.Fatal(err)
	}
	if sandbox.Name != "box" || sandbox.Image != row.RegistryRef || sandbox.CPU != 2 ||
		sandbox.MemoryMB != 1024 || sandbox.DiskGB != 20 || sandbox.Runtime != "docker" {
		t.Errorf("restored = %+v", sandbox)
	}
	if sandbox.Tags["app"] != "demo" || sandbox.Tags[archiveRestoredTag] != "sb-1" {
		t.Errorf("restored tags = %v", sandbox.Tags)
	}
	restored, ok := f.fake.Sandbox(sandbox.ID)
	if !ok || restored.Name != "box" {
		t.Errorf("the fake does not hold the restored sandbox: %+v", restored)
	}
	updated, err := f.s.q.GetArchiveByID(context.Background(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastRestoredSandboxID != sandbox.ID || !updated.LastRestoredAt.Valid ||
		!updated.LastRestoredAt.Time.Equal(f.clock.at) {
		t.Errorf("last restore = %+v", updated)
	}

	// The old name is taken, so the second restore gets a suffix.
	again, err := f.s.restoreArchive(context.Background(), row)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(again.Name, "box-") || len(again.Name) != len("box-")+4 {
		t.Errorf("second restore name = %q", again.Name)
	}
	if again.ID == sandbox.ID {
		t.Errorf("the second restore reused the first sandbox: %+v", again)
	}
}

func TestDeleteArchive(t *testing.T) {
	f := newArchiveFixture(t)
	row := f.archivedRow(t)
	repo := "cluster/" + f.cluster.ID + "/snapshots/tenant-x/archive-sb-1"
	prefix := f.bucketPrefix(repo)
	f.put(t, prefix+"manifests/latest/link")

	if err := f.s.deleteArchive(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	if snapshots := f.fake.Snapshots(f.org.Team); len(snapshots) != 0 {
		t.Errorf("the snapshot survived the delete: %+v", snapshots)
	}
	for key := range f.bucket.Objects("assets") {
		if strings.HasPrefix(key, prefix) {
			t.Errorf("the repository survived the delete: %s", key)
		}
	}
	deleted, err := f.s.q.GetArchiveByID(context.Background(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Status != archiveDeleted || !deleted.DeletedAt.Valid {
		t.Errorf("deleted = %+v", deleted)
	}
}

func TestDeleteArchiveRefusesAForeignRepository(t *testing.T) {
	f := newArchiveFixture(t)
	cluster, err := f.s.q.GetClusterByID(context.Background(), f.cluster.ID)
	if err != nil {
		t.Fatal(err)
	}
	foreign := f.bucketPrefix("cluster/another-cluster/snapshots/tenant-x/archive-sb-1")
	f.put(t, foreign+"manifests/latest/link")
	notArchive := f.bucketPrefix("cluster/" + f.cluster.ID + "/snapshots/tenant-x/other")
	f.put(t, notArchive+"manifests/latest/link")

	for _, ref := range []string{
		archiveRegistryHost + "/cluster/another-cluster/snapshots/tenant-x/archive-sb-1:latest",
		archiveRegistryHost + "/cluster/" + f.cluster.ID + "/snapshots/tenant-x/other:latest",
	} {
		if err := f.s.deleteArchiveFromBucket(context.Background(), cluster, ref); err == nil {
			t.Errorf("deleteArchiveFromBucket(%q) deleted something", ref)
		}
	}
	objects := f.bucket.Objects("assets")
	if _, ok := objects[foreign+"manifests/latest/link"]; !ok {
		t.Error("another cluster's repository was deleted")
	}
	if _, ok := objects[notArchive+"manifests/latest/link"]; !ok {
		t.Error("a customer snapshot was deleted")
	}
}

func TestArchiveCancelsWhenSandboxRanAgainDuringThePush(t *testing.T) {
	f := newArchiveFixture(t)
	f.addStopped(t, "sb-1")
	f.pass(t)

	// Started and stopped again after the snapshot: it is stopped, but the
	// snapshot misses what it wrote in between.
	time.Sleep(2 * time.Millisecond)
	f.fake.SetSandboxStatus("sb-1", "started")
	f.fake.SetSandboxStatus("sb-1", "stopped")
	f.fake.SetSnapshotPush(f.org.Team, "archive-sb-1", "active", f.archiveRef("archive-sb-1"))
	f.advance(5 * time.Second)
	f.pass(t)

	row := f.row(t, "sb-1")
	if row.Status != archiveCancelled || !strings.Contains(row.Error, "stopped again") {
		t.Fatalf("archive = %+v, want it cancelled", row)
	}
	if _, ok := f.fake.Sandbox("sb-1"); !ok {
		t.Fatal("the sandbox was deleted although its snapshot is stale")
	}
	if snapshots := f.fake.Snapshots(f.org.Team); len(snapshots) != 0 {
		t.Errorf("the stale snapshot survived: %+v", snapshots)
	}

	// The next pass archives it afresh.
	f.pass(t)
	rows, err := f.s.q.ListArchivesBySandbox(context.Background(), db.ListArchivesBySandboxParams{ClusterID: f.cluster.ID, SandboxID: "sb-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Status == archiveCancelled {
		t.Fatalf("archives = %+v, want a new live one", rows)
	}
}

// member adds an account to the fixture's workspace and returns its context.
func (f *archiveFixture) member(t *testing.T, email, role string) context.Context {
	t.Helper()
	createUser(t, f.s, email, false)
	if role != "" {
		if _, err := f.s.AddMember(localCtx(), "acme", AddMemberRequest{Email: email, Role: role}); err != nil {
			t.Fatalf("add %s: %v", email, err)
		}
	}
	return userCtx(t, f.s, email)
}

// archiveStopped archives one stopped sandbox of another workspace.
func (f *archiveFixture) archiveStopped(t *testing.T, org Organization, sandboxID string) db.Archive {
	t.Helper()
	f.fake.AddSandbox(org.Team, aerolvm.Sandbox{
		ID: sandboxID, Name: "other-box", Image: "ubuntu:22.04", Status: "stopped",
		Runtime: "docker", CPU: 1, MemoryMB: 512, DiskGB: 5,
		CreatedAt: time.Date(2026, 11, 12, 11, 0, 0, 0, time.UTC),
	})
	f.sync(t)
	f.pass(t)
	f.fake.SetSnapshotPush(org.Team, "archive-"+sandboxID, "active", f.archiveRef("archive-"+sandboxID))
	f.advance(5 * time.Second)
	f.pass(t)
	rows, err := f.s.q.ListArchivesBySandbox(context.Background(), db.ListArchivesBySandboxParams{
		ClusterID: f.cluster.ID, SandboxID: sandboxID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || rows[0].Status != archiveArchived {
		t.Fatalf("archive for %q = %+v", sandboxID, rows)
	}
	return rows[0]
}

func TestListArchivesScoping(t *testing.T) {
	f := newArchiveFixture(t)
	acmeRow := f.archivedRow(t)

	outsider := f.member(t, "outsider@example.com", "")
	other, err := f.s.CreateOrganization(localCtx(), CreateOrganizationRequest{
		Slug: "other", Name: "Other", Owner: "outsider@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SetSubscription(localCtx(), other.Slug, "eu-west", SetSubscriptionRequest{Plan: "free"}); err != nil {
		t.Fatal(err)
	}
	otherRow := f.archiveStopped(t, other, "sb-2")
	member := f.member(t, "member@example.com", roleMember)
	viewer := f.member(t, "viewer@example.com", roleViewer)

	for name, ctx := range map[string]context.Context{"member": member, "viewer": viewer} {
		list, err := f.s.ListArchives(ctx, RegionScope{Org: "acme"})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(list.Data) != 1 || list.Data[0].ID != acmeRow.ID || list.Data[0].Org != nil {
			t.Errorf("%s sees %+v", name, list.Data)
		}
	}

	if _, err := f.s.ListArchives(outsider, RegionScope{Org: "acme"}); err != nil {
		wantCode(t, err, CodeNotFound)
	}
	list, err := f.s.ListArchives(outsider, RegionScope{Org: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || list.Data[0].ID != otherRow.ID || list.Data[0].Org != nil {
		t.Errorf("outsider sees %+v", list.Data)
	}

	// An instance admin with no workspace sees every workspace's archives,
	// with the owner on each row.
	list, err = f.s.ListArchives(localCtx(), RegionScope{})
	if err != nil {
		t.Fatal(err)
	}
	owners := make(map[string]string)
	for _, archive := range list.Data {
		if archive.Org == nil {
			t.Fatalf("fleet archive without an organization = %+v", archive)
		}
		owners[archive.ID] = *archive.Org
	}
	if len(owners) != 2 || owners[acmeRow.ID] != "acme" || owners[otherRow.ID] != "other" {
		t.Errorf("fleet owners = %v", owners)
	}
	list, err = f.s.ListArchives(localCtx(), RegionScope{Region: "eu-west"})
	if err != nil || len(list.Data) != 2 {
		t.Errorf("filtered fleet list = %+v, %v", list.Data, err)
	}
	if _, err := f.s.ListArchives(localCtx(), RegionScope{Region: "missing"}); err != nil {
		wantCode(t, err, CodeNotFound)
	}
}

func TestGetArchive(t *testing.T) {
	f := newArchiveFixture(t)
	row := f.archivedRow(t)
	outsider := f.member(t, "outsider@example.com", "")
	if _, err := f.s.CreateOrganization(localCtx(), CreateOrganizationRequest{
		Slug: "other", Name: "Other", Owner: "outsider@example.com",
	}); err != nil {
		t.Fatal(err)
	}
	member := f.member(t, "member@example.com", roleMember)

	got, err := f.s.GetArchive(member, row.ID, RegionScope{Org: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != row.ID || got.Region != "eu-west" || got.SandboxID != "sb-1" ||
		got.SandboxName != "box" || got.Status != archiveArchived || got.RegistryRef != row.RegistryRef {
		t.Errorf("archive = %+v", got)
	}
	if got.Tags["app"] != "demo" || got.ArchivedAt == nil {
		t.Errorf("archive = %+v", got)
	}
	if _, err := f.s.GetArchive(outsider, row.ID, RegionScope{Org: "other"}); err != nil {
		wantCode(t, err, CodeNotFound)
	}
	if _, err := f.s.GetArchive(member, "not-a-uuid", RegionScope{Org: "acme"}); err != nil {
		wantCode(t, err, CodeInvalid)
	}
}

func TestRestoreArchiveAPI(t *testing.T) {
	f := newArchiveFixture(t)
	row := f.archivedRow(t)
	member := f.member(t, "member@example.com", roleMember)
	viewer := f.member(t, "viewer@example.com", roleViewer)

	if _, err := f.s.RestoreArchive(viewer, row.ID, RegionScope{Org: "acme"}); err != nil {
		wantCode(t, err, CodeForbidden)
	}
	out, err := f.s.RestoreArchive(member, row.ID, RegionScope{Org: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Sandbox.ID == "" || out.Sandbox.Name != "box" || out.Sandbox.Region != "eu-west" ||
		out.Sandbox.Status == "" {
		t.Errorf("restored sandbox = %+v", out.Sandbox)
	}
	sandbox, ok := f.fake.Sandbox(out.Sandbox.ID)
	if !ok || sandbox.Tags[archiveRestoredTag] != "sb-1" {
		t.Errorf("fake sandbox = %+v, %v", sandbox, ok)
	}
	if out.Archive.LastRestoredSandboxID != out.Sandbox.ID || out.Archive.LastRestoredAt == nil {
		t.Errorf("restored archive = %+v", out.Archive)
	}

	// A pending row is not archived yet.
	f.addStopped(t, "sb-2")
	f.pass(t)
	pending := f.row(t, "sb-2")
	if _, err := f.s.RestoreArchive(member, pending.ID, RegionScope{Org: "acme"}); err != nil {
		wantCode(t, err, CodePrecondition)
	}

	// A suspended workspace does not get new sandboxes.
	status := orgSuspended
	if _, err := f.s.UpdateOrganization(localCtx(), "acme", UpdateOrganizationRequest{Status: &status}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RestoreArchive(member, row.ID, RegionScope{Org: "acme"}); err != nil {
		wantCode(t, err, CodePrecondition)
	}
}

func TestDeleteArchiveAPI(t *testing.T) {
	f := newArchiveFixture(t)
	row := f.archivedRow(t)
	member := f.member(t, "member@example.com", roleMember)
	viewer := f.member(t, "viewer@example.com", roleViewer)

	if _, err := f.s.DeleteArchive(viewer, row.ID, RegionScope{Org: "acme"}); err != nil {
		wantCode(t, err, CodeForbidden)
	}
	out, err := f.s.DeleteArchive(member, row.ID, RegionScope{Org: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != row.ID || out.Status != archiveDeleted || out.RegistryRef != row.RegistryRef {
		t.Errorf("deleted archive = %+v", out)
	}
	if snapshots := f.fake.Snapshots(f.org.Team); len(snapshots) != 0 {
		t.Errorf("the snapshot survived: %+v", snapshots)
	}
	list, err := f.s.ListArchives(member, RegionScope{Org: "acme"})
	if err != nil || len(list.Data) != 0 {
		t.Errorf("list after delete = %+v, %v", list.Data, err)
	}
}

func TestListArchivesOmitsCancelled(t *testing.T) {
	f := newArchiveFixture(t)
	f.archivedRow(t)
	f.addStopped(t, "sb-2")
	f.pass(t)
	cancelled := f.row(t, "sb-2")
	if err := f.s.q.CancelArchive(context.Background(), db.CancelArchiveParams{Error: "test", ID: cancelled.ID}); err != nil {
		t.Fatal(err)
	}

	list, err := f.s.ListArchives(localCtx(), RegionScope{Org: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	for _, archive := range list.Data {
		if archive.Status == archiveCancelled || archive.Status == archiveDeleted {
			t.Errorf("list included %+v", archive)
		}
	}
	if len(list.Data) != 1 || list.Data[0].SandboxID != "sb-1" {
		t.Errorf("list = %+v", list.Data)
	}
}

func TestStorageProfileUpdateRefusesWhileArchivesExist(t *testing.T) {
	f := newArchiveFixture(t)
	row := f.archivedRow(t)

	prefix := "moved"
	_, err := f.s.UpdateStorageProfile(localCtx(), f.profile.Name, UpdateStorageProfileRequest{Prefix: &prefix})
	wantCode(t, err, CodeConflict)

	// A rename does not move the data, so it is allowed.
	name := "renamed"
	if _, err := f.s.UpdateStorageProfile(localCtx(), f.profile.Name, UpdateStorageProfileRequest{Name: &name}); err != nil {
		t.Fatal(err)
	}

	// Deleting the archive frees the profile.
	if _, err := f.s.DeleteArchive(localCtx(), row.ID, RegionScope{Org: "acme"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.UpdateStorageProfile(localCtx(), name, UpdateStorageProfileRequest{Prefix: &prefix}); err != nil {
		t.Fatal(err)
	}
}

// Archives of one workspace advance in parallel and share its team token.
// Two mints in the same millisecond used to get the same token name, and
// minting the second revoked the first while it was in use: one archive
// failed with a 401 and waited for a retry.
func TestArchivePassSameWorkspaceInParallel(t *testing.T) {
	f := newArchiveFixture(t)
	ids := []string{"sb-p1", "sb-p2", "sb-p3", "sb-p4"}
	for _, id := range ids {
		f.addStopped(t, id)
	}
	f.pass(t)
	for _, id := range ids {
		row := f.row(t, id)
		if row.Status != archivePushing || row.Error != "" {
			t.Errorf("%s: status %q, error %q; want pushing with no error", id, row.Status, row.Error)
		}
	}
}
