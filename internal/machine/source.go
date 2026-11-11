package machine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	// PinnedVersion is the AerolVM version this Lewbox release was tested with.
	PinnedVersion = "v0.7.0"
	// PinnedInstallSHA256 is the checksum of that release's install.sh.
	PinnedInstallSHA256 = "0305d18cb86ef06b11dd6b3cc40f91f8caa6abc8b5975895231ff894b9848d33"
	// ReleaseBaseURL is where AerolVM publishes its installer and binaries.
	ReleaseBaseURL = "https://github.com/aerol-ai/microvm/releases"
	// installTimeout bounds one installer download.
	installTimeout = 2 * time.Minute
)

// DownloadOption is one installer flag that points at a mirrored file.
type DownloadOption struct {
	Flag  string
	Value string
}

// Installer is a verified install.sh and the options that make it use a
// local mirror instead of downloading.
type Installer struct {
	Version string
	Path    string
	Script  []byte
	Options []DownloadOption
	// Warning is set when Version is not the pinned, tested one.
	Warning string
}

// Source resolves an AerolVM version to a verified installer. A non-empty
// From reads it from a mirror directory instead of GitHub.
type Source struct {
	From          string
	BaseURL       string
	PinnedVersion string
	PinnedSHA256  string
	CacheDir      string
	Arch          string
	Client        *http.Client
}

// NewSource returns a Source with the release's defaults. An empty from falls
// back to LEWBOX_AEROLVM_FROM.
func NewSource(from string) *Source {
	if strings.TrimSpace(from) == "" {
		from = os.Getenv("LEWBOX_AEROLVM_FROM")
	}
	return &Source{
		From:          strings.TrimSpace(from),
		BaseURL:       ReleaseBaseURL,
		PinnedVersion: PinnedVersion,
		PinnedSHA256:  PinnedInstallSHA256,
		Arch:          runtime.GOARCH,
		Client:        &http.Client{Timeout: installTimeout},
	}
}

// Installer returns the install.sh for version and checks its SHA-256 against
// the pinned value or the release's checksums.txt.
//
// "latest" means AerolVM's newest release, read from its release.json; it is
// untested with this Lewbox release, and Warning says so.
func (s *Source) Installer(ctx context.Context, version string) (Installer, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		version = s.pinnedVersion()
	}
	if version == "latest" {
		if s.From != "" {
			return Installer{}, fmt.Errorf("a mirror has no \"latest\": pass --aerolvm-version <tag>")
		}
		latest, err := s.latestVersion(ctx)
		if err != nil {
			return Installer{}, err
		}
		version = latest
	}
	// The version comes from flags and from a cluster's /health answer. It
	// becomes a path under the mirror and the cache and part of a URL, so
	// it must be a plain release tag and nothing that could walk out of them.
	if !releaseTag.MatchString(version) {
		return Installer{}, fmt.Errorf("%q is not an AerolVM release tag such as v0.7.0", version)
	}
	var (
		inst Installer
		err  error
	)
	if s.From != "" {
		inst, err = s.fromMirror(version)
	} else {
		inst, err = s.fromNetwork(ctx, version)
	}
	if err == nil && version != s.pinnedVersion() {
		inst.Warning = fmt.Sprintf("AerolVM %s is not the version this Lewbox release was tested with (%s)", version, s.pinnedVersion())
	}
	return inst, err
}

