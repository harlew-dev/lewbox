package service

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestArchiveSnapshotName(t *testing.T) {
	long := strings.Repeat("a", 60)
	for _, tc := range []struct {
		style, id, name, want string
	}{
		{"id", "sb-1", "keeper", "archive-sb-1"},
		{"", "sb-1", "keeper", "archive-sb-1"},
		{"name", "sb-1", "keeper", "archive-keeper-sb-1"},
		{"name", "sb-1", "My Box_2.0", "archive-my-box-2-0-sb-1"},
		{"name", "sb-1", "--Hello--World--", "archive-hello-world-sb-1"},
		{"name", "sb-1", "", "archive-sb-1"},
		{"name", "sb-1", "日本", "archive-sb-1"},
		{"name", "sb-1", long, "archive-" + strings.Repeat("a", maxSnapshotNamePart) + "-sb-1"},
		{"name", "sb-1", strings.Repeat("a", maxSnapshotNamePart-1) + "-b", "archive-" + strings.Repeat("a", maxSnapshotNamePart-1) + "-sb-1"},
	} {
		got := archiveSnapshotName(tc.style, tc.id, tc.name)
		if got != tc.want {
			t.Errorf("archiveSnapshotName(%q, %q, %q) = %q, want %q", tc.style, tc.id, tc.name, got, tc.want)
		}
		if !isArchiveSnapshot(got) || !strings.HasSuffix(got, "-"+tc.id) {
			t.Errorf("%q must start with archive- and end with the sandbox ID", got)
		}
	}
}

func TestArchiveRetentionAge(t *testing.T) {
	for _, tc := range []struct {
		retention string
		age       time.Duration
		cleanup   bool
	}{
		{"instant", 0, true},
		{"1h", time.Hour, true},
		{"1d", 24 * time.Hour, true},
		{"7d", 7 * 24 * time.Hour, true},
		{"never", 0, false},
		{"", 0, true},
	} {
		age, cleanup := ArchiveRetentionAge(tc.retention)
		if age != tc.age || cleanup != tc.cleanup {
			t.Errorf("ArchiveRetentionAge(%q) = %v, %v; want %v, %v", tc.retention, age, cleanup, tc.age, tc.cleanup)
		}
	}
}

