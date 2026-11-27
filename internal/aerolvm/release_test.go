package aerolvm_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
)

func sum(data string) string {
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

// releaseServer serves a release's checksums.txt and release.json under
// /<version>/.
func releaseServer(t *testing.T, version string, files map[string]string, checksumsOverride string) *httptest.Server {
	t.Helper()
	var lines []string
	for name, data := range files {
		lines = append(lines, sum(data)+"  "+name)
	}
	checksums := strings.Join(lines, "\n") + "\n"
	if checksumsOverride != "" {
		checksums = checksumsOverride
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /"+version+"/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(checksums))
	})
	mux.HandleFunc("GET /"+version+"/release.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(files["release.json"]))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func goodRelease(version string) map[string]string {
	return map[string]string{
		"sandboxd_linux_amd64": "sandboxd",
		"toolboxd_linux_amd64": "toolboxd",
		"caddy_linux_amd64":    "caddy",
		"release.json":         `{"version":"` + version + `","upgrade_from_min":"v0.6.11"}`,
	}
}

func TestReleaseFetch(t *testing.T) {
	rel := releaseServer(t, "v0.7.1", goodRelease("v0.7.1"), "")
	got, err := aerolvm.NewReleaseSource(rel.URL).Fetch(context.Background(), "v0.7.1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "v0.7.1" || got.Metadata.UpgradeFromMin != "v0.6.11" {
		t.Fatalf("release = %+v", got)
	}
	if len(got.Files) != 4 || got.Files["sandboxd_linux_amd64"] != sum("sandboxd") || got.Files["release.json"] == "" {
		t.Fatalf("files = %+v", got.Files)
	}
}

func TestReleaseFetchPinnedChecksums(t *testing.T) {
	rel := releaseServer(t, aerolvm.PinnedVersion, goodRelease(aerolvm.PinnedVersion), "")
	_, err := aerolvm.NewReleaseSource(rel.URL).Fetch(context.Background(), aerolvm.PinnedVersion)
	if err == nil || !strings.Contains(err.Error(), "pins") {
		t.Fatalf("err = %v", err)
	}
}

func TestReleaseFetchRejectsBadChecksums(t *testing.T) {
	rel := releaseServer(t, "v0.7.1", goodRelease("v0.7.1"), "not a checksums line\n")
	_, err := aerolvm.NewReleaseSource(rel.URL).Fetch(context.Background(), "v0.7.1")
	if err == nil || !strings.Contains(err.Error(), "not \"<sha256>  <file>\"") {
		t.Fatalf("err = %v", err)
	}
}

func TestReleaseFetchNeedsToolboxd(t *testing.T) {
	files := goodRelease("v0.7.1")
	delete(files, "toolboxd_linux_amd64")
	rel := releaseServer(t, "v0.7.1", files, "")
	_, err := aerolvm.NewReleaseSource(rel.URL).Fetch(context.Background(), "v0.7.1")
	if err == nil || !strings.Contains(err.Error(), "no toolboxd_linux_amd64") {
		t.Fatalf("err = %v", err)
	}
}

func TestReleaseFetchRejectsReplacedMetadata(t *testing.T) {
	files := goodRelease("v0.7.1")
	// Serve a different release.json than the checksums line describes.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v0.7.1/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sum(files["sandboxd_linux_amd64"]) + "  sandboxd_linux_amd64\n" +
			sum(files["toolboxd_linux_amd64"]) + "  toolboxd_linux_amd64\n" +
			sum(files["release.json"]) + "  release.json\n"))
	})
	mux.HandleFunc("GET /v0.7.1/release.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"v0.7.1"}`))
	})
	bad := httptest.NewServer(mux)
	t.Cleanup(bad.Close)

	_, err := aerolvm.NewReleaseSource(bad.URL).Fetch(context.Background(), "v0.7.1")
	if err == nil || !strings.Contains(err.Error(), "refusing it") {
		t.Fatalf("err = %v", err)
	}
}

func TestReleaseFetchRejectsLatestAndBadTags(t *testing.T) {
	src := aerolvm.NewReleaseSource("http://127.0.0.1:1")
	if _, err := src.Fetch(context.Background(), "latest"); err == nil || !strings.Contains(err.Error(), "latest is not supported") {
		t.Fatalf("latest err = %v", err)
	}
	if _, err := src.Fetch(context.Background(), "../v0.7.1"); err == nil || !strings.Contains(err.Error(), "not an AerolVM release tag") {
		t.Fatalf("tag err = %v", err)
	}
}

func TestCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"v0.7.0", "v0.7.1", -1},
		{"v0.7.1", "v0.7.0", 1},
		{"v0.7.0", "v0.7.0", 0},
		{"v0.7.0-rc.1", "v0.7.0", -1},
		{"v0.10.0", "v0.9.9", 1},
	} {
		got, err := aerolvm.CompareVersions(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Errorf("CompareVersions(%s, %s) = %d, %v; want %d", tc.a, tc.b, got, err, tc.want)
		}
	}
	if _, err := aerolvm.CompareVersions("latest", "v0.7.0"); err == nil {
		t.Error("latest parsed")
	}
}

// An unpinned version's checksums.txt is trusted only over TLS, so a plain
// HTTP mirror on another machine is refused. Loopback (the tests above) is
// allowed.
func TestReleaseFetchRefusesPlainHTTPMirror(t *testing.T) {
	_, err := aerolvm.NewReleaseSource("http://mirror.example.com/releases").Fetch(context.Background(), "v0.7.1")
	if err == nil || !strings.Contains(err.Error(), "is not HTTPS") {
		t.Fatalf("err = %v, want a refusal of the plain HTTP mirror", err)
	}
}
