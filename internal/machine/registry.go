package machine

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// The node registry is CNCF Distribution on loopback, backed by the cluster's
// bucket. AerolVM pushes snapshots to it (DECISIONS D35,
// docs/design/snapshot-archive.md).
const (
	// RegistryVersion is the pinned Distribution release.
	RegistryVersion = "3.1.2"
	// RegistryAddr is where the registry listens. Loopback only: it has no auth.
	RegistryAddr = "127.0.0.1:5000"
	// RegistryBinary is the installed registry binary.
	RegistryBinary = "/usr/local/bin/lewbox-registry"
	// RegistryConfigDir holds its config, readable by root and its user.
	RegistryConfigDir = "/etc/lewbox-registry"
	// RegistryConfigFile is the registry's config, with the bucket's key.
	RegistryConfigFile = RegistryConfigDir + "/config.yml"
	// RegistryUnitFile runs the registry.
	RegistryUnitFile = "/etc/systemd/system/lewbox-registry.service"
	// RegistryUser runs the registry.
	RegistryUser = "lewbox-registry"
	// RegistryTokenFile is what AerolVM sends the registry as a password. The
	// registry has no auth, so it holds random bytes and never the PAT.
	RegistryTokenFile = "/etc/sandboxd/lewbox-registry.token"

	// RegistryReleaseURL is where Distribution publishes its releases.
	RegistryReleaseURL = "https://github.com/distribution/distribution/releases/download"

	registryDownloadTimeout = 5 * time.Minute
	registryHealthTimeout   = 30 * time.Second
	// r2ChunkSize is the multipart part size for Cloudflare R2, which refuses
	// parts of unequal size; Distribution's default is too small for it.
	r2ChunkSize = 100 << 20
)

// registrySHA256 pins each architecture's release archive. Both match the
// release's own .sha256 files.
var registrySHA256 = map[string]string{
	"amd64": "40df2224d410f72ae425c3371873b078bbdbda3b8b612be9571f0e6751f3acc8",
	"arm64": "09d26f88d2c0f161bd1b8bfc6c123571cc3d291dcca8af5477b3cacc4ec95e73",
}

// RegistryStorage is the bucket the registry stores in.
type RegistryStorage struct {
	Endpoint        string // "" = AWS S3
	Region          string
	Bucket          string
	RootDirectory   string // e.g. "/lewbox/clusters/<cluster id>"
	PathStyle       bool
	AccessKeyID     string
	SecretAccessKey string
}

// RegistryRoot is the bucket path a cluster's registry stores under: the
// profile's prefix, then clusters/<cluster id>.
func RegistryRoot(prefix, clusterID string) string {
	parts := []string{}
	if p := strings.Trim(prefix, "/"); p != "" {
		parts = append(parts, p)
	}
	parts = append(parts, "clusters", strings.ToLower(clusterID))
	return "/" + strings.Join(parts, "/")
}

// RegistryArchive is the release archive's file name for arch.
func RegistryArchive(arch string) string {
	return "registry_" + RegistryVersion + "_linux_" + arch + ".tar.gz"
}

// RegistrySource finds the pinned registry binary: in a mirror directory
// (<from>/registry/<archive>), in the cache, or on GitHub. Every copy is
// checked against the pinned SHA-256.
type RegistrySource struct {
	From     string
	BaseURL  string
	CacheDir string
	Arch     string
	Client   *http.Client
	// PinnedSHA256 replaces the release's pinned hash. Tests set it to the
	// hash of a fake archive; it is never set otherwise.
	PinnedSHA256 string
}

// NewRegistrySource returns a RegistrySource with the release's defaults. An
// empty from falls back to LEWBOX_AEROLVM_FROM, like the installer.
func NewRegistrySource(from string) *RegistrySource {
	if strings.TrimSpace(from) == "" {
		from = os.Getenv("LEWBOX_AEROLVM_FROM")
	}
	return &RegistrySource{
		From:    strings.TrimSpace(from),
		BaseURL: RegistryReleaseURL,
		Arch:    runtime.GOARCH,
		Client:  &http.Client{Timeout: registryDownloadTimeout},
	}
}