func TestClusterArchiveSettings(t *testing.T) {
	s, storage := nodeSettingsFixture(t)
	createStorageProfile(t, s, newStorageProfileRequest(storage, "s3-main"))
	fake := newClusterFake(t)
	setMembers(fake, "node-a")

	for _, tc := range []struct {
		retention, name, want string
	}{
		{"2d", "", "archive_local_retention must be instant, 1h, 1d, 7d or never"},
		{"", "slug", "archive_snapshot_name must be id or name"},
	} {
		req := clusterRequest(fake, "bad")
		req.ArchiveLocalRetention, req.ArchiveSnapshotName = tc.retention, tc.name
		if _, err := s.AddCluster(localCtx(), req); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("add with %q/%q: %v, want %q", tc.retention, tc.name, err, tc.want)
		}
	}

	req := clusterRequest(fake, "lab")
	req.StorageProfile = "s3-main"
	detail, err := s.AddCluster(localCtx(), req)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ArchiveLocalRetention != "instant" || detail.ArchiveSnapshotName != "id" {
		t.Errorf("defaults = %q, %q; want instant, id", detail.ArchiveLocalRetention, detail.ArchiveSnapshotName)
	}

	detail, err = s.UpdateCluster(localCtx(), "lab", UpdateClusterRequest{
		ArchiveLocalRetention: ptrString(" 1D "),
		ArchiveSnapshotName:   ptrString("Name"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if detail.ArchiveLocalRetention != "1d" || detail.ArchiveSnapshotName != "name" {
		t.Errorf("after update = %q, %q; want 1d, name", detail.ArchiveLocalRetention, detail.ArchiveSnapshotName)
	}
	if _, err := s.UpdateCluster(localCtx(), "lab", UpdateClusterRequest{ArchiveLocalRetention: ptrString("forever")}); err == nil {
		t.Error("an unknown retention was accepted")
	}
	// Changing one setting keeps the other.
	detail, err = s.UpdateCluster(localCtx(), "lab", UpdateClusterRequest{ArchiveLocalRetention: ptrString("never")})
	if err != nil {
		t.Fatal(err)
	}
	if detail.ArchiveLocalRetention != "never" || detail.ArchiveSnapshotName != "name" {
		t.Errorf("after a second update = %q, %q", detail.ArchiveLocalRetention, detail.ArchiveSnapshotName)
	}

	settings, err := s.ClusterNodeSettings(localCtx(), "lab")
	if err != nil {
		t.Fatal(err)
	}
	if settings.ArchiveLocalRetention != "never" {
		t.Errorf("node settings retention = %q, want never", settings.ArchiveLocalRetention)
	}
}

func TestReportNodeSettingsRetention(t *testing.T) {
	s, storage := nodeSettingsFixture(t)
	createStorageProfile(t, s, newStorageProfileRequest(storage, "s3-main"))
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	req := clusterRequest(fake, "lab")
	req.StorageProfile = "s3-main"
	if _, err := s.AddCluster(localCtx(), req); err != nil {
		t.Fatal(err)
	}

	// A node configured before the setting existed reports no retention: it
	// removes copies at once, which matches the default.
	node, err := s.ReportNodeSettings(localCtx(), "node-a", ReportNodeSettingsRequest{
		StorageProfile: ptrString("s3-main"), StorageVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if node.Settings.Outdated {
		t.Fatalf("a node without a reported retention on an instant cluster: %v", node.Settings.Reasons)
	}

	if _, err := s.UpdateCluster(localCtx(), "lab", UpdateClusterRequest{ArchiveLocalRetention: ptrString("1d")}); err != nil {
		t.Fatal(err)
	}
	node, err = s.GetNode(localCtx(), "node-a")
	if err != nil {
		t.Fatal(err)
	}
	want := "archive_local_retention is 1d; this node applied instant"
	if !node.Settings.Outdated || len(node.Settings.Reasons) != 1 || node.Settings.Reasons[0] != want {
		t.Errorf("reasons = %v, want %q", node.Settings.Reasons, want)
	}

	node, err = s.ReportNodeSettings(localCtx(), "node-a", ReportNodeSettingsRequest{
		StorageProfile: ptrString("s3-main"), StorageVersion: 1, ArchiveRetention: ptrString("1d"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if node.Settings.Outdated {
		t.Errorf("after reporting 1d: %v", node.Settings.Reasons)
	}

	if _, err := s.ReportNodeSettings(localCtx(), "node-a", ReportNodeSettingsRequest{
		StorageProfile: ptrString("s3-main"), StorageVersion: 1, ArchiveRetention: ptrString("2d"),
	}); err == nil {
		t.Error("an unknown retention was recorded")
	}

	// Without storage the retention means nothing, and is not judged.
	if _, err := s.UpdateCluster(localCtx(), "lab", UpdateClusterRequest{StorageProfile: ptrString("none")}); err != nil {
		t.Fatal(err)
	}
	node, err = s.ReportNodeSettings(localCtx(), "node-a", ReportNodeSettingsRequest{ArchiveRetention: ptrString("1d")})
	if err != nil {
		t.Fatal(err)
	}
	if node.Settings.Outdated {
		t.Errorf("a node without storage: %v", node.Settings.Reasons)
	}
}

func TestArchiveUsesTheClusterSnapshotNameStyle(t *testing.T) {
	f := newArchiveFixture(t)
	if _, err := f.conn.ExecContext(context.Background(),
		`UPDATE clusters SET archive_snapshot_name = 'name' WHERE id = ?`, f.cluster.ID); err != nil {
		t.Fatal(err)
	}
	f.addStopped(t, "sb-1")

	f.pass(t)
	row := f.row(t, "sb-1")
	if row.SnapshotName != "archive-box-sb-1" {
		t.Fatalf("snapshot name = %q, want archive-box-sb-1", row.SnapshotName)
	}
	snapshots := f.fake.Snapshots(f.org.Team)
	if len(snapshots) != 1 || snapshots[0].Name != "archive-box-sb-1" {
		t.Fatalf("snapshots = %+v", snapshots)
	}

	ref := f.archiveRef("archive-box-sb-1")
	f.fake.SetSnapshotPush(f.org.Team, "archive-box-sb-1", "active", ref)
	f.advance(5 * time.Second)
	f.pass(t)
	if row = f.row(t, "sb-1"); row.Status != archiveArchived || row.RegistryRef != ref {
		t.Fatalf("archived = %+v", row)
	}
}
