package service

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
	"uuid"

	"github.com/harlew-dev/lewbox/internal/db"
)

// TestListOrganizationsPagesSortsSearchesAndFilters covers the shared paging
// contract on a real database: every page joined has no repeats or gaps, the
// sorts are stable, filters are multi-value, and % and _ in q are literal.
func TestListOrganizationsPagesSortsSearchesAndFilters(t *testing.T) {
	s := newTestService(t)
	ctx := localCtx()
	base := s.now()
	statuses := []string{orgActive, orgSuspended, orgPending}
	for i := 0; i < 45; i++ {
		createOrgRow(t, s, fmt.Sprintf("org-%02d", i), fmt.Sprintf("Org %02d", i), statuses[i%3], base.Add(time.Duration(i)*time.Minute))
	}
	createOrgRow(t, s, "fifty-50", "Fifty 50% off", orgActive, base.Add(100*time.Minute))
	createOrgRow(t, s, "ab-c", "a_b c", orgActive, base.Add(101*time.Minute))

	all := listOrgs(t, s, ListOrganizationsRequest{ListQuery: ListQuery{PerPage: "100"}})
	if all.Pages.Items != 47 || all.Pages.Total != 1 {
		t.Fatalf("all = %+v", all.Pages)
	}

	// Join pages 1-3: every organization appears once.
	seen := map[string]bool{}
	for page := 1; page <= 3; page++ {
		got := listOrgs(t, s, ListOrganizationsRequest{ListQuery: ListQuery{Page: fmt.Sprint(page), PerPage: "20"}})
		for _, org := range got.Data {
			if seen[org.ID] {
				t.Fatalf("%s appears on two pages", org.Slug)
			}
			seen[org.ID] = true
		}
	}
	if len(seen) != 47 {
		t.Errorf("pages joined = %d organizations, want 47", len(seen))
	}

	// A page past the end is empty with the real totals.
	past := listOrgs(t, s, ListOrganizationsRequest{ListQuery: ListQuery{Page: "9", PerPage: "20"}})
	if len(past.Data) != 0 || past.Pages.Total != 3 || past.Pages.Current != 9 {
		t.Errorf("past the end = %+v", past)
	}

	// name ascending is the default; -created puts the newest first.
	first := listOrgs(t, s, ListOrganizationsRequest{ListQuery: ListQuery{Sort: "name", PerPage: "1"}})
	if len(first.Data) != 1 || first.Data[0].Slug != "ab-c" {
		t.Errorf("first by name = %+v", first.Data)
	}
	newest := listOrgs(t, s, ListOrganizationsRequest{ListQuery: ListQuery{Sort: "-created", PerPage: "1"}})
	if len(newest.Data) != 1 || newest.Data[0].Slug != "ab-c" {
		t.Errorf("newest = %+v", newest.Data)
	}

	// Multi-value filters: active + suspended, and one status alone.
	two := listOrgs(t, s, ListOrganizationsRequest{Status: orgActive + "," + orgSuspended})
	if two.Pages.Items != 32 {
		t.Errorf("active+suspended = %d, want 32", two.Pages.Items)
	}
	pending := listOrgs(t, s, ListOrganizationsRequest{Status: orgPending})
	if pending.Pages.Items != 15 {
		t.Errorf("pending = %d, want 15", pending.Pages.Items)
	}

	// % and _ are matched literally.
	percent := listOrgs(t, s, ListOrganizationsRequest{ListQuery: ListQuery{Q: "50%"}})
	if len(percent.Data) != 1 || percent.Data[0].Slug != "fifty-50" {
		t.Errorf("q=50%% = %+v", percent.Data)
	}
	underscore := listOrgs(t, s, ListOrganizationsRequest{ListQuery: ListQuery{Q: "a_b"}})
	if len(underscore.Data) != 1 || underscore.Data[0].Slug != "ab-c" {
		t.Errorf("q=a_b = %+v", underscore.Data)
	}

	// Bad values are 400s that name the parameter.
	for _, req := range []ListOrganizationsRequest{
		{ListQuery: ListQuery{Sort: "nope"}},
		{ListQuery: ListQuery{Page: "0"}},
		{ListQuery: ListQuery{PerPage: "101"}},
		{Status: "active,,suspended"},
		{Status: manyValues(51)},
	} {
		if _, err := s.ListOrganizations(ctx, req); err == nil {
			t.Errorf("%+v was accepted", req)
		} else {
			wantField(t, err)
		}
	}
}

