package machine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
)

const (
	// EnvironmentFile is Lewbox's own env file. The installer never touches it.
	EnvironmentFile = "/etc/sandboxd/lewbox.env"
	// DropInDir holds the systemd drop-in that loads EnvironmentFile.
	DropInDir = "/etc/systemd/system/sandboxd.service.d"
	// DropInFile is the drop-in itself.
	DropInFile = DropInDir + "/lewbox.conf"

	// healthTimeout bounds a wait for sandboxd to answer /health.
	healthTimeout = 3 * time.Minute
)

// EnvFileContent turns on identity mode and lets tokens last as long as
// Lewbox needs.
const EnvFileContent = "SB_IDENTITY_ENABLED=true\nSB_TOKEN_MAX_TTL=876000h\n"

// DropInContent loads Lewbox's env file into sandboxd.
const DropInContent = "[Service]\nEnvironmentFile=" + EnvironmentFile + "\n"

// ApplySettings writes Lewbox's two files on this node, reloads systemd and
// restarts sandboxd when something changed or it is not running. envFile is
// the content of Lewbox's env file: EnvFileContent, plus machine.PushEnv when
// the cluster has storage. It then waits for /health. report, when set, sees
// every privileged command first so the CLI can print it.
func ApplySettings(ctx context.Context, runner Runner, baseURL string, sleep func(context.Context, time.Duration), report func(Cmd), envFileContent string) (bool, error) {
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
	envFile, _ := readFile(output, EnvironmentFile)
	dropIn, _ := readFile(output, DropInFile)
	changed := false

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
