package machine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

const fullScript = `#!/usr/bin/env bash
# Usage: install.sh [options] [init|join]
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

const oldScript = `#!/usr/bin/env bash
# Usage: install.sh [options]
#   --domain <domain>
#   --dns-provider <provider>
#   --dns-api-token <token>
#   --sandboxd-url <url>
#   --toolboxd-url <url>
#   --checksums-url <url>
`

func sum(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

func TestInstallArgs(t *testing.T) {
	cases := []struct {
		name   string
		script string
		spec   InstallSpec
		want   []string
	}{
		{
			name:   "local",
			script: fullScript,
			spec:   InstallSpec{Version: "v0.7.0", NodeName: "node-1", Local: true},
			want:   []string{"--version", "v0.7.0", "--node-name", "node-1", "--local"},
		},
		{
			name:   "domain",
			script: fullScript,
			spec: InstallSpec{
				Version: "v0.7.0", NodeName: "eu-west-1", Domain: "eu.example",
				DNSToken: "cf", ACMEEmail: "ops@example", PAScope: true, NoStart: true,
			},
			want: []string{
				"--version", "v0.7.0", "--pat-scope", "host", "--node-name", "eu-west-1",
				"--domain", "eu.example", "--dns-provider", "cloudflare",
				"--dns-api-token", "cf", "--acme-email", "ops@example", "--no-start",
			},
		},
		{
			name:   "public host firecracker",
			script: fullScript,
			spec: InstallSpec{
				Version: "v0.7.0", NodeName: "node-1", PublicHost: "10.0.0.7",
				Firecracker: true, PAScope: true, NoStart: true,
			},
			want: []string{
				"--version", "v0.7.0", "--pat-scope", "host", "--node-name", "node-1",
				"--public-host", "10.0.0.7", "--with-firecracker", "--no-start",
			},
		},
		{
			name:   "passthrough comes last",
			script: fullScript,
			spec:   InstallSpec{Version: "v0.7.0", Passthrough: []string{"--lb-health-port", "8081"}},
			want:   []string{"--version", "v0.7.0", "--lb-health-port", "8081"},
		},
		{
			name:   "old installer skips unknown options",
			script: oldScript,
			spec: InstallSpec{
				Version: "", Local: false, Domain: "eu.example", DNSToken: "cf", PAScope: true, NoStart: true,
				Options: []DownloadOption{
					{Flag: "--sandboxd-url", Value: "file:///s"},
					{Flag: "--cli-url", Value: "file:///c"},
				},
			},
			want: []string{
				"--sandboxd-url", "file:///s",
				"--domain", "eu.example", "--dns-provider", "cloudflare", "--dns-api-token", "cf",
			},
		},
		{
			name:   "no version for join",
			script: fullScript,
			spec:   InstallSpec{},
			want:   nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := InstallArgs([]byte(tc.script), tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Errorf("args = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInstallArgsMissingRequired(t *testing.T) {
	cases := []struct {
		name   string
		script string
		spec   InstallSpec
	}{
		{"version", oldScript, InstallSpec{Version: "v0.7.0"}},
		{"local", oldScript, InstallSpec{Local: true}},
		{"domain", "#  --version <tag>\n", InstallSpec{Domain: "eu.example"}},
		{"public host", oldScript, InstallSpec{PublicHost: "10.0.0.7"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := InstallArgs([]byte(tc.script), tc.spec); err == nil {
				t.Fatal("expected an error")
			} else if !strings.Contains(err.Error(), "does not support") {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestSupportsFlagBoundary(t *testing.T) {
	script := []byte("#  --locality x\n#  --local\ty\n")
	if supportsFlag(script, "--local") != true {
		t.Error("--local not found")
	}
	if supportsFlag([]byte("#  --locality x\n"), "--local") {
		t.Error("--locality matched --local")
	}
}

func newMirror(t *testing.T, version string, script []byte, checksum string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "install.sh"), script, 0o644); err != nil {
		t.Fatal(err)
	}
	sums := checksum + "  install.sh\n"
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(sums), 0o644); err != nil {
		t.Fatal(err)
	}
	arch := runtime.GOARCH
	for _, name := range []string{"sandboxd_linux_" + arch, "toolboxd_linux_" + arch, "caddy_linux_" + arch, "aerolvm_linux_" + arch} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("binary"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Dir(dir)
}

func TestSourceFromMirror(t *testing.T) {
	script := []byte(fullScript)
	mirror := newMirror(t, "v0.7.0", script, sum(script))
	src := &Source{From: mirror, PinnedSHA256: sum(script), CacheDir: t.TempDir()}
	got, err := src.Installer(context.Background(), "v0.7.0")
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Script) != string(script) {
		t.Error("script differs")
	}
	if !strings.HasSuffix(got.Path, filepath.Join("v0.7.0", "install.sh")) {
		t.Errorf("path = %s", got.Path)
	}
	if len(got.Options) != 5 {
		t.Fatalf("options = %+v", got.Options)
	}
	for _, opt := range got.Options {
		if !strings.HasPrefix(opt.Value, "file://") {
			t.Errorf("%s = %s, want a file URL", opt.Flag, opt.Value)
		}
	}
}

func TestSourceFromMirrorRefusesBadChecksum(t *testing.T) {
	script := []byte(fullScript)
	mirror := newMirror(t, "v0.7.0", script, strings.Repeat("0", 64))
	src := &Source{From: mirror}
	if _, err := src.Installer(context.Background(), "v0.7.0"); err == nil {
		t.Fatal("bad checksum accepted")
	}
}

func TestSourceNetwork(t *testing.T) {
	pinned := []byte(fullScript)
	other := []byte(oldScript)
	var checksumRequests atomic.Int32
	handler := http.NewServeMux()
	handler.HandleFunc("/download/v0.7.0/install.sh", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(pinned)
	})
	handler.HandleFunc("/download/v0.7.0/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		checksumRequests.Add(1)
		_, _ = w.Write([]byte(sum(pinned) + "  install.sh\n"))
	})
	handler.HandleFunc("/download/v0.9.0/install.sh", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(other)
	})
	handler.HandleFunc("/download/v0.9.0/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		checksumRequests.Add(1)
		_, _ = w.Write([]byte(sum(other) + "  install.sh\n"))
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	base := func() *Source {
		return &Source{
			BaseURL:       srv.URL,
			PinnedVersion: "v0.7.0",
			PinnedSHA256:  sum(pinned),
			CacheDir:      t.TempDir(),
			Client:        srv.Client(),
		}
	}

	t.Run("pinned", func(t *testing.T) {
		inst, err := base().Installer(context.Background(), "v0.7.0")
		if err != nil {
			t.Fatal(err)
		}
		if string(inst.Script) != string(pinned) {
			t.Error("script differs")
		}
		if checksumRequests.Load() != 0 {
			t.Error("pinned version asked for checksums.txt")
		}
		if _, err := os.Stat(inst.Path); err != nil {
			t.Errorf("installer was not cached: %v", err)
		}
	})

	t.Run("pinned bad hash", func(t *testing.T) {
		src := base()
		src.PinnedSHA256 = strings.Repeat("0", 64)
		if _, err := src.Installer(context.Background(), "v0.7.0"); err == nil {
			t.Fatal("bad hash accepted")
		}
	})

	t.Run("other version uses checksums.txt", func(t *testing.T) {
		inst, err := base().Installer(context.Background(), "v0.9.0")
		if err != nil {
			t.Fatal(err)
		}
		if string(inst.Script) != string(other) {
			t.Error("script differs")
		}
		if checksumRequests.Load() == 0 {
			t.Error("checksums.txt was not read")
		}
	})

	t.Run("cached pinned download is reused", func(t *testing.T) {
		src := base()
		if _, err := src.Installer(context.Background(), "v0.7.0"); err != nil {
			t.Fatal(err)
		}
		// Remove the network source: a cached installer must still work.
		src.BaseURL = "http://127.0.0.1:1"
		if _, err := src.Installer(context.Background(), "v0.7.0"); err != nil {
			t.Errorf("cached installer not used: %v", err)
		}
	})
}

func TestReleaseSourceDirectory(t *testing.T) {
	dir := t.TempDir()
	binary := []byte("lewbox binary")
	files := map[string]string{
		"release.json":       `{"version":"v0.4.0","aerolvm_version":"v0.7.1"}`,
		"checksums.txt":      sum(binary) + "  lewbox_linux_amd64\n",
		"lewbox_linux_amd64": string(binary),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	src := &ReleaseSource{Dir: dir, GOOS: "linux", GOARCH: "amd64"}
	rel, err := src.Release(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "v0.4.0" || rel.AerolVMVersion != "v0.7.1" {
		t.Errorf("release = %+v", rel)
	}
	if src.BinaryName() != "lewbox_linux_amd64" {
		t.Errorf("binary name = %s", src.BinaryName())
	}
	data, err := src.Asset(context.Background(), "", src.BinaryName())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(binary) {
		t.Error("binary differs")
	}
	got, err := src.Checksum(context.Background(), "", src.BinaryName())
	if err != nil {
		t.Fatal(err)
	}
	if got != sum(binary) {
		t.Errorf("checksum = %s", got)
	}
}

func TestReleaseSourceURLs(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "release.json") {
			_, _ = w.Write([]byte(`{"version":"v0.4.0","aerolvm_version":"v0.7.1"}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	src := &ReleaseSource{BaseURL: srv.URL, Client: srv.Client(), GOOS: "linux", GOARCH: "amd64"}
	if _, err := src.Release(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Release(context.Background(), "v0.3.0"); err != nil {
		t.Fatal(err)
	}
	want := []string{"/latest/download/release.json", "/download/v0.3.0/release.json"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("paths = %v, want %v", got, want)
	}
}

