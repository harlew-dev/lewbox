package machine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/harlew-dev/lewbox/internal/service"
)

// Env is the machine facts the install checks read. Tests replace them.
type Env struct {
	GOOS   string
	Listen func(network, address string) (net.Listener, error)
	// TCPListening reports whether anything listens on a TCP port. The
	// ports check asks it when Listen is refused, which happens for every
	// port below 1024 when the CLI runs as a normal user.
	TCPListening    func(port int) (bool, error)
	Stat            func(name string) (os.FileInfo, error)
	Run             func(ctx context.Context, cmd Cmd) error
	Terminal        bool
	SandboxdRunning func(ctx context.Context) bool
}

// DefaultEnv reads the real machine.
func DefaultEnv() Env {
	return Env{
		GOOS:         runtime.GOOS,
		Listen:       net.Listen,
		TCPListening: procTCPListening,
		Stat:         os.Stat,
		Run: func(ctx context.Context, cmd Cmd) error {
			return (&ExecRunner{Stdout: io.Discard, Stderr: io.Discard}).Run(ctx, cmd)
		},
		SandboxdRunning: func(ctx context.Context) bool {
			client := &http.Client{Timeout: 2 * time.Second}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:21212/health", nil)
			if err != nil {
				return false
			}
			res, err := client.Do(req)
			if err != nil {
				return false
			}
			defer res.Body.Close()
			return res.StatusCode == http.StatusOK
		},
	}
}

// CheckOptions describe the install being checked.
type CheckOptions struct {
	Mode          string
	Runtime       string
	Domain        string
	DNSTokenStdin bool
	DNSProfile    bool // a DNS profile supplies the Cloudflare token
}

// Check runs every precondition for an install. A failed check's Detail says
// how to fix it.
func Check(ctx context.Context, env Env, opts CheckOptions) []service.Check {
	checks := make([]service.Check, 0, 6)
	checks = append(checks, checkOS(env, opts))
	if env.GOOS == "darwin" && opts.Mode == ModeLocal {
		checks = append(checks, service.Check{Name: "sudo", OK: true, Detail: "not needed with --mode local on macOS"})
	} else {
		checks = append(checks, checkSudo(ctx, env))
	}
	if opts.Runtime == "firecracker" {
		checks = append(checks, checkKVM(env))
	}
	if opts.Runtime == "firecracker" && opts.Mode == ModeLocal {
		checks = append(checks, service.Check{
			Name:   "runtime",
			Detail: "firecracker needs a cluster and cannot be used with --mode local; use --runtime docker here",
		})
	}
	checks = append(checks, checkPorts(ctx, env, opts))
	if opts.Mode == ModeDomain {
		check := service.Check{Name: "dns_token", OK: true, Detail: "a Cloudflare token will be read from stdin"}
		switch {
		case opts.DNSProfile:
			check.Detail = "the DNS profile supplies the Cloudflare token"
		case opts.DNSTokenStdin:
		default:
			check.OK = false
			check.Detail = "domain mode needs --dns-profile or --dns-api-token-stdin: use a saved DNS profile, or pipe a Cloudflare token with Zone:Read and DNS:Edit on stdin"
		}
		checks = append(checks, check)
	}
	return checks
}

func checkOS(env Env, opts CheckOptions) service.Check {
	switch {
	case env.GOOS == "linux":
		return service.Check{Name: "os", OK: true, Detail: "Linux"}
	case env.GOOS == "darwin":
		// AerolVM's installer does support macOS with --local (a per-user
		// LaunchAgent), but Lewbox's own steps after it assume systemd and
		// /etc/sandboxd. Refuse up front rather than fail half way.
		return service.Check{
			Name:   "os",
			Detail: "macOS isn't supported by lewbox install yet (Lewbox's settings step assumes Linux and systemd); use a Linux machine or VM",
		}
	default:
		return service.Check{
			Name:   "os",
			Detail: "AerolVM runs on Linux; on macOS use --mode local",
		}
	}
}