// releaseTag is the shape of an AerolVM release tag, as install.sh accepts it.
var releaseTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+([.+-][0-9A-Za-z.-]+)?$`)

// latestVersion reads the newest release's tag from its release.json.
func (s *Source) latestVersion(ctx context.Context) (string, error) {
	data, err := s.fetch(ctx, s.baseURL()+"/latest/download/release.json")
	if err != nil {
		return "", fmt.Errorf("find AerolVM's latest release: %w", err)
	}
	var rel struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &rel); err != nil || rel.Version == "" {
		return "", fmt.Errorf("AerolVM's latest release.json has no version")
	}
	return rel.Version, nil
}

func (s *Source) fromMirror(version string) (Installer, error) {
	dir, err := filepath.Abs(filepath.Join(s.From, version))
	if err != nil {
		return Installer{}, fmt.Errorf("resolve mirror directory: %w", err)
	}
	script, err := os.ReadFile(filepath.Join(dir, "install.sh"))
	if err != nil {
		return Installer{}, fmt.Errorf("read install.sh from %s: %w", dir, err)
	}
	sums, err := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	if err != nil {
		return Installer{}, fmt.Errorf("read checksums.txt from %s: %w", dir, err)
	}
	want, ok := checksumFor(sums, "install.sh")
	if !ok {
		return Installer{}, fmt.Errorf("%s/checksums.txt has no line for install.sh", dir)
	}
	// A mirror could rewrite both files, so the pinned version is held to
	// the hash built into this release, not just the mirror's own list.
	if version == s.pinnedVersion() {
		want = s.pinnedSHA256()
	}
	if err := verifySum(script, want); err != nil {
		return Installer{}, fmt.Errorf("install.sh from %s: %w", dir, err)
	}
	// Run a copy of the bytes just verified, never the mirror's file: it
	// could change between the check and the run, and a mounted mirror is
	// often read-only and not executable anyway.
	path, err := s.stage(version, script)
	if err != nil {
		return Installer{}, err
	}
	return Installer{
		Version: version,
		Path:    path,
		Script:  script,
		Options: mirrorOptions(dir),
	}, nil
}

func (s *Source) fromNetwork(ctx context.Context, version string) (Installer, error) {
	want := ""
	if version == s.pinnedVersion() {
		want = s.pinnedSHA256()
	}
	if want == "" {
		sums, err := s.fetch(ctx, s.assetURL(version, "checksums.txt"))
		if err != nil {
			return Installer{}, fmt.Errorf("get checksums.txt for %s: %w", version, err)
		}
		var ok bool
		want, ok = checksumFor(sums, "install.sh")
		if !ok {
			return Installer{}, fmt.Errorf("checksums.txt for %s has no line for install.sh", version)
		}
	}

	cache, cacheErr := s.cachePath(version)
	if cacheErr == nil {
		if data, err := os.ReadFile(cache); err == nil {
			if verifySum(data, want) == nil {
				return Installer{Version: version, Path: cache, Script: data}, nil
			}
		}
	}

	script, err := s.fetch(ctx, s.assetURL(version, "install.sh"))
	if err != nil {
		return Installer{}, fmt.Errorf("get install.sh for %s: %w", version, err)
	}
	if err := verifySum(script, want); err != nil {
		return Installer{}, fmt.Errorf("install.sh for %s: %w", version, err)
	}
	path, err := s.stage(version, script)
	if err != nil {
		return Installer{}, err
	}
	return Installer{Version: version, Path: path, Script: script}, nil
}

// stage writes verified installer bytes to the cache (or, without one, a
// temporary file) and returns the path to run.
func (s *Source) stage(version string, script []byte) (string, error) {
	if cache, err := s.cachePath(version); err == nil {
		if err := writeCache(cache, script); err != nil {
			return "", err
		}
		return cache, nil
	}
	f, err := os.CreateTemp("", "lewbox-install-*.sh")
	if err != nil {
		return "", fmt.Errorf("stage install.sh: %w", err)
	}
	if _, err := f.Write(script); err != nil {
		f.Close()
		return "", fmt.Errorf("stage install.sh: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("stage install.sh: %w", err)
	}
	return f.Name(), nil
}

func (s *Source) pinnedSHA256() string {
	if s.PinnedSHA256 != "" {
		return s.PinnedSHA256
	}
	return PinnedInstallSHA256
}

func (s *Source) pinnedVersion() string {
	if s.PinnedVersion != "" {
		return s.PinnedVersion
	}
	return PinnedVersion
}

func (s *Source) baseURL() string {
	if s.BaseURL != "" {
		return strings.TrimRight(s.BaseURL, "/")
	}
	return ReleaseBaseURL
}

func (s *Source) assetURL(version, name string) string {
	return s.baseURL() + "/download/" + url.PathEscape(version) + "/" + name
}

func (s *Source) cachePath(version string) (string, error) {
	dir := s.CacheDir
	if dir == "" {
		var err error
		dir, err = defaultCacheDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(dir, version, "install.sh"), nil
}

func (s *Source) fetch(ctx context.Context, raw string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: installTimeout}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", raw, res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	return data, nil
}

// defaultCacheDir is $XDG_CACHE_HOME/lewbox/aerolvm, falling back to
// ~/.cache/lewbox/aerolvm.
func defaultCacheDir() (string, error) {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "lewbox", "aerolvm"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "lewbox", "aerolvm"), nil
}

func writeCache(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return fmt.Errorf("create installer cache: %w", err)
	}
	if err := os.WriteFile(path, data, fileMode); err != nil {
		return fmt.Errorf("cache install.sh: %w", err)
	}
	return nil
}

// mirrorOptions point the installer at the files in a mirror directory so it
// downloads nothing.
func mirrorOptions(dir string) []DownloadOption {
	arch := runtime.GOARCH
	return []DownloadOption{
		{Flag: "--sandboxd-url", Value: fileURL(filepath.Join(dir, "sandboxd_linux_"+arch))},
		{Flag: "--toolboxd-url", Value: fileURL(filepath.Join(dir, "toolboxd_linux_"+arch))},
		{Flag: "--checksums-url", Value: fileURL(filepath.Join(dir, "checksums.txt"))},
		{Flag: "--caddy-binary-url", Value: fileURL(filepath.Join(dir, "caddy_linux_"+arch))},
		{Flag: "--cli-url", Value: fileURL(filepath.Join(dir, "aerolvm_linux_"+arch))},
	}
}

func fileURL(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

// checksumFor finds the SHA-256 for name in a checksums.txt file.
func checksumFor(data []byte, name string) (string, bool) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		file := strings.TrimPrefix(fields[len(fields)-1], "*")
		if file != name {
			continue
		}
		return strings.ToLower(strings.TrimPrefix(fields[0], "sha256:")), true
	}
	return "", false
}

// VerifySum checks data's SHA-256 against hexWant.
func VerifySum(data []byte, hexWant string) error {
	return verifySum(data, hexWant)
}

func verifySum(data []byte, hexWant string) error {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, strings.TrimSpace(hexWant)) {
		return fmt.Errorf("sha256 is %s, want %s", got, strings.TrimSpace(hexWant))
	}
	return nil
}
