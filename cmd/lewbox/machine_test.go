package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/aerolvm/aerolvmtest"
	"github.com/harlew-dev/lewbox/internal/machine"
	"github.com/harlew-dev/lewbox/internal/machine/machinetest"
	"github.com/harlew-dev/lewbox/internal/service"
)

const testScript = `#!/usr/bin/env bash
# Usage: install.sh [options]
#   --version <tag>
#   --node-name <name>
#   --local
#   --domain <domain>
#   --dns-provider <provider>
#   --dns-api-token <token>
#   --acme-email <email>
#   --public-host <host>
#   --with-firecracker
#   --pat-scope <scope>
#   --no-start
#   --sandboxd-url <url>
#   --toolboxd-url <url>
#   --checksums-url <url>
#   --caddy-binary-url <url>
#   --cli-url <url>
`

type harness struct {
	t        *testing.T
	runner   *machinetest.Runner
	hd       *hostDeps
	files    map[string]string
	active   bool
	pat      string
	bin      string
	execArgs []string
	probeErr error
}

func newHarness(t *testing.T, localURL string) *harness {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("LEWBOX_CONTEXT", "")
	t.Setenv("LEWBOX_API_URL", "")
	t.Setenv("LEWBOX_TOKEN", "")
	t.Setenv("LEWBOX_OUTPUT", "")
	t.Setenv("LEWBOX_AEROLVM_FROM", "")
	t.Setenv("LEWBOX_RELEASE_FROM", "")
	t.Setenv("LEWBOX_ALLOW_ROOT", "")
	h := &harness{
		t:      t,
		runner: machinetest.New(),
		files:  make(map[string]string),
		pat:    "pat-secret-value",
		bin:    filepath.Join(t.TempDir(), "lewbox"),
	}
	if err := os.WriteFile(h.bin, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.runner.RunFn = h.run
	h.runner.OutputFn = h.output

	info, _ := os.Stat("/dev/null")
	h.hd = &hostDeps{
		runner: h.runner,
		env: machine.Env{
			GOOS: "linux",
			Listen: func(string, string) (net.Listener, error) {
				return stubListener{}, nil
			},
			Stat: func(string) (os.FileInfo, error) {
				return info, nil
			},
			Run:             func(context.Context, machine.Cmd) error { return nil },
			SandboxdRunning: func(context.Context) bool { return false },
		},
		localAPIURL: localURL,
		probe: func(context.Context, string) error {
			return h.probeErr
		},
		executable: func() (string, error) { return h.bin, nil },
		euid:       func() int { return 1000 },
		execFn: func(_ string, argv []string, _ []string) error {
			h.execArgs = append([]string(nil), argv...)
			return nil
		},
		sleep: func(context.Context, time.Duration) {},
		now: func() time.Time {
			return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
		},
		hostname: func() (string, error) { return "node-1", nil },
	}
	return h
}

type stubListener struct{}

func (stubListener) Accept() (net.Conn, error) { return nil, io.EOF }
func (stubListener) Close() error              { return nil }
func (stubListener) Addr() net.Addr            { return &net.TCPAddr{} }

func (h *harness) run(_ context.Context, cmd machine.Cmd) error {
	if cmd.Name == "install" && len(cmd.Args) > 0 {
		path := cmd.Args[len(cmd.Args)-1]
		content := ""
		if cmd.Stdin != nil {
			b, _ := io.ReadAll(cmd.Stdin)
			content = string(b)
		}
		h.files[path] = content
		return nil
	}
	if cmd.Name == "systemctl" && len(cmd.Args) > 0 && cmd.Args[0] == "restart" {
		h.active = true
	}
	return nil
}

func (h *harness) output(_ context.Context, cmd machine.Cmd) ([]byte, error) {
	switch cmd.Name {
	case "cat":
		path := cmd.Args[len(cmd.Args)-1]
		if path == "/etc/sandboxd/sandboxd.env" {
			return []byte("OTHER=1\nSB_PAT_TOKEN=" + h.pat + "\n"), nil
		}
		if content, ok := h.files[path]; ok {
			return []byte(content), nil
		}
		return nil, os.ErrNotExist
	case "systemctl":
		if len(cmd.Args) > 0 && cmd.Args[0] == "is-active" {
			if h.active {
				return []byte("active\n"), nil
			}
			return nil, fmt.Errorf("inactive")
		}
	}
	return nil, fmt.Errorf("unexpected Output: %s", cmd.Display())
}

func (h *harness) runCLI(stdin string, args ...string) (string, string, int) {
	h.t.Helper()
	var out, errOut strings.Builder
	code := runWithApp(context.Background(), args, strings.NewReader(stdin), &out, &errOut, h.hd)
	return out.String(), errOut.String(), code
}

// cliWith runs the CLI against the test orchestrator's database in local
// mode, with the harness's machine dependencies.
func (e *env) cliWith(h *harness, stdin string, args ...string) (string, string, int) {
	e.t.Helper()
	var out, errOut strings.Builder
	args = append([]string{"--db", e.dbPath, "--context", "local"}, args...)
	code := runWithApp(context.Background(), args, strings.NewReader(stdin), &out, &errOut, h.hd)
	return out.String(), errOut.String(), code
}

func sumHex(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

// writeMirror builds a --from directory for one version.
func writeMirror(t *testing.T, version, script string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "install.sh"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(sumHex([]byte(script))+"  install.sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The pinned version is checked against the hash built into the binary,
	// not the mirror's list, so pin this fake installer's hash.
	installerPin = sumHex([]byte(script))
	t.Cleanup(func() { installerPin = "" })
	arch := runtime.GOARCH
	for _, name := range []string{"sandboxd_linux_" + arch, "toolboxd_linux_" + arch, "caddy_linux_" + arch, "aerolvm_linux_" + arch} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("binary"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Dir(dir)
}

func saveState(t *testing.T, state machine.State) {
	t.Helper()
	if err := state.Save(); err != nil {
		t.Fatal(err)
	}
}

func loadState(t *testing.T) machine.State {
	t.Helper()
	state, err := machine.Load()
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// writeRelease builds a Lewbox release directory.
func writeRelease(t *testing.T, version, aerolvmVersion string, binary []byte) string {
	t.Helper()
	dir := t.TempDir()
	name := "lewbox_" + runtime.GOOS + "_" + runtime.GOARCH
	files := map[string][]byte{
		"release.json":  []byte(fmt.Sprintf(`{"version":%q,"aerolvm_version":%q}`, version, aerolvmVersion)),
		"checksums.txt": []byte(sumHex(binary) + "  " + name + "\n"),
		name:            binary,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestInstallDryRun(t *testing.T) {
	from := writeMirror(t, "v0.7.0", testScript)
	h := newHarness(t, "http://127.0.0.1:1")

	cases := []struct {
		name  string
		args  []string
		stdin string
		want  []string
		mode  string
	}{
		{
			name: "local",
			args: []string{"install", "--dry-run", "--from", from, "--name", "node-1", "--aerolvm-version", "v0.7.0"},
			want: []string{"--version v0.7.0", "--node-name node-1", "--local"},
			mode: machine.ModeLocal,
		},
		{
			name: "public host",
			args: []string{"install", "--dry-run", "--from", from, "--name", "node-1", "--public-host", "10.0.0.7"},
			want: []string{
				"--version v0.7.0", "--pat-scope host", "--node-name node-1",
				"--public-host 10.0.0.7", "--no-start",
			},
			mode: machine.ModePublicHost,
		},
		{
			name: "domain",
			args: []string{
				"install", "--dry-run", "--from", from, "--name", "eu-west-1",
				"--domain", "eu.example", "--dns-api-token-stdin", "--acme-email", "ops@example",
			},
			stdin: "cf-secret\n",
			want: []string{
				"--pat-scope host", "--domain eu.example", "--dns-provider cloudflare",
				"--dns-api-token ******", "--acme-email ops@example", "--no-start",
			},
			mode: machine.ModeDomain,
		},
		{
			name: "firecracker",
			args: []string{"install", "--dry-run", "--from", from, "--public-host", "10.0.0.7", "--runtime", "firecracker"},
			want: []string{"--with-firecracker", "--no-start"},
			mode: machine.ModePublicHost,
		},
		{
			name: "passthrough",
			args: []string{"install", "--dry-run", "--from", from, "--public-host", "10.0.0.7", "--", "--lb-health-port", "8081"},
			want: []string{"--lb-health-port 8081"},
			mode: machine.ModePublicHost,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h.runner.Reset()
			out, errOut, code := h.runCLI(tc.stdin, tc.args...)
			if code != 0 {
				t.Fatalf("exit %d: %s", code, errOut)
			}
			var got installOutput
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatal(err)
			}
			if got.Mode != tc.mode || got.Started || got.AerolVMVersion != "v0.7.0" {
				t.Errorf("output = %+v", got)
			}
			for _, want := range tc.want {
				if !strings.Contains(errOut, want) {
					t.Errorf("stderr does not contain %q:\n%s", want, errOut)
				}
			}
			if !strings.Contains(errOut, "sudo bash ") || !strings.Contains(errOut, "file://") {
				t.Errorf("command not printed with mirror URLs:\n%s", errOut)
			}
			if strings.Contains(errOut, "cf-secret") {
				t.Errorf("DNS token leaked:\n%s", errOut)
			}
			if len(h.runner.Commands()) != 0 {
				t.Errorf("dry run ran commands: %v", h.runner.Displays())
			}
		})
	}
}

func TestInstallRefusesBadChecksum(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "v0.7.0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "install.sh"), []byte(testScript), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(strings.Repeat("0", 64)+"  install.sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, "http://127.0.0.1:1")
	_, errOut, code := h.runCLI("", "install", "--dry-run", "--from", filepath.Dir(dir), "--public-host", "10.0.0.7")
	if code != 1 || !strings.Contains(errOut, "sha256") {
		t.Errorf("exit %d stderr %q", code, errOut)
	}
	if len(h.runner.Commands()) != 0 {
		t.Errorf("ran commands: %v", h.runner.Displays())
	}
}

func TestInstallRefusesRoot(t *testing.T) {
	from := writeMirror(t, "v0.7.0", testScript)
	h := newHarness(t, "http://127.0.0.1:1")
	h.hd.euid = func() int { return 0 }
	_, errOut, code := h.runCLI("", "install", "--dry-run", "--from", from, "--public-host", "10.0.0.7")
	if code != 1 || !strings.Contains(errOut, "running as root") || !strings.Contains(errOut, "LEWBOX_ALLOW_ROOT=1 lewbox") {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
	t.Setenv("LEWBOX_ALLOW_ROOT", "1")
	if _, errOut, code := h.runCLI("", "install", "--dry-run", "--from", from, "--public-host", "10.0.0.7"); code != 0 {
		t.Fatalf("with LEWBOX_ALLOW_ROOT: exit %d: %s", code, errOut)
	}
}

func TestNodeAddNewCluster(t *testing.T) {
	e := newEnv(t)
	fake := aerolvmtest.New(t)
	h := newHarness(t, fake.URL())
	// The PAT is the only credential on the host before the admin token.
	fake.AdminToken = h.pat
	fake.SetMembers([]aerolvm.Member{{
		NodeID: "node-1", NodeName: "node-1", APIURL: fake.URL(), Alive: true,
		Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
	}})
	from := writeMirror(t, "v0.7.0", testScript)
	saveState(t, machine.State{
		Name: "node-1", Mode: machine.ModePublicHost, PublicHost: "10.0.0.7",
		Runtime: "docker", AerolVMVersion: "v0.7.0", From: from,
	})

	out, errOut, code := e.cliWith(h, "", "node", "add", "node-1", "--new-cluster", "lab",
		"--admin-url", fake.URL(), "--endpoint-url", fake.URL())
	if code != 0 {
		t.Fatalf("node add: exit %d: %s", code, errOut)
	}
	var detail service.ClusterDetail
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Slug != "lab" || detail.Nodes != 1 || len(detail.NodeList) != 1 || detail.NodeList[0].Name != "node-1" {
		t.Errorf("cluster = %+v", detail)
	}
	if state := loadState(t); state.Cluster != "lab" {
		t.Errorf("state cluster = %q", state.Cluster)
	}

	displays := h.runner.Displays()
	if len(displays) == 0 {
		t.Fatal("no commands ran")
	}
	init := displays[0]
	for _, want := range []string{"bash", "--version v0.7.0", "--pat-scope host", "init --role mixed --node-id node-1 --advertise 10.0.0.7"} {
		if !strings.Contains(init, want) {
			t.Errorf("init command does not contain %q: %s", want, init)
		}
	}
	if !strings.Contains(init, "sandboxd_linux_") {
		t.Errorf("mirror URLs missing: %s", init)
	}
	if !h.runner.Ran("cat", "/etc/sandboxd/sandboxd.env") {
		t.Error("PAT was not read with sudo cat")
	}
	if h.files[machine.EnvironmentFile] != machine.EnvFileContent {
		t.Errorf("%s = %q", machine.EnvironmentFile, h.files[machine.EnvironmentFile])
	}
	if h.files[machine.DropInFile] != machine.DropInContent {
		t.Errorf("%s = %q", machine.DropInFile, h.files[machine.DropInFile])
	}
	if _, ok := h.files["/etc/sandboxd/sandboxd.env"]; ok {
		t.Error("Lewbox wrote the installer's sandboxd.env")
	}
	if !h.runner.Ran("systemctl", "restart", "sandboxd") {
		t.Error("sandboxd was not restarted")
	}

	var minted *aerolvm.Token
	for _, token := range fake.Tokens() {
		if token.Name == "lewbox-lab" {
			token := token
			minted = &token
		}
	}
	if minted == nil || minted.Role != "admin" || minted.Team != "*" {
		t.Fatalf("minted tokens = %+v", fake.Tokens())
	}
	if detail.AdminTokenLast4 != minted.Token[len(minted.Token)-4:] {
		t.Errorf("cluster token last4 = %q, minted %q", detail.AdminTokenLast4, minted.Token)
	}
}

func TestNodeAddNewClusterIsIdempotent(t *testing.T) {
	e := newEnv(t)
	fake := aerolvmtest.New(t)
	h := newHarness(t, fake.URL())
	fake.AdminToken = h.pat
	fake.SetMembers([]aerolvm.Member{{
		NodeID: "node-1", NodeName: "node-1", APIURL: fake.URL(), Alive: true,
		Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
	}})
	from := writeMirror(t, "v0.7.0", testScript)
	saveState(t, machine.State{
		Name: "node-1", Mode: machine.ModePublicHost, PublicHost: "10.0.0.7",
		Runtime: "docker", AerolVMVersion: "v0.7.0", From: from,
	})

	args := []string{"node", "add", "node-1", "--new-cluster", "lab", "--admin-url", fake.URL(), "--endpoint-url", fake.URL()}
	for i := 0; i < 2; i++ {
		out, errOut, code := e.cliWith(h, "", args...)
		if code != 0 {
			t.Fatalf("run %d: exit %d: %s", i+1, code, errOut)
		}
		var detail service.ClusterDetail
		if err := json.Unmarshal([]byte(out), &detail); err != nil {
			t.Fatal(err)
		}
		if detail.Slug != "lab" || detail.Nodes != 1 {
			t.Errorf("run %d: cluster = %+v", i+1, detail)
		}
		if detail.Status != "ready" {
			t.Errorf("run %d: status = %q (%s)", i+1, detail.Status, detail.StatusDetail)
		}
	}
	count := 0
	for _, token := range fake.Tokens() {
		if token.Name == "lewbox-lab" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("lewbox-lab tokens = %d, want 1", count)
	}
}

func TestNodeAddJoin(t *testing.T) {
	e := newEnv(t)
	fake := aerolvmtest.New(t)
	const clusterToken = "avm_cluster_token"
	fake.AdminToken = clusterToken
	seed := aerolvm.Member{
		NodeID: "seed", NodeName: "seed", APIURL: fake.URL(), Alive: true,
		Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
	}
	fake.SetMembers([]aerolvm.Member{seed})

	if _, errOut, code := e.cliIn(clusterToken+"\n", "cluster", "add", "lab",
		"--admin-url", fake.URL(), "--endpoint-url", fake.URL(), "--admin-token-stdin"); code != 0 {
		t.Fatalf("cluster add: exit %d: %s", code, errOut)
	}

	h := newHarness(t, fake.URL())
	h.runner.RunFn = func(ctx context.Context, cmd machine.Cmd) error {
		if cmd.Name == "bash" && strings.Contains(strings.Join(cmd.Args, " "), " join ") {
			fake.SetMembers([]aerolvm.Member{seed, {
				NodeID: "node-2", NodeName: "node-2", APIURL: fake.URL(), Alive: true,
				Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
			}})
		}
		return h.run(ctx, cmd)
	}
	from := writeMirror(t, "v0.7.0", testScript)
	saveState(t, machine.State{
		Name: "node-2", Mode: machine.ModePublicHost, PublicHost: "10.0.0.8",
		Runtime: "docker", AerolVMVersion: "v0.7.0", From: from,
	})

	t.Setenv("LEWBOX_TOKEN", e.token)
	out, errOut, code := h.runCLI("", "--api-url", e.srv.URL, "node", "add", "node-2", "--cluster", "lab")
	if code != 0 {
		t.Fatalf("node add join: exit %d: %s", code, errOut)
	}
	var node service.Node
	if err := json.Unmarshal([]byte(out), &node); err != nil {
		t.Fatal(err)
	}
	if node.Name != "node-2" || node.Cluster != "lab" {
		t.Errorf("node = %+v", node)
	}
	if state := loadState(t); state.Cluster != "lab" || state.AerolVMVersion != "v0.7.0" {
		t.Errorf("state = %+v", state)
	}

	joins := fake.JoinTokens()
	if len(joins) != 1 {
		t.Fatalf("join tokens = %+v", joins)
	}
	joinLine := ""
	for _, line := range h.runner.Displays() {
		if strings.Contains(line, " join ") {
			joinLine = line
		}
	}
	if joinLine == "" {
		t.Fatalf("no join command: %v", h.runner.Displays())
	}
	for _, want := range []string{" join ", "--token ******", "--ca-cert-hash sha256:fake", "--node-id node-2", "--role mixed"} {
		if !strings.Contains(joinLine, want) {
			t.Errorf("join command missing %q: %s", want, joinLine)
		}
	}
	if strings.Contains(errOut, joins[0].Token) {
		t.Errorf("join token leaked to stderr: %s", errOut)
	}
}

func TestNodeAddJoinAlreadyMember(t *testing.T) {
	e := newEnv(t)
	fake := aerolvmtest.New(t)
	const clusterToken = "avm_cluster_token"
	fake.AdminToken = clusterToken
	fake.SetMembers([]aerolvm.Member{
		{NodeID: "seed", NodeName: "seed", APIURL: fake.URL(), Alive: true, Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}}},
		{NodeID: "node-2", NodeName: "node-2", APIURL: fake.URL(), Alive: true, Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}}},
	})
	if _, errOut, code := e.cliIn(clusterToken+"\n", "cluster", "add", "lab",
		"--admin-url", fake.URL(), "--endpoint-url", fake.URL(), "--admin-token-stdin"); code != 0 {
		t.Fatalf("cluster add: exit %d: %s", code, errOut)
	}
	h := newHarness(t, fake.URL())
	saveState(t, machine.State{
		Name: "node-2", Mode: machine.ModePublicHost, PublicHost: "10.0.0.8",
		Runtime: "docker", AerolVMVersion: "v0.7.0", Cluster: "lab",
	})

	out, errOut, code := e.cliWith(h, "", "node", "add", "node-2", "--cluster", "lab")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var node service.Node
	if err := json.Unmarshal([]byte(out), &node); err != nil {
		t.Fatal(err)
	}
	if node.Name != "node-2" {
		t.Errorf("node = %+v", node)
	}
	for _, line := range h.runner.Displays() {
		if strings.Contains(line, "bash") {
			t.Errorf("ran the installer for an existing member: %s", line)
		}
	}
}

func TestJoinRefusals(t *testing.T) {
	cases := []struct {
		name     string
		mode     string
		runtime  string
		cluster  string
		probeErr error
		want     string
	}{
		{name: "local mode", mode: machine.ModeLocal, runtime: "docker", cluster: "lab", want: "local-mode install can't join"},
		{name: "runtime mismatch", mode: machine.ModePublicHost, runtime: "docker", cluster: "lab", want: "runs firecracker"},
		{name: "unreachable", mode: machine.ModePublicHost, runtime: "docker", cluster: "lab", probeErr: fmt.Errorf("timeout"), want: "7000-7003"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			fake := aerolvmtest.New(t)
			const clusterToken = "avm_cluster_token"
			fake.AdminToken = clusterToken
			clusterRuntime := "docker"
			capacity := "docker"
			if tc.name == "runtime mismatch" {
				clusterRuntime = "firecracker"
				capacity = "firecracker"
			}
			fake.SetMembers([]aerolvm.Member{{
				NodeID: "seed", NodeName: "seed", APIURL: fake.URL(), Alive: true,
				Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{capacity}},
			}})
			if _, errOut, code := e.cliIn(clusterToken+"\n", "cluster", "add", "lab",
				"--admin-url", fake.URL(), "--endpoint-url", fake.URL(),
				"--runtime", clusterRuntime, "--admin-token-stdin"); code != 0 {
				t.Fatalf("cluster add: exit %d: %s", code, errOut)
			}
			h := newHarness(t, fake.URL())
			h.probeErr = tc.probeErr
			saveState(t, machine.State{
				Name: "node-2", Mode: tc.mode, PublicHost: "10.0.0.8",
				Runtime: tc.runtime, AerolVMVersion: "v0.7.0",
			})
			_, errOut, code := e.cliWith(h, "", "node", "add", "node-2", "--cluster", "lab")
			if code != 1 || !strings.Contains(errOut, tc.want) {
				t.Errorf("exit %d stderr %q, want %q", code, errOut, tc.want)
			}
			for _, line := range h.runner.Displays() {
				if strings.Contains(line, "bash") {
					t.Errorf("ran the installer despite the refusal: %s", line)
				}
			}
		})
	}
}

