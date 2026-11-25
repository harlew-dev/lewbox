package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/harlew-dev/lewbox/internal/db"
	"github.com/harlew-dev/lewbox/internal/service"
)

// TestPagedListsRejectBadParameters walks every paged route and checks that a
// bad page, sort or filter value is a 400 that names the parameter.
func TestPagedListsRejectBadParameters(t *testing.T) {
	ts := newTestServer(t)
	ctx := service.WithPrincipal(context.Background(), service.LocalPrincipal())
	if _, err := ts.svc.CreateOrganization(ctx, service.CreateOrganizationRequest{
		Slug: "acme", Name: "Acme", Owner: adminEmail,
	}); err != nil {
		t.Fatal(err)
	}
	token := ts.bearer(t)
	headers := map[string]string{"Authorization": "Bearer " + token, "Lewbox-Organization": "acme"}

	cases := []struct {
		path  string
		param string
	}{
		{"/v0/organizations", "status"},
		{"/v0/users", "status"},
		{"/v0/sandboxes", "status"},
		{"/v0/snapshots", "status"},
		{"/v0/nodes", "role"},
		{"/v0/members", "role"},
		{"/v0/sandbox-keys", "status"},
		{"/v0/admin-keys", "status"},
		{"/v0/access-tokens", "status"},
		{"/v0/setup-links", "status"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			for _, query := range []string{
				"?sort=nope",
				"?page=0",
				"?page=10001",
				"?per_page=101",
				"?" + tc.param + "=a,,b",
				"?" + tc.param + "=" + fiftyValues(),
			} {
				res, body := ts.do("GET", tc.path+query, "", headers)
				if res.StatusCode != http.StatusBadRequest {
					t.Errorf("%s: status %d body %s", query, res.StatusCode, body)
					continue
				}
				if !strings.Contains(body, `"code":"bad_request"`) {
					t.Errorf("%s: code in %s", query, body)
				}
				field := tc.param
				if strings.HasPrefix(query, "?sort") {
					field = "sort"
				} else if strings.HasPrefix(query, "?page") {
					field = "page"
				} else if strings.HasPrefix(query, "?per_page") {
					field = "per_page"
				}
				if !strings.Contains(body, `"field":"`+field+`"`) {
					t.Errorf("%s: details do not name %q: %s", query, field, body)
				}
			}
		})
	}
}

// TestPagedListsAnswerTheEnvelope checks the happy path shape and paging.
func TestPagedListsAnswerTheEnvelope(t *testing.T) {
	ts := newTestServer(t)
	token := ts.bearer(t)
	headers := map[string]string{"Authorization": "Bearer " + token}

	res, body := ts.do("GET", "/v0/users?page=1&per_page=2&sort=email&q=admin", "", headers)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}
	var got struct {
		Data  []service.User `json:"data"`
		Pages service.Pages  `json:"pages"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Data) != 1 || got.Data[0].Email != adminEmail {
		t.Errorf("data = %+v", got.Data)
	}
	if got.Pages.Current != 1 || got.Pages.PerPage != 2 || got.Pages.Items != 1 || got.Pages.Total != 1 {
		t.Errorf("pages = %+v", got.Pages)
	}

	// A page past the end is 200 with an empty array and the real totals.
	res, body = ts.do("GET", "/v0/users?page=9&per_page=2", "", headers)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"data":[]`) || !strings.Contains(body, `"total":1`) {
		t.Errorf("past the end: status %d body %s", res.StatusCode, body)
	}
}

// TestEventsFiltersAreMultiValue checks the audit trail accepts comma lists.
func TestEventsFiltersAreMultiValue(t *testing.T) {
	ts := newTestServer(t)
	token := ts.bearer(t)
	headers := map[string]string{"Authorization": "Bearer " + token}
	for _, path := range []string{
		"/v0/events?actor=admin@example.com,other@example.com",
		"/v0/events?organization=acme,globex",
		"/v0/events?action=plan.updated,plan.created",
		"/v0/events?actor=admin@example.com&organization=acme&action=user.created",
	} {
		res, body := ts.do("GET", path, "", headers)
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d body %s", path, res.StatusCode, body)
		}
	}
	res, body := ts.do("GET", "/v0/events?action=not-an-event", "", headers)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, `"field":"action"`) {
		t.Errorf("bad action: status %d body %s", res.StatusCode, body)
	}
}

// TestListExtras covers the opt-in summary, the alive filter and the
// organization's running count.
func TestListExtras(t *testing.T) {
	ts := newTestServer(t)
	ctx := service.WithPrincipal(context.Background(), service.LocalPrincipal())
	org, err := ts.svc.CreateOrganization(ctx, service.CreateOrganizationRequest{
		Slug: "acme", Name: "Acme", Owner: adminEmail,
	})
	if err != nil {
		t.Fatal(err)
	}
	q := db.New(ts.conn)
	cluster, err := q.CreateCluster(ctx, db.CreateClusterParams{
		ID: uuid.NewV7().String(), Slug: "eu-west", Name: "EU West",
		AdminURL: "https://eu.example", EndpointURL: "https://eu.example",
		Runtime: "docker", AdminTokenEnc: []byte{}, AdminTokenID: "tok", AdminTokenLast4: "1234",
		Status: "ready", DnsRecords: "auto", StopPolicy: "keep", ArchiveSnapshotName: "id",
		Checks: "[]", CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.UpsertSandbox(ctx, db.UpsertSandboxParams{
		ClusterID: cluster.ID, ID: "sb-1", OrgID: sql.NullString{String: org.ID, Valid: true},
		Node: "node-a", Name: "box", Image: "ubuntu:22.04", Status: "started", Runtime: "docker",
		Cpu: 1, MemoryMb: 512, DiskGb: 5, CreatedAt: time.Now().UTC(), LastSeenAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.UpsertClusterNode(ctx, db.UpsertClusterNodeParams{
		ID: uuid.NewV7().String(), Name: "node-a", URL: "https://node-a.example",
		ClusterID: sql.NullString{String: cluster.ID, Valid: true}, Role: "mixed",
		Alive: sql.NullInt64{Int64: 1, Valid: true}, LastSeenAt: sql.NullTime{Time: time.Now().UTC(), Valid: true},
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	token := ts.bearer(t)
	headers := map[string]string{"Authorization": "Bearer " + token}

	res, body := ts.do("GET", "/v0/organizations?status=active", "", headers)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"slug":"acme"`) || !strings.Contains(body, `"running":1`) {
		t.Errorf("organizations running: status %d body %s", res.StatusCode, body)
	}

	res, body = ts.do("GET", "/v0/nodes?alive=true&per_page=1", "", headers)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"items":1`) {
		t.Errorf("alive=true: status %d body %s", res.StatusCode, body)
	}
	res, body = ts.do("GET", "/v0/nodes?alive=t", "", headers)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, `"field":"alive"`) {
		t.Errorf("alive=t: status %d body %s", res.StatusCode, body)
	}

	res, body = ts.do("GET", "/v0/snapshots?summary=include", "", headers)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"summary":{"count":0`) {
		t.Errorf("summary=include: status %d body %s", res.StatusCode, body)
	}
	res, body = ts.do("GET", "/v0/snapshots?summary=yes", "", headers)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, `"field":"summary"`) {
		t.Errorf("summary=yes: status %d body %s", res.StatusCode, body)
	}
}

func fiftyValues() string {
	var b strings.Builder
	for i := 0; i < 51; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("v")
		b.WriteString(strings.Repeat("x", i+1))
	}
	return b.String()
}