func TestListUsersPagesSortsSearchesAndFilters(t *testing.T) {
	s := newTestService(t)
	ctx := localCtx()
	for i := 0; i < 45; i++ {
		insertTestUser(t, s, fmt.Sprintf("user%02d@example.com", i), i%2 == 0)
	}
	insertTestUser(t, s, "fifty%@example.com", false)
	insertTestUser(t, s, "a_b@example.com", false)

	// Every sort key in both directions still visits every row once.
	for _, sortKey := range []string{"email", "-email", "created", "-created", "status", "-status"} {
		seen := map[string]bool{}
		for page := 1; page <= 3; page++ {
			got, err := s.ListUsers(ctx, ListUsersRequest{ListQuery: ListQuery{Sort: sortKey, Page: fmt.Sprint(page), PerPage: "20"}})
			if err != nil {
				t.Fatalf("%s page %d: %v", sortKey, page, err)
			}
			for _, user := range got.Data {
				if seen[user.ID] {
					t.Fatalf("%s: %s repeats", sortKey, user.Email)
				}
				seen[user.ID] = true
			}
		}
		if len(seen) != 47 {
			t.Errorf("%s visited %d accounts, want 47", sortKey, len(seen))
		}
	}

	admins, err := s.ListUsers(ctx, ListUsersRequest{Role: "admin", ListQuery: ListQuery{PerPage: "100"}})
	if err != nil || admins.Pages.Items != 23 {
		t.Errorf("admins = %+v, %v", admins.Pages, err)
	}
	if _, err := s.SetUserDisabled(ctx, "user00@example.com", true); err != nil {
		t.Fatal(err)
	}
	disabled, err := s.ListUsers(ctx, ListUsersRequest{Status: "disabled"})
	if err != nil || disabled.Pages.Items != 1 || disabled.Data[0].Email != "user00@example.com" {
		t.Errorf("disabled = %+v, %v", disabled.Data, err)
	}
	percent, err := s.ListUsers(ctx, ListUsersRequest{ListQuery: ListQuery{Q: "%"}})
	if err != nil || len(percent.Data) != 1 || percent.Data[0].Email != "fifty%@example.com" {
		t.Errorf("q=%% = %+v, %v", percent.Data, err)
	}
	underscore, err := s.ListUsers(ctx, ListUsersRequest{ListQuery: ListQuery{Q: "a_b"}})
	if err != nil || len(underscore.Data) != 1 || underscore.Data[0].Email != "a_b@example.com" {
		t.Errorf("q=a_b = %+v, %v", underscore.Data, err)
	}
}