func TestUpdateCheck(t *testing.T) {
	dir := writeRelease(t, "v9.9.9", "v0.7.1", []byte("new binary"))
	h := newHarness(t, "http://127.0.0.1:1")
	saveState(t, machine.State{Name: "node-1", Mode: machine.ModeLocal, Runtime: "docker", AerolVMVersion: "v0.6.13"})

	out, errOut, code := h.runCLI("", "update", "--check", "--release-from", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var got updateCheckOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Current != "dev" || got.Latest != "v9.9.9" || got.AerolVMInstalled != "v0.6.13" || got.AerolVMTarget != "v0.7.1" || !got.UpdateAvailable {
		t.Errorf("check = %+v", got)
	}
	if len(h.runner.Commands()) != 0 {
		t.Errorf("--check ran commands: %v", h.runner.Displays())
	}
	if data, _ := os.ReadFile(h.bin); string(data) != "old binary" {
		t.Error("--check replaced the binary")
	}
}

func TestUpdateReplacesBinary(t *testing.T) {
	dir := writeRelease(t, "v9.9.9", "v0.7.1", []byte("new binary"))
	h := newHarness(t, "http://127.0.0.1:1")
	out, errOut, code := h.runCLI("", "update", "--release-from", dir, "--yes")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var got updateOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Replaced || got.Version != "v9.9.9" {
		t.Errorf("output = %+v", got)
	}
	data, err := os.ReadFile(h.bin)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new binary" {
		t.Errorf("binary = %q", data)
	}
	info, err := os.Stat(h.bin)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %o", info.Mode().Perm())
	}
}

