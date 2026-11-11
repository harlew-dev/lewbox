package httpapi_test

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/harlew-dev/lewbox/internal/docs"
	"github.com/harlew-dev/lewbox/internal/httpapi"
)

func testDocs(t *testing.T) *docs.Site {
	t.Helper()
	site, err := docs.Load(fstest.MapFS{
		"README.md": {Data: []byte("---\ntitle: Home\n---\n\n## Get started\n\n| | |\n|---|---|\n| [Quickstart](guide.md) | Start here |\n")},
		"guide.md":  {Data: []byte("---\ntitle: Quickstart\n---\n\nHello.\n")},
	})
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}
	return site
}

func TestDocsRoutesArePublic(t *testing.T) {
	ts := newTestServerWithOptions(t, []httpapi.RouterOption{httpapi.WithDocs(testDocs(t))})

	res, body := ts.do("GET", "/v0/docs", "", nil)
	if res.StatusCode != 200 {
		t.Fatalf("GET /v0/docs = %d %s", res.StatusCode, body)
	}
	var nav docs.Nav
	if err := json.Unmarshal([]byte(body), &nav); err != nil {
		t.Fatalf("decode nav: %v", err)
	}
	if len(nav.Groups) != 1 || nav.Groups[0].Title != "Get started" || len(nav.Groups[0].Items) != 1 {
		t.Fatalf("nav = %+v", nav)
	}
	if nav.Groups[0].Items[0].Path != "guide" || nav.Groups[0].Items[0].Description != "Start here" {
		t.Fatalf("nav item = %+v", nav.Groups[0].Items[0])
	}

	res, body = ts.do("GET", "/v0/docs/guide", "", nil)
	if res.StatusCode != 200 {
		t.Fatalf("GET /v0/docs/guide = %d %s", res.StatusCode, body)
	}
	var page docs.Page
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if page.Path != "guide" || page.Title != "Quickstart" || !strings.Contains(page.Markdown, "Hello.") {
		t.Fatalf("page = %+v", page)
	}

	res, body = ts.do("GET", "/v0/docs/", "", nil)
	if res.StatusCode != 200 || !strings.Contains(body, `"path":""`) {
		t.Fatalf("GET /v0/docs/ = %d %s", res.StatusCode, body)
	}
}

func TestDocsRouteNotFound(t *testing.T) {
	ts := newTestServerWithOptions(t, []httpapi.RouterOption{httpapi.WithDocs(testDocs(t))})

	res, body := ts.do("GET", "/v0/docs/nope", "", nil)
	if res.StatusCode != 404 {
		t.Fatalf("status = %d %s", res.StatusCode, body)
	}
	if !strings.Contains(body, `"code":"not_found"`) || !strings.Contains(body, `no docs page \"nope\"`) {
		t.Fatalf("body = %s", body)
	}
}

func TestDocsRoutesWithoutSite(t *testing.T) {
	ts := newTestServer(t)

	res, body := ts.do("GET", "/v0/docs", "", nil)
	if res.StatusCode != 404 || !strings.Contains(body, `"code":"not_found"`) {
		t.Fatalf("status/body = %d %s", res.StatusCode, body)
	}
	res, body = ts.do("GET", "/v0/docs/guide", "", nil)
	if res.StatusCode != 404 || !strings.Contains(body, `"code":"not_found"`) {
		t.Fatalf("status/body = %d %s", res.StatusCode, body)
	}
}