func checkSudo(ctx context.Context, env Env) service.Check {
	if env.Run == nil {
		return service.Check{Name: "sudo", Detail: "cannot test sudo: no runner"}
	}
	if err := env.Run(ctx, Cmd{Name: "sudo", Args: []string{"-n", "true"}}); err == nil {
		return service.Check{Name: "sudo", OK: true, Detail: "sudo works without a password"}
	}
	if env.Terminal {
		if err := env.Run(ctx, Cmd{Name: "sudo", Args: []string{"-v"}}); err == nil {
			return service.Check{Name: "sudo", OK: true, Detail: "sudo works"}
		}
	}
	return service.Check{
		Name:   "sudo",
		Detail: "`sudo -n true` failed: run `sudo -v` in a terminal first, then retry",
	}
}

func checkKVM(env Env) service.Check {
	stat := env.Stat
	if stat == nil {
		stat = os.Stat
	}
	if _, err := stat("/dev/kvm"); err != nil {
		return service.Check{
			Name:   "kvm",
			Detail: "firecracker needs /dev/kvm: enable virtualization in the BIOS and load the kvm module",
		}
	}
	return service.Check{Name: "kvm", OK: true, Detail: "/dev/kvm exists"}
}

func checkPorts(ctx context.Context, env Env, opts CheckOptions) service.Check {
	ports := []int{21212}
	if opts.Mode == ModeDomain {
		ports = append(ports, 443)
	}
	if opts.Mode != ModeLocal {
		ports = append(ports, 7000, 7001, 7002, 7003)
	}
	listen := env.Listen
	if listen == nil {
		listen = net.Listen
	}
	var busy []string
	ours := false
	for _, port := range ports {
		l, err := listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
		if err == nil {
			_ = l.Close()
			continue
		}
		if errors.Is(err, os.ErrPermission) && env.TCPListening != nil {
			// Not allowed to bind (a privileged port, as a normal user):
			// look for a listener instead. If that fails too, count the
			// port as busy.
			if listening, lerr := env.TCPListening(port); lerr == nil && !listening {
				continue
			}
		}
		if !ours && env.SandboxdRunning != nil && env.SandboxdRunning(ctx) {
			ours = true
		}
		if ours {
			continue
		}
		busy = append(busy, fmt.Sprint(port))
	}
	if len(busy) > 0 {
		return service.Check{
			Name:   "ports",
			Detail: fmt.Sprintf("ports %s are in use: stop the process using them, or stop an old sandboxd first", joinComma(busy)),
		}
	}
	detail := fmt.Sprintf("ports %s are free", joinComma(ints(ports)))
	if ours {
		detail = "the ports are held by this machine's own sandboxd"
	}
	return service.Check{Name: "ports", OK: true, Detail: detail}
}

// procTCPListening reads /proc/net/tcp and /proc/net/tcp6, which any user
// may read, for a socket in LISTEN state on port.
func procTCPListening(port int) (bool, error) {
	read := 0
	for _, name := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		f, err := os.Open(name)
		if errors.Is(err, os.ErrNotExist) && name == "/proc/net/tcp6" {
			continue // IPv6 disabled
		}
		if err != nil {
			return false, err
		}
		found, err := tcpTableListening(f, port)
		f.Close()
		if err != nil {
			return false, fmt.Errorf("%s: %w", name, err)
		}
		if found {
			return true, nil
		}
		read++
	}
	if read == 0 {
		return false, errors.New("no TCP tables to read")
	}
	return false, nil
}

// tcpTableListening scans one /proc/net/tcp{,6} table. Each row's
// local_address is HEXIP:HEXPORT, and state 0A is LISTEN.
func tcpTableListening(r io.Reader, port int) (bool, error) {
	sc := bufio.NewScanner(r)
	header := true
	for sc.Scan() {
		if header {
			header = false
			continue
		}
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}
		_, hexPort, ok := strings.Cut(fields[1], ":")
		if !ok {
			continue
		}
		p, err := strconv.ParseUint(hexPort, 16, 16)
		if err != nil {
			continue
		}
		if int(p) == port && fields[3] == "0A" {
			return true, nil
		}
	}
	return false, sc.Err()
}

func ints(values []int) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, fmt.Sprint(v))
	}
	return out
}

func joinComma(values []string) string {
	if len(values) == 0 {
		return ""
	}
	out := values[0]
	for _, v := range values[1:] {
		out += ", " + v
	}
	return out
}

// Failed reports the first failed check, which is what the CLI shows.
func Failed(checks []service.Check) (service.Check, bool) {
	for _, c := range checks {
		if !c.OK {
			return c, true
		}
	}
	return service.Check{}, false
}