func TestUpdateRefusesBadChecksum(t *testing.T) {
	dir := writeRelease(t, "v9.9.9", "v0.7.1", []byte("new binary"))
	// Break checksums.txt after the release directory is built.
	name := "lewbox_" + runtime.GOOS + "_" + runtime.GOARCH
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(strings.Repeat("0", 64)+"  "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, "http://127.0.0.1:1")
	_, errOut, code := h.runCLI("", "update", "--release-from", dir, "--yes")
	if code != 1 || !strings.Contains(errOut, "checksums.txt") {
		t.Errorf("exit %d stderr %q", code, errOut)
	}
	if data, _ := os.ReadFile(h.bin); string(data) != "old binary" {
		t.Error("binary was replaced despite the bad checksum")
	}
}

func TestUpdateNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to a read-only directory")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "lewbox")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	h := newHarness(t, "http://127.0.0.1:1")
	h.bin = target
	release := writeRelease(t, "v9.9.9", "v0.7.1", []byte("new binary"))
	_, errOut, code := h.runCLI("", "update", "--release-from", release, "--yes")
	if code != 1 || !strings.Contains(errOut, "sudo install -m 0755") {
		t.Errorf("exit %d stderr %q", code, errOut)
	}
}

func TestUpdateAerolVMOnlyRefusesMultiNodeCluster(t *testing.T) {
	e := newEnv(t)
	fake := aerolvmtest.New(t)
	const clusterToken = "avm_cluster_token"
	fake.AdminToken = clusterToken
	fake.SetMembers([]aerolvm.Member{
		{NodeID: "node-1", NodeName: "node-1", APIURL: fake.URL(), Alive: true, Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}}},
		{NodeID: "node-2", NodeName: "node-2", APIURL: fake.URL(), Alive: true, Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}}},
	})
	if _, errOut, code := e.cliIn(clusterToken+"\n", "cluster", "add", "lab",
		"--admin-url", fake.URL(), "--endpoint-url", fake.URL(), "--admin-token-stdin"); code != 0 {
		t.Fatalf("cluster add: exit %d: %s", code, errOut)
	}
	h := newHarness(t, fake.URL())
	h.pat = clusterToken // the local AerolVM answers the members call
	saveState(t, machine.State{
		Name: "node-1", Mode: machine.ModePublicHost, PublicHost: "10.0.0.7",
		Runtime: "docker", AerolVMVersion: "v0.6.13", Cluster: "lab",
	})
	_, errOut, code := e.cliWith(h, "", "update", "--aerolvm-only", "--aerolvm-version", "v0.7.1")
	if code != 1 || !strings.Contains(errOut, "lewbox cluster upgrade lab") {
		t.Errorf("exit %d stderr %q", code, errOut)
	}
	for _, line := range h.runner.Displays() {
		if strings.Contains(line, "bash") {
			t.Errorf("ran the installer anyway: %s", line)
		}
	}
}

