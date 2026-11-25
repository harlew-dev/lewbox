package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/harlew-dev/lewbox/internal/db"
)

// Bounds on every paged list. They bound the work one request can ask for.
const (
	defaultPerPage  = 20
	maxPerPage      = 100
	maxPage         = 10000
	maxFilterValues = 50
	maxSearchRunes  = 100
)

// Pages is the envelope every paged list carries beside its data. Total is at
// least 1, so an empty list is "page 1 of 1".
type Pages struct {
	Current int `json:"current"`
	Total   int `json:"total"`
	PerPage int `json:"per_page"`
	Items   int `json:"items"`
}

// ListQuery is a list's paging, search and sort as the caller sent them. Empty
// strings mean the default. It is parsed by the service, so the CLI and the
// API validate a value the same way.
type ListQuery struct {
	Page    string `json:"page,omitempty"`
	PerPage string `json:"per_page,omitempty"`
	Q       string `json:"q,omitempty"`
	Sort    string `json:"sort,omitempty"`
}

// listQuery is a parsed ListQuery.
type listQuery struct {
	page    int
	perPage int
	q       string
	pattern string // the LIKE pattern, empty when there is no search
	sortKey string // the validated public sort key
	sort    string // the ORDER BY clause from the list's fixed sort map
}

// parseListQuery parses paging, search and sort. sorts maps a public sort key
// to SQL written here; defaultSort is used when the caller sends none.
func parseListQuery(q ListQuery, sorts map[string]string, defaultSort string) (listQuery, error) {
	page, err := parsePage(q.Page)
	if err != nil {
		return listQuery{}, err
	}
	perPage, err := parsePerPage(q.PerPage)
	if err != nil {
		return listQuery{}, err
	}
	search, err := parseSearch(q.Q)
	if err != nil {
		return listQuery{}, err
	}
	key := strings.TrimSpace(q.Sort)
	if key == "" {
		key = defaultSort
	}
	order, ok := sorts[key]
	if !ok {
		keys := make([]string, 0, len(sorts))
		for k := range sorts {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		return listQuery{}, badRequestField("sort", "sort must be one of: %s", strings.Join(keys, ", "))
	}
	return listQuery{
		page:    page,
		perPage: perPage,
		q:       search,
		pattern: likePattern(search),
		sortKey: key,
		sort:    order,
	}, nil
}

// parsePage reads page: plain digits, 1 to maxPage, default 1.
func parsePage(raw string) (int, error) {
	if raw == "" {
		return 1, nil
	}
	n, ok := plainDigits(raw)
	if !ok || n < 1 || n > maxPage {
		return 0, badRequestField("page", "page must be a whole number from 1 to %d", maxPage)
	}
	return n, nil
}

// parsePerPage reads per_page: plain digits, 1 to maxPerPage, default 20.
func parsePerPage(raw string) (int, error) {
	if raw == "" {
		return defaultPerPage, nil
	}
	n, ok := plainDigits(raw)
	if !ok || n < 1 || n > maxPerPage {
		return 0, badRequestField("per_page", "per_page must be a whole number from 1 to %d", maxPerPage)
	}
	return n, nil
}

// plainDigits parses unsigned decimal digits without a leading zero, except
// "0" itself. A sign, space, exponent or leading zero is refused.
func plainDigits(raw string) (int, bool) {
	if raw == "" {
		return 0, false
	}
	n := 0
	for i, c := range raw {
		if c < '0' || c > '9' {
			return 0, false
		}
		if i == 0 && c == '0' && len(raw) > 1 {
			return 0, false
		}
		n = n*10 + int(c-'0')
		if n > maxPage*10 {
			return 0, false
		}
	}
	return n, true
}

// parseSearch trims q, removes control and invisible formatting characters,
// and caps it at maxSearchRunes. An empty q is no search.
func parseSearch(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	var b strings.Builder
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		b.WriteRune(r)
	}
	q := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(q) > maxSearchRunes {
		return "", badRequestField("q", "q must be at most %d characters", maxSearchRunes)
	}
	return q, nil
}

// likePattern escapes \, % and _ so they match literally and wraps the term
// in %...% for a contains match. Bind it to
// "lower(column) LIKE lower(?) ESCAPE '\'"; never put it in the SQL text.
func likePattern(q string) string {
	if q == "" {
		return ""
	}
	var b strings.Builder
	b.WriteByte('%')
	for _, r := range q {
		switch r {
		case '\\', '%', '_':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('%')
	return b.String()
}

// parseList reads one comma-separated multi-value filter. Values are trimmed,
// duplicates are ignored, empty items and values outside allowed are a 400,
// and at most maxFilterValues values are accepted. allowed nil accepts any
// non-empty value.
func parseList(param, raw string, allowed []string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	seen := make(map[string]bool, len(parts))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			return nil, badRequestField(param, "%s must not contain an empty value", param)
		}
		if seen[value] {
			continue
		}
		if len(out) >= maxFilterValues {
			return nil, badRequestField(param, "%s accepts at most %d values", param, maxFilterValues)
		}
		if allowed != nil && !slices.Contains(allowed, value) {
			return nil, badRequestField(param, "%s must be one of: %s", param, strings.Join(allowed, ", "))
		}
		seen[value] = true
		out = append(out, value)
	}
	return out, nil
}

// newPages builds the envelope for a page.
func newPages(page, perPage, items int) Pages {
	total := (items + perPage - 1) / perPage
	if total < 1 {
		total = 1
	}
	return Pages{Current: page, Total: total, PerPage: perPage, Items: items}
}