// Binary returns the verified registry binary.
func (s *RegistrySource) Binary(ctx context.Context) ([]byte, error) {
	arch := s.Arch
	if arch == "" {
		arch = runtime.GOARCH
	}
	want, ok := registrySHA256[arch]
	if s.PinnedSHA256 != "" {
		want, ok = s.PinnedSHA256, true
	}
	if !ok {
		return nil, fmt.Errorf("no pinned registry release for %s", arch)
	}
	name := RegistryArchive(arch)

	var archive []byte
	cache, cacheErr := s.cachePath(name)
	switch {
	case s.From != "":
		data, err := os.ReadFile(filepath.Join(s.From, "registry", name))
		if err != nil {
			return nil, fmt.Errorf("read the registry from the mirror: %w", err)
		}
		archive = data
	case cacheErr == nil && fileHasSum(cache, want):
		archive, _ = os.ReadFile(cache)
	default:
		data, err := s.fetch(ctx, strings.TrimRight(s.baseURL(), "/")+"/v"+RegistryVersion+"/"+name)
		if err != nil {
			return nil, fmt.Errorf("download the registry: %w", err)
		}
		archive = data
	}
	if err := verifySum(archive, want); err != nil {
		return nil, fmt.Errorf("registry %s: %w", name, err)
	}
	if s.From == "" && cacheErr == nil {
		_ = writeCacheFile(cache, archive)
	}
	return extractRegistry(archive)
}

func (s *RegistrySource) baseURL() string {
	if s.BaseURL != "" {
		return s.BaseURL
	}
	return RegistryReleaseURL
}

func (s *RegistrySource) cachePath(name string) (string, error) {
	dir := s.CacheDir
	if dir == "" {
		base, err := defaultCacheDir()
		if err != nil {
			return "", err
		}
		// defaultCacheDir is .../lewbox/aerolvm; the registry sits beside it.
		dir = filepath.Join(filepath.Dir(base), "registry")
	}
	return filepath.Join(dir, name), nil
}

func (s *RegistrySource) fetch(ctx context.Context, raw string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: registryDownloadTimeout}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", raw, res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, 128<<20))
}

func fileHasSum(path, want string) bool {
	data, err := os.ReadFile(path)
	return err == nil && verifySum(data, want) == nil
}

func writeCacheFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return err
	}
	return os.WriteFile(path, data, fileMode)
}

// extractRegistry returns the "registry" file from a release archive.
func extractRegistry(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open the registry archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("the registry archive has no registry binary")
		}
		if err != nil {
			return nil, fmt.Errorf("read the registry archive: %w", err)
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == "registry" {
			return io.ReadAll(io.LimitReader(tr, 256<<20))
		}
	}
}

// RegistryConfig renders the registry's config. Every value is written as a
// JSON string, which YAML reads as-is, so no value can change the structure.
func RegistryConfig(st RegistryStorage, httpSecret string) string {
	q := func(s string) string {
		b, _ := json.Marshal(s)
		return string(b)
	}
	var b strings.Builder
	b.WriteString("# Written by lewbox node configure. Do not edit: it is rewritten.\n")
	b.WriteString("version: 0.1\n")
	b.WriteString("log:\n  level: info\n")
	b.WriteString("storage:\n  s3:\n")
	fmt.Fprintf(&b, "    accesskey: %s\n", q(st.AccessKeyID))
	fmt.Fprintf(&b, "    secretkey: %s\n", q(st.SecretAccessKey))
	region := st.Region
	if region == "" {
		region = "us-east-1"
	}
	fmt.Fprintf(&b, "    region: %s\n", q(region))
	if st.Endpoint != "" {
		fmt.Fprintf(&b, "    regionendpoint: %s\n", q(st.Endpoint))
	}
	fmt.Fprintf(&b, "    forcepathstyle: %t\n", st.PathStyle)
	fmt.Fprintf(&b, "    bucket: %s\n", q(st.Bucket))
	fmt.Fprintf(&b, "    rootdirectory: %s\n", q(st.RootDirectory))
	if isR2(st.Endpoint) {
		fmt.Fprintf(&b, "    chunksize: %d\n", r2ChunkSize)
	}
	b.WriteString("  delete:\n    enabled: true\n")
	b.WriteString("http:\n")
	fmt.Fprintf(&b, "  addr: %s\n", q(RegistryAddr))
	fmt.Fprintf(&b, "  secret: %s\n", q(httpSecret))
	return b.String()
}

