package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/machine"
	"github.com/harlew-dev/lewbox/internal/service"
)

// hostDeps are the machine-facing dependencies the install, node add and
// update commands use. Tests replace them; zero values get real ones.
type hostDeps struct {
	runner         machine.Runner
	env            machine.Env
	localAPIURL    string
	probe          func(ctx context.Context, addr string) error
	executable     func() (string, error)
	euid           func() int
	execFn         func(argv0 string, argv []string, envv []string) error
	sleep          func(ctx context.Context, d time.Duration)
	now            func() time.Time
	hostname       func() (string, error)
	releaseURL     string
	interfaceAddrs func() ([]netip.Prefix, error)
}

// defaultHostDeps wires the commands to the real machine.
func defaultHostDeps(a *app) *hostDeps {
	env := machine.DefaultEnv()
	// On a terminal, the sudo check may fall back to `sudo -v`, which prompts
	// for the password through /dev/tty; without one it must not wait.
	env.Terminal = isTerminalReader(a.stdin)
	return &hostDeps{
		runner: &machine.ExecRunner{
			Stdout:        a.stderr,
			Stderr:        a.stderr,
			Stdin:         a.stdin,
			StdinTerminal: isTerminalReader(a.stdin),
		},
		env:         env,
		localAPIURL: "http://127.0.0.1:21212",
		probe: func(ctx context.Context, addr string) error {
			d := net.Dialer{Timeout: 2 * time.Second}
			conn, err := d.DialContext(ctx, "tcp", addr)
			if err != nil {
				return err
			}
			return conn.Close()
		},
		executable: os.Executable,
		euid:       os.Geteuid,
		execFn:     syscall.Exec,
		sleep:      sleepContext,
		now:        time.Now,
		hostname:   os.Hostname,
		releaseURL: envOr("LEWBOX_RELEASE_URL", machine.DefaultReleaseURL),
		interfaceAddrs: func() ([]netip.Prefix, error) {
			addrs, err := net.InterfaceAddrs()
			if err != nil {
				return nil, err
			}
			prefixes := make([]netip.Prefix, 0, len(addrs))
			for _, addr := range addrs {
				ipnet, ok := addr.(*net.IPNet)
				if !ok {
					continue
				}
				prefix, err := netip.ParsePrefix(ipnet.String())
				if err != nil {
					continue
				}
				prefixes = append(prefixes, prefix)
			}
			return prefixes, nil
		},
	}
}

// host returns the command dependencies, filling in real ones on first use.
func (a *app) host() *hostDeps {
	if a.hd == nil {
		a.hd = defaultHostDeps(a)
	}
	hd := a.hd
	base := defaultHostDeps(a)
	if hd.runner == nil {
		hd.runner = base.runner
	}
	if hd.env.GOOS == "" {
		hd.env = base.env
		hd.env.Run = nil
	}
	if hd.env.Run == nil {
		hd.env.Run = func(ctx context.Context, cmd machine.Cmd) error {
			return hd.runner.Run(ctx, cmd)
		}
	}
	if hd.localAPIURL == "" {
		hd.localAPIURL = base.localAPIURL
	}
	if hd.probe == nil {
		hd.probe = base.probe
	}
	if hd.executable == nil {
		hd.executable = base.executable
	}
	if hd.euid == nil {
		hd.euid = base.euid
	}
	if hd.execFn == nil {
		hd.execFn = base.execFn
	}
	if hd.sleep == nil {
		hd.sleep = base.sleep
	}
	if hd.now == nil {
		hd.now = base.now
	}
	if hd.hostname == nil {
		hd.hostname = base.hostname
	}
	if hd.releaseURL == "" {
		hd.releaseURL = base.releaseURL
	}
	if hd.interfaceAddrs == nil {
		hd.interfaceAddrs = base.interfaceAddrs
	}
	return hd
}