func TestListSandboxesPagesSortsFiltersAndSearches(t *testing.T) {
	s := newTestService(t)
	ctx := localCtx()
	cluster := createTestCluster(t, s, "eu-west")
	base := s.now()
	for i := 0; i < 45; i++ {
		insertTestSandbox(t, s, cluster.ID, "", fmt.Sprintf("sb-%02d", i), i%2 == 0, base.Add(time.Duration(i)*time.Minute))
	}
	insertTestSandbox(t, s, cluster.ID, "", "sb-50%", true, base.Add(100*time.Minute))
	insertTestSandbox(t, s, cluster.ID, "", "sb-a_b", false, base.Add(101*time.Minute))

	seen := map[string]bool{}
	for page := 1; page <= 3; page++ {
		got, err := s.ListSandboxes(ctx, ListSandboxesRequest{ListQuery: ListQuery{Sort: "-created", Page: fmt.Sprint(page), PerPage: "20"}})
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, sandbox := range got.Data {
			if seen[sandbox.ID] {
				t.Fatalf("%s repeats", sandbox.ID)
			}
			seen[sandbox.ID] = true
		}
	}
	if len(seen) != 47 {
		t.Errorf("pages joined = %d sandboxes, want 47", len(seen))
	}

	started, err := s.ListSandboxes(ctx, ListSandboxesRequest{Status: "started"})
	if err != nil || started.Pages.Items != 24 {
		t.Errorf("started = %d, %v; want 24", started.Pages.Items, err)
	}
	both, err := s.ListSandboxes(ctx, ListSandboxesRequest{Status: "started,stopped", ListQuery: ListQuery{PerPage: "100"}})
	if err != nil || both.Pages.Items != 47 {
		t.Errorf("both statuses = %d, %v; want 47", both.Pages.Items, err)
	}
	percent, err := s.ListSandboxes(ctx, ListSandboxesRequest{ListQuery: ListQuery{Q: "50%"}})
	if err != nil || len(percent.Data) != 1 || percent.Data[0].ID != "sb-50%" {
		t.Errorf("q=50%% = %+v, %v", percent.Data, err)
	}
}

