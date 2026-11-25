package web

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// docsLink is a quoted link from the console into one of the two books:
// /docs/... is docs/consumer and /platform/... is docs/platform.
var docsLink = regexp.MustCompile(`["'](/(docs|platform)/[a-z0-9/-]+?)/?(#[a-z0-9-]*)?["']`)

// docsBuilder is the same link built by lib/routes.ts: routes.docs("path")
// or routes.platformDocs("path").
var docsBuilder = regexp.MustCompile(`routes\.(docs|platformDocs)\(\s*["']([a-z0-9/-]+)["']\s*\)`)

// TestConsoleDocsLinks fails when a console page links to a docs page that
// doesn't exist, such as an operator page under /docs instead of /platform.
func TestConsoleDocsLinks(t *testing.T) {
	books := map[string]string{"docs": "consumer", "platform": "platform", "platformDocs": "platform"}
	found := 0
	check := func(path, page, book string) {
		found++
		if exists(filepath.Join("..", "docs", book, page+".md")) || exists(filepath.Join("..", "docs", book, page, "README.md")) {
			return
		}
		t.Errorf("%s links to %s, but there is no %s.md in docs/%s", path, page, page, book)
	}
	err := filepath.WalkDir("src", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".tsx") && !strings.HasSuffix(path, ".ts") || strings.Contains(path, ".test.") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range docsLink.FindAllStringSubmatch(string(src), -1) {
			page := strings.TrimPrefix(m[1], "/"+m[2]+"/")
			check(path, page, books[m[2]])
		}
		for _, m := range docsBuilder.FindAllStringSubmatch(string(src), -1) {
			check(path, m[2], books[m[1]])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == 0 {
		t.Fatal("found no docs links in web/src; has the pattern stopped matching?")
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