// A mirror can rewrite install.sh and its checksums.txt together, so for the
// pinned version only the hash built into the binary counts. The script that
// runs is a staged copy, never the mirror's own file.
func TestSourceMirrorHeldToPinnedHash(t *testing.T) {
	script := []byte(fullScript)
	mirror := newMirror(t, "v0.7.0", script, sum(script)) // self-consistent mirror
	src := &Source{From: mirror, PinnedSHA256: strings.Repeat("ab", 32), CacheDir: t.TempDir()}
	if _, err := src.Installer(context.Background(), "v0.7.0"); err == nil {
		t.Fatal("a mirror whose install.sh differs from the pinned hash was accepted")
	}

	src.PinnedSHA256 = sum(script)
	got, err := src.Installer(context.Background(), "v0.7.0")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(got.Path, mirror) {
		t.Errorf("runs the mirror's file %s, want a staged copy", got.Path)
	}
	staged, err := os.ReadFile(got.Path)
	if err != nil || string(staged) != string(script) {
		t.Errorf("staged copy = %q, %v", staged, err)
	}
}

// Versions reach file paths (mirror, cache) and URLs, and one of them comes
// from a cluster's /health answer, so only plain release tags pass.
func TestSourceRejectsNonTagVersions(t *testing.T) {
	script := []byte(fullScript)
	mirror := newMirror(t, "v0.7.0", script, sum(script))
	for _, v := range []string{"../../etc", "v0.7.0/../../x", "v1", "0.7.0", "v0.7.0 --evil", "latest"} {
		src := &Source{From: mirror, PinnedSHA256: sum(script), CacheDir: t.TempDir()}
		if _, err := src.Installer(context.Background(), v); err == nil {
			t.Errorf("version %q was accepted", v)
		}
	}
	for _, v := range []string{"v0.7.0", ""} {
		src := &Source{From: mirror, PinnedVersion: "v0.7.0", PinnedSHA256: sum(script), CacheDir: t.TempDir()}
		if _, err := src.Installer(context.Background(), v); err != nil {
			t.Errorf("version %q: %v", v, err)
		}
	}
}

// "latest" resolves through the newest release's release.json and warns that
// it is untested.
func TestSourceLatest(t *testing.T) {
	script := []byte(fullScript)
	handler := http.NewServeMux()
	handler.HandleFunc("/latest/download/release.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"v0.7.9"}`))
	})
	handler.HandleFunc("/download/v0.7.9/install.sh", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(script) })
	handler.HandleFunc("/download/v0.7.9/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sum(script) + "  install.sh\n"))
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	src := &Source{BaseURL: srv.URL, PinnedVersion: "v0.7.0", PinnedSHA256: strings.Repeat("0", 64), CacheDir: t.TempDir(), Client: srv.Client()}
	inst, err := src.Installer(context.Background(), "latest")
	if err != nil {
		t.Fatal(err)
	}
	if inst.Version != "v0.7.9" || !strings.Contains(inst.Warning, "not the version this Lewbox release was tested with") {
		t.Errorf("installer = version %q warning %q", inst.Version, inst.Warning)
	}
}