func sleepContext(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// refuseRoot stops a machine-changing command from running as root. The CLI
// uses sudo for the steps that need it.
func (a *app) refuseRoot() error {
	if a.host().euid() == 0 && os.Getenv("LEWBOX_ALLOW_ROOT") != "1" {
		return preconditionError("running as root is not supported: run as your own user and let lewbox use sudo (set LEWBOX_ALLOW_ROOT=1 to override)")
	}
	return nil
}

// preconditionError is a failed requirement, printed with its checks.
func preconditionError(format string, args ...any) error {
	return &service.Error{Code: service.CodePrecondition, Message: fmt.Sprintf(format, args...)}
}

// say writes a progress line to stderr. Only the final result goes to stdout.
func (a *app) say(format string, args ...any) {
	fmt.Fprintf(a.stderr, format+"\n", args...)
}

// printChecks prints ✓/✗ lines for the install checks.
func (a *app) printChecks(checks []service.Check) {
	for _, c := range checks {
		mark := "✓"
		if !c.OK {
			mark = "✗"
		}
		if c.Detail == "" {
			fmt.Fprintf(a.stderr, "  %s %s\n", mark, c.Name)
			continue
		}
		fmt.Fprintf(a.stderr, "  %s %s: %s\n", mark, c.Name, c.Detail)
	}
}

// checkFailure turns failed checks into one precondition error.
func checkFailure(checks []service.Check, format string, args ...any) error {
	return &service.Error{
		Code:    service.CodePrecondition,
		Message: fmt.Sprintf(format, args...),
		Details: checks,
	}
}

// newInstallerSource resolves a mirror directory, with the environment as the
// fallback for --from.
func newInstallerSource(from string) *machine.Source {
	s := machine.NewSource(from)
	if installerPin != "" {
		s.PinnedSHA256 = installerPin
	}
	return s
}

// installerPin replaces the pinned install.sh hash. Tests set it to the hash
// of their fake installer; it is never set otherwise.
var installerPin string

// loadState loads this machine's State. A missing file is a precondition with
// the fix the user needs.
func (a *app) loadState() (machine.State, error) {
	state, err := machine.Load()
	if err != nil {
		if machine.IsNotInstalled(err) {
			return machine.State{}, preconditionError("run lewbox install first: this machine has no saved AerolVM install")
		}
		return machine.State{}, err
	}
	return state, nil
}

// mustInstallArgs builds installer arguments or reports a precondition.
func mustInstallArgs(script []byte, spec machine.InstallSpec) ([]string, error) {
	args, err := machine.InstallArgs(script, spec)
	if err != nil {
		return nil, preconditionError("%s", err)
	}
	return args, nil
}

// runInstaller prints and runs one installer command, or only prints it in a
// dry run.
func (a *app) runInstaller(ctx context.Context, runner machine.Runner, cmd machine.Cmd, dryRun bool) error {
	a.say("  → %s", cmd.Display())
	if dryRun {
		return nil
	}
	return runner.Run(ctx, cmd)
}

// installerCommand is `sudo bash <install.sh> <args>`.
func installerCommand(env machine.Env, path string, args []string, secrets ...string) machine.Cmd {
	return machine.Cmd{
		Name:    "bash",
		Args:    append([]string{path}, args...),
		Sudo:    env.GOOS != "darwin",
		Secrets: secrets,
	}
}

// applySettings writes Lewbox's files on this node and waits for health.
func (a *app) applySettings(ctx context.Context, runner machine.Runner) error {
	a.say("Lewbox settings")
	changed, err := machine.ApplySettings(ctx, runner, a.host().localAPIURL, a.host().sleep, func(cmd machine.Cmd) {
		a.say("  → %s", cmd.Display())
	})
	if err != nil {
		return err
	}
	if changed {
		a.say("  ✓ wrote %s and %s, restarted sandboxd", machine.EnvironmentFile, machine.DropInFile)
	} else {
		a.say("  ✓ %s and %s already in place", machine.EnvironmentFile, machine.DropInFile)
	}
	a.say("  ✓ sandboxd healthy at %s/health", a.host().localAPIURL)
	return nil
}

// mintAdminToken reads the PAT and mints the cluster's admin token over
// loopback.
func (a *app) mintAdminToken(ctx context.Context, runner machine.Runner, name string) (string, error) {
	a.say("  → sudo cat /etc/sandboxd/sandboxd.env")
	pat, err := machine.LocalPAT(ctx, runner)
	if err != nil {
		return "", err
	}
	client := aerolvm.New(a.host().localAPIURL, pat)
	token, err := machine.MintAdminToken(ctx, client, name)
	if err != nil {
		return "", fmt.Errorf("mint admin token: %w", err)
	}
	a.say("  ✓ admin token %q minted over %s", name, a.host().localAPIURL)
	return token, nil
}

// localClusterSize asks this machine's AerolVM, over loopback with the PAT,
// how many members its cluster has. A node that isn't in cluster mode is one.
func (a *app) localClusterSize(ctx context.Context, runner machine.Runner) (int, error) {
	pat, err := machine.LocalPAT(ctx, runner)
	if err != nil {
		return 0, err
	}
	client := aerolvm.New(a.host().localAPIURL, pat, aerolvm.WithRetry(15*time.Second))
	members, err := client.Members(ctx)
	if err != nil {
		var apiErr *aerolvm.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusServiceUnavailable && strings.Contains(apiErr.Message, "not enabled") {
			return 1, nil
		}
		return 0, err
	}
	return len(members.Members), nil
}

// dialAddressForJoin returns the host and port to check before joining.
func dialAddressForJoin(joinURL string) (string, error) {
	u, err := url.Parse(joinURL)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("join URL %q has no host", joinURL)
	}
	return net.JoinHostPort(u.Hostname(), "7003"), nil
}

// instanceName is the default node name: the short hostname, lowercased.
func (a *app) instanceName() string {
	host, err := a.host().hostname()
	if err != nil {
		return "lewbox-node"
	}
	host = strings.ToLower(host)
	if i := strings.IndexByte(host, '.'); i >= 0 {
		host = host[:i]
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return "lewbox-node"
	}
	return host
}

// executablePath resolves the running binary to a real path.
func executablePath(fn func() (string, error)) (string, error) {
	path, err := fn()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}
	return resolved, nil
}
