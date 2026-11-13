package machine

import (
	"context"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/harlew-dev/lewbox/internal/service"
)

func TestMaskLine(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		secrets []string
		want    string
	}{
		{"pat line", "PAT token: abc123", nil, "PAT token: ******"},
		{"pat line with text", "PAT token: abc123 and more", nil, "PAT token: ******"},
		{"dns token", "--dns-api-token cf-secret", []string{"cf-secret"}, "--dns-api-token ******"},
		{"join token", "--token id.secret", []string{"id.secret"}, "--token ******"},
		{"several", "a=one b=two", []string{"one", "two"}, "a=****** b=******"},
		{"empty secret", "nothing", []string{""}, "nothing"},
		{"plain", "installing", nil, "installing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaskLine(tc.line, tc.secrets); got != tc.want {
				t.Errorf("MaskLine(%q) = %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}

func TestCmdDisplay(t *testing.T) {
	cmd := Cmd{
		Name:    "bash",
		Args:    []string{"/tmp/install.sh", "--version", "v0.7.0", "--token", "id.secret"},
		Sudo:    true,
		Secrets: []string{"id.secret"},
	}
	want := "sudo bash /tmp/install.sh --version v0.7.0 --token ******"
	if got := cmd.Display(); got != want {
		t.Errorf("Display() = %q, want %q", got, want)
	}
	if got := (Cmd{Name: "true"}).Display(); got != "true" {
		t.Errorf("Display() = %q", got)
	}
	if got := (Cmd{Name: "printf", Args: []string{"a b"}}).Display(); got != "printf 'a b'" {
		t.Errorf("Display() quoted = %q", got)
	}
}

func TestExecRunnerMasksStreamedOutput(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	var out strings.Builder
	r := &ExecRunner{Stdout: &out, Stderr: io.Discard}
	err := r.Run(context.Background(), Cmd{
		Name:    "/bin/sh",
		Args:    []string{"-c", `printf 'PAT token: abc\ntoken=id.secret\nplain\n'`},
		Secrets: []string{"id.secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "PAT token: ******\ntoken=******\nplain\n"
	if out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}
}

func TestStateAPIURL(t *testing.T) {
	cases := []struct {
		name string
		st   State
		want string
	}{
		{"local", State{Mode: ModeLocal}, "http://127.0.0.1:21212"},
		{"domain", State{Mode: ModeDomain, Domain: "eu.example"}, "https://eu.example"},
		{"public host", State{Mode: ModePublicHost, PublicHost: "10.0.0.7"}, "http://10.0.0.7:21212"},
		{"unknown", State{Mode: "other"}, "http://127.0.0.1:21212"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.st.APIURL(); got != tc.want {
				t.Errorf("APIURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStateSaveLoad(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	want := State{
		Name:           "node-1",
		Mode:           ModeDomain,
		Domain:         "eu.example",
		Runtime:        "docker",
		AerolVMVersion: "v0.7.0",
		InstalledAt:    time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		Cluster:        "lab",
	}
	if err := want.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != want.Name || got.Mode != want.Mode || got.Cluster != want.Cluster || got.AerolVMVersion != want.AerolVMVersion {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", info.Mode().Perm())
	}
}

func TestLoadMissingState(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, err := Load()
	if !IsNotInstalled(err) {
		t.Fatalf("err = %v, want a not-exist error", err)
	}
}

type stubListener struct{}

func (stubListener) Accept() (net.Conn, error) { return nil, io.EOF }
func (stubListener) Close() error              { return nil }
func (stubListener) Addr() net.Addr            { return &net.TCPAddr{} }

// testEnv returns a Linux machine whose checks pass, so each case can break
// exactly one thing.
func testEnv() Env {
	info, _ := os.Stat("/dev/null")
	return Env{
		GOOS: "linux",
		Listen: func(string, string) (net.Listener, error) {
			return stubListener{}, nil
		},
		Stat: func(string) (os.FileInfo, error) {
			if info == nil {
				return nil, os.ErrNotExist
			}
			return info, nil
		},
		Run:             func(context.Context, Cmd) error { return nil },
		SandboxdRunning: func(context.Context) bool { return false },
	}
}

func TestCheck(t *testing.T) {
	busy := func(network, addr string) (net.Listener, error) {
		if strings.HasSuffix(addr, ":7001") {
			return nil, &net.OpError{Op: "listen", Err: syscall.EADDRINUSE}
		}
		return stubListener{}, nil
	}
	refused443 := func(network, addr string) (net.Listener, error) {
		if strings.HasSuffix(addr, ":443") {
			return nil, &net.OpError{Op: "listen", Err: syscall.EACCES}
		}
		return stubListener{}, nil
	}
	cases := []struct {
		name   string
		env    Env
		opts   CheckOptions
		failed string // "" means every check passed
	}{
		{name: "linux local", env: testEnv(), opts: CheckOptions{Mode: ModeLocal, Runtime: "docker"}},
		{name: "linux public host", env: testEnv(), opts: CheckOptions{Mode: ModePublicHost, Runtime: "docker"}},
		{
			name: "domain with token",
			env:  testEnv(),
			opts: CheckOptions{Mode: ModeDomain, Runtime: "docker", Domain: "eu.example", DNSTokenStdin: true},
		},
		{
			name:   "domain without token",
			env:    testEnv(),
			opts:   CheckOptions{Mode: ModeDomain, Runtime: "docker", Domain: "eu.example"},
			failed: "dns_token",
		},
		{
			name:   "darwin domain",
			env:    func() Env { e := testEnv(); e.GOOS = "darwin"; return e }(),
			opts:   CheckOptions{Mode: ModeDomain, Runtime: "docker", DNSTokenStdin: true},
			failed: "os",
		},
		{
			name:   "darwin local is refused until Lewbox's settings step supports launchd",
			env:    func() Env { e := testEnv(); e.GOOS = "darwin"; return e }(),
			opts:   CheckOptions{Mode: ModeLocal, Runtime: "docker"},
			failed: "os",
		},
		{
			name: "firecracker without kvm",
			env: func() Env {
				e := testEnv()
				e.Stat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
				return e
			}(),
			opts:   CheckOptions{Mode: ModePublicHost, Runtime: "firecracker"},
			failed: "kvm",
		},
		{
			name:   "firecracker local",
			env:    testEnv(),
			opts:   CheckOptions{Mode: ModeLocal, Runtime: "firecracker"},
			failed: "runtime",
		},
		{
			name:   "busy port",
			env:    func() Env { e := testEnv(); e.Listen = busy; return e }(),
			opts:   CheckOptions{Mode: ModePublicHost, Runtime: "docker"},
			failed: "ports",
		},
		{
			// A normal user may not bind 443, but nothing listens on it.
			name: "privileged port refused but free",
			env: func() Env {
				e := testEnv()
				e.Listen = refused443
				e.TCPListening = func(int) (bool, error) { return false, nil }
				return e
			}(),
			opts: CheckOptions{Mode: ModeDomain, Runtime: "docker", Domain: "eu.example", DNSTokenStdin: true},
		},
		{
			name: "privileged port refused and listening",
			env: func() Env {
				e := testEnv()
				e.Listen = refused443
				e.TCPListening = func(port int) (bool, error) { return port == 443, nil }
				return e
			}(),
			opts:   CheckOptions{Mode: ModeDomain, Runtime: "docker", Domain: "eu.example", DNSTokenStdin: true},
			failed: "ports",
		},
		{
			name: "privileged port refused and unknown fails closed",
			env: func() Env {
				e := testEnv()
				e.Listen = refused443
				e.TCPListening = func(int) (bool, error) { return false, os.ErrNotExist }
				return e
			}(),
			opts:   CheckOptions{Mode: ModeDomain, Runtime: "docker", Domain: "eu.example", DNSTokenStdin: true},
			failed: "ports",
		},
		{
			name: "busy port is ours",
			env: func() Env {
				e := testEnv()
				e.Listen = busy
				e.SandboxdRunning = func(context.Context) bool { return true }
				return e
			}(),
			opts: CheckOptions{Mode: ModePublicHost, Runtime: "docker"},
		},
		{
			name: "sudo fails",
			env: func() Env {
				e := testEnv()
				e.Run = func(context.Context, Cmd) error { return os.ErrPermission }
				return e
			}(),
			opts:   CheckOptions{Mode: ModeLocal, Runtime: "docker"},
			failed: "sudo",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checks := Check(context.Background(), tc.env, tc.opts)
			if len(checks) == 0 {
				t.Fatal("no checks ran")
			}
			bad, failed := Failed(checks)
			if tc.failed == "" {
				if failed {
					t.Errorf("check %q failed: %s", bad.Name, bad.Detail)
				}
				return
			}
			if !failed || bad.Name != tc.failed {
				t.Errorf("failed check = %q (%v), want %q", bad.Name, failed, tc.failed)
			}
			if bad.Detail == "" {
				t.Errorf("check %q has no fix", bad.Name)
			}
		})
	}
}

func TestFailedReturnsFirst(t *testing.T) {
	checks := []service.Check{{Name: "a", OK: true}, {Name: "b"}, {Name: "c"}}
	got, ok := Failed(checks)
	if !ok || got.Name != "b" {
		t.Errorf("Failed() = %q %v", got.Name, ok)
	}
}

// Without passwordless sudo, the check may prompt (`sudo -v`) only when the
// CLI runs on a terminal; elsewhere it fails fast instead of waiting.
func TestCheckSudoPromptsOnlyOnATerminal(t *testing.T) {
	var ran []string
	env := testEnv()
	env.Run = func(_ context.Context, cmd Cmd) error {
		ran = append(ran, strings.Join(cmd.Args, " "))
		if cmd.Args[0] == "-n" {
			return os.ErrPermission // a password is required
		}
		return nil
	}
	for _, terminal := range []bool{false, true} {
		ran = nil
		env.Terminal = terminal
		got := checkSudo(context.Background(), env)
		if got.OK != terminal {
			t.Errorf("terminal=%v: check = %+v", terminal, got)
		}
		prompted := len(ran) == 2 && ran[1] == "-v"
		if prompted != terminal {
			t.Errorf("terminal=%v: ran %v", terminal, ran)
		}
	}
}

func TestTCPTableListening(t *testing.T) {
	// Rows from a real /proc/net/tcp: 443 established (01), 21212 (52DC) listening (0A).
	table := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:52DC 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
   1: 0100007F:01BB 0100007F:9C40 01 00000000:00000000 00:00000000 00000000  1000        0 23456 1 0000000000000000 20 4 30 10 -1
`
	for port, want := range map[int]bool{21212: true, 443: false, 7000: false} {
		got, err := tcpTableListening(strings.NewReader(table), port)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("port %d: listening = %v, want %v", port, got, want)
		}
	}
}

func TestProcTCPListening(t *testing.T) {
	if _, err := os.Stat("/proc/net/tcp"); err != nil {
		t.Skip("no /proc/net/tcp")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	got, err := procTCPListening(port)
	if err != nil || !got {
		t.Fatalf("while listening on %d: got %v, %v; want true", port, got, err)
	}
	l.Close()
	if got, err := procTCPListening(port); err != nil || got {
		t.Fatalf("after closing %d: got %v, %v; want false", port, got, err)
	}
}
