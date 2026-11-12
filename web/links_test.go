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

// TestConsoleDocsLinks fails when a console page links to a docs page that
// doesn't exist, such as an operator page under /docs instead of /platform.
func TestConsoleDocsLinks(t *testing.T) {
	books := map[string]string{"docs": "consumer", "platform": "platform"}
	found := 0
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
			found++
			page := strings.TrimPrefix(m[1], "/"+m[2]+"/")
			dir := filepath.Join("..", "docs", books[m[2]])
			if exists(filepath.Join(dir, page+".md")) || exists(filepath.Join(dir, page, "README.md")) {
				continue
			}
			t.Errorf("%s links to %s, but there is no %s.md in docs/%s", path, m[1], page, books[m[2]])
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
