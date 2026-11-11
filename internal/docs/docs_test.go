package docs_test

import (
	"strings"
	"testing"
	"testing/fstest"

	docsfs "github.com/harlew-dev/lewbox/docs"
	"github.com/harlew-dev/lewbox/internal/docs"
)

func load(t *testing.T, files fstest.MapFS) *docs.Site {
	t.Helper()
	site, err := docs.Load(files)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return site
}

func TestFrontMatter(t *testing.T) {
	site := load(t, fstest.MapFS{
		"README.md": {Data: []byte("---\ntitle: Home\ndescription: 'The index'\n---\n\nWelcome.\n")},
		"quoted.md": {Data: []byte("---\ntitle: \"Quoted title\"\ndescription: Plain\n---\n\nBody.\n")},
	})

	page, ok := site.Page("")
	if !ok {
		t.Fatal("home page missing")
	}
	if page.Title != "Home" || page.Description != "The index" {
		t.Fatalf("home title/description = %q/%q", page.Title, page.Description)
	}
	if strings.Contains(page.Markdown, "title:") || strings.Contains(page.Markdown, "---") {
		t.Fatalf("front matter not stripped: %q", page.Markdown)
	}
	if !strings.Contains(page.Markdown, "Welcome.") {
		t.Fatalf("body missing: %q", page.Markdown)
	}

	quoted, ok := site.Page("quoted")
	if !ok {
		t.Fatal("quoted page missing")
	}
	if quoted.Title != "Quoted title" {
		t.Fatalf("quoted title = %q", quoted.Title)
	}
}

func TestHeadingFallback(t *testing.T) {
	site := load(t, fstest.MapFS{
		"plain.md":    {Data: []byte("Intro paragraph.\n\n# Real title\n\nText after.\n")},
		"fenced.md":   {Data: []byte("```\n# Not a title\n```\n\n# Later title\n\nText.\n")},
		"untitled.md": {Data: []byte("No heading here.\n")},
	})

	plain, _ := site.Page("plain")
	if plain.Title != "Real title" {
		t.Fatalf("title = %q", plain.Title)
	}
	if strings.Contains(plain.Markdown, "# Real title") {
		t.Fatalf("H1 not removed: %q", plain.Markdown)
	}
	if !strings.Contains(plain.Markdown, "Intro paragraph.") || !strings.Contains(plain.Markdown, "Text after.") {
		t.Fatalf("body damaged: %q", plain.Markdown)
	}

	fenced, _ := site.Page("fenced")
	if fenced.Title != "Later title" {
		t.Fatalf("fenced title = %q", fenced.Title)
	}
	if !strings.Contains(fenced.Markdown, "# Not a title") {
		t.Fatalf("heading inside a fence was removed: %q", fenced.Markdown)
	}
	if strings.Contains(fenced.Markdown, "# Later title") {
		t.Fatalf("H1 outside a fence was not removed: %q", fenced.Markdown)
	}

	untitled, _ := site.Page("untitled")
	if untitled.Title != "untitled" {
		t.Fatalf("fallback title = %q", untitled.Title)
	}
}

