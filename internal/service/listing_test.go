package service

import (
	"errors"
	"strings"
	"testing"
)

func TestParsePageAndPerPage(t *testing.T) {
	cases := []struct {
		name    string
		page    string
		perPage string
		wantP   int
		wantPP  int
		wantErr string
	}{
		{"defaults", "", "", 1, 20, ""},
		{"bounds", "10000", "100", 10000, 100, ""},
		{"page zero", "0", "", 0, 0, "page"},
		{"page too large", "10001", "", 0, 0, "page"},
		{"page leading zero", "02", "", 0, 0, "page"},
		{"page sign", "+2", "", 0, 0, "page"},
		{"page space", " 2", "", 0, 0, "page"},
		{"page decimal", "2.0", "", 0, 0, "page"},
		{"per page zero", "", "0", 0, 0, "per_page"},
		{"per page too large", "", "101", 0, 0, "per_page"},
		{"per page leading zero", "", "020", 0, 0, "per_page"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, err := parsePage(tc.page)
			if tc.wantErr == "page" {
				wantField(t, err)
				return
			}
			if err != nil {
				t.Fatalf("page: %v", err)
			}
			perPage, err := parsePerPage(tc.perPage)
			if tc.wantErr == "per_page" {
				wantField(t, err)
				return
			}
			if err != nil {
				t.Fatalf("per_page: %v", err)
			}
			if page != tc.wantP || perPage != tc.wantPP {
				t.Errorf("page, per_page = %d, %d; want %d, %d", page, perPage, tc.wantP, tc.wantPP)
			}
		})
	}
}

func TestParseSearchStripsControlCharactersAndCapsLength(t *testing.T) {
	got, err := parseSearch("  ac\x00me\u202e  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "acme" {
		t.Errorf("search = %q", got)
	}
	if _, err := parseSearch(strings.Repeat("a", maxSearchRunes+1)); err == nil {
		t.Error("a search longer than the cap was accepted")
	}
}

func TestLikePatternEscapesWildcards(t *testing.T) {
	cases := map[string]string{
		"":       "",
		"acme":   "%acme%",
		"50%":    `%50\%%`,
		"a_b":    `%a\_b%`,
		`a\b`:    `%a\\b%`,
		"ACME%":  `%ACME\%%`,
		"100%_x": `%100\%\_x%`,
	}
	for in, want := range cases {
		if got := likePattern(in); got != want {
			t.Errorf("likePattern(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseListTrimsDeduplicatesAndValidates(t *testing.T) {
	got, err := parseList("status", " started , stopped,started ", []string{"started", "stopped"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "started" || got[1] != "stopped" {
		t.Errorf("list = %v", got)
	}
	if got, err := parseList("status", "  ", nil); err != nil || got != nil {
		t.Errorf("blank list = %v, %v", got, err)
	}
	if _, err := parseList("status", "a,,b", nil); err == nil {
		t.Error("an empty item was accepted")
	}
	if _, err := parseList("status", "started,nope", []string{"started"}); err == nil {
		t.Error("an unknown value was accepted")
	} else {
		wantField(t, err)
	}
	many := make([]string, maxFilterValues+1)
	for i := range many {
		many[i] = "v" + strings.Repeat("x", i+1)
	}
	if _, err := parseList("status", strings.Join(many, ","), nil); err == nil {
		t.Error("more than the value cap was accepted")
	}
}

func TestParseListQuerySortIsAllowListed(t *testing.T) {
	q, err := parseListQuery(ListQuery{Sort: "-created"}, userSorts, "email")
	if err != nil {
		t.Fatal(err)
	}
	if q.sortKey != "-created" || q.sort != userSorts["-created"] {
		t.Errorf("sort = %q %q", q.sortKey, q.sort)
	}
	if _, err := parseListQuery(ListQuery{Sort: "nope"}, userSorts, "email"); err == nil {
		t.Error("an unknown sort key was accepted")
	} else {
		wantField(t, err)
	}
}

func TestPagesTreatAnEmptyListAsPageOneOfOne(t *testing.T) {
	p := newPages(1, 20, 0)
	if p.Total != 1 || p.Items != 0 {
		t.Errorf("empty pages = %+v", p)
	}
	p = newPages(2, 20, 131)
	if p.Total != 7 || p.Current != 2 || p.Items != 131 {
		t.Errorf("pages = %+v", p)
	}
}

func TestPageSlicePastTheEndIsEmptyNotAnError(t *testing.T) {
	items := make([]int, 5)
	got, pages := pageSlice(listQuery{page: 9, perPage: 20}, items)
	if len(got) != 0 || pages.Total != 1 || pages.Current != 9 {
		t.Errorf("slice = %v pages = %+v", got, pages)
	}
}

// wantField asserts err is a 400 that names a field in details.
func wantField(t *testing.T, err error) {
	t.Helper()
	var se *Error
	if !errors.As(err, &se) {
		t.Fatalf("error = %v, want *Error", err)
	}
	if se.Code != CodeBadRequest {
		t.Fatalf("code = %s, want %s (%v)", se.Code, CodeBadRequest, err)
	}
	if _, ok := se.Details.(FieldDetails); !ok {
		t.Fatalf("details = %#v, want FieldDetails", se.Details)
	}
}
