// Package docs parses the Lewbox documentation markdown into the small site
// model the web console renders: a nav tree and one page at a time.
package docs

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// Nav is the docs sidebar.
type Nav struct {
	Groups []NavGroup `json:"groups"`
}

// NavGroup is one "## Heading" section of README.md.
type NavGroup struct {
	Title string    `json:"title"`
	Items []NavItem `json:"items"`
}

// NavItem is one row of a README.md table. Items that live outside the docs
// folder have an empty Path and an External path relative to the repository
// root.
type NavItem struct {
	Title       string `json:"title"`
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
	External    string `json:"external,omitempty"`
}

// PageLink is a prev or next link.
type PageLink struct {
	Title string `json:"title"`
	Path  string `json:"path"`
}

// Page is one documentation page.
type Page struct {
	Path        string    `json:"path"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Markdown    string    `json:"markdown"`
	Prev        *PageLink `json:"prev,omitempty"`
	Next        *PageLink `json:"next,omitempty"`
}

// Site is a parsed documentation tree. It is read-only after Load.
type Site struct {
	pages map[string]Page
	nav   Nav
}

// Load parses every .md file in fsys, which must be rooted at the docs folder.
func Load(fsys fs.FS) (*Site, error) {
	s := &Site{pages: make(map[string]Page)}
	var readme string
	err := fs.WalkDir(fsys, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(name, ".md") {
			return nil
		}
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		title, description, markdown := parse(string(raw))
		if name == "README.md" {
			readme = markdown
		}
		if title == "" {
			title = strings.TrimSuffix(path.Base(name), ".md")
		}
		p := pagePath(name)
		s.pages[p] = Page{
			Path:        p,
			Title:       title,
			Description: description,
			Markdown:    markdown,
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read docs: %w", err)
	}
	s.nav = parseNav(readme, s.pages)
	linkPages(s.pages, s.nav)
	return s, nil
}

// Nav returns the navigation tree.
func (s *Site) Nav() Nav {
	return s.nav
}

// Page returns one page by its slash path, normalising what a caller may
// reasonably send. It never reads a file: the lookup is against the set built
// by Load.
func (s *Site) Page(p string) (Page, bool) {
	if strings.Contains(p, "..") || strings.ContainsAny(p, "\\\x00") {
		return Page{}, false
	}
	p = strings.Trim(p, "/")
	p = strings.TrimSuffix(p, ".md")
	page, ok := s.pages[p]
	return page, ok
}

// pagePath turns a file name from the docs tree into a page path. README.md is
// the home page; a folder's README.md becomes the folder path.
func pagePath(name string) string {
	name = strings.TrimSuffix(name, ".md")
	if name == "README" {
		return ""
	}
	if folder, ok := strings.CutSuffix(name, "/README"); ok {
		return folder
	}
	return name
}

// parse splits an optional front matter block off and, when the front matter
// carries no title, promotes the first H1 to the title and removes that line.
func parse(src string) (title, description, markdown string) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	body := src
	if fields, rest, ok := frontMatter(src); ok {
		title = fields["title"]
		description = fields["description"]
		body = rest
		if title != "" {
			return title, description, body
		}
	}

	lines := strings.Split(body, "\n")
	inFence, fence := false, ""
	for i, line := range lines {
		if marker, ok := fenceMarker(strings.TrimSpace(line)); ok {
			switch {
			case !inFence:
				inFence, fence = true, marker
			case marker == fence:
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}
		heading, ok := heading1(line)
		if !ok {
			continue
		}
		title = heading
		lines = append(lines[:i:i], lines[i+1:]...)
		body = strings.Join(lines, "\n")
		break
	}
	return title, description, body
}

// frontMatter parses a leading "---" block of "key: value" lines. The block
// must sit at the very top of the file, and an unclosed block is no block.
func frontMatter(src string) (map[string]string, string, bool) {
	lines := strings.Split(src, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, src, false
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "---" {
			continue
		}
		fields := make(map[string]string)
		for _, line := range lines[1:i] {
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			fields[strings.TrimSpace(key)] = unquote(strings.TrimSpace(value))
		}
		rest := strings.Join(lines[i+1:], "\n")
		rest = strings.TrimPrefix(rest, "\n")
		return fields, rest, true
	}
	return nil, src, false
}

// unquote strips one pair of matching surrounding quotes.
func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// fenceMarker reports the code fence a line opens or closes, if any.
func fenceMarker(trimmed string) (string, bool) {
	for _, marker := range []string{"```", "~~~"} {
		if strings.HasPrefix(trimmed, marker) {
			return marker, true
		}
	}
	return "", false
}

// heading1 reports the text of an ATX H1 line.
func heading1(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 {
		return "", false
	}
	if trimmed != "#" && !strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "#\t") {
		return "", false
	}
	text := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
	text = strings.TrimSpace(strings.TrimRight(text, "#"))
	if text == "" {
		return "", false
	}
	return text, true
}

