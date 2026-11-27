package machine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
)

const (
	// EnvironmentFile is Lewbox's own env file. The installer never touches it.
	EnvironmentFile = "/etc/sandboxd/lewbox.env"
	// CreatePolicyFile is the AerolVM create policy Lewbox writes on each
	// node: it allows only the cluster's runtime.
	CreatePolicyFile = "/etc/sandboxd/lewbox-create-policy.yaml"
	// DropInDir holds the systemd drop-in that loads EnvironmentFile.
	DropInDir = "/etc/systemd/system/sandboxd.service.d"
	// DropInFile is the drop-in itself.
	DropInFile = DropInDir + "/lewbox.conf"

	// healthTimeout bounds a wait for sandboxd to answer /health.
	healthTimeout = 3 * time.Minute
)

// AdvancedSettings is the per-cluster AerolVM tuning Lewbox writes on a node.
// A nil pointer or an empty duration means the setting is off, or AerolVM's
// own default.
type AdvancedSettings struct {
	DockerTimeout      string
	MaxSandboxCPU      *float64
	MaxSandboxMemoryMB *int64
	MaxSandboxDiskGB   *int64
	IdleStopMinutes    *int64
	DestroyIfIdleFor   string
	MaxAge             string
	FirecrackerVMSlots *int64
}

// NodeFiles is everything that decides Lewbox's files on a node: the create
// policy's runtime, the env lines for snapshot push, and the advanced
// settings. Every path that applies settings builds one of these from the
// cluster's node settings, so the files cannot drift apart.
type NodeFiles struct {
	Runtime  string
	PushEnv  string // machine.PushEnv(cluster ID), or "" without storage
	Advanced AdvancedSettings
}

// EnvFileContent is Lewbox's env file: identity mode, long token lifetimes,
// the create policy, the HTTP client timeout (15m unless set), and the
// lifecycle and Firecracker pool settings that are on. PushEnv follows when
// the cluster has storage.
func (f NodeFiles) EnvFileContent() string {
	timeout := f.Advanced.DockerTimeout
	if timeout == "" {
		timeout = "15m"
	}
	var b strings.Builder
	b.WriteString("SB_IDENTITY_ENABLED=true\n")
	b.WriteString("SB_TOKEN_MAX_TTL=876000h\n")
	b.WriteString("SB_CREATE_POLICY_FILE=" + CreatePolicyFile + "\n")
	b.WriteString("SB_HTTP_CLIENT_TIMEOUT=" + timeout + "\n")
	if f.Advanced.IdleStopMinutes != nil {
		b.WriteString("SB_IDLE_TIMEOUT_MIN=" + strconv.FormatInt(*f.Advanced.IdleStopMinutes, 10) + "\n")
	}
	if f.Advanced.DestroyIfIdleFor != "" {
		b.WriteString("SB_DEFAULT_DESTROY_IF_IDLE_FOR=" + f.Advanced.DestroyIfIdleFor + "\n")
	}
	if f.Advanced.MaxAge != "" {
		b.WriteString("SB_MAX_AGE=" + f.Advanced.MaxAge + "\n")
	}
	if f.Advanced.FirecrackerVMSlots != nil && f.Runtime == "firecracker" {
		b.WriteString("SB_FIRECRACKER_TAP_POOL_SIZE=" + strconv.FormatInt(*f.Advanced.FirecrackerVMSlots, 10) + "\n")
	}
	b.WriteString(f.PushEnv)
	return b.String()
}

// CreatePolicyContent is the create policy that allows only the cluster's
// runtime, plus any sandbox size caps. AerolVM refuses every create if the
// file is missing or invalid, so it is written before the env file that names
// it.
func (f NodeFiles) CreatePolicyContent() string {
	var b strings.Builder
	b.WriteString("version: 1\nruntimes: [" + f.Runtime + "]\n")
	if f.Advanced.MaxSandboxCPU == nil && f.Advanced.MaxSandboxMemoryMB == nil && f.Advanced.MaxSandboxDiskGB == nil {
		return b.String()
	}
	b.WriteString("limits:\n")
	if f.Advanced.MaxSandboxCPU != nil {
		b.WriteString("  max_cpu: " + strconv.FormatFloat(*f.Advanced.MaxSandboxCPU, 'f', -1, 64) + "\n")
	}
	if f.Advanced.MaxSandboxMemoryMB != nil {
		b.WriteString("  max_memory_mb: " + strconv.FormatInt(*f.Advanced.MaxSandboxMemoryMB, 10) + "\n")
	}
	if f.Advanced.MaxSandboxDiskGB != nil {
		b.WriteString("  max_disk_gb: " + strconv.FormatInt(*f.Advanced.MaxSandboxDiskGB, 10) + "\n")
	}
	return b.String()
}

// DropInContent loads Lewbox's env file into sandboxd.
const DropInContent = "[Service]\nEnvironmentFile=" + EnvironmentFile + "\n"

