package aerolvm

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// PinnedVersion is the AerolVM version this Lewbox release was tested
	// with. It is the default target of a cluster upgrade.
	PinnedVersion = "v0.7.0"
	// PinnedChecksumsSHA256 is the SHA-256 of PinnedVersion's checksums.txt.
	// A mirror is held to it, so a mirror cannot substitute its own hashes
	// for the release Lewbox was tested with.
	PinnedChecksumsSHA256 = "21a45c118d95424526f2eae38d94c554c8648f21617112c92f6d8c6989715128"
	// DefaultReleaseURL is where AerolVM publishes each release. A file is
	// at <base>/<version>/<name>.
	DefaultReleaseURL = "https://github.com/aerol-ai/microvm/releases/download"
)

const (
	checksumsName = "checksums.txt"
	metadataName  = "release.json"
	maxMetaBytes  = 1 << 20
)

var (
	// releaseTagRE is the shape of an AerolVM release tag, and the only
	// version that may be used in a URL or a path.
	releaseTagRE = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+([.+-][0-9A-Za-z.-]+)?$`)
	// assetRE is one release file an upgrade carries: a Linux sandboxd,
	// toolboxd or caddy for one CPU type.
	assetRE = regexp.MustCompile(`^(sandboxd|toolboxd|caddy)_linux_([a-z0-9]+)$`)
	// sha256RE is a SHA-256 in the shape the API accepts.
	sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ReleaseMetadata is the part of release.json a cluster upgrade reads.
type ReleaseMetadata struct {
	Version               string `json:"version"`
	UpgradeFromMin        string `json:"upgrade_from_min"`
	OneWayMigrations      []int  `json:"one_way_migrations"`
	HostSetupSHA256       string `json:"host_setup_sha256"`
	HostSetupNeedsNetwork bool   `json:"host_setup_needs_network"`
	EgressGatewayProtocol []int  `json:"egress_gateway_protocol"`
	FSMOpsVersion         int    `json:"fsm_ops_version"`
}

// ReleaseFiles is one published release, checked and ready to hand to
// AerolVM's upgrade API. Files maps the assets every node installs to their
// SHA-256; the checksums file is the trust anchor, and release.json has
// already been checked against it.
type ReleaseFiles struct {
	Version      string
	URL          string
	Files        map[string]string
	Metadata     ReleaseMetadata
	ChecksumsSum string
}

// ReleaseSource reads published AerolVM releases. BaseURL is the download
// base; empty means GitHub.
type ReleaseSource struct {
	BaseURL string
	Client  *http.Client
}

// NewReleaseSource returns a source for baseURL, or GitHub when it is empty.
func NewReleaseSource(baseURL string) *ReleaseSource {
	return &ReleaseSource{
		BaseURL: strings.TrimSpace(baseURL),
		Client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *ReleaseSource) base() string {
	if s.BaseURL != "" {
		return strings.TrimRight(s.BaseURL, "/")
	}
	return DefaultReleaseURL
}

// overTLS reports whether the release is read over HTTPS, or from this
// machine (a local mirror or a test server), where there is no network to
// tamper with.
func (s *ReleaseSource) overTLS() bool {
	u, err := url.Parse(s.base())
	if err != nil {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *ReleaseSource) url(version, name string) string {
	return s.base() + "/" + url.PathEscape(version) + "/" + url.PathEscape(name)
}

func (s *ReleaseSource) get(ctx context.Context, raw string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", raw, resp.Status)
	}
	raw2, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw2)) > max {
		return nil, fmt.Errorf("%s is larger than %d bytes", raw, max)
	}
	return raw2, nil
}

// Fetch reads and checks the release at version: its checksums.txt (pinned
// for PinnedVersion), the upgrade files it lists, and release.json checked
// against its checksums line. It fails closed on anything missing,
// malformed, or from a different version.
func (s *ReleaseSource) Fetch(ctx context.Context, version string) (ReleaseFiles, error) {
	version = strings.TrimSpace(version)
	if version == "latest" {
		return ReleaseFiles{}, errors.New("latest is not supported: give a release tag such as v0.7.1")
	}
	if !releaseTagRE.MatchString(version) {
		return ReleaseFiles{}, fmt.Errorf("%q is not an AerolVM release tag such as v0.7.0", version)
	}
	// Only the pinned version's checksums.txt is checked against a known hash;
	// any other version's is trusted because it came over TLS.
	if version != PinnedVersion && !s.overTLS() {
		return ReleaseFiles{}, fmt.Errorf("%s is not HTTPS: only %s can be read from a plain HTTP mirror, because its %s is pinned", s.base(), PinnedVersion, checksumsName)
	}

	raw, err := s.get(ctx, s.url(version, checksumsName), maxMetaBytes)
	if err != nil {
		return ReleaseFiles{}, fmt.Errorf("read %s for %s: %w", checksumsName, version, err)
	}
	sum := sha256.Sum256(raw)
	checksumsSum := hex.EncodeToString(sum[:])
	if version == PinnedVersion && checksumsSum != PinnedChecksumsSHA256 {
		return ReleaseFiles{}, fmt.Errorf("%s for %s has SHA-256 %s, but this Lewbox release pins %s; refusing it",
			checksumsName, version, checksumsSum, PinnedChecksumsSHA256)
	}
	sums, err := parseChecksums(raw)
	if err != nil {
		return ReleaseFiles{}, fmt.Errorf("%s for %s: %w", checksumsName, version, err)
	}
	files, err := pickUpgradeFiles(sums)
	if err != nil {
		return ReleaseFiles{}, fmt.Errorf("release %s: %w", version, err)
	}

	metaRaw, err := s.get(ctx, s.url(version, metadataName), maxMetaBytes)
	if err != nil {
		return ReleaseFiles{}, fmt.Errorf("read %s for %s: %w", metadataName, version, err)
	}
	metaSum := sha256.Sum256(metaRaw)
	if got := hex.EncodeToString(metaSum[:]); got != sums[metadataName] {
		return ReleaseFiles{}, fmt.Errorf("%s for %s has SHA-256 %s, but %s says %s; refusing it",
			metadataName, version, got, checksumsName, sums[metadataName])
	}
	var meta ReleaseMetadata
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return ReleaseFiles{}, fmt.Errorf("%s for %s: %w", metadataName, version, err)
	}
	if meta.Version != version {
		return ReleaseFiles{}, fmt.Errorf("%s for %s describes %s", metadataName, version, meta.Version)
	}
	if meta.UpgradeFromMin != "" && !releaseTagRE.MatchString(meta.UpgradeFromMin) {
		return ReleaseFiles{}, fmt.Errorf("%s for %s has an invalid upgrade_from_min %q", metadataName, version, meta.UpgradeFromMin)
	}

	return ReleaseFiles{
		Version:      version,
		URL:          s.base() + "/" + version,
		Files:        files,
		Metadata:     meta,
		ChecksumsSum: checksumsSum,
	}, nil
}

// parseChecksums reads sha256sum output: "<hex>  <name>" or "<hex> *<name>"
// per line. A line it cannot read is an error, not a skip.
func parseChecksums(raw []byte) (map[string]string, error) {
	sums := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	sc.Buffer(make([]byte, 64<<10), 64<<10)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		sum, name, ok := strings.Cut(text, " ")
		name = strings.TrimPrefix(strings.TrimSpace(name), "*")
		if !ok || !sha256RE.MatchString(sum) || name == "" || name != strings.TrimSpace(name) ||
			strings.ContainsAny(name, `/\`) {
			return nil, fmt.Errorf("line %d is not \"<sha256>  <file>\"", line)
		}
		if prev, dup := sums[name]; dup && prev != sum {
			return nil, fmt.Errorf("names %s twice with different hashes", name)
		}
		sums[name] = sum
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(sums) == 0 {
		return nil, errors.New("is empty")
	}
	return sums, nil
}

// pickUpgradeFiles chooses the assets an upgrade carries: each CPU type with
// a sandboxd needs its toolboxd, caddy is included when the release has it,
// and release.json is required.
func pickUpgradeFiles(sums map[string]string) (map[string]string, error) {
	roles := map[string]map[string]bool{}
	for name := range sums {
		if m := assetRE.FindStringSubmatch(name); m != nil {
			if roles[m[2]] == nil {
				roles[m[2]] = map[string]bool{}
			}
			roles[m[2]][m[1]] = true
		}
	}
	out := map[string]string{}
	var arches []string
	for arch := range roles {
		arches = append(arches, arch)
	}
	sort.Strings(arches)
	for _, arch := range arches {
		if !roles[arch]["sandboxd"] {
			continue // a toolboxd-only platform (freebsd, darwin) is not a node
		}
		if !roles[arch]["toolboxd"] {
			return nil, fmt.Errorf("there is a sandboxd_linux_%s but no toolboxd_linux_%s", arch, arch)
		}
		for role := range roles[arch] {
			name := role + "_linux_" + arch
			out[name] = sums[name]
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no sandboxd_linux_<cpu> file")
	}
	sum, ok := sums[metadataName]
	if !ok {
		return nil, fmt.Errorf("%s is not in %s", metadataName, checksumsName)
	}
	out[metadataName] = sum
	return out, nil
}

var versionRE = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

// CompareVersions orders two release tags: -1 when a is older, 0 when they
// are the same, 1 when a is newer. A prerelease sorts before its release.
func CompareVersions(a, b string) (int, error) {
	pa, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	pb, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	for i := range 3 {
		if pa.nums[i] != pb.nums[i] {
			if pa.nums[i] < pb.nums[i] {
				return -1, nil
			}
			return 1, nil
		}
	}
	switch {
	case pa.pre == "" && pb.pre == "":
		return 0, nil
	case pa.pre == "":
		return 1, nil
	case pb.pre == "":
		return -1, nil
	case pa.pre < pb.pre:
		return -1, nil
	case pa.pre > pb.pre:
		return 1, nil
	}
	return 0, nil
}

type parsedVersion struct {
	nums [3]int
	pre  string
}

func parseVersion(v string) (parsedVersion, error) {
	m := versionRE.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return parsedVersion{}, fmt.Errorf("%q is not a release tag such as v0.7.0", v)
	}
	var out parsedVersion
	for i := range 3 {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return parsedVersion{}, fmt.Errorf("%q is not a release tag such as v0.7.0", v)
		}
		out.nums[i] = n
	}
	out.pre = m[4]
	return out, nil
}
