package webui_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/harlew-dev/lewbox/internal/webui"
)

func testDist() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><div id=root></div>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
		"favicon.svg":   {Data: []byte("<svg></svg>")},
		"secret.txt":    {Data: []byte("inside dist")},
	}
}

func newHandler(dist fstest.MapFS) http.Handler {
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Test-API", "yes")
		_, _ = w.Write([]byte(`{"api":true}`))
	})
	return webui.Handler(api, dist)
}

func request(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestAssetCaching(t *testing.T) {
	h := newHandler(testDist())

	rec := request(t, h, http.MethodGet, "/assets/app.js")
	if rec.Code != http.StatusOK || rec.Body.String() != "console.log(1)" {
		t.Fatalf("asset = %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("asset Cache-Control = %q", got)
	}

	rec = request(t, h, http.MethodGet, "/favicon.svg")
	if rec.Code != http.StatusOK || rec.Body.String() != "<svg></svg>" {
		t.Fatalf("favicon = %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("favicon Cache-Control = %q", got)
	}
}

func TestSPAFallback(t *testing.T) {
	h := newHandler(testDist())

	for _, target := range []string{"/", "/docs/reference/cli", "/clusters/eu-west"} {
		rec := request(t, h, http.MethodGet, target)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d", target, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `<div id=root>`) {
			t.Fatalf("%s did not serve index.html: %q", target, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
			t.Errorf("%s Content-Type = %q", target, got)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s Cache-Control = %q", target, got)
		}
		if rec.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("%s has no CSP", target)
		}
	}
}

func TestHeadFallbackHasNoBody(t *testing.T) {
	rec := request(t, newHandler(testDist()), http.MethodHead, "/clusters/x")
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("HEAD = %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("HEAD Content-Type = %q", got)
	}
}

func TestMissingIndex(t *testing.T) {
	rec := request(t, newHandler(fstest.MapFS{}), http.MethodGet, "/clusters/x")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "isn't built") || !strings.Contains(body, "npm --prefix web ci") {
		t.Fatalf("not-built page = %q", body)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("not-built page has no CSP")
	}
}

func TestMethodNotAllowed(t *testing.T) {
	rec := request(t, newHandler(testDist()), http.MethodPost, "/clusters")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("Allow = %q", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "invalid_request" || body.Error.Message != "method not allowed" {
		t.Fatalf("error = %+v", body.Error)
	}
}

func TestDelegatesToAPI(t *testing.T) {
	h := newHandler(testDist())

	for _, target := range []string{"/v0", "/v0/clusters", "/v0/clusters/eu-west"} {
		rec := request(t, h, http.MethodGet, target)
		if rec.Header().Get("X-Test-API") != "yes" {
			t.Errorf("%s was not delegated to the API", target)
		}
		if rec.Body.String() != `{"api":true}` {
			t.Errorf("%s body = %q", target, rec.Body.String())
		}
	}

	// A non-GET /v0 request still goes to the API, which owns its methods.
	rec := request(t, h, http.MethodPost, "/v0/clusters")
	if rec.Header().Get("X-Test-API") != "yes" {
		t.Error("POST /v0/clusters was not delegated")
	}
}

func TestSecurityHeadersEverywhere(t *testing.T) {
	h := newHandler(testDist())

	for _, target := range []string{"/", "/assets/app.js", "/v0/health", "/v0/docs", "/no-such-page"} {
		rec := request(t, h, http.MethodGet, target)
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s X-Content-Type-Options = %q", target, got)
		}
		if got := rec.Header().Get("Referrer-Policy"); got != "same-origin" {
			t.Errorf("%s Referrer-Policy = %q", target, got)
		}
		if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("%s X-Frame-Options = %q", target, got)
		}
	}
}

func TestTraversalStaysInsideDist(t *testing.T) {
	h := newHandler(testDist())
	index := `<!doctype html><div id=root></div>`

	// go.mod is not in dist, so this must fall back to index.html rather than
	// read the repository's go.mod.
	rec := request(t, h, http.MethodGet, "/../go.mod")
	if rec.Code != http.StatusOK || rec.Body.String() != index {
		t.Fatalf("/../go.mod = %d %q", rec.Code, rec.Body.String())
	}

	// Cleaning clamps the path to dist's root, so this serves dist/secret.txt
	// and never a file above dist.
	rec = request(t, h, http.MethodGet, "/assets/../../secret.txt")
	if rec.Code != http.StatusOK || rec.Body.String() != "inside dist" {
		t.Fatalf("/assets/../../secret.txt = %d %q", rec.Code, rec.Body.String())
	}

	rec = request(t, h, http.MethodGet, "/../../etc/passwd")
	if rec.Code != http.StatusOK || rec.Body.String() != index {
		t.Fatalf("/../../etc/passwd = %d %q", rec.Code, rec.Body.String())
	}
}

func TestMissingAssetIs404(t *testing.T) {
	rec := request(t, newHandler(testDist()), http.MethodGet, "/assets/index-old.js")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON", ct)
	}
}