func isR2(endpoint string) bool {
	return strings.Contains(strings.ToLower(endpoint), ".r2.cloudflarestorage.com")
}

// registryHTTPSecret reads the http secret from an existing config, so a
// rewrite with the same storage doesn't count as a change.
func registryHTTPSecret(config string) string {
	scanner := bufio.NewScanner(strings.NewReader(config))
	inHTTP := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, " ") {
			inHTTP = strings.TrimSpace(line) == "http:"
			continue
		}
		if !inHTTP {
			continue
		}
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "secret:"); ok {
			var s string
			if json.Unmarshal([]byte(strings.TrimSpace(v)), &s) == nil {
				return s
			}
		}
	}
	return ""
}

// RegistryUnit is the systemd unit that runs the registry, locked down.
const RegistryUnit = `# Written by lewbox node configure.
[Unit]
Description=Lewbox registry: AerolVM snapshots in the cluster's bucket
After=network-online.target
Wants=network-online.target

[Service]
User=` + RegistryUser + `
Group=` + RegistryUser + `
ExecStart=` + RegistryBinary + ` serve ` + RegistryConfigFile + `
Restart=on-failure
RestartSec=5
MemoryMax=512M
Environment=GOMEMLIMIT=400MiB
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
LockPersonality=true

[Install]
WantedBy=multi-user.target
`

// PushEnv is the part of Lewbox's env file that turns on AerolVM's snapshot
// push to the node registry.
func PushEnv(clusterID string) string {
	return "SB_SNAPSHOT_PUSH_ENABLED=true\n" +
		"SB_MIRROR_PUSH_HOST=" + RegistryAddr + "\n" +
		"SB_IMAGE_DISTRIBUTION_AOCR_HOST=" + RegistryAddr + "\n" +
		"SB_AUTO_IMPORT_CLUSTER_ID=" + strings.ToLower(clusterID) + "\n" +
		"SB_AUTO_IMPORT_CLUSTER_PAT_PATH=" + RegistryTokenFile + "\n" +
		"SB_SNAPSHOT_PUSH_RECONCILE_INTERVAL=30s\n"
}

