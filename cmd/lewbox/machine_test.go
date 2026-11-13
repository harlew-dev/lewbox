package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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
	"github.com/harlew-dev/lewbox/internal/dns/cloudflaretest"
	"github.com/harlew-dev/lewbox/internal/machine"
	"github.com/harlew-dev/lewbox/internal/machine/machinetest"
	"github.com/harlew-dev/lewbox/internal/objstore/objstoretest"
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
		waitRegistry: func(context.Context) error {
			return nil
		},
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
		} else if len(cmd.Args) >= 2 {
			// A copy such as `install -D -m 0755 -o root -g root <exe> <dest>`.
			if data, err := os.ReadFile(cmd.Args[len(cmd.Args)-2]); err == nil {
				content = string(data)
			}
		}
		h.files[path] = content
		return nil
	}
	if cmd.Name == "rm" {
		for _, arg := range cmd.Args {
			if strings.HasPrefix(arg, "-") {
				continue
			}
			delete(h.files, arg)
			prefix := strings.TrimRight(arg, "/") + "/"
			for path := range h.files {
				if strings.HasPrefix(path, prefix) {
					delete(h.files, path)
				}
			}
		}
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
	case "sha256sum":
		path := cmd.Args[0]
		if content, ok := h.files[path]; ok {
			return []byte(sumHex([]byte(content)) + "  " + path + "\n"), nil
		}
		return nil, os.ErrNotExist
	case "test":
		if _, ok := h.files[cmd.Args[len(cmd.Args)-1]]; ok {
			return nil, nil
		}
		return nil, fmt.Errorf("missing")
	case "id":
		return []byte("999\n"), nil
	case "docker":
		return []byte(""), nil
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

// writeRegistryMirror writes the fake registry release the mirror serves at
// <from>/registry/<archive>, and pins its hash.
func writeRegistryMirror(t *testing.T, from string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	data := []byte("fake registry binary")
	if err := tw.WriteHeader(&tar.Header{Name: "registry", Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(from, "registry")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, machine.RegistryArchive(runtime.GOARCH)), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	registryPin = sumHex(buf.Bytes())
	t.Cleanup(func() { registryPin = "" })
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

// newProfileEnv is an env with a tested DNS profile for example.com and its
// fake Cloudflare API. The service reads it from LEWBOX_CLOUDFLARE_API_URL.
func newProfileEnv(t *testing.T) (*env, *cloudflaretest.Fake) {
	t.Helper()
	e := newEnv(t)
	fake := cloudflaretest.New(t)
	fake.AddZone("example.com")
	t.Setenv("LEWBOX_CLOUDFLARE_API_URL", fake.URL)
	if _, errOut, code := e.cliIn(fake.Token+"\n", "dns-profile", "create", "cf-prod",
		"--zone", "example.com", "--acme-email", "ops@example.com", "--api-token-stdin"); code != 0 {
		t.Fatalf("dns-profile create: exit %d: %s", code, errOut)
	}
	return e, fake
}

func TestInstallDNSProfile(t *testing.T) {
	e, cf := newProfileEnv(t)
	fake := aerolvmtest.New(t)
	h := newHarness(t, fake.URL())
	fake.AdminToken = h.pat
	fake.SetMembers([]aerolvm.Member{{
		NodeID: "node-1", NodeName: "node-1", APIURL: fake.URL(), Alive: true,
		PublicHost: "203.0.113.10", Role: "mixed", Version: "v0.7.0",
		Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
	}})
	from := writeMirror(t, "v0.7.0", testScript)

	out, errOut, code := e.cliWith(h, "", "install", "--from", from, "--name", "node-1",
		"--domain", "eu.example.com", "--dns-profile", "cf-prod", "--yes")
	if code != 0 {
		t.Fatalf("install: exit %d: %s", code, errOut)
	}
	if strings.Contains(out, cf.Token) || strings.Contains(errOut, cf.Token) {
		t.Errorf("the DNS profile's token leaked:\n%s\n%s", out, errOut)
	}
	for _, want := range []string{
		"--domain eu.example.com", "--dns-provider cloudflare",
		"--dns-api-token ******", "--acme-email ops@example.com",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("installer command missing %q:\n%s", want, errOut)
		}
	}

	state := loadState(t)
	if state.Mode != machine.ModeDomain || state.Domain != "eu.example.com" ||
		state.DNSProfile != "cf-prod" || state.DNSProfileVersion != 1 {
		t.Errorf("state = %+v", state)
	}
	path, err := machine.Path()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"dns_profile":"cf-prod"`) {
		t.Errorf("state file has no profile name: %s", data)
	}
	if strings.Contains(string(data), cf.Token) {
		t.Errorf("state file carries the token: %s", data)
	}

	// The cluster registered from this install keeps its domain and profile.
	out, errOut, code = e.cliWith(h, "", "node", "add", "node-1", "--new-cluster", "lab",
		"--admin-url", fake.URL(), "--endpoint-url", fake.URL())
	if code != 0 {
		t.Fatalf("node add: exit %d: %s", code, errOut)
	}
	var detail service.ClusterDetail
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Slug != "lab" || detail.Domain != "eu.example.com" ||
		detail.DNSProfile == nil || *detail.DNSProfile != "cf-prod" || detail.DNSRecords != "auto" {
		t.Errorf("cluster = %+v", detail.Cluster)
	}
}

func TestInstallDNSProfileRefusals(t *testing.T) {
	e, cf := newProfileEnv(t)
	from := writeMirror(t, "v0.7.0", testScript)
	h := newHarness(t, "http://127.0.0.1:1")

	out, errOut, code := e.cliWith(h, "", "install", "--dry-run", "--from", from,
		"--domain", "eu.example.com", "--dns-profile", "cf-prod", "--dns-api-token-stdin")
	if code != 2 || !strings.Contains(errOut, "cannot be used together") {
		t.Errorf("both token sources: exit %d stderr %q", code, errOut)
	}

	out, errOut, code = e.cliWith(h, "", "install", "--dry-run", "--from", from,
		"--domain", "eu.other.org", "--dns-profile", "cf-prod")
	if code != 2 || !strings.Contains(errOut, "is not in DNS profile \"cf-prod\"'s zone example.com") {
		t.Errorf("outside the zone: exit %d stderr %q", code, errOut)
	}
	if displays := h.runner.Displays(); len(displays) != 0 {
		t.Errorf("commands ran before the zone check: %v", displays)
	}

	out, errOut, code = e.cliWith(h, "", "install", "--dry-run", "--from", from,
		"--domain", "eu.example.com")
	if code != 1 || !strings.Contains(errOut, "--dns-profile or --dns-api-token-stdin") {
		t.Errorf("no token source: exit %d stdout %q stderr %q", code, out, errOut)
	}
	if strings.Contains(out, "cf-prod") {
		t.Errorf("stdout = %q", out)
	}

	if _, errOut, code := h.runCLI("", "install", "--dry-run", "--from", from, "--dns-profile", "cf-prod"); code != 2 ||
		!strings.Contains(errOut, "only makes sense with --domain") {
		t.Errorf("profile without a domain: exit %d stderr %q", code, errOut)
	}

	// A dry run still fetches the profile, so it can show the masked command,
	// but it saves nothing and runs nothing.
	out, errOut, code = e.cliWith(h, "", "install", "--dry-run", "--from", from,
		"--domain", "eu.example.com", "--dns-profile", "cf-prod")
	if code != 0 || !strings.Contains(errOut, "--dns-api-token ******") {
		t.Errorf("dry run: exit %d stdout %q stderr %q", code, out, errOut)
	}
	if strings.Contains(out+errOut, cf.Token) {
		t.Errorf("the DNS profile's token leaked:\n%s\n%s", out, errOut)
	}
	if len(h.runner.Commands()) != 0 {
		t.Errorf("dry run ran commands: %v", h.runner.Displays())
	}
	if _, err := machine.Load(); err == nil {
		t.Error("dry run saved state")
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

// In local mode AerolVM is one node outside cluster mode, and it names its
// only member "standalone", not this machine's name. node add must still
// report its settings, to that node.
func TestNodeAddLocalMode(t *testing.T) {
	e := newEnv(t)
	fake := aerolvmtest.New(t)
	h := newHarness(t, fake.URL())
	fake.AdminToken = h.pat
	fake.SetMembers([]aerolvm.Member{{
		NodeID: "standalone", NodeName: "standalone", APIURL: fake.URL(), Alive: true,
		Role: "mixed", Version: "v0.6.13", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
	}})
	saveState(t, machine.State{Name: "node-3", Mode: machine.ModeLocal, Runtime: "docker", AerolVMVersion: "v0.6.13"})

	out, errOut, code := e.cliWith(h, "", "node", "add", "--admin-url", fake.URL(), "--endpoint-url", fake.URL())
	if code != 0 {
		t.Fatalf("node add: exit %d: %s", code, errOut)
	}
	var detail service.ClusterDetail
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Slug != "node-3" || len(detail.NodeList) != 1 || detail.NodeList[0].Name != "standalone" {
		t.Fatalf("cluster = %+v", detail)
	}
	nodeOut, errOut, code := e.cli("node", "get", "standalone")
	if code != 0 {
		t.Fatalf("node get: exit %d: %s", code, errOut)
	}
	if !strings.Contains(nodeOut, `"applied_at":"`) {
		t.Errorf("the local node did not report its settings: %s", nodeOut)
	}

	// node configure on its own finds the same node.
	if _, errOut, code := e.cliWith(h, "", "node", "configure"); code != 0 {
		t.Fatalf("node configure: exit %d: %s", code, errOut)
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

// storageEnv creates a storage profile and a cluster that uses it, with
// node-1 as its only member. It returns the cluster's ID.
func storageEnv(t *testing.T, e *env, fake *aerolvmtest.Server) string {
	t.Helper()
	storage := objstoretest.New(t)
	storage.AddBucket("assets")
	if _, errOut, code := e.cliIn("objstore-secret\n", "storage-profile", "create", "s3-main",
		"--endpoint", storage.URL, "--region", "us-east-1", "--bucket", "assets",
		"--prefix", "lewbox", "--access-key-id", storage.AccessKeyID, "--secret-access-key-stdin"); code != 0 {
		t.Fatalf("storage-profile create: exit %d: %s", code, errOut)
	}
	fake.AdminToken = "avm_cluster_token"
	fake.SetMembers([]aerolvm.Member{{
		NodeID: "node-1", NodeName: "node-1", APIURL: fake.URL(), Alive: true,
		Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
	}})
	out, errOut, code := e.cliIn(fake.AdminToken+"\n", "cluster", "add", "lab",
		"--admin-url", fake.URL(), "--endpoint-url", fake.URL(), "--admin-token-stdin")
	if code != 0 {
		t.Fatalf("cluster add: exit %d: %s", code, errOut)
	}
	if _, errOut, code := e.cli("cluster", "set", "lab",
		"--storage-profile", "s3-main", "--stop-policy", "archive"); code != 0 {
		t.Fatalf("cluster set: exit %d: %s", code, errOut)
	}
	var detail service.ClusterDetail
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatal(err)
	}
	return detail.ID
}

func TestNodeConfigureStorage(t *testing.T) {
	e := newEnv(t)
	fake := aerolvmtest.New(t)
	clusterID := storageEnv(t, e, fake)
	from := writeMirror(t, "v0.7.0", testScript)
	writeRegistryMirror(t, from)
	h := newHarness(t, fake.URL())
	h.pat = fake.AdminToken
	saveState(t, machine.State{
		Name: "node-1", Mode: machine.ModePublicHost, PublicHost: "10.0.0.7",
		Runtime: "docker", AerolVMVersion: "v0.7.0", Cluster: "lab", From: from,
	})

	out, errOut, code := e.cliWith(h, "", "node", "configure")
	if code != 0 {
		t.Fatalf("node configure: exit %d: %s", code, errOut)
	}
	var got configureOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Node != "node-1" || got.Cluster != "lab" || got.Storage != "applied" ||
		!got.RegistryChanged || !got.EnvChanged || got.DNS != "none" || !got.CleanupTimer {
		t.Errorf("configure = %+v", got)
	}

	wantEnv := machine.EnvFileContent + machine.PushEnv(clusterID)
	if h.files[machine.EnvironmentFile] != wantEnv {
		t.Errorf("%s = %q, want %q", machine.EnvironmentFile, h.files[machine.EnvironmentFile], wantEnv)
	}
	config := h.files[machine.RegistryConfigFile]
	for _, want := range []string{
		`bucket: "assets"`,
		`rootdirectory: "/lewbox/clusters/` + clusterID + `"`,
		`secretkey: "objstore-secret"`,
	} {
		if !strings.Contains(config, want) {
			t.Errorf("registry config is missing %q:\n%s", want, config)
		}
	}
	if h.files[machine.RegistryUnitFile] != machine.RegistryUnit {
		t.Error("registry unit not written")
	}
	if h.files[machine.CleanupService] != machine.CleanupServiceUnit(0) {
		t.Error("cleanup service not written")
	}
	if h.files[machine.CleanupTimer] != machine.CleanupTimerUnit {
		t.Error("cleanup timer not written")
	}
	if h.files[machine.CleanupBinary] != "old binary" {
		t.Errorf("cleanup binary = %q, want a copy of the running binary", h.files[machine.CleanupBinary])
	}
	if !h.runner.Ran("systemctl", "enable", "--now", machine.CleanupTimerName) {
		t.Error("the cleanup timer was not enabled")
	}
	for _, line := range h.runner.Displays() {
		if strings.Contains(line, "objstore-secret") {
			t.Errorf("a command shows the bucket secret: %s", line)
		}
	}

	// The node reported what it applied.
	nodeOut, errOut, code := e.cli("node", "get", "node-1")
	if code != 0 {
		t.Fatalf("node get: exit %d: %s", code, errOut)
	}
	if !strings.Contains(nodeOut, `"outdated":false`) || !strings.Contains(nodeOut, `"applied_at":"`) {
		t.Errorf("node settings = %s", nodeOut)
	}

	// A second run changes nothing.
	h.runner.Reset()
	out, errOut, code = e.cliWith(h, "", "node", "configure")
	if code != 0 {
		t.Fatalf("second node configure: exit %d: %s", code, errOut)
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Storage != "applied" || got.RegistryChanged || got.EnvChanged || got.CleanupTimer {
		t.Errorf("second configure = %+v", got)
	}
	if h.runner.Ran("systemctl", "restart", "lewbox-registry") {
		t.Error("an unchanged registry was restarted")
	}
}

func TestNodeConfigureArchiveRetention(t *testing.T) {
	e := newEnv(t)
	fake := aerolvmtest.New(t)
	storageEnv(t, e, fake)
	from := writeMirror(t, "v0.7.0", testScript)
	writeRegistryMirror(t, from)
	h := newHarness(t, fake.URL())
	h.pat = fake.AdminToken
	saveState(t, machine.State{
		Name: "node-1", Mode: machine.ModePublicHost, PublicHost: "10.0.0.7",
		Runtime: "docker", AerolVMVersion: "v0.7.0", Cluster: "lab", From: from,
	})
	if _, errOut, code := e.cliWith(h, "", "node", "configure"); code != 0 {
		t.Fatalf("node configure: exit %d: %s", code, errOut)
	}
	outdated := func() string {
		t.Helper()
		out, errOut, code := e.cli("node", "get", "node-1")
		if code != 0 {
			t.Fatalf("node get: exit %d: %s", code, errOut)
		}
		var node service.Node
		if err := json.Unmarshal([]byte(out), &node); err != nil {
			t.Fatal(err)
		}
		return strings.Join(node.Settings.Reasons, "; ")
	}

	// 1d: the node is outdated until configure writes --older-than.
	if _, errOut, code := e.cli("cluster", "set", "lab", "--archive-local-retention", "1d"); code != 0 {
		t.Fatalf("cluster set: exit %d: %s", code, errOut)
	}
	if got := outdated(); got != "archive_local_retention is 1d; this node applied instant" {
		t.Errorf("reasons = %q", got)
	}
	h.runner.Reset()
	out, errOut, code := e.cliWith(h, "", "node", "configure")
	if code != 0 {
		t.Fatalf("node configure 1d: exit %d: %s", code, errOut)
	}
	var got configureOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Storage != "applied" || got.RegistryChanged || !got.CleanupTimer {
		t.Errorf("configure 1d = %+v", got)
	}
	if h.files[machine.CleanupService] != machine.CleanupServiceUnit(24*time.Hour) {
		t.Errorf("cleanup service = %q", h.files[machine.CleanupService])
	}
	if got := outdated(); got != "" {
		t.Errorf("after configure: %q", got)
	}

	// never: the registry stays, the timer goes.
	if _, errOut, code := e.cli("cluster", "set", "lab", "--archive-local-retention", "never"); code != 0 {
		t.Fatalf("cluster set never: exit %d: %s", code, errOut)
	}
	h.runner.Reset()
	out, errOut, code = e.cliWith(h, "", "node", "configure")
	if code != 0 {
		t.Fatalf("node configure never: exit %d: %s", code, errOut)
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Storage != "applied" || got.RegistryChanged || !got.CleanupTimer {
		t.Errorf("configure never = %+v", got)
	}
	if _, ok := h.files[machine.RegistryUnitFile]; !ok {
		t.Error("the registry was removed")
	}
	for _, path := range []string{machine.CleanupService, machine.CleanupTimer, machine.CleanupBinary} {
		if _, ok := h.files[path]; ok {
			t.Errorf("%s was not removed", path)
		}
	}
	if !h.runner.Ran("systemctl", "disable", "--now", machine.CleanupTimerName) {
		t.Error("the cleanup timer was not disabled")
	}
	if got := outdated(); got != "" {
		t.Errorf("after configure never: %q", got)
	}
}

func TestNodeAddArchiveFlagsOnlyForANewCluster(t *testing.T) {
	e := newEnv(t)
	h := newHarness(t, "http://127.0.0.1:1")
	for _, flag := range []string{"--archive-local-retention", "--archive-snapshot-name"} {
		_, errOut, code := e.cliWith(h, "", "node", "add", "node-2", "--cluster", "lab", flag, "1d")
		if code != 2 || !strings.Contains(errOut, "only apply to a new cluster") {
			t.Errorf("%s with --cluster: exit %d: %s", flag, code, errOut)
		}
	}
}

func TestNodeConfigureRemovesStorage(t *testing.T) {
	e := newEnv(t)
	fake := aerolvmtest.New(t)
	storageEnv(t, e, fake)
	from := writeMirror(t, "v0.7.0", testScript)
	writeRegistryMirror(t, from)
	h := newHarness(t, fake.URL())
	h.pat = fake.AdminToken
	saveState(t, machine.State{
		Name: "node-1", Mode: machine.ModePublicHost, PublicHost: "10.0.0.7",
		Runtime: "docker", AerolVMVersion: "v0.7.0", Cluster: "lab", From: from,
	})

	if _, errOut, code := e.cliWith(h, "", "node", "configure"); code != 0 {
		t.Fatalf("node configure: exit %d: %s", code, errOut)
	}
	if _, errOut, code := e.cli("cluster", "set", "lab", "--storage-profile", "none", "--stop-policy", "keep"); code != 0 {
		t.Fatalf("cluster set: exit %d: %s", code, errOut)
	}

	h.runner.Reset()
	out, errOut, code := e.cliWith(h, "", "node", "configure")
	if code != 0 {
		t.Fatalf("node configure after clearing: exit %d: %s", code, errOut)
	}
	var got configureOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Storage != "removed" || !got.RegistryChanged || !got.EnvChanged || !got.CleanupTimer {
		t.Errorf("configure = %+v", got)
	}
	if h.files[machine.EnvironmentFile] != machine.EnvFileContent {
		t.Errorf("%s = %q", machine.EnvironmentFile, h.files[machine.EnvironmentFile])
	}
	for _, path := range []string{machine.RegistryUnitFile, machine.RegistryConfigFile, machine.CleanupService, machine.CleanupTimer, machine.CleanupBinary} {
		if _, ok := h.files[path]; ok {
			t.Errorf("%s was not removed", path)
		}
	}
	if !h.runner.Ran("systemctl", "disable", "--now", machine.CleanupTimerName) {
		t.Error("the cleanup timer was not disabled")
	}
}

func TestNodeConfigureDNSVersion(t *testing.T) {
	cf := cloudflaretest.New(t)
	cf.AddZone("example.com")
	t.Setenv("LEWBOX_CLOUDFLARE_API_URL", cf.URL)
	e := newEnvWith(t, service.WithCloudflareURL(cf.URL))
	if _, errOut, code := e.cliIn(cf.Token+"\n", "dns-profile", "create", "cf-prod",
		"--zone", "example.com", "--api-token-stdin"); code != 0 {
		t.Fatalf("dns-profile create: exit %d: %s", code, errOut)
	}
	fake := aerolvmtest.New(t)
	fake.AdminToken = "avm_cluster_token"
	fake.SetMembers([]aerolvm.Member{{
		NodeID: "node-1", NodeName: "node-1", APIURL: fake.URL(), Alive: true,
		Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
	}})
	if _, errOut, code := e.cliIn(fake.AdminToken+"\n", "cluster", "add", "lab",
		"--admin-url", fake.URL(), "--endpoint-url", fake.URL(), "--admin-token-stdin"); code != 0 {
		t.Fatalf("cluster add: exit %d: %s", code, errOut)
	}
	if _, errOut, code := e.cli("cluster", "set", "lab", "--domain", "eu.example.com", "--dns-profile", "cf-prod"); code != 0 {
		t.Fatalf("cluster set: exit %d: %s", code, errOut)
	}
	// A new token bumps the profile to version 2.
	if _, errOut, code := e.cliIn(cf.Token+"\n", "dns-profile", "update", "cf-prod", "--api-token-stdin"); code != 0 {
		t.Fatalf("dns-profile update: exit %d: %s", code, errOut)
	}

	from := writeMirror(t, "v0.7.0", testScript)
	h := newHarness(t, fake.URL())
	h.pat = fake.AdminToken
	saveState(t, machine.State{
		Name: "node-1", Mode: machine.ModeDomain, Domain: "eu.example.com",
		DNSProfile: "cf-prod", DNSProfileVersion: 1,
		Runtime: "docker", AerolVMVersion: "v0.7.0", Cluster: "lab", From: from,
	})

	out, errOut, code := e.cliWith(h, "", "node", "configure")
	if code != 0 {
		t.Fatalf("node configure: exit %d: %s", code, errOut)
	}
	var got configureOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.DNS != "updated" {
		t.Errorf("configure = %+v", got)
	}
	installer := ""
	for _, line := range h.runner.Displays() {
		if strings.Contains(line, "bash ") {
			installer = line
		}
		if strings.Contains(line, " init ") || strings.Contains(line, " join ") {
			t.Errorf("configure passed a word to the installer: %s", line)
		}
	}
	for _, want := range []string{"--version v0.7.0", "--dns-provider cloudflare", "--dns-api-token ******"} {
		if !strings.Contains(installer, want) {
			t.Errorf("installer command is missing %q: %s", want, installer)
		}
	}
	if strings.Contains(out+errOut, cf.Token) {
		t.Errorf("the DNS token leaked:\n%s\n%s", out, errOut)
	}
	if state := loadState(t); state.DNSProfileVersion != 2 || state.DNSProfile != "cf-prod" {
		t.Errorf("state = %+v", state)
	}
	nodeOut, _, code := e.cli("node", "get", "node-1")
	if code != 0 || !strings.Contains(nodeOut, `"outdated":false`) {
		t.Errorf("node settings = %s", nodeOut)
	}
}

func TestNodeCleanupAsRootUsesNoSudo(t *testing.T) {
	fake := aerolvmtest.New(t)
	fake.AdminToken = "avm_local_pat"
	fake.AddSnapshot("t1", aerolvm.Snapshot{
		Name: "archive-sb-1", Image: "aerolvm-snapshot/aaa:latest",
		PushState: "active", ImageRegistryRef: "127.0.0.1:5000/cluster/c1/snapshots/t1/archive-sb-1:latest",
	})
	h := newHarness(t, fake.URL())
	h.pat = fake.AdminToken
	h.hd.euid = func() int { return 0 }

	out, errOut, code := h.runCLI("", "node", "cleanup")
	if code != 0 {
		t.Fatalf("node cleanup: exit %d: %s", code, errOut)
	}
	var got machine.CleanupResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Removed) != 1 || got.Removed[0] != "aerolvm-snapshot/aaa:latest" {
		t.Errorf("removed = %v", got.Removed)
	}
	for _, cmd := range h.runner.Commands() {
		if cmd.Sudo {
			t.Errorf("ran %s with sudo as root", cmd.Display())
		}
	}
	if strings.Contains(errOut, "sudo") {
		t.Errorf("stderr shows sudo as root: %s", errOut)
	}
}