// bounds returns the slice range for a page of items. A page past the end is
// an empty range, not an error.
func (q listQuery) bounds(items int) (start, end int) {
	start = (q.page - 1) * q.perPage
	if start > items {
		start = items
	}
	end = start + q.perPage
	if end > items {
		end = items
	}
	return start, end
}

// pageSlice returns one page of items and its envelope.
func pageSlice[T any](q listQuery, items []T) ([]T, Pages) {
	start, end := q.bounds(len(items))
	return items[start:end], newPages(q.page, q.perPage, len(items))
}

// sortDirections is the shape most lists share: both directions of one key.
// The caller keeps each order total by ending it in the row's unique column.
func sortDirections(key, ascending, descending string) map[string]string {
	return map[string]string{
		key:       ascending,
		"-" + key: descending,
	}
}

// regionSlugsForFilter resolves region references (IDs or slugs) to slugs,
// validated against the regions the caller may see. For a workspace scope,
// that is its subscribed regions; otherwise every region. An unknown or
// unsubscribed region is a 400 that names the parameter.
func (s *Service) regionSlugsForFilter(ctx context.Context, org *db.Organization, refs []string, param string) ([]string, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	rows, err := s.queries(ctx).ListClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	allowed := make(map[string]bool, len(rows))
	if org != nil {
		subs, err := s.subscriptionsByCluster(ctx, org.ID)
		if err != nil {
			return nil, err
		}
		for id := range subs {
			allowed[id] = true
		}
	} else {
		for _, row := range rows {
			allowed[row.ID] = true
		}
	}
	byRef := make(map[string]db.Cluster, len(rows)*2)
	for _, row := range rows {
		byRef[row.ID] = row
		byRef[row.Slug] = row
	}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		row, ok := byRef[ref]
		if !ok || !allowed[row.ID] {
			return nil, badRequestField(param, "unknown %s %q", param, ref)
		}
		out = append(out, row.Slug)
	}
	return out, nil
}

// regionIDsForFilter is regionSlugsForFilter returning cluster IDs.
func (s *Service) regionIDsForFilter(ctx context.Context, org *db.Organization, refs []string, param string) ([]string, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	slugs, err := s.regionSlugsForFilter(ctx, org, refs, param)
	if err != nil {
		return nil, err
	}
	rows, err := s.queries(ctx).ListClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	bySlug := make(map[string]string, len(rows))
	for _, row := range rows {
		bySlug[row.Slug] = row.ID
	}
	out := make([]string, 0, len(slugs))
	for _, slug := range slugs {
		out = append(out, bySlug[slug])
	}
	return out, nil
}

// planSlugsForFilter resolves plan references (IDs or slugs) to slugs. An
// unknown plan is a 400 that names the parameter.
func (s *Service) planSlugsForFilter(ctx context.Context, refs []string, param string) ([]string, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	rows, err := s.queries(ctx).ListPlans(ctx)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	byRef := make(map[string]string, len(rows)*2)
	for _, row := range rows {
		byRef[row.ID] = row.Slug
		byRef[row.Slug] = row.Slug
	}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		slug, ok := byRef[ref]
		if !ok {
			return nil, badRequestField(param, "unknown %s %q", param, ref)
		}
		out = append(out, slug)
	}
	return out, nil
}

// planIDsForFilter resolves plan references to IDs.
func (s *Service) planIDsForFilter(ctx context.Context, refs []string, param string) ([]string, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	slugs, err := s.planSlugsForFilter(ctx, refs, param)
	if err != nil {
		return nil, err
	}
	rows, err := s.queries(ctx).ListPlans(ctx)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	bySlug := make(map[string]string, len(rows))
	for _, row := range rows {
		bySlug[row.Slug] = row.ID
	}
	out := make([]string, 0, len(slugs))
	for _, slug := range slugs {
		out = append(out, bySlug[slug])
	}
	return out, nil
}

// clusterIDsForFilter resolves cluster references to IDs. Nodes and setup
// links are admin-only, so an unknown cluster is a 400.
func (s *Service) clusterIDsForFilter(ctx context.Context, refs []string, param string) ([]string, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	rows, err := s.queries(ctx).ListClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	byRef := make(map[string]string, len(rows)*2)
	for _, row := range rows {
		byRef[row.ID] = row.ID
		byRef[row.Slug] = row.ID
	}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		id, ok := byRef[ref]
		if !ok {
			return nil, badRequestField(param, "unknown %s %q", param, ref)
		}
		out = append(out, id)
	}
	return out, nil
}

// organizationIDsForFilter resolves workspace references to IDs for an
// instance admin's filter. A non-admin may not filter by workspace at all.
func (s *Service) organizationIDsForFilter(ctx context.Context, refs []string, param string) ([]string, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if !p.InstanceAdmin {
		return nil, forbidden("only an instance admin can filter by workspace")
	}
	rows, err := s.queries(ctx).ListOrganizations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}
	byRef := make(map[string]string, len(rows)*2)
	for _, row := range rows {
		byRef[row.ID] = row.ID
		byRef[row.Slug] = row.ID
	}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		id, ok := byRef[ref]
		if !ok {
			return nil, badRequestField(param, "unknown %s %q", param, ref)
		}
		out = append(out, id)
	}
	return out, nil
}

// mergeSorts combines several sortDirections maps.
func mergeSorts(maps ...map[string]string) map[string]string {
	out := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}
