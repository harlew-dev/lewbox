package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/aerolvm/aerolvmtest"
	"github.com/harlew-dev/lewbox/internal/service"
)

func cliUpgradeRelease(t *testing.T, version, floor string) *httptest.Server {
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

func TestClusterUpgradeCLIParity(t *testing.T) {
	rel := cliUpgradeRelease(t, "v0.7.1", "v0.6.11")
	t.Setenv("LEWBOX_AEROLVM_RELEASE_URL", rel.URL)
	e := newEnvWith(t, service.WithAerolVMReleaseURL(rel.URL))
	fake := aerolvmtest.New(t)
	const token = "avm_admin_upgrade_token"
	fake.AdminToken = token
	fake.SetMembers([]aerolvm.Member{
		{NodeID: "node-a", NodeName: "node-a", APIURL: fake.URL(), Alive: true, Role: "mixed", Version: "v0.7.0", Leader: true, Voter: true, Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}}},
		{NodeID: "node-b", NodeName: "node-b", APIURL: "http://node-b.test:21212", Alive: true, Role: "mixed", Version: "v0.7.0", Voter: true, Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}}},
	})
	if _, errOut, code := e.cliIn(token+"\n", "cluster", "add", "lab",
		"--admin-url", fake.URL(), "--endpoint-url", fake.URL(), "--admin-token-stdin"); code != 0 {
		t.Fatalf("cluster add: exit %d: %s", code, errOut)
	}

	e.sameError([]string{"cluster", "upgrade", "status", "lab"}, "GET", "/v0/clusters/lab/upgrade", "", 404)
	planJSON := e.same([]string{"cluster", "upgrade", "lab", "--check", "--version", "v0.7.1"},
		"GET", "/v0/clusters/lab/upgrade/plan?version=v0.7.1", "", 200)
	if !strings.Contains(planJSON, `"needs_allow_downtime":true`) {
		t.Errorf("the plan does not say downtime is needed: %s", planJSON)
	}

	// Start, and compare the CLI's --json body with reading the same rollout
	// over the API.
	out, errOut, code := e.cli("cluster", "upgrade", "lab", "--version", "v0.7.1", "--allow-downtime", "--hold", "--yes")
	if code != 0 {
		t.Fatalf("cluster upgrade: exit %d: %s", code, errOut)
	}
	status, got := e.http("GET", "/v0/clusters/lab/upgrade", "")
	if status != http.StatusOK || out != got {
		t.Fatalf("start: CLI %s, HTTP (%d) %s", out, status, got)
	}
	if !strings.Contains(errOut, "Upgrade plan: v0.7.1") {
		t.Errorf("the plan was not shown before starting: %s", errOut)
	}

	e.same([]string{"cluster", "upgrade", "stop", "lab"}, "POST", "/v0/clusters/lab/upgrade/stop", "", 200)
	e.same([]string{"cluster", "upgrade", "resume", "lab"}, "POST", "/v0/clusters/lab/upgrade/resume", "", 200)
	e.same([]string{"cluster", "upgrade", "rollback", "lab", "--yes"}, "POST", "/v0/clusters/lab/upgrade/rollback", "", 200)
	e.same([]string{"cluster", "upgrade", "finalize", "lab", "--yes"}, "POST", "/v0/clusters/lab/upgrade/finalize", "", 200)
}