// ApplyRegistry installs or updates the registry on this node and waits for
// it to answer. It reports whether anything changed. report sees every
// privileged command first.
func ApplyRegistry(ctx context.Context, runner Runner, binary []byte, st RegistryStorage, report func(Cmd)) (bool, error) {
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
	changed := false

	sum := sha256.Sum256(binary)
	want := hex.EncodeToString(sum[:])
	have := ""
	if out, err := output(Cmd{Name: "sha256sum", Args: []string{RegistryBinary}, Sudo: true}); err == nil {
		if fields := strings.Fields(string(out)); len(fields) > 0 {
			have = fields[0]
		}
	}
	if have != want {
		if err := run(Cmd{Name: "install", Args: []string{"-m", "0755", "/dev/stdin", RegistryBinary}, Sudo: true, Stdin: bytes.NewReader(binary)}); err != nil {
			return false, err
		}
		changed = true
	}

	if _, err := output(Cmd{Name: "id", Args: []string{"-u", RegistryUser}, Sudo: true}); err != nil {
		if err := run(Cmd{Name: "useradd", Args: []string{"--system", "--no-create-home", "--shell", "/usr/sbin/nologin", RegistryUser}, Sudo: true}); err != nil {
			return false, err
		}
		changed = true
	}
	if err := run(Cmd{Name: "install", Args: []string{"-d", "-m", "0750", "-o", "root", "-g", RegistryUser, RegistryConfigDir}, Sudo: true}); err != nil {
		return false, err
	}

	current, _ := readFile(output, RegistryConfigFile)
	secret := registryHTTPSecret(current)
	if secret == "" {
		var err error
		if secret, err = randomToken(24); err != nil {
			return false, err
		}
	}
	config := RegistryConfig(st, secret)
	if current != config {
		if err := run(Cmd{
			Name:    "install",
			Args:    []string{"-m", "0640", "-o", "root", "-g", RegistryUser, "/dev/stdin", RegistryConfigFile},
			Sudo:    true,
			Stdin:   strings.NewReader(config),
			Secrets: []string{st.SecretAccessKey, secret},
		}); err != nil {
			return false, err
		}
		changed = true
	}

	unit, _ := readFile(output, RegistryUnitFile)
	if unit != RegistryUnit {
		if err := writeFile(run, RegistryUnitFile, "0644", RegistryUnit); err != nil {
			return false, err
		}
		if err := run(Cmd{Name: "systemctl", Args: []string{"daemon-reload"}, Sudo: true}); err != nil {
			return false, err
		}
		changed = true
	}

	if _, err := output(Cmd{Name: "test", Args: []string{"-s", RegistryTokenFile}, Sudo: true}); err != nil {
		token, err := randomToken(32)
		if err != nil {
			return false, err
		}
		if err := run(Cmd{Name: "install", Args: []string{"-m", "0600", "/dev/stdin", RegistryTokenFile}, Sudo: true, Stdin: strings.NewReader(token + "\n"), Secrets: []string{token}}); err != nil {
			return false, err
		}
		changed = true
	}

	active := false
	if out, err := output(Cmd{Name: "systemctl", Args: []string{"is-active", "lewbox-registry"}, Sudo: true}); err == nil {
		active = strings.TrimSpace(string(out)) == "active"
	}
	if err := run(Cmd{Name: "systemctl", Args: []string{"enable", "lewbox-registry"}, Sudo: true}); err != nil {
		return changed, err
	}
	if changed || !active {
		if err := run(Cmd{Name: "systemctl", Args: []string{"restart", "lewbox-registry"}, Sudo: true}); err != nil {
			return changed, err
		}
	}
	return changed, nil
}

// RemoveRegistry stops and removes the registry, when it is installed.
func RemoveRegistry(ctx context.Context, runner Runner, report func(Cmd)) (bool, error) {
	run := func(cmd Cmd) error {
		if report != nil {
			report(cmd)
		}
		return runner.Run(ctx, cmd)
	}
	if _, err := runner.Output(ctx, Cmd{Name: "test", Args: []string{"-e", RegistryUnitFile}, Sudo: true}); err != nil {
		return false, nil
	}
	steps := []Cmd{
		{Name: "systemctl", Args: []string{"disable", "--now", "lewbox-registry"}, Sudo: true},
		{Name: "rm", Args: []string{"-f", RegistryUnitFile, RegistryBinary, RegistryTokenFile}, Sudo: true},
		{Name: "rm", Args: []string{"-rf", RegistryConfigDir}, Sudo: true},
		{Name: "systemctl", Args: []string{"daemon-reload"}, Sudo: true},
	}
	for _, step := range steps {
		if err := run(step); err != nil {
			return true, err
		}
	}
	return true, nil
}

// WaitRegistry polls the registry's /v2/ until it answers.
func WaitRegistry(ctx context.Context, sleep func(context.Context, time.Duration)) error {
	if sleep == nil {
		sleep = sleepContext
	}
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(registryHealthTimeout)
	url := "http://" + RegistryAddr + "/v2/"
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if res, err := client.Do(req); err == nil {
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the registry did not answer %s within %s; see journalctl -u lewbox-registry", url, registryHealthTimeout)
		}
		sleep(ctx, time.Second)
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("make a random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
