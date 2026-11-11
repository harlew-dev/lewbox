package machine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// DefaultReleaseURL is where Lewbox publishes its releases.
	DefaultReleaseURL = "https://github.com/harlew-dev/lewbox/releases"
	// releaseTimeout bounds one release download.
	releaseTimeout = 2 * time.Minute
)

// Release is one Lewbox release: its own version and the AerolVM version it
// was tested with.
type Release struct {
	Version        string `json:"version"`
	AerolVMVersion string `json:"aerolvm_version"`
}

// ReleaseSource reads Lewbox releases from a directory or a release URL.
type ReleaseSource struct {
	Dir     string
	BaseURL string
	GOOS    string
	GOARCH  string
	Client  *http.Client
}

// NewReleaseSource returns a source with the release defaults. An empty dir
// falls back to LEWBOX_RELEASE_FROM.
func NewReleaseSource(dir string) *ReleaseSource {
	if strings.TrimSpace(dir) == "" {
		dir = os.Getenv("LEWBOX_RELEASE_FROM")
	}
	base := os.Getenv("LEWBOX_RELEASE_URL")
	if base == "" {
		base = DefaultReleaseURL
	}
	return &ReleaseSource{
		Dir:     strings.TrimSpace(dir),
		BaseURL: strings.TrimRight(base, "/"),
		GOOS:    runtime.GOOS,
		GOARCH:  runtime.GOARCH,
		Client:  &http.Client{Timeout: releaseTimeout},
	}
}

// BinaryName is the asset name for this OS and architecture.
func (r *ReleaseSource) BinaryName() string {
	goos, goarch := r.GOOS, r.GOARCH
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	return "lewbox_" + goos + "_" + goarch
}

// Release reads release.json for tag, or for the latest release when tag is
// empty.
func (r *ReleaseSource) Release(ctx context.Context, tag string) (Release, error) {
	data, err := r.Asset(ctx, tag, "release.json")
	if err != nil {
		return Release{}, err
	}
	var rel Release
	if err := json.Unmarshal(data, &rel); err != nil {
		return Release{}, fmt.Errorf("parse release.json: %w", err)
	}
	if rel.Version == "" {
		return Release{}, fmt.Errorf("release.json has no version")
	}
	if rel.AerolVMVersion == "" {
		return Release{}, fmt.Errorf("release.json has no aerolvm_version")
	}
	return rel, nil
}

// Checksum reads checksums.txt and returns the SHA-256 for name.
func (r *ReleaseSource) Checksum(ctx context.Context, tag, name string) (string, error) {
	data, err := r.Asset(ctx, tag, "checksums.txt")
	if err != nil {
		return "", err
	}
	sum, ok := checksumFor(data, name)
	if !ok {
		return "", fmt.Errorf("checksums.txt has no line for %s", name)
	}
	return sum, nil
}

// Asset reads one file from a release.
func (r *ReleaseSource) Asset(ctx context.Context, tag, name string) ([]byte, error) {
	if r.Dir != "" {
		data, err := os.ReadFile(filepath.Join(r.Dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s from %s: %w", name, r.Dir, err)
		}
		return data, nil
	}
	return r.fetch(ctx, r.assetURL(tag, name))
}

func (r *ReleaseSource) assetURL(tag, name string) string {
	base := r.BaseURL
	if base == "" {
		base = DefaultReleaseURL
	}
	if tag == "" {
		return base + "/latest/download/" + name
	}
	return base + "/download/" + url.PathEscape(tag) + "/" + name
}

func (r *ReleaseSource) fetch(ctx context.Context, raw string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: releaseTimeout}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", raw, res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 128<<20))
	if err != nil {
		return nil, err
	}
	return data, nil
}