func TestUpdateAerolVMOnly(t *testing.T) {
	fake := aerolvmtest.New(t)
	fake.SetVersion("v0.7.1")
	fake.AdminToken = "avm_local_pat"
	h := newHarness(t, fake.URL())
	h.pat = fake.AdminToken
	from := writeMirror(t, "v0.7.1", testScript)
	saveState(t, machine.State{
		Name: "node-1", Mode: machine.ModeLocal, Runtime: "docker",
		AerolVMVersion: "v0.6.13", From: from,
	})
	_, errOut, code := h.runCLI("", "update", "--aerolvm-only", "--aerolvm-version", "v0.7.1", "--yes")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	displays := h.runner.Displays()
	installer := ""
	for _, line := range displays {
		if strings.Contains(line, "bash ") {
			installer = line
			break
		}
	}
	if !strings.Contains(installer, "--version v0.7.1") {
		t.Fatalf("installer command = %q in %v", installer, displays)
	}
	for _, line := range displays {
		if strings.Contains(line, " init ") || strings.Contains(line, " join ") {
			t.Errorf("update passed a word to the installer: %s", line)
		}
	}
	if state := loadState(t); state.AerolVMVersion != "v0.7.1" {
		t.Errorf("state version = %q", state.AerolVMVersion)
	}
}