func TestNavAndLinks(t *testing.T) {
	site := load(t, fstest.MapFS{
		"README.md": {Data: []byte("---\ntitle: Home\n---\n\n" +
			"## Get started\n\n" +
			"| | |\n|---|---|\n" +
			"| [Quickstart](getting-started/quickstart.md) | One machine |\n" +
			"| [Concepts](getting-started/concepts.md#top) | The ideas |\n" +
			"| [Missing](nope.md) | Dropped |\n" +
			"| [Repo](../AGENTS.md) | How the code is laid out |\n" +
			"| [Web](https://example.com) | Dropped |\n\n" +
			"## Empty\n\nNo table here.\n\n" +
			"## Reference\n\n" +
			"| | |\n|---|---|\n" +
			"| [CLI](reference/cli.md) | Every flag |\n")},
		"getting-started/quickstart.md": {Data: []byte("---\ntitle: Quickstart\n---\n\nStart.\n")},
		"getting-started/concepts.md":   {Data: []byte("# Concepts\n\nIdeas.\n")},
		"reference/cli.md":              {Data: []byte("---\ntitle: CLI\n---\n\nFlags.\n")},
	})

	nav := site.Nav()
	if len(nav.Groups) != 2 {
		t.Fatalf("groups = %+v", nav.Groups)
	}
	if nav.Groups[0].Title != "Get started" || nav.Groups[1].Title != "Reference" {
		t.Fatalf("group titles = %q, %q", nav.Groups[0].Title, nav.Groups[1].Title)
	}

	items := nav.Groups[0].Items
	if len(items) != 3 {
		t.Fatalf("first group items = %+v", items)
	}
	if items[0].Title != "Quickstart" || items[0].Path != "getting-started/quickstart" || items[0].Description != "One machine" {
		t.Fatalf("first item = %+v", items[0])
	}
	if items[1].Path != "getting-started/concepts" || items[1].Description != "The ideas" {
		t.Fatalf("fragment item = %+v", items[1])
	}
	if items[2].Title != "Repo" || items[2].Path != "" || items[2].External != "AGENTS.md" {
		t.Fatalf("external item = %+v", items[2])
	}

	first, _ := site.Page("getting-started/quickstart")
	if first.Prev != nil {
		t.Fatalf("first page has prev: %+v", first.Prev)
	}
	if first.Next == nil || first.Next.Path != "getting-started/concepts" {
		t.Fatalf("first page next = %+v", first.Next)
	}
	second, _ := site.Page("getting-started/concepts")
	if second.Prev == nil || second.Prev.Title != "Quickstart" {
		t.Fatalf("second page prev = %+v", second.Prev)
	}
	if second.Next == nil || second.Next.Path != "reference/cli" {
		t.Fatalf("second page next = %+v", second.Next)
	}
	last, _ := site.Page("reference/cli")
	if last.Next != nil {
		t.Fatalf("last page has next: %+v", last.Next)
	}

	home, _ := site.Page("")
	if home.Next == nil || home.Next.Path != "getting-started/quickstart" {
		t.Fatalf("home next = %+v", home.Next)
	}
}

func TestPagePathNormalisation(t *testing.T) {
	site := load(t, fstest.MapFS{
		"README.md":            {Data: []byte("Home.\n")},
		"getting-started/a.md": {Data: []byte("A.\n")},
		"design/archive/b.md":  {Data: []byte("B.\n")},
		"folder/README.md":     {Data: []byte("---\ntitle: Folder\n---\n\nFolder.\n")},
	})

	for _, p := range []string{"getting-started/a", "/getting-started/a", "getting-started/a.md", "getting-started/a/"} {
		if _, ok := site.Page(p); !ok {
			t.Errorf("Page(%q) not found", p)
		}
	}
	if _, ok := site.Page("design/archive/b"); !ok {
		t.Error("nested page not found")
	}
	if page, ok := site.Page("folder"); !ok || page.Title != "Folder" {
		t.Errorf("folder README not mapped: %+v %v", page, ok)
	}
	for _, p := range []string{"../AGENTS", "getting-started/../../secret", `getting-started\a`, "getting-started\x00a", "missing"} {
		if _, ok := site.Page(p); ok {
			t.Errorf("Page(%q) unexpectedly found", p)
		}
	}
}

func TestEmbeddedDocs(t *testing.T) {
	site, err := docs.Load(docsfs.FS)
	if err != nil {
		t.Fatalf("Load embedded docs: %v", err)
	}
	nav := site.Nav()
	if len(nav.Groups) == 0 {
		t.Fatal("embedded docs have an empty nav")
	}
	groups := make(map[string]bool)
	resolved := 0
	for _, group := range nav.Groups {
		groups[group.Title] = true
		for _, item := range group.Items {
			if item.Path == "" {
				continue
			}
			if _, ok := site.Page(item.Path); !ok {
				t.Errorf("nav item %q points at missing page %q", item.Title, item.Path)
			}
			resolved++
		}
	}
	if resolved == 0 {
		t.Fatal("no nav item resolves to a page")
	}
	for _, want := range []string{"Get started", "Reference"} {
		if !groups[want] {
			t.Errorf("nav is missing group %q", want)
		}
	}
	if _, ok := site.Page("reference/cli"); !ok {
		t.Error("reference/cli is missing")
	}
	if _, ok := site.Page("design/archive/cli-first-draft"); !ok {
		t.Error("design/archive/cli-first-draft is missing")
	}
}
