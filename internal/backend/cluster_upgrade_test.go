package backend_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/backend"
	"github.com/harlew-dev/lewbox/internal/service"
)

func upgradeReleaseServer(t *testing.T, version, floor string) *httptest.Server {
	t.Helper()
	sum := func(data string) string {
		h := sha256.Sum256([]byte(data))
		return hex.EncodeToString(h[:])
	}
	meta, _ := json.Marshal(map[string]any{"version": version, "upgrade_from_min": floor})
	checksums := strings.Join([]string{
		sum("sandboxd") + "  sandboxd_linux_amd64",
		sum("toolboxd") + "  toolboxd_linux_amd64",
		sum("caddy") + "  caddy_linux_amd64",
		sum(string(meta)) + "  release.json",
	}, "\n") + "\n"
	mux := http.NewServeMux()
	mux.HandleFunc("GET /"+version+"/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(checksums))
	})
	mux.HandleFunc("GET /"+version+"/release.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(meta)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestClusterUpgradeBackendsAgree(t *testing.T) {
	rel := upgradeReleaseServer(t, "v0.7.1", "v0.6.11")
	h := newHarness(t, service.WithAerolVMReleaseURL(rel.URL))
	ctx := context.Background()
	h.fake.SetMembers([]aerolvm.Member{
		{NodeID: "node-a", NodeName: "node-a", APIURL: h.fake.URL(), Alive: true, Role: "mixed", Version: "v0.7.0", Leader: true, Voter: true},
		{NodeID: "node-b", NodeName: "node-b", APIURL: "http://node-b.test:21212", Alive: true, Role: "mixed", Version: "v0.7.0", Voter: true},
	})
	if _, err := h.local.AddCluster(ctx, service.AddClusterRequest{
		Slug:        "lab",
		AdminURL:    h.fake.URL(),
		EndpointURL: h.fake.URL(),
		Runtime:     "docker",
		AdminToken:  clusterToken,
	}); err != nil {
		t.Fatal(err)
	}

	status := func(ctx context.Context, b backend.Backend) (any, error) {
		return b.GetClusterUpgrade(ctx, "lab")
	}
	plan := func(ctx context.Context, b backend.Backend) (any, error) {
		return b.PlanClusterUpgrade(ctx, "lab", service.PlanClusterUpgradeRequest{Version: "v0.7.1"})
	}
	start := func(ctx context.Context, b backend.Backend) (any, error) {
		return b.StartClusterUpgrade(ctx, "lab", service.StartClusterUpgradeRequest{
			Version: "v0.7.1", AllowDowntime: true, Hold: true,
		})
	}

	runSteps(t, h, []step{
		both("plan", plan),
		both("status before a rollout", status),
		{name: "start (local) and read it back (remote)", local: start, remote: status, compare: true},
		{
			name: "stop",
			local: func(ctx context.Context, b backend.Backend) (any, error) {
				return b.StopClusterUpgrade(ctx, "lab")
			},
			remote:  status,
			compare: true,
		},
		{
			name: "resume",
			local: func(ctx context.Context, b backend.Backend) (any, error) {
				return b.ResumeClusterUpgrade(ctx, "lab")
			},
			remote:  status,
			compare: true,
		},
		{
			name: "rollback",
			local: func(ctx context.Context, b backend.Backend) (any, error) {
				return b.RollbackClusterUpgrade(ctx, "lab")
			},
			remote:  status,
			compare: true,
		},
		{
			name: "finalize",
			local: func(ctx context.Context, b backend.Backend) (any, error) {
				return b.FinalizeClusterUpgrade(ctx, "lab")
			},
			remote:  status,
			compare: true,
		},
	})
}