// TestMemberSearchDoesNotRevealAnOperatorsEmail checks that q cannot be used
// to probe the real address the response masks as "operator".
func TestMemberSearchDoesNotRevealAnOperatorsEmail(t *testing.T) {
	f := newOrgFixture(t)
	if _, err := f.s.AddMember(f.root, "acme", AddMemberRequest{Email: "root@example.com"}); err != nil {
		t.Fatal(err)
	}
	customer, err := f.s.ListMembers(f.owner, ListMembersRequest{Org: "acme", ListQuery: ListQuery{Q: "root@"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(customer.Data) != 0 {
		t.Errorf("a customer's search found the operator: %+v", customer.Data)
	}
	operator, err := f.s.ListMembers(f.root, ListMembersRequest{Org: "acme", ListQuery: ListQuery{Q: "root@"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(operator.Data) != 1 || operator.Data[0].Email != "root@example.com" {
		t.Errorf("the operator's own search = %+v", operator.Data)
	}
}

func TestListOrganizationsCountsRunningSandboxes(t *testing.T) {
	s := newTestService(t)
	cluster := createTestCluster(t, s, "eu-west")
	acme := createOrgRow(t, s, "acme", "Acme", orgActive, s.now())
	globex := createOrgRow(t, s, "globex", "Globex", orgActive, s.now())
	insertTestSandbox(t, s, cluster.ID, acme, "sb-1", true, s.now())
	insertTestSandbox(t, s, cluster.ID, acme, "sb-2", false, s.now())
	insertTestSandbox(t, s, cluster.ID, globex, "sb-3", true, s.now())

	list := listOrgs(t, s, ListOrganizationsRequest{ListQuery: ListQuery{PerPage: "100"}})
	running := make(map[string]int, len(list.Data))
	for _, org := range list.Data {
		running[org.Slug] = org.Running
	}
	if running["acme"] != 1 || running["globex"] != 1 {
		t.Errorf("running = %v, want one started sandbox each", running)
	}

	// A single-organization response counts the same way.
	org, err := s.GetOrganization(localCtx(), "acme")
	if err != nil || org.Running != 1 {
		t.Errorf("acme = %+v, %v", org, err)
	}
}

func TestListNodesAliveFilter(t *testing.T) {
	s := newTestService(t)
	cluster := createTestCluster(t, s, "eu-west")
	now := s.now()
	for i, alive := range []sql.NullInt64{{Int64: 1, Valid: true}, {Int64: 0, Valid: true}, {}} {
		if _, err := s.queries(localCtx()).UpsertClusterNode(localCtx(), db.UpsertClusterNodeParams{
			ID:         uuid.NewV7().String(),
			Name:       fmt.Sprintf("node-%d", i),
			URL:        fmt.Sprintf("https://node-%d.example", i),
			ClusterID:  sql.NullString{String: cluster.ID, Valid: true},
			Role:       "mixed",
			Alive:      alive,
			LastSeenAt: sql.NullTime{Time: now, Valid: true},
			CreatedAt:  now,
		}); err != nil {
			t.Fatalf("insert node %d: %v", i, err)
		}
	}

	// per_page=1 is how the console counts without loading the page.
	up, err := s.ListNodes(localCtx(), ListNodesRequest{Alive: "true", ListQuery: ListQuery{Page: "1", PerPage: "1"}})
	if err != nil || len(up.Data) != 1 || up.Pages.Items != 1 {
		t.Errorf("alive=true = %+v, %v; want one item", up, err)
	}
	down, err := s.ListNodes(localCtx(), ListNodesRequest{Alive: "false", ListQuery: ListQuery{Page: "1", PerPage: "1"}})
	if err != nil || len(down.Data) != 1 || down.Pages.Items != 1 {
		t.Errorf("alive=false = %+v, %v; want one item", down, err)
	}
	all, err := s.ListNodes(localCtx(), ListNodesRequest{})
	if err != nil || all.Pages.Items != 3 {
		t.Errorf("no alive filter = %d, %v; want 3", all.Pages.Items, err)
	}
	if _, err := s.ListNodes(localCtx(), ListNodesRequest{Alive: "t"}); err == nil {
		t.Error("alive=t was accepted")
	} else {
		wantField(t, err)
	}
}

func TestSnapshotSortIsTotal(t *testing.T) {
	at := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	data := []Snapshot{
		{Name: "b", Region: "eu-west", CreatedAt: at, DiskGB: 2, Kind: snapshotKindSnapshot},
		{Name: "a", Region: "eu-west", CreatedAt: at, DiskGB: 5, Kind: snapshotKindArchive},
		{Name: "c", Region: "eu-west", CreatedAt: at.Add(-time.Hour), DiskGB: 1, Kind: snapshotKindSnapshot},
	}
	sortSnapshots(data, "size")
	if data[0].Name != "c" || data[1].Name != "b" || data[2].Name != "a" {
		t.Errorf("by size = %v", snapshotNames(data))
	}
	sortSnapshots(data, "-created")
	if data[0].Name != "a" || data[1].Name != "b" || data[2].Name != "c" {
		t.Errorf("newest first = %v", snapshotNames(data))
	}
}

func TestSnapshotSummaryTotalsAndSorts(t *testing.T) {
	nodeA := "node-a"
	data := []Snapshot{
		{Name: "s1", Region: "eu-west", Node: &nodeA, Kind: snapshotKindSnapshot, Status: snapshotStatusError, DiskGB: 10, MemoryMB: 1024, Organization: &SandboxOrganization{Slug: "acme", Name: "Acme"}},
		{Name: "s2", Region: "eu-west", Node: &nodeA, Kind: snapshotKindSnapshot, Status: snapshotStatusActive, DiskGB: 5, MemoryMB: 512, Organization: &SandboxOrganization{Slug: "acme", Name: "Acme"}},
		{Name: "a1", Region: "eu-west", Kind: snapshotKindArchive, Status: snapshotStatusActive, DiskGB: 20, MemoryMB: 2048, Organization: &SandboxOrganization{Slug: "globex", Name: "Globex"}},
		{Name: "a2", Region: "us-east", Kind: snapshotKindArchive, Status: snapshotStatusActive, DiskGB: 1, MemoryMB: 128, Organization: &SandboxOrganization{Slug: "globex", Name: "Globex"}},
	}
	summary := snapshotSummary(data, nil, true)
	if summary.Count != 4 || summary.DiskGB != 36 || summary.MemoryMB != 3712 ||
		summary.Failed != 1 || summary.Workspaces != 2 {
		t.Errorf("summary = %+v", summary)
	}
	if len(summary.ByNode) != 3 ||
		summary.ByNode[0].Region != "eu-west" || summary.ByNode[0].Node != nil || summary.ByNode[0].DiskGB != 20 ||
		summary.ByNode[1].Node == nil || *summary.ByNode[1].Node != "node-a" || summary.ByNode[1].DiskGB != 15 {
		t.Errorf("by node = %+v", summary.ByNode)
	}
	if len(summary.ByWorkspace) != 2 || summary.ByWorkspace[0].Slug != "globex" ||
		summary.ByWorkspace[0].DiskGB != 21 || summary.ByWorkspace[1].Slug != "acme" {
		t.Errorf("by workspace = %+v", summary.ByWorkspace)
	}
}

func snapshotNames(data []Snapshot) []string {
	out := make([]string, len(data))
	for i, s := range data {
		out[i] = s.Name
	}
	return out
}

func listOrgs(t *testing.T, s *Service, req ListOrganizationsRequest) OrganizationList {
	t.Helper()
	out, err := s.ListOrganizations(localCtx(), req)
	if err != nil {
		t.Fatalf("list organizations %+v: %v", req, err)
	}
	return out
}

func createOrgRow(t *testing.T, s *Service, slug, name, status string, createdAt time.Time) string {
	t.Helper()
	id := uuid.NewV7().String()
	if _, err := s.queries(localCtx()).CreateOrganization(localCtx(), db.CreateOrganizationParams{
		ID:        id,
		Slug:      slug,
		Name:      name,
		Status:    status,
		CreatedAt: createdAt,
		Team:      sql.NullString{String: teamForID(id), Valid: true},
	}); err != nil {
		t.Fatalf("create organization %s: %v", slug, err)
	}
	return id
}

func insertTestUser(t *testing.T, s *Service, email string, admin bool) {
	t.Helper()
	if _, err := s.insertUser(localCtx(), newUser{email: email, hash: "test-hash", instanceAdmin: admin}); err != nil {
		t.Fatalf("insert user %s: %v", email, err)
	}
}

func createTestCluster(t *testing.T, s *Service, slug string) db.Cluster {
	t.Helper()
	row, err := s.queries(localCtx()).CreateCluster(localCtx(), db.CreateClusterParams{
		ID:                    uuid.NewV7().String(),
		Slug:                  slug,
		Name:                  slug,
		AdminURL:              "https://" + slug + ".example",
		EndpointURL:           "https://" + slug + ".example",
		Runtime:               "docker",
		AdminTokenEnc:         []byte{},
		AdminTokenID:          "tok",
		AdminTokenLast4:       "1234",
		Status:                "ready",
		DnsRecords:            "auto",
		StopPolicy:            "keep",
		ArchiveLocalRetention: "",
		ArchiveSnapshotName:   "id",
		Checks:                "[]",
		CreatedAt:             s.now(),
	})
	if err != nil {
		t.Fatalf("create cluster: %v", err)
	}
	return row
}

func insertTestSandbox(t *testing.T, s *Service, clusterID, orgID, id string, started bool, createdAt time.Time) {
	t.Helper()
	status := "stopped"
	if started {
		status = "started"
	}
	if err := s.queries(localCtx()).UpsertSandbox(localCtx(), db.UpsertSandboxParams{
		ClusterID:  clusterID,
		ID:         id,
		OrgID:      sql.NullString{String: orgID, Valid: orgID != ""},
		Node:       "node-a",
		Name:       id,
		Image:      "ubuntu:22.04",
		Status:     status,
		Runtime:    "docker",
		Cpu:        1,
		MemoryMb:   512,
		DiskGb:     5,
		CreatedAt:  createdAt,
		LastSeenAt: createdAt,
	}); err != nil {
		t.Fatalf("insert sandbox %s: %v", id, err)
	}
}

func manyValues(n int) string {
	out := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprintf("v%d", i)
	}
	return out
}
