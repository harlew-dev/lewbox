package httpapi_test

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/harlew-dev/lewbox/internal/docs"
	"github.com/harlew-dev/lewbox/internal/httpapi"
)

func testDocs(t *testing.T, readme, page string) *docs.Site {
	t.Helper()
	site, err := docs.Load(fstest.MapFS{
		"README.md": {Data: []byte(readme)},
		"guide.md":  {Data: []byte(page)},
	})
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}
	return site
}

func testConsumerDocs(t *testing.T) *docs.Site {
	t.Helper()
	return testDocs(t,
		"---\ntitle: Home\n---\n\n## Get started\n\n| | |\n|---|---|\n| [Quickstart](guide.md) | Start here |\n",
		"---\ntitle: Quickstart\n---\n\nHello. See the [platform book](../platform/README.md).\n",
	)
}

func testPlatformDocs(t *testing.T) *docs.Site {
	t.Helper()
	return testDocs(t,
		"---\ntitle: Platform\n---\n\n## Install\n\n| | |\n|---|---|\n| [Install](guide.md) | Set up a node |\n",
		"---\ntitle: Install\n---\n\nInstall. See the [customer book](../consumer/README.md).\n",
	)
}

func docsOptions(t *testing.T) []httpapi.RouterOption {
	t.Helper()
	return []httpapi.RouterOption{
		httpapi.WithDocs(testConsumerDocs(t)),
		httpapi.WithPlatform(testPlatformDocs(t)),
	}
}

func TestDocsRoutesArePublic(t *testing.T) {
	ts := newTestServerWithOptions(t, docsOptions(t))

	for _, test := range []struct {
		base     string
		group    string
		title    string
		contains string
	}{
		{"/v0/docs", "Get started", "Quickstart", "Hello."},
		{"/v0/platform", "Install", "Install", "Install."},
	} {
		res, body := ts.do("GET", test.base, "", nil)
		if res.StatusCode != 200 {
			t.Fatalf("GET %s = %d %s", test.base, res.StatusCode, body)
		}
		var nav docs.Nav
		if err := json.Unmarshal([]byte(body), &nav); err != nil {
			t.Fatalf("decode %s nav: %v", test.base, err)
		}
		if len(nav.Groups) != 1 || nav.Groups[0].Title != test.group || len(nav.Groups[0].Items) != 1 {
			t.Fatalf("%s nav = %+v", test.base, nav)
		}
		if nav.Groups[0].Items[0].Path != "guide" {
			t.Fatalf("%s nav item = %+v", test.base, nav.Groups[0].Items[0])
		}

		res, body = ts.do("GET", test.base+"/guide", "", nil)
		if res.StatusCode != 200 {
			t.Fatalf("GET %s/guide = %d %s", test.base, res.StatusCode, body)
		}
		var page docs.Page
		if err := json.Unmarshal([]byte(body), &page); err != nil {
			t.Fatalf("decode %s page: %v", test.base, err)
		}
		if page.Path != "guide" || page.Title != test.title || !strings.Contains(page.Markdown, test.contains) {
			t.Fatalf("%s page = %+v", test.base, page)
		}

		res, body = ts.do("GET", test.base+"/", "", nil)
		if res.StatusCode != 200 || !strings.Contains(body, `"path":""`) {
			t.Fatalf("GET %s/ = %d %s", test.base, res.StatusCode, body)
		}
	}
}

func TestDocsRouteNotFound(t *testing.T) {
	ts := newTestServerWithOptions(t, docsOptions(t))

	for _, base := range []string{"/v0/docs", "/v0/platform"} {
		res, body := ts.do("GET", base+"/nope", "", nil)
		if res.StatusCode != 404 {
			t.Fatalf("GET %s/nope = %d %s", base, res.StatusCode, body)
		}
		if !strings.Contains(body, `"code":"not_found"`) || !strings.Contains(body, `no docs page \"nope\"`) {
			t.Fatalf("%s body = %s", base, body)
		}
	}
}

func TestDocsRoutesWithoutSite(t *testing.T) {
	ts := newTestServer(t)

	for _, base := range []string{"/v0/docs", "/v0/platform"} {
		res, body := ts.do("GET", base, "", nil)
		if res.StatusCode != 404 || !strings.Contains(body, `"code":"not_found"`) {
			t.Fatalf("GET %s status/body = %d %s", base, res.StatusCode, body)
		}
		res, body = ts.do("GET", base+"/guide", "", nil)
		if res.StatusCode != 404 || !strings.Contains(body, `"code":"not_found"`) {
			t.Fatalf("GET %s/guide status/body = %d %s", base, res.StatusCode, body)
		}
	}
}