// ApplySettings writes Lewbox's files on this node, reloads systemd and
// restarts sandboxd when something changed or it is not running. files is
// everything the cluster's node settings decide: the create policy content,
// the env file, and the runtime between them. The policy is written first, so
// sandboxd never starts pointing at a missing file. It then waits for /health.
// report, when set, sees every privileged command first so the CLI can print
// it.
func ApplySettings(ctx context.Context, runner Runner, baseURL string, sleep func(context.Context, time.Duration), report func(Cmd), files NodeFiles) (bool, error) {
	if files.Runtime != "docker" && files.Runtime != "firecracker" {
		return false, fmt.Errorf("create policy runtime is %q: neither the cluster record nor the saved install names a runtime; want docker or firecracker", files.Runtime)
	}
	run := func(cmd Cmd) error {
		if report != nil {
			report(cmd)
		}
		return runner.Run(ctx, cmd)
	}
	output := func(cmd Cmd) ([]byte, error) {
		if report != nil {
			report(cmd)
		}
		return runner.Output(ctx, cmd)
	}
	policy, _ := readFile(output, CreatePolicyFile)
	envFile, _ := readFile(output, EnvironmentFile)
	dropIn, _ := readFile(output, DropInFile)
	changed := false

	// The policy goes before the env file that points sandboxd at it.
	policyContent := files.CreatePolicyContent()
	envFileContent := files.EnvFileContent()
	if policy != policyContent {
		if err := writeFile(run, CreatePolicyFile, "0644", policyContent); err != nil {
			return false, err
		}
		changed = true
	}
	if envFile != envFileContent {
		if err := writeFile(run, EnvironmentFile, "0600", envFileContent); err != nil {
			return false, err
		}
		changed = true
	}
	if dropIn != DropInContent {
		if err := run(Cmd{Name: "mkdir", Args: []string{"-p", DropInDir}, Sudo: true}); err != nil {
			return false, err
		}
		if err := writeFile(run, DropInFile, "0644", DropInContent); err != nil {
			return false, err
		}
		changed = true
	}

	active := false
	if out, err := output(Cmd{Name: "systemctl", Args: []string{"is-active", "sandboxd"}, Sudo: true}); err == nil {
		active = strings.TrimSpace(string(out)) == "active"
	}
	if changed || !active {
		if err := run(Cmd{Name: "systemctl", Args: []string{"daemon-reload"}, Sudo: true}); err != nil {
			return changed, err
		}
		if err := run(Cmd{Name: "systemctl", Args: []string{"restart", "sandboxd"}, Sudo: true}); err != nil {
			return changed, err
		}
	}
	if _, err := WaitHealth(ctx, baseURL, "", sleep); err != nil {
		return changed, err
	}
	return changed, nil
}

func readFile(output func(Cmd) ([]byte, error), path string) (string, error) {
	out, err := output(Cmd{Name: "cat", Args: []string{path}, Sudo: true})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func writeFile(run func(Cmd) error, path, mode, content string) error {
	return run(Cmd{
		Name:  "install",
		Args:  []string{"-m", mode, "/dev/stdin", path},
		Sudo:  true,
		Stdin: strings.NewReader(content),
	})
}

// LocalPAT reads SB_PAT_TOKEN from the root-only installer env file.
func LocalPAT(ctx context.Context, runner Runner) (string, error) {
	return LocalPATWithSudo(ctx, runner, true)
}

// LocalPATWithSudo reads SB_PAT_TOKEN, with or without sudo. A process that
// already runs as root reads the file directly.
func LocalPATWithSudo(ctx context.Context, runner Runner, sudo bool) (string, error) {
	out, err := runner.Output(ctx, Cmd{Name: "cat", Args: []string{"/etc/sandboxd/sandboxd.env"}, Sudo: sudo})
	if err != nil {
		return "", fmt.Errorf("read /etc/sandboxd/sandboxd.env: %w", err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if value, ok := strings.CutPrefix(line, "SB_PAT_TOKEN="); ok && value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("/etc/sandboxd/sandboxd.env has no SB_PAT_TOKEN")
}

// MintAdminToken revokes any earlier token with the same name, then mints an
// admin token over loopback. Re-runs do not pile up tokens.
func MintAdminToken(ctx context.Context, client *aerolvm.Client, name string) (string, error) {
	tokens, err := client.ListTokens(ctx)
	if err == nil {
		for _, token := range tokens {
			if token.Name == name {
				_ = client.RevokeToken(ctx, token.ID)
			}
		}
	}
	created, err := client.CreateToken(ctx, aerolvm.CreateTokenRequest{
		Name:      name,
		Role:      "admin",
		Team:      "*",
		ExpiresIn: "36500d",
	})
	if err != nil {
		return "", err
	}
	return created.Token, nil
}

// WaitHealth polls /health until it is 200, and until it reports wantVersion
// when that is set.
func WaitHealth(ctx context.Context, baseURL, wantVersion string, sleep func(context.Context, time.Duration)) (string, error) {
	if sleep == nil {
		sleep = sleepContext
	}
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(healthTimeout)
	url := strings.TrimRight(baseURL, "/") + "/health"
	last := ""
	for {
		version, err := healthVersion(ctx, client, url)
		if err == nil {
			last = version
			if wantVersion == "" || version == wantVersion {
				return version, nil
			}
		}
		if time.Now().After(deadline) {
			if last != "" {
				return last, fmt.Errorf("sandboxd is healthy but reports %s, want %s", last, wantVersion)
			}
			return "", fmt.Errorf("sandboxd did not answer %s within %s", url, healthTimeout)
		}
		sleep(ctx, time.Second)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
}

func healthVersion(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: HTTP %d", url, res.StatusCode)
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return "", err
	}
	return body.Version, nil
}

func sleepContext(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