// parseNav reads the "## Heading" sections and their table rows from the docs
// README. Only rows whose first cell holds a link become items.
func parseNav(readme string, pages map[string]Page) Nav {
	nav := Nav{Groups: []NavGroup{}}
	if readme == "" {
		return nav
	}
	seen := make(map[string]bool)
	current := -1
	inFence, fence := false, ""
	for _, line := range strings.Split(readme, "\n") {
		trimmed := strings.TrimSpace(line)
		if marker, ok := fenceMarker(trimmed); ok {
			switch {
			case !inFence:
				inFence, fence = true, marker
			case marker == fence:
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}
		if title, ok := heading2(trimmed); ok {
			nav.Groups = append(nav.Groups, NavGroup{Title: title, Items: []NavItem{}})
			current = len(nav.Groups) - 1
			continue
		}
		if current < 0 || !strings.HasPrefix(trimmed, "|") {
			continue
		}
		item, ok := navItem(trimmed, pages)
		if !ok {
			continue
		}
		key := item.Path
		if key == "" {
			key = "external:" + item.External
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		nav.Groups[current].Items = append(nav.Groups[current].Items, item)
	}

	groups := nav.Groups[:0]
	for _, group := range nav.Groups {
		if len(group.Items) > 0 {
			groups = append(groups, group)
		}
	}
	nav.Groups = groups
	return nav
}

// heading2 reports the text of an ATX H2 line.
func heading2(trimmed string) (string, bool) {
	if !strings.HasPrefix(trimmed, "## ") && !strings.HasPrefix(trimmed, "##\t") {
		return "", false
	}
	text := strings.TrimSpace(strings.TrimPrefix(trimmed, "##"))
	text = strings.TrimSpace(strings.TrimRight(text, "#"))
	if text == "" {
		return "", false
	}
	return text, true
}

// navItem turns one README table row into a nav item.
func navItem(row string, pages map[string]Page) (NavItem, bool) {
	cells := strings.Split(row, "|")
	if len(cells) < 3 {
		return NavItem{}, false
	}
	title, target, ok := firstLink(cells[1])
	if !ok {
		return NavItem{}, false
	}
	title = strings.TrimSpace(title)
	target = strings.TrimSpace(target)
	if fragment := strings.IndexByte(target, '#'); fragment >= 0 {
		target = target[:fragment]
	}
	if title == "" || target == "" || hasScheme(target) {
		return NavItem{}, false
	}
	description := strings.TrimSpace(cells[2])

	rel := path.Clean(path.Join(".", target))
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return NavItem{
			Title:       title,
			Description: description,
			External:    strings.TrimPrefix(rel, "../"),
		}, true
	}
	rel = strings.TrimSuffix(strings.TrimSuffix(rel, "/"), ".md")
	if rel == "." {
		rel = ""
	}
	page, ok := pages[rel]
	if !ok {
		return NavItem{}, false
	}
	return NavItem{Title: title, Path: page.Path, Description: description}, true
}

// firstLink returns the text and target of the first markdown link in s.
func firstLink(s string) (text, target string, ok bool) {
	open := strings.IndexByte(s, '[')
	if open < 0 {
		return "", "", false
	}
	closeBracket := strings.IndexByte(s[open:], ']')
	if closeBracket < 0 {
		return "", "", false
	}
	closeBracket += open
	if closeBracket+1 >= len(s) || s[closeBracket+1] != '(' {
		return "", "", false
	}
	closeParen := strings.IndexByte(s[closeBracket+2:], ')')
	if closeParen < 0 {
		return "", "", false
	}
	closeParen += closeBracket + 2
	return s[open+1 : closeBracket], s[closeBracket+2 : closeParen], true
}

// hasScheme reports whether a link target is a URL rather than a file path.
func hasScheme(target string) bool {
	return strings.Contains(target, "://") || strings.HasPrefix(target, "//") ||
		strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "tel:")
}

// linkPages fills prev and next from the order of the in-docs nav items. The
// home page links to the first item.
func linkPages(pages map[string]Page, nav Nav) {
	var order []string
	seen := make(map[string]bool)
	for _, group := range nav.Groups {
		for _, item := range group.Items {
			if item.Path == "" || seen[item.Path] {
				continue
			}
			if _, ok := pages[item.Path]; !ok {
				continue
			}
			seen[item.Path] = true
			order = append(order, item.Path)
		}
	}
	for i, p := range order {
		page := pages[p]
		if i > 0 {
			page.Prev = linkTo(pages, order[i-1])
		}
		if i+1 < len(order) {
			page.Next = linkTo(pages, order[i+1])
		}
		pages[p] = page
	}
	if len(order) > 0 {
		home := pages[""]
		home.Next = linkTo(pages, order[0])
		pages[""] = home
	}
}

func linkTo(pages map[string]Page, p string) *PageLink {
	page := pages[p]
	return &PageLink{Title: page.Title, Path: page.Path}
}