func TestUpdateAerolVMNoState(t *testing.T) {
	h := newHarness(t, "http://127.0.0.1:1")
	_, errOut, code := h.runCLI("", "update", "--aerolvm-only")
	if code != 0 || !strings.Contains(errOut, "no AerolVM") {
		t.Errorf("exit %d stderr %q", code, errOut)
	}
	if len(h.runner.Commands()) != 0 {
		t.Errorf("ran commands: %v", h.runner.Displays())
	}
}

func TestUpdateAerolVMExecsNewBinary(t *testing.T) {
	dir := writeRelease(t, "v9.9.9", "v0.7.1", []byte("new binary"))
	h := newHarness(t, "http://127.0.0.1:1")
	out, errOut, code := h.runCLI("", "update", "--aerolvm", "--release-from", dir, "--yes")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var got updateOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(h.execArgs, " "), "update --aerolvm-only --aerolvm-version v0.7.1") {
		t.Errorf("exec args = %v", h.execArgs)
	}
	if !got.Replaced {
		t.Errorf("output = %+v", got)
	}
}

// Without an answer from this machine's AerolVM, update must not upgrade:
// it could be one member of a bigger cluster.
func TestUpdateAerolVMOnlyFailsClosed(t *testing.T) {
	h := newHarness(t, "http://127.0.0.1:1") // nothing listens there
	saveState(t, machine.State{
		Name: "node-1", Mode: machine.ModePublicHost, PublicHost: "10.0.0.7",
		Runtime: "docker", AerolVMVersion: "v0.6.13", Cluster: "lab",
	})
	_, errOut, code := h.runCLI("", "update", "--aerolvm-only", "--aerolvm-version", "v0.7.1", "--yes")
	if code != 1 || !strings.Contains(errOut, "not upgrading it alone") {
		t.Errorf("exit %d stderr %q", code, errOut)
	}
	for _, line := range h.runner.Displays() {
		if strings.Contains(line, "bash") {
			t.Errorf("ran the installer anyway: %s", line)
		}
	}
}

// A node outside cluster mode (local mode) is a cluster of one.
func TestUpdateAerolVMOnlySingleNodeMode(t *testing.T) {
	fake := aerolvmtest.New(t)
	fake.SetVersion("v0.7.1")
	fake.SetClusterMode(false)
	fake.AdminToken = "avm_local_pat"
	h := newHarness(t, fake.URL())
	h.pat = fake.AdminToken
	from := writeMirror(t, "v0.7.1", testScript)
	saveState(t, machine.State{Name: "n", Mode: machine.ModeLocal, Runtime: "docker", AerolVMVersion: "v0.6.13", From: from})
	if _, errOut, code := h.runCLI("", "update", "--aerolvm-only", "--aerolvm-version", "v0.7.1", "--yes"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}
