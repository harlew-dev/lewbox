package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
)

// releaseFixture serves checksums.txt and release.json for one version, the
// way the release's download base does.
func releaseFixture(t *testing.T, version, metadata string) *httptest.Server {
	t.Helper()
	sum := func(data string) string {
		h := sha256.Sum256([]byte(data))
		return hex.EncodeToString(h[:])
	}
	checksums := strings.Join([]string{
		sum("sandboxd") + "  sandboxd_linux_amd64",
		sum("toolboxd") + "  toolboxd_linux_amd64",
		sum("caddy") + "  caddy_linux_amd64",
		sum(metadata) + "  release.json",
	}, "\n") + "\n"
	mux := http.NewServeMux()
	mux.HandleFunc("GET /"+version+"/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(checksums))
	})
	mux.HandleFunc("GET /"+version+"/release.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(metadata))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func releaseMetadata(version, floor string, migrations ...int) string {
	meta := map[string]any{
		"version":            version,
		"upgrade_from_min":   floor,
		"one_way_migrations": migrations,
	}
	raw, _ := json.Marshal(meta)
	return string(raw)
}

func TestPlanClusterUpgrade(t *testing.T) {
	rel := releaseFixture(t, "v0.7.1", releaseMetadata("v0.7.1", "v0.6.11"))
	s := newClusterService(t, WithAerolVMReleaseURL(rel.URL))
	fake := newClusterFake(t)
	fake.SetMembers([]aerolvm.Member{
		{NodeID: "node-a", NodeName: "node-a", APIURL: "http://node-a.test:21212", Role: "mixed", Alive: true, Version: "v0.7.0", Leader: true, Voter: true},
		{NodeID: "node-b", NodeName: "node-b", APIURL: "http://node-b.test:21212", Role: "mixed", Alive: true, Version: "v0.7.0", Voter: true},
	})
	detail := addTestCluster(t, s, fake, "lab")

	plan, err := s.PlanClusterUpgrade(localCtx(), "lab", PlanClusterUpgradeRequest{Version: "v0.7.1"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Allowed || plan.Tested || plan.TargetVersion != "v0.7.1" || plan.ClusterID != detail.ID {
		t.Fatalf("plan = %+v", plan)
	}
	if len(plan.Nodes) != 2 || plan.Nodes[0].Node != "node-b" || plan.Nodes[1].Node != "node-a" {
		t.Fatalf("node order = %+v", plan.Nodes)
	}
	if plan.Nodes[0].Order != 1 || plan.Nodes[1].Order != 2 {
		t.Fatalf("orders = %+v", plan.Nodes)
	}
	if plan.Skew.UpgradeFromMin != "v0.6.11" || len(plan.Skew.Below) != 0 {
		t.Fatalf("skew = %+v", plan.Skew)
	}
	if len(plan.Warnings) == 0 {
		t.Fatal("expected warnings")
	}
}

func TestPlanClusterUpgradeNeedsAllowDowntime(t *testing.T) {
	rel := releaseFixture(t, "v0.7.1", releaseMetadata("v0.7.1", "v0.6.11"))
	for _, tc := range []struct {
		voters int
		want   bool
	}{
		{1, true},
		{2, true},
		{3, false},
	} {
		t.Run(fmt.Sprintf("%d voters", tc.voters), func(t *testing.T) {
			s := newClusterService(t, WithAerolVMReleaseURL(rel.URL))
			fake := newClusterFake(t)
			members := make([]aerolvm.Member, 0, tc.voters)
			for i := range tc.voters {
				members = append(members, aerolvm.Member{
					NodeID:   fmt.Sprintf("node-%d", i),
					NodeName: fmt.Sprintf("node-%d", i),
					APIURL:   fmt.Sprintf("http://node-%d.test:21212", i),
					Role:     "mixed",
					Alive:    true,
					Version:  "v0.7.0",
					Voter:    true,
					Leader:   i == 0,
				})
			}
			fake.SetMembers(members)
			addTestCluster(t, s, fake, "lab")

			plan, err := s.PlanClusterUpgrade(localCtx(), "lab", PlanClusterUpgradeRequest{Version: "v0.7.1"})
			if err != nil {
				t.Fatal(err)
			}
			if plan.NeedsAllowDowntime != tc.want {
				t.Fatalf("needs_allow_downtime = %v, want %v (plan %+v)", plan.NeedsAllowDowntime, tc.want, plan)
			}
		})
	}
}

func TestPlanClusterUpgradeRefusesSkew(t *testing.T) {
	rel := releaseFixture(t, "v0.7.1", releaseMetadata("v0.7.1", "v0.7.1"))
	s := newClusterService(t, WithAerolVMReleaseURL(rel.URL))
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	addTestCluster(t, s, fake, "lab")

	plan, err := s.PlanClusterUpgrade(localCtx(), "lab", PlanClusterUpgradeRequest{Version: "v0.7.1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Allowed {
		t.Fatalf("plan allowed with skew: %+v", plan)
	}
	if len(plan.Refusals) != 1 || !strings.Contains(plan.Refusals[0], "needs v0.7.1 or later") {
		t.Fatalf("refusals = %v", plan.Refusals)
	}
	if len(plan.Skew.Below) != 1 || plan.Skew.Below[0] != "v0.7.0" {
		t.Fatalf("below = %v", plan.Skew.Below)
	}
}

func TestPlanClusterUpgradeRefusesDowngrade(t *testing.T) {
	rel := releaseFixture(t, "v0.6.13", releaseMetadata("v0.6.13", "v0.6.0"))
	s := newClusterService(t, WithAerolVMReleaseURL(rel.URL))
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	addTestCluster(t, s, fake, "lab")

	plan, err := s.PlanClusterUpgrade(localCtx(), "lab", PlanClusterUpgradeRequest{Version: "v0.6.13"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Allowed || len(plan.Refusals) == 0 || !strings.Contains(plan.Refusals[0], "newer than") {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestPlanClusterUpgradeOneWayMigrations(t *testing.T) {
	rel := releaseFixture(t, "v0.7.1", releaseMetadata("v0.7.1", "v0.6.11", 7))
	s := newClusterService(t, WithAerolVMReleaseURL(rel.URL))
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	addTestCluster(t, s, fake, "lab")

	plan, err := s.PlanClusterUpgrade(localCtx(), "lab", PlanClusterUpgradeRequest{Version: "v0.7.1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.RollbackAfterFinalize || len(plan.OneWayMigrations) != 1 || plan.OneWayMigrations[0] != 7 {
		t.Fatalf("migrations = %+v rollback = %v", plan.OneWayMigrations, plan.RollbackAfterFinalize)
	}
}

func TestPlanClusterUpgradeFailsClosed(t *testing.T) {
	// A release server that answers 404 for everything.
	rel := httptest.NewServer(http.NotFoundHandler())
	rel.Close()
	s := newClusterService(t, WithAerolVMReleaseURL(rel.URL))
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	addTestCluster(t, s, fake, "lab")

	_, err := s.PlanClusterUpgrade(localCtx(), "lab", PlanClusterUpgradeRequest{Version: "v0.7.1"})
	var se *Error
	if !errors.As(err, &se) || se.Code != CodeUnavailable {
		t.Fatalf("err = %v", err)
	}
}

func TestStartClusterUpgradeNeedsAllowDowntime(t *testing.T) {
	rel := releaseFixture(t, "v0.7.1", releaseMetadata("v0.7.1", "v0.6.11"))
	s := newClusterService(t, WithAerolVMReleaseURL(rel.URL))
	fake := newClusterFake(t)
	fake.SetMembers([]aerolvm.Member{
		{NodeID: "node-a", NodeName: "node-a", APIURL: "http://node-a.test:21212", Role: "mixed", Alive: true, Version: "v0.7.0", Voter: true},
		{NodeID: "node-b", NodeName: "node-b", APIURL: "http://node-b.test:21212", Role: "mixed", Alive: true, Version: "v0.7.0", Voter: true},
	})
	addTestCluster(t, s, fake, "lab")

	_, err := s.StartClusterUpgrade(localCtx(), "lab", StartClusterUpgradeRequest{Version: "v0.7.1"})
	var se *Error
	if !errors.As(err, &se) || se.Code != CodeConflict || !strings.Contains(se.Message, "--allow-downtime") {
		t.Fatalf("err = %v", err)
	}
	if applies := fake.UpgradeApplies(); len(applies) != 0 {
		t.Fatalf("applied without allow-downtime: %+v", applies)
	}

	up, err := s.StartClusterUpgrade(localCtx(), "lab", StartClusterUpgradeRequest{
		Version: "v0.7.1", AllowDowntime: true, Hold: true, Batch: "25%", PauseAfter: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if up.Phase != "running" || len(up.Nodes) != 2 || up.TargetVersion != "v0.7.1" {
		t.Fatalf("upgrade = %+v", up)
	}
	applies := fake.UpgradeApplies()
	if len(applies) != 1 || applies[0].Source != aerolvm.UpgradeSourceRelease || !applies[0].AllowDowntime ||
		!applies[0].Hold || applies[0].Batch != "25%" || applies[0].PauseAfter != 1 {
		t.Fatalf("applies = %+v", applies)
	}
	if applies[0].Files["sandboxd_linux_amd64"] == "" || applies[0].Files["release.json"] == "" {
		t.Fatalf("files = %+v", applies[0].Files)
	}
	list, err := s.ListEvents(localCtx(), ListEventsRequest{Action: "cluster.upgrade.started"})
	if err != nil || len(list.Data) != 1 {
		t.Fatalf("events = %v, %v", eventNames(list), err)
	}
}

func TestGetClusterUpgradeNoRollout(t *testing.T) {
	s := newClusterService(t)
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	addTestCluster(t, s, fake, "lab")

	_, err := s.GetClusterUpgrade(localCtx(), "lab")
	var se *Error
	if !errors.As(err, &se) || se.Code != CodeNotFound || !strings.Contains(se.Message, "no upgrade has run on cluster") {
		t.Fatalf("err = %v", err)
	}
}

func TestClusterUpgradeCommandsRecordEvents(t *testing.T) {
	s := newClusterService(t)
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	addTestCluster(t, s, fake, "lab")
	fake.SetUpgrade(&aerolvm.UpgradeRollout{
		ID: "r-1", State: aerolvm.UpgradeStateRunning, Direction: aerolvm.UpgradeDirectionUpgrade,
		TargetVersion: "v0.7.1", PreviousVersion: "v0.7.0",
		Nodes: []aerolvm.UpgradeNode{{NodeID: "node-a", Role: "mixed", State: aerolvm.UpgradeNodePending, Version: "v0.7.0", Order: 1}},
	})

	for _, tc := range []struct {
		command string
		phase   string
		event   string
		call    func(context.Context, *Service) (ClusterUpgrade, error)
	}{
		{"stop", "stopped", "cluster.upgrade.stopped", func(ctx context.Context, s *Service) (ClusterUpgrade, error) {
			return s.StopClusterUpgrade(ctx, "lab")
		}},
		{"resume", "running", "cluster.upgrade.resumed", func(ctx context.Context, s *Service) (ClusterUpgrade, error) {
			return s.ResumeClusterUpgrade(ctx, "lab")
		}},
		{"rollback", "rolled_back", "cluster.upgrade.rolled_back", func(ctx context.Context, s *Service) (ClusterUpgrade, error) {
			return s.RollbackClusterUpgrade(ctx, "lab")
		}},
		{"finalize", "finalized", "cluster.upgrade.finalized", func(ctx context.Context, s *Service) (ClusterUpgrade, error) {
			return s.FinalizeClusterUpgrade(ctx, "lab")
		}},
	} {
		up, err := tc.call(localCtx(), s)
		if err != nil {
			t.Fatalf("%s: %v", tc.command, err)
		}
		if up.Phase != tc.phase {
			t.Fatalf("%s phase = %q", tc.command, up.Phase)
		}
		list, err := s.ListEvents(localCtx(), ListEventsRequest{Action: tc.event})
		if err != nil || len(list.Data) != 1 {
			t.Fatalf("%s events = %v, %v", tc.command, eventNames(list), err)
		}
	}
}

func TestClusterUpgradeIsAdminOnly(t *testing.T) {
	s := newClusterService(t)
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	addTestCluster(t, s, fake, "lab")
	ctx := WithPrincipal(context.Background(), Principal{Kind: "user", UserID: "user-1"})
	for name, call := range map[string]func() error{
		"status": func() error { _, err := s.GetClusterUpgrade(ctx, "lab"); return err },
		"plan": func() error {
			_, err := s.PlanClusterUpgrade(ctx, "lab", PlanClusterUpgradeRequest{Version: "v0.7.1"})
			return err
		},
		"start": func() error {
			_, err := s.StartClusterUpgrade(ctx, "lab", StartClusterUpgradeRequest{Version: "v0.7.1"})
			return err
		},
		"stop": func() error { _, err := s.StopClusterUpgrade(ctx, "lab"); return err },
	} {
		var se *Error
		if err := call(); !errors.As(err, &se) || se.Code != CodeForbidden {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestReleaseMetadataFixtureMatchesVersion(t *testing.T) {
	rel := releaseFixture(t, "v0.7.1", fmt.Sprintf(`{"version":%q,"upgrade_from_min":"v0.6.11"}`, "v0.7.1"))
	files, err := aerolvm.NewReleaseSource(rel.URL).Fetch(context.Background(), "v0.7.1")
	if err != nil {
		t.Fatal(err)
	}
	if files.Version != "v0.7.1" || files.Metadata.UpgradeFromMin != "v0.6.11" || len(files.Files) != 4 {
		t.Fatalf("release = %+v", files)
	}
}
