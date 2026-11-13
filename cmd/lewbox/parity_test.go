package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/aerolvm/aerolvmtest"
	"github.com/harlew-dev/lewbox/internal/db"
	"github.com/harlew-dev/lewbox/internal/dns/cloudflaretest"
	"github.com/harlew-dev/lewbox/internal/httpapi"
	"github.com/harlew-dev/lewbox/internal/objstore/objstoretest"
	"github.com/harlew-dev/lewbox/internal/secrets"
	"github.com/harlew-dev/lewbox/internal/service"
)

// env is one database shared by an HTTP server and CLI invocations, the way
// `lewbox orchestrator` and `lewbox node ...` share one file.
type env struct {
	t      *testing.T
	dbPath string
	srv    *httptest.Server
	token  string
}

const adminEmail = "admin@example.com"
const adminPassword = "admin-password-1"

func newEnv(t *testing.T) *env {
	t.Helper()
	return newEnvWith(t)
}

// newEnvWith is newEnv with extra service options, such as a test Cloudflare
// API URL.
func newEnvWith(t *testing.T, opts ...service.Option) *env {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lewbox.db")
	conn, err := db.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	keyring, err := secrets.LoadOrCreate(filepath.Join(filepath.Dir(path), "keyring.key"))
	if err != nil {
		t.Fatal(err)
	}
	opts = append([]service.Option{service.WithKeyring(keyring)}, opts...)
	srv := httptest.NewServer(httpapi.NewRouter(service.New(db.New(conn), opts...), slog.New(slog.DiscardHandler)))
	t.Cleanup(srv.Close)
	e := &env{t: t, dbPath: path, srv: srv}

	// The first admin is made through the CLI in local mode, then signs in
	// over HTTP. Every other HTTP call carries that session.
	if _, errOut, code := e.cliIn(adminPassword+"\n", "user", "create", adminEmail, "--instance-admin", "--password-stdin"); code != 0 {
		t.Fatalf("user create: exit %d: %s", code, errOut)
	}
	status, body := e.httpWith("POST", "/v0/auth/login", `{"email":"`+adminEmail+`","password":"`+adminPassword+`"}`, "")
	if status != http.StatusOK {
		t.Fatalf("login: status %d body %s", status, body)
	}
	var login service.LoginResponse
	if err := json.Unmarshal([]byte(body), &login); err != nil {
		t.Fatal(err)
	}
	e.token = login.Token
	return e
}

// cli runs `lewbox --db <path> args...` with empty stdin.
func (e *env) cli(args ...string) (stdout, stderr string, code int) {
	e.t.Helper()
	return e.cliIn("", args...)
}

// cliIn runs `lewbox --db <path> --context local args...` with the given
// stdin. The explicit context keeps the developer's saved sign-ins out of the
// test.
func (e *env) cliIn(input string, args ...string) (stdout, stderr string, code int) {
	e.t.Helper()
	var out, errb bytes.Buffer
	args = append([]string{"--db", e.dbPath, "--context", "local"}, args...)
	code = runWithInput(context.Background(), args, strings.NewReader(input), &out, &errb)
	return out.String(), errb.String(), code
}

// http sends an authenticated request and returns the status and body.
func (e *env) http(method, path, body string) (int, string) {
	e.t.Helper()
	return e.httpWith(method, path, body, e.token)
}

// httpWith sends a request with an explicit bearer token ("" for anonymous).
func (e *env) httpWith(method, path, body, token string) (int, string) {
	e.t.Helper()
	return e.httpOrgWith(method, path, body, token, "")
}

// httpOrg sends an authenticated request for an organization, named in the
// Lewbox-Organization header.
func (e *env) httpOrg(org, method, path, body string) (int, string) {
	e.t.Helper()
	return e.httpOrgWith(method, path, body, e.token, org)
}

func (e *env) httpOrgWith(method, path, body, token, org string) (int, string) {
	e.t.Helper()
	req, err := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if org != "" {
		req.Header.Set(httpapi.OrganizationHeader, org)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		e.t.Errorf("%s %s: content-type = %q", method, path, ct)
	}
	return res.StatusCode, string(b)
}

// same asserts a CLI success and an HTTP response are byte-identical.
func (e *env) same(args []string, method, path, body string, wantStatus int) string {
	e.t.Helper()
	out, errOut, code := e.cli(args...)
	if code != 0 {
		e.t.Fatalf("lewbox %v: exit %d, stderr %s", args, code, errOut)
	}
	status, got := e.http(method, path, body)
	if status != wantStatus {
		e.t.Fatalf("%s %s: status %d, want %d (body %s)", method, path, status, wantStatus, got)
	}
	if out != got {
		e.t.Fatalf("lewbox %v and %s %s differ:\ncli:  %s\nhttp: %s", args, method, path, out, got)
	}
	return out
}

// sameOrg is same for an organization route.
func (e *env) sameOrg(org string, args []string, method, path string, wantStatus int) string {
	e.t.Helper()
	out, errOut, code := e.cli(args...)
	if code != 0 {
		e.t.Fatalf("lewbox %v: exit %d, stderr %s", args, code, errOut)
	}
	status, got := e.httpOrg(org, method, path, "")
	if status != wantStatus {
		e.t.Fatalf("%s %s: status %d, want %d (body %s)", method, path, status, wantStatus, got)
	}
	if out != got {
		e.t.Fatalf("lewbox %v and %s %s differ:\ncli:  %s\nhttp: %s", args, method, path, out, got)
	}
	return out
}

// sameOrgBody is sameOrg with a request body.
func (e *env) sameOrgBody(org string, args []string, method, path, body string, wantStatus int) string {
	e.t.Helper()
	out, errOut, code := e.cli(args...)
	if code != 0 {
		e.t.Fatalf("lewbox %v: exit %d, stderr %s", args, code, errOut)
	}
	status, got := e.httpOrg(org, method, path, body)
	if status != wantStatus {
		e.t.Fatalf("%s %s: status %d, want %d (body %s)", method, path, status, wantStatus, got)
	}
	if out != got {
		e.t.Fatalf("lewbox %v and %s %s differ:\ncli:  %s\nhttp: %s", args, method, path, out, got)
	}
	return out
}

// sameError asserts a CLI failure prints the HTTP error body on stderr.
func (e *env) sameError(args []string, method, path, body string, wantStatus int) {
	e.t.Helper()
	out, errOut, code := e.cli(args...)
	if code != 1 || out != "" {
		e.t.Fatalf("lewbox %v: exit %d stdout %q, want exit 1 and no stdout", args, code, out)
	}
	status, got := e.http(method, path, body)
	if status != wantStatus {
		e.t.Fatalf("%s %s: status %d, want %d (body %s)", method, path, status, wantStatus, got)
	}
	if errOut != got {
		e.t.Fatalf("lewbox %v and %s %s errors differ:\ncli:  %s\nhttp: %s", args, method, path, errOut, got)
	}
}

func TestHealth(t *testing.T) {
	e := newEnv(t)
	if got := e.same([]string{"health"}, "GET", "/v0/health", "", 200); got != `{"ok":true}`+"\n" {
		t.Errorf("health = %q", got)
	}
}

func TestNodes(t *testing.T) {
	e := newEnv(t)

	// Add through the CLI; the HTTP API sees it, by name and by ID.
	out, errOut, code := e.cli("node", "add", "eu-west-1", "--url", "https://eu-west-1.internal:8443/")
	if code != 0 {
		t.Fatalf("node add: exit %d: %s", code, errOut)
	}
	var n service.Node
	if err := json.Unmarshal([]byte(out), &n); err != nil {
		t.Fatal(err)
	}
	if id, err := uuid.Parse(n.ID); err != nil || id.String() != n.ID {
		t.Errorf("id %q is not a canonical UUID", n.ID)
	}
	if n.Name != "eu-west-1" || n.URL != "https://eu-west-1.internal:8443" || n.CreatedAt.IsZero() {
		t.Errorf("added node = %+v", n)
	}
	if got := e.same([]string{"node", "get", "eu-west-1"}, "GET", "/v0/nodes/eu-west-1", "", 200); got != out {
		t.Errorf("get by name = %s, want %s", got, out)
	}
	if got := e.same([]string{"node", "get", n.ID}, "GET", "/v0/nodes/"+n.ID, "", 200); got != out {
		t.Errorf("get by id = %s, want %s", got, out)
	}

	// Add through HTTP; the CLI sees it. List is sorted by name.
	status, _ := e.http("POST", "/v0/nodes", `{"name":"ap-south-1","url":"http://10.0.0.7:8080"}`)
	if status != http.StatusCreated {
		t.Fatalf("POST /v0/nodes: status %d", status)
	}
	var list service.NodeList
	if err := json.Unmarshal([]byte(e.same([]string{"node", "ls"}, "GET", "/v0/nodes", "", 200)), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 2 || list.Data[0].Name != "ap-south-1" || list.Data[1].Name != "eu-west-1" {
		t.Errorf("list = %+v", list.Data)
	}

	// Remove through the CLI; then both report the same not_found.
	rm, errOut, code := e.cli("node", "rm", "eu-west-1", "--yes")
	if code != 0 || rm != out {
		t.Fatalf("node rm: exit %d, stdout %s, stderr %s", code, rm, errOut)
	}
	e.sameError([]string{"node", "get", "eu-west-1"}, "GET", "/v0/nodes/eu-west-1", "", 404)
}

func TestNodeErrors(t *testing.T) {
	e := newEnv(t)
	if _, errOut, code := e.cli("node", "create", "eu-west-1", "--url", "https://a.example"); code != 0 {
		t.Fatalf("node create: %s", errOut)
	}

	cases := []struct {
		name   string
		args   []string
		body   string
		status int
	}{
		{"duplicate", []string{"node", "add", "eu-west-1", "--url", "https://b.example"}, `{"name":"eu-west-1","url":"https://b.example"}`, 409},
		{"bad name", []string{"node", "add", "EU_West", "--url", "https://b.example"}, `{"name":"EU_West","url":"https://b.example"}`, 400},
		{"uuid name", []string{"node", "add", "0192f0c4a3b27c6d8e9f0a1b2c3d4e5f", "--url", "https://b.example"}, `{"name":"0192f0c4a3b27c6d8e9f0a1b2c3d4e5f","url":"https://b.example"}`, 400},
		{"bad url", []string{"node", "add", "b", "--url", "b.example:8443"}, `{"name":"b","url":"b.example:8443"}`, 400},
		{"url with query", []string{"node", "add", "b", "--url", "https://b.example?x=1"}, `{"name":"b","url":"https://b.example?x=1"}`, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e.sameError(tc.args, "POST", "/v0/nodes", tc.body, tc.status)
		})
	}

	t.Run("unknown field", func(t *testing.T) {
		status, body := e.http("POST", "/v0/nodes", `{"name":"b","url":"https://b.example","region":"eu"}`)
		if status != 400 || !strings.Contains(body, `"code":"invalid_request"`) {
			t.Errorf("status %d body %s", status, body)
		}
	})

	t.Run("missing --url", func(t *testing.T) {
		if _, errOut, code := e.cli("node", "add", "b"); code != 1 || !strings.Contains(errOut, "run lewbox install first") {
			t.Errorf("exit %d stderr %q", code, errOut)
		}
	})
}

func TestUsers(t *testing.T) {
	e := newEnv(t)

	var list service.UserList
	if err := json.Unmarshal([]byte(e.same([]string{"user", "list"}, "GET", "/v0/users", "", 200)), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || list.Data[0].Email != adminEmail || !list.Data[0].InstanceAdmin {
		t.Fatalf("users = %+v", list.Data)
	}

	byEmail := e.same([]string{"user", "get", adminEmail}, "GET", "/v0/users/"+adminEmail, "", 200)
	if byEmail != e.same([]string{"user", "get", list.Data[0].ID}, "GET", "/v0/users/"+list.Data[0].ID, "", 200) {
		t.Errorf("get by email and by id differ")
	}

	if _, errOut, code := e.cliIn("secret-password-1\n", "user", "create", "second@example.com", "--password-stdin"); code != 0 {
		t.Fatalf("user create: exit %d: %s", code, errOut)
	}
	e.same([]string{"user", "get", "second@example.com"}, "GET", "/v0/users/second@example.com", "", 200)
}

func TestClusterSet(t *testing.T) {
	cf := cloudflaretest.New(t)
	cf.AddZone("example.com")
	e := newEnvWith(t, service.WithCloudflareURL(cf.URL))
	t.Setenv("LEWBOX_CLOUDFLARE_API_URL", cf.URL)
	if _, errOut, code := e.cliIn(cf.Token+"\n", "dns-profile", "create", "cf-prod",
		"--zone", "example.com", "--api-token-stdin"); code != 0 {
		t.Fatalf("dns-profile create: exit %d: %s", code, errOut)
	}

	fake := aerolvmtest.New(t)
	const token = "avm_admin_cluster_set_token"
	fake.AdminToken = token
	fake.SetMembers([]aerolvm.Member{{
		NodeID: "node-a", NodeName: "node-a", APIURL: fake.URL(), Alive: true,
		PublicHost: "203.0.113.10", Role: "mixed", Version: "v0.7.0",
		Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
	}})
	if _, errOut, code := e.cliIn(token+"\n", "cluster", "add", "eu-west",
		"--admin-url", fake.URL(), "--endpoint-url", fake.URL(), "--admin-token-stdin"); code != 0 {
		t.Fatalf("cluster add: exit %d: %s", code, errOut)
	}

	out := e.same([]string{"cluster", "set", "eu-west", "--domain", "eu.example.com",
		"--dns-profile", "cf-prod", "--dns-records", "off"},
		"PATCH", "/v0/clusters/eu-west",
		`{"domain":"eu.example.com","dns_profile":"cf-prod","dns_records":"off"}`, 200)
	var detail service.ClusterDetail
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Domain != "eu.example.com" || detail.DNSProfile == nil ||
		*detail.DNSProfile != "cf-prod" || detail.DNSRecords != "off" {
		t.Errorf("cluster = %+v", detail.Cluster)
	}

	out = e.same([]string{"cluster", "set", "eu-west", "--domain", "", "--dns-profile", "none"},
		"PATCH", "/v0/clusters/eu-west", `{"domain":"","dns_profile":"none"}`, 200)
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Domain != "" || detail.DNSProfile != nil {
		t.Errorf("cleared cluster = %+v", detail.Cluster)
	}

	// A domain outside the profile's zone and a bad record mode are refused
	// the same way by both entry points.
	e.sameError([]string{"cluster", "set", "eu-west", "--domain", "eu.example.com", "--dns-profile", "cf-prod", "--dns-records", "sometimes"},
		"PATCH", "/v0/clusters/eu-west",
		`{"domain":"eu.example.com","dns_profile":"cf-prod","dns_records":"sometimes"}`, 400)
	if status, body := e.http("PATCH", "/v0/clusters/eu-west", `{}`); status != 400 || !strings.Contains(body, "nothing to change") {
		t.Errorf("empty update: status %d body %s", status, body)
	}
}

func TestClusters(t *testing.T) {
	e := newEnv(t)
	fake := aerolvmtest.New(t)
	const token = "avm_admin_parity_token"
	fake.AdminToken = token
	fake.SetMembers([]aerolvm.Member{
		{
			NodeID: "node-a", NodeName: "node-a", APIURL: fake.URL(), Alive: true,
			Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
		},
		{
			NodeID: "node-b", NodeName: "node-b", APIURL: "http://node-b.test:21212", Alive: true,
			Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
		},
	})

	out, errOut, code := e.cliIn(token+"\n", "cluster", "add", "eu-west",
		"--admin-url", fake.URL(), "--endpoint-url", fake.URL(), "--admin-token-stdin")
	if code != 0 {
		t.Fatalf("cluster add: exit %d: %s", code, errOut)
	}
	var added service.ClusterDetail
	if err := json.Unmarshal([]byte(out), &added); err != nil {
		t.Fatal(err)
	}
	if added.Slug != "eu-west" || added.Status != "ready" || added.Nodes != 2 || len(added.NodeList) != 2 {
		t.Fatalf("cluster add = %+v", added)
	}

	e.same([]string{"cluster", "list"}, "GET", "/v0/clusters", "", 200)
	e.same([]string{"cluster", "get", "eu-west"}, "GET", "/v0/clusters/eu-west", "", 200)
	e.same([]string{"cluster", "get", added.ID}, "GET", "/v0/clusters/"+added.ID, "", 200)
	e.same([]string{"node", "list", "--cluster", "eu-west"}, "GET", "/v0/nodes?cluster=eu-west", "", 200)
	e.same([]string{"node", "list", "--cluster", added.ID}, "GET", "/v0/nodes?cluster="+added.ID, "", 200)
	e.same([]string{"region", "list"}, "GET", "/v0/regions", "", 200)
}

func TestClusterHTTP(t *testing.T) {
	e := newEnv(t)
	fake := aerolvmtest.New(t)
	const token = "avm_admin_http_token"
	fake.AdminToken = token
	fake.SetMembers([]aerolvm.Member{
		{NodeID: "node-a", NodeName: "node-a", APIURL: fake.URL(), Alive: true, Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}}},
		{NodeID: "node-b", NodeName: "node-b", APIURL: "http://node-b.test:21212", Alive: true, Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}}},
	})

	body := fmt.Sprintf(`{"slug":"eu-west","admin_url":%q,"endpoint_url":%q,"runtime":"docker","admin_token":%q}`,
		fake.URL(), fake.URL(), token)
	status, out := e.http("POST", "/v0/clusters", body)
	if status != http.StatusCreated {
		t.Fatalf("POST /v0/clusters: status %d body %s", status, out)
	}
	var detail service.ClusterDetail
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Slug != "eu-west" || detail.Nodes != 2 || len(detail.NodeList) != 2 {
		t.Fatalf("cluster = %+v", detail)
	}

	if status, out := e.http("POST", "/v0/clusters/eu-west/check", ""); status != http.StatusOK {
		t.Fatalf("POST check: status %d body %s", status, out)
	}
	if status, out := e.http("GET", "/v0/clusters/resolve?ref=cluster/eu-west", ""); status != http.StatusOK {
		t.Fatalf("GET resolve cluster/x: status %d body %s", status, out)
	}
	if status, out := e.http("GET", "/v0/clusters/resolve?ref=node/node-a", ""); status != http.StatusOK {
		t.Fatalf("GET resolve node/x: status %d body %s", status, out)
	}

	status, out = e.http("POST", "/v0/clusters/eu-west/join-tokens", `{"role":"worker"}`)
	if status != http.StatusCreated {
		t.Fatalf("POST join-tokens: status %d body %s", status, out)
	}
	var join service.JoinToken
	if err := json.Unmarshal([]byte(out), &join); err != nil {
		t.Fatal(err)
	}
	if join.Role != "worker" || join.TokenRole != "worker" || join.Token == "" {
		t.Errorf("join token = %+v", join)
	}

	if status, out := e.http("DELETE", "/v0/nodes/node-b", ""); status != http.StatusOK {
		t.Fatalf("DELETE node: status %d body %s", status, out)
	}
	if status, out := e.http("GET", "/v0/nodes/node-b", ""); status != http.StatusNotFound {
		t.Fatalf("GET removed node: status %d body %s", status, out)
	}
	if status, out := e.http("DELETE", "/v0/clusters/eu-west", ""); status != http.StatusOK {
		t.Fatalf("DELETE cluster: status %d body %s", status, out)
	}
	if status, out := e.http("GET", "/v0/clusters", ""); status != http.StatusOK || !strings.Contains(out, `"data":[]`) {
		t.Fatalf("GET clusters after delete: status %d body %s", status, out)
	}
}

func TestUnauthenticatedNodeList(t *testing.T) {
	e := newEnv(t)
	status, body := e.httpWith("GET", "/v0/nodes", "", "")
	want := `{"error":{"code":"unauthorized","message":"sign in first: lewbox login"}}` + "\n"
	if status != http.StatusUnauthorized || body != want {
		t.Errorf("status %d body %q, want 401 %q", status, body, want)
	}
}

func TestUnknownRouteUsesErrorShape(t *testing.T) {
	status, body := newEnv(t).http("GET", "/v0/nope", "")
	want := `{"error":{"code":"not_found","message":"no route for GET /v0/nope"}}` + "\n"
	if status != 404 || body != want {
		t.Errorf("status %d body %q, want 404 %q", status, body, want)
	}
}

func TestVersionDoesNotCreateDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "lewbox.db")
	var out bytes.Buffer
	if code := run(context.Background(), []string{"--db", path, "version"}, &out, io.Discard); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("version created %s (stat err %v)", filepath.Dir(path), err)
	}
}

func TestOrganizations(t *testing.T) {
	e := newEnv(t)
	for _, email := range []string{"owner@example.com", "member@example.com"} {
		if _, errOut, code := e.cliIn("member-password-1\n", "user", "create", email, "--password-stdin"); code != 0 {
			t.Fatalf("user create %s: %s", email, errOut)
		}
	}

	// The HTTP caller is an instance admin who isn't a member, like the
	// local principal, so neither output has a role.
	out, errOut, code := e.cli("org", "create", "acme", "--name", "Acme", "--owner", "owner@example.com")
	if code != 0 {
		t.Fatalf("org create: exit %d: %s", code, errOut)
	}
	var org service.Organization
	if err := json.Unmarshal([]byte(out), &org); err != nil {
		t.Fatal(err)
	}
	if org.Slug != "acme" || org.Name != "Acme" || org.Members != 1 || org.Status != "active" {
		t.Fatalf("org = %+v", org)
	}
	if got := e.same([]string{"org", "get", "acme"}, "GET", "/v0/organizations/acme", "", 200); got != out {
		t.Errorf("get = %s, want %s", got, out)
	}
	e.same([]string{"org", "get", org.ID}, "GET", "/v0/organizations/"+org.ID, "", 200)
	e.same([]string{"org", "list"}, "GET", "/v0/organizations", "", 200)
	e.same([]string{"org", "list", "--user", "owner@example.com"}, "GET", "/v0/organizations?user=owner@example.com", "", 200)
	if got := e.sameOrg("acme", []string{"org", "get", "--org", "acme"}, "GET", "/v0/organization", 200); got != out {
		t.Errorf("current organization = %s, want %s", got, out)
	}

	if _, errOut, code := e.cli("--org", "acme", "member", "add", "member@example.com", "--role", "viewer"); code != 0 {
		t.Fatalf("member add: %s", errOut)
	}
	e.sameOrg("acme", []string{"member", "list", "--org", "acme"}, "GET", "/v0/members", 200)
	if _, errOut, code := e.cli("member", "set-role", "member@example.com", "admin", "--org", "acme"); code != 0 {
		t.Fatalf("member set-role: %s", errOut)
	}
	if got := e.sameOrg("acme", []string{"member", "list", "--org", "acme"}, "GET", "/v0/members", 200); !strings.Contains(got, `"role":"admin"`) {
		t.Errorf("members after set-role = %s", got)
	}

	out, errOut, code = e.cli("admin-key", "create", "backend", "--org", "acme", "--expires-in", "30d")
	if code != 0 {
		t.Fatalf("admin-key create: %s", errOut)
	}
	var key service.NewAdminKey
	if err := json.Unmarshal([]byte(out), &key); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key.Token, "lba_") || key.ExpiresAt == nil {
		t.Fatalf("key = %+v", key)
	}
	list := e.sameOrg("acme", []string{"admin-key", "list", "--org", "acme"}, "GET", "/v0/admin-keys", 200)
	if strings.Contains(list, key.Token) || strings.Contains(list, `"token"`) {
		t.Errorf("the key list shows the token: %s", list)
	}

	// The key reads its organization over HTTP, without the header.
	if status, body := e.httpOrgWith("GET", "/v0/members", "", key.Token, ""); status != 200 || !strings.Contains(body, "member@example.com") {
		t.Errorf("members with key: %d %s", status, body)
	}
	if status, body := e.httpOrgWith("GET", "/v0/nodes", "", key.Token, ""); status != 403 {
		t.Errorf("nodes with key: %d %s", status, body)
	}

	if _, errOut, code := e.cli("admin-key", "revoke", "backend", "--org", "acme", "--yes"); code != 0 {
		t.Fatalf("admin-key revoke: %s", errOut)
	}
	if status, _ := e.httpOrgWith("GET", "/v0/members", "", key.Token, ""); status != 401 {
		t.Errorf("revoked key: status %d", status)
	}
	e.sameOrg("acme", []string{"admin-key", "list", "--org", "acme"}, "GET", "/v0/admin-keys", 200)

	if _, errOut, code := e.cli("org", "suspend", "acme", "--yes"); code != 0 {
		t.Fatalf("org suspend: %s", errOut)
	}
	if got := e.same([]string{"org", "get", "acme"}, "GET", "/v0/organizations/acme", "", 200); !strings.Contains(got, `"status":"suspended"`) {
		t.Errorf("suspended org = %s", got)
	}
	if _, errOut, code := e.cli("org", "restore", "acme"); code != 0 {
		t.Fatalf("org restore: %s", errOut)
	}

	// Errors read the same either way.
	e.sameError([]string{"member", "list"}, "GET", "/v0/members", "", 400)
	e.sameError([]string{"org", "get", "missing"}, "GET", "/v0/organizations/missing", "", 404)
	e.sameError([]string{"org", "create", "acme"}, "POST", "/v0/organizations", `{"slug":"acme"}`, 409)
}

func TestSandboxKeys(t *testing.T) {
	e := newEnv(t)
	fake := aerolvmtest.New(t)
	const token = "avm_admin_sandbox_token"
	fake.AdminToken = token
	fake.SetMembers([]aerolvm.Member{{
		NodeID: "node-a", NodeName: "node-a", APIURL: fake.URL(), Alive: true,
		Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
	}})
	if _, errOut, code := e.cliIn(token+"\n", "cluster", "add", "eu-west",
		"--admin-url", fake.URL(), "--endpoint-url", fake.URL(), "--admin-token-stdin"); code != 0 {
		t.Fatalf("cluster add: exit %d: %s", code, errOut)
	}
	if _, errOut, code := e.cli("org", "create", "acme"); code != 0 {
		t.Fatalf("org create: %s", errOut)
	}
	if _, errOut, code := e.cli("subscription", "set", "eu-west", "--plan", "free", "--org", "acme"); code != 0 {
		t.Fatalf("subscription set: %s", errOut)
	}
	if got := e.sameOrg("acme", []string{"key", "list", "--org", "acme"}, "GET", "/v0/sandbox-keys", 200); got != `{"data":[]}`+"\n" {
		t.Errorf("empty key list = %q", got)
	}

	out, errOut, code := e.cli("key", "create", "laptop", "--org", "acme", "--region", "eu-west", "--expires-in", "30d")
	if code != 0 {
		t.Fatalf("key create: exit %d: %s", code, errOut)
	}
	var created service.NewSandboxKey
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Token, "avm_") || created.Name != "laptop" || created.Region.Slug != "eu-west" ||
		created.ExpiresAt == nil || created.Env["SB_API_TOKEN"] != created.Token || created.E2BEnv["E2B_API_KEY"] != created.Token {
		t.Fatalf("created = %+v", created)
	}

	status, body := e.httpOrg("acme", "POST", "/v0/sandbox-keys", `{"name":"agent","region":"eu-west"}`)
	if status != http.StatusCreated {
		t.Fatalf("POST sandbox-keys: status %d body %s", status, body)
	}
	var remote service.NewSandboxKey
	if err := json.Unmarshal([]byte(body), &remote); err != nil {
		t.Fatal(err)
	}
	if remote.Name != "agent" || remote.Token == created.Token {
		t.Errorf("remote key = %+v", remote)
	}
	list := e.sameOrg("acme", []string{"key", "list", "--org", "acme"}, "GET", "/v0/sandbox-keys", 200)
	if !strings.Contains(list, `"name":"laptop"`) || !strings.Contains(list, `"name":"agent"`) {
		t.Errorf("list = %s", list)
	}

	status, body = e.httpOrg("acme", "DELETE", "/v0/sandbox-keys/agent", "")
	if status != http.StatusOK {
		t.Fatalf("DELETE sandbox key: status %d body %s", status, body)
	}
	var revoked service.SandboxKey
	if err := json.Unmarshal([]byte(body), &revoked); err != nil {
		t.Fatal(err)
	}
	if revoked.Name != "agent" || revoked.RevokedAt == nil {
		t.Errorf("revoked = %+v", revoked)
	}

	out, errOut, code = e.cli("key", "revoke", "laptop", "--org", "acme", "--yes")
	if code != 0 {
		t.Fatalf("key revoke: exit %d: %s", code, errOut)
	}
	if err := json.Unmarshal([]byte(out), &revoked); err != nil {
		t.Fatal(err)
	}
	if revoked.Name != "laptop" || revoked.RevokedAt == nil {
		t.Errorf("revoked = %+v", revoked)
	}
	e.sameOrg("acme", []string{"key", "list", "--org", "acme"}, "GET", "/v0/sandbox-keys", 200)
	out, errOut, code = e.cli("key", "revoke", "laptop", "--org", "acme", "--yes")
	if code != 1 || out != "" {
		t.Fatalf("revoke a revoked key: exit %d stdout %q stderr %q", code, out, errOut)
	}
	status, body = e.httpOrg("acme", "DELETE", "/v0/sandbox-keys/laptop", "")
	if status != http.StatusNotFound || errOut != body {
		t.Errorf("revoked key error = %d %q, want 404 %q", status, body, errOut)
	}
}

// TestSetupLinkParity exercises the operator's onboarding links through both
// entry points.
func TestSetupLinkParity(t *testing.T) {
	e := newEnv(t)
	fake := aerolvmtest.New(t)
	const token = "avm_admin_setup_token"
	fake.AdminToken = token
	fake.SetMembers([]aerolvm.Member{{
		NodeID: "node-a", NodeName: "node-a", APIURL: fake.URL(), Alive: true,
		Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
	}})
	if _, errOut, code := e.cliIn(token+"\n", "cluster", "add", "eu-west",
		"--admin-url", fake.URL(), "--endpoint-url", fake.URL(), "--admin-token-stdin"); code != 0 {
		t.Fatalf("cluster add: exit %d: %s", code, errOut)
	}

	out, errOut, code := e.cli("setup-link", "create", "--plan", "free", "--region", "eu-west",
		"--expires", "7d", "--note", "acme onboarding")
	if code != 0 {
		t.Fatalf("setup-link create: exit %d: %s", code, errOut)
	}
	var created service.SetupLink
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Token, "lbs_") || len(created.Token) != 36 || created.Status != "unused" ||
		created.Note != "acme onboarding" || created.Plan.Slug != "free" || created.Region.Slug != "eu-west" {
		t.Fatalf("created = %+v", created)
	}
	if created.ExpiresAt.Sub(created.CreatedAt) != 7*24*time.Hour {
		t.Errorf("lifetime = %s", created.ExpiresAt.Sub(created.CreatedAt))
	}

	list := e.same([]string{"setup-link", "list"}, "GET", "/v0/setup-links", "", 200)
	if strings.Contains(list, created.Token) || strings.Contains(list, `"token"`) {
		t.Errorf("the list shows a token: %s", list)
	}

	status, body := e.http("POST", "/v0/setup-links", `{"plan":"free","region":"eu-west","expires_in_hours":48}`)
	if status != http.StatusCreated {
		t.Fatalf("POST setup-links: status %d body %s", status, body)
	}
	var remote service.SetupLink
	if err := json.Unmarshal([]byte(body), &remote); err != nil {
		t.Fatal(err)
	}
	if remote.Token == created.Token || remote.Status != "unused" ||
		remote.ExpiresAt.Sub(remote.CreatedAt) != 48*time.Hour {
		t.Errorf("remote link = %+v", remote)
	}

	out, errOut, code = e.cli("setup-link", "revoke", created.ID, "--yes")
	if code != 0 {
		t.Fatalf("setup-link revoke: exit %d: %s", code, errOut)
	}
	var revoked service.SetupLink
	if err := json.Unmarshal([]byte(out), &revoked); err != nil {
		t.Fatal(err)
	}
	if revoked.ID != created.ID || revoked.Token != "" {
		t.Errorf("revoked = %+v", revoked)
	}
	e.sameError([]string{"setup-link", "revoke", created.ID, "--yes"}, "DELETE", "/v0/setup-links/"+created.ID, "", 404)

	status, body = e.http("DELETE", "/v0/setup-links/"+remote.ID, "")
	if status != http.StatusOK {
		t.Fatalf("DELETE setup link: status %d body %s", status, body)
	}
	e.same([]string{"setup-link", "list"}, "GET", "/v0/setup-links", "", 200)
}

// TestSandboxesAndUsage exercises the mirror, sandboxes, usage and limits
// commands against one HTTP server and the same database in local mode.
func TestSandboxesAndUsage(t *testing.T) {
	e := newEnv(t)
	fake := aerolvmtest.New(t)
	const token = "avm_admin_sandbox_parity"
	fake.AdminToken = token
	fake.SetMembers([]aerolvm.Member{{
		NodeID: "node-a", NodeName: "node-a", APIURL: fake.URL(), Alive: true,
		Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
	}})
	if _, errOut, code := e.cliIn(token+"\n", "cluster", "add", "eu-west",
		"--admin-url", fake.URL(), "--endpoint-url", fake.URL(), "--admin-token-stdin"); code != 0 {
		t.Fatalf("cluster add: exit %d: %s", code, errOut)
	}
	out, errOut, code := e.cli("org", "create", "acme")
	if code != 0 {
		t.Fatalf("org create: %s", errOut)
	}
	var org service.Organization
	if err := json.Unmarshal([]byte(out), &org); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := e.cli("subscription", "set", "eu-west", "--plan", "free", "--org", "acme"); code != 0 {
		t.Fatalf("subscription set: %s", errOut)
	}
	fake.AddSandbox(org.Team, aerolvm.Sandbox{
		ID: "sb-1", Name: "agent", Image: "ubuntu:22.04", Status: "started", Runtime: "docker",
		CPU: 1, MemoryMB: 512, DiskGB: 10, CreatedAt: time.Now().UTC().Truncate(time.Second),
	})

	// Run one sync pass through a second service on the same database.
	conn, err := db.Open(context.Background(), e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	keyring, err := secrets.LoadOrCreate(filepath.Join(filepath.Dir(e.dbPath), "keyring.key"))
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(db.New(conn), service.WithKeyring(keyring), service.WithAerolVMOptions(aerolvm.WithRetry(0)))
	if err := svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	e.same([]string{"sandbox", "list"}, "GET", "/v0/sandboxes", "", 200)
	e.same([]string{"sandbox", "list", "--region", "eu-west", "--status", "started"},
		"GET", "/v0/sandboxes?region=eu-west&status=started", "", 200)
	e.sameOrg("acme", []string{"sandbox", "list", "--org", "acme"}, "GET", "/v0/sandboxes", 200)
	e.sameOrg("acme", []string{"sandbox", "stop", "sb-1", "--region", "eu-west", "--org", "acme"},
		"POST", "/v0/sandboxes/sb-1/stop?region=eu-west", 200)
	deleted, errOut, code := e.cli("sandbox", "delete", "sb-1", "--region", "eu-west", "--org", "acme", "--yes")
	if code != 0 {
		t.Fatalf("sandbox delete: exit %d: %s", code, errOut)
	}
	var gone service.Sandbox
	if err := json.Unmarshal([]byte(deleted), &gone); err != nil {
		t.Fatal(err)
	}
	if gone.ID != "sb-1" || gone.Status != "destroyed" {
		t.Errorf("deleted sandbox = %+v", gone)
	}
	// A second delete is a 404 either way.
	e.sameError([]string{"sandbox", "delete", "sb-1", "--region", "eu-west", "--org", "acme", "--yes"},
		"DELETE", "/v0/sandboxes/sb-1?region=eu-west", "", 404)
	e.sameOrg("acme", []string{"usage", "--org", "acme", "--from", "2026-11-01", "--to", "2026-11-03"},
		"GET", "/v0/usage?from=2026-11-01&to=2026-11-03", 200)
	e.sameOrg("acme", []string{"limits", "--org", "acme"}, "GET", "/v0/limits", 200)
}

// TestSnapshots exercises listing and deleting a workspace's snapshots
// through the CLI and HTTP.
func TestSnapshots(t *testing.T) {
	e := newEnv(t)
	fake := aerolvmtest.New(t)
	const token = "avm_admin_snapshot_parity"
	fake.AdminToken = token
	fake.SetMembers([]aerolvm.Member{{
		NodeID: "node-a", NodeName: "node-a", APIURL: fake.URL(), Alive: true,
		Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
	}})
	if _, errOut, code := e.cliIn(token+"\n", "cluster", "add", "eu-west",
		"--admin-url", fake.URL(), "--endpoint-url", fake.URL(), "--admin-token-stdin"); code != 0 {
		t.Fatalf("cluster add: exit %d: %s", code, errOut)
	}
	out, errOut, code := e.cli("org", "create", "acme")
	if code != 0 {
		t.Fatalf("org create: %s", errOut)
	}
	var org service.Organization
	if err := json.Unmarshal([]byte(out), &org); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := e.cli("subscription", "set", "eu-west", "--plan", "free", "--org", "acme"); code != 0 {
		t.Fatalf("subscription set: %s", errOut)
	}
	fake.AddSnapshot(org.Team, aerolvm.Snapshot{
		Name: "ckpt-1", Image: "ubuntu:22.04", SourceSandboxID: "sb-1",
		CPU: 1, MemoryMB: 512, DiskGB: 10, PushState: "active",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	})

	e.sameOrg("acme", []string{"snapshot", "list", "--org", "acme"}, "GET", "/v0/snapshots", 200)
	e.sameOrg("acme", []string{"snapshot", "list", "--region", "eu-west", "--org", "acme"},
		"GET", "/v0/snapshots?region=eu-west", 200)

	deleted, errOut, code := e.cli("snapshot", "delete", "ckpt-1", "--region", "eu-west", "--org", "acme", "--yes")
	if code != 0 {
		t.Fatalf("snapshot delete: exit %d: %s", code, errOut)
	}
	var gone service.Snapshot
	if err := json.Unmarshal([]byte(deleted), &gone); err != nil {
		t.Fatal(err)
	}
	if gone.Name != "ckpt-1" || gone.Region != "eu-west" || gone.SourceSandboxID != "sb-1" {
		t.Errorf("deleted snapshot = %+v", gone)
	}
	e.sameError([]string{"snapshot", "delete", "ckpt-1", "--region", "eu-west", "--org", "acme", "--yes"},
		"DELETE", "/v0/snapshots/ckpt-1?region=eu-west", "", 404)
	if got, errOut, code := e.cli("snapshot", "list", "--org", "acme"); code != 0 || got != `{"data":[],"partial_nodes":[]}`+"\n" {
		t.Errorf("list after delete: exit %d stdout %q stderr %q", code, got, errOut)
	}
}

// TestArchives exercises listing, showing, restoring and deleting archived
// sandboxes through the CLI and HTTP.
func TestArchives(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	fake := aerolvmtest.New(t)
	const token = "avm_admin_archive_parity"
	fake.AdminToken = token
	fake.SetMembers([]aerolvm.Member{{
		NodeID: "node-a", NodeName: "node-a", APIURL: fake.URL(), Alive: true,
		Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
	}})
	if _, errOut, code := e.cliIn(token+"\n", "cluster", "add", "eu-west",
		"--admin-url", fake.URL(), "--endpoint-url", fake.URL(), "--admin-token-stdin"); code != 0 {
		t.Fatalf("cluster add: exit %d: %s", code, errOut)
	}
	out, errOut, code := e.cli("org", "create", "acme")
	if code != 0 {
		t.Fatalf("org create: %s", errOut)
	}
	var org service.Organization
	if err := json.Unmarshal([]byte(out), &org); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := e.cli("subscription", "set", "eu-west", "--plan", "free", "--org", "acme"); code != 0 {
		t.Fatalf("subscription set: %s", errOut)
	}

	bucket := objstoretest.New(t)
	bucket.AddBucket("assets")
	profileOut, errOut, code := e.cliIn(bucket.SecretAccessKey+"\n", "storage-profile", "create", "archive",
		"--endpoint", bucket.URL, "--region", "us-east-1", "--bucket", "assets",
		"--access-key-id", bucket.AccessKeyID, "--secret-access-key-stdin")
	if code != 0 {
		t.Fatalf("storage profile create: exit %d: %s", code, errOut)
	}
	var profile service.StorageProfile
	if err := json.Unmarshal([]byte(profileOut), &profile); err != nil {
		t.Fatal(err)
	}
	clusterOut, errOut, code := e.cli("cluster", "get", "eu-west")
	if code != 0 {
		t.Fatalf("cluster get: %s", errOut)
	}
	var cluster service.ClusterDetail
	if err := json.Unmarshal([]byte(clusterOut), &cluster); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Open(ctx, e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx,
		`UPDATE clusters SET stop_policy = 'archive', storage_profile_id = ? WHERE id = ?`,
		profile.ID, cluster.ID); err != nil {
		t.Fatal(err)
	}
	for _, sandboxID := range []string{"sb-1", "sb-2"} {
		fake.AddSandbox(org.Team, aerolvm.Sandbox{
			ID: sandboxID, Name: "box", Image: "ubuntu:22.04", Status: "stopped", Runtime: "docker",
			CPU: 2, MemoryMB: 1024, DiskGB: 20, Tags: map[string]string{"app": "demo"},
			CreatedAt: time.Now().UTC().Truncate(time.Second),
		})
	}

	// Run the sync loop and the archiver through a second service on the
	// same database, as TestSandboxesAndUsage does.
	keyring, err := secrets.LoadOrCreate(filepath.Join(filepath.Dir(e.dbPath), "keyring.key"))
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(db.New(conn), service.WithKeyring(keyring), service.WithAerolVMOptions(aerolvm.WithRetry(0)))
	if err := svc.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	for _, sandboxID := range []string{"sb-1", "sb-2"} {
		ref := "127.0.0.1:5000/cluster/" + cluster.ID + "/snapshots/tenant-x/archive-" + sandboxID + ":latest"
		fake.SetSnapshotPush(org.Team, "archive-"+sandboxID, "active", ref)
	}
	if err := svc.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}

	ids := make(map[string]string)
	for _, sandboxID := range []string{"sb-1", "sb-2"} {
		var id string
		if err := conn.QueryRowContext(ctx, `SELECT id FROM archives WHERE sandbox_id = ?`, sandboxID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[sandboxID] = id
	}

	e.sameOrg("acme", []string{"archive", "list", "--org", "acme"}, "GET", "/v0/archives", 200)
	e.sameOrg("acme", []string{"archive", "list", "--region", "eu-west", "--org", "acme"},
		"GET", "/v0/archives?region=eu-west", 200)
	e.sameOrg("acme", []string{"archive", "show", ids["sb-1"], "--org", "acme"},
		"GET", "/v0/archives/"+ids["sb-1"], 200)

	// Restore both archives, one through each entry point.
	restored, errOut, code := e.cli("archive", "restore", ids["sb-1"], "--org", "acme")
	if code != 0 {
		t.Fatalf("archive restore: exit %d: %s", code, errOut)
	}
	var first service.RestoredArchive
	if err := json.Unmarshal([]byte(restored), &first); err != nil {
		t.Fatal(err)
	}
	if first.Sandbox.ID == "" || first.Sandbox.Name != "box" || first.Sandbox.Region != "eu-west" ||
		first.Archive.SandboxID != "sb-1" || first.Archive.LastRestoredSandboxID != first.Sandbox.ID {
		t.Errorf("restored = %+v", first)
	}
	if sandbox, ok := fake.Sandbox(first.Sandbox.ID); !ok || sandbox.Tags["lewbox.restored_from"] != "sb-1" {
		t.Errorf("restored sandbox = %+v, %v", sandbox, ok)
	}

	status, body := e.httpOrg("acme", "POST", "/v0/archives/"+ids["sb-2"]+"/restore", "")
	if status != http.StatusOK {
		t.Fatalf("HTTP restore: status %d: %s", status, body)
	}
	var second service.RestoredArchive
	if err := json.Unmarshal([]byte(body), &second); err != nil {
		t.Fatal(err)
	}
	if second.Sandbox.ID == "" || second.Sandbox.ID == first.Sandbox.ID || second.Archive.SandboxID != "sb-2" {
		t.Errorf("HTTP restored = %+v", second)
	}

	// Delete both archives, one through each entry point.
	deleted, errOut, code := e.cli("archive", "delete", ids["sb-1"], "--org", "acme", "--yes")
	if code != 0 {
		t.Fatalf("archive delete: exit %d: %s", code, errOut)
	}
	var firstDeleted service.Archive
	if err := json.Unmarshal([]byte(deleted), &firstDeleted); err != nil {
		t.Fatal(err)
	}
	if firstDeleted.ID != ids["sb-1"] || firstDeleted.Status != "deleted" {
		t.Errorf("deleted = %+v", firstDeleted)
	}

	status, body = e.httpOrg("acme", "DELETE", "/v0/archives/"+ids["sb-2"], "")
	if status != http.StatusOK {
		t.Fatalf("HTTP delete: status %d: %s", status, body)
	}
	var secondDeleted service.Archive
	if err := json.Unmarshal([]byte(body), &secondDeleted); err != nil {
		t.Fatal(err)
	}
	if secondDeleted.ID != ids["sb-2"] || secondDeleted.Status != "deleted" {
		t.Errorf("HTTP deleted = %+v", secondDeleted)
	}
	e.sameOrg("acme", []string{"archive", "list", "--org", "acme"}, "GET", "/v0/archives", 200)
}

func TestMonitoring(t *testing.T) {
	e := newEnv(t)
	fake := aerolvmtest.New(t)
	const token = "avm_admin_monitoring_parity"
	fake.AdminToken = token
	fake.SetMembers([]aerolvm.Member{{
		NodeID: "node-a", NodeName: "node-a", APIURL: fake.URL(), Alive: true, Role: "mixed", Version: "v0.7.0",
		Capacity: aerolvm.Capacity{
			HostCPUCores: 4, HostMemoryTotalMB: 15364, LiveMemoryFreeMB: 8552, HostDiskFreeGB: 13,
			ReservedCPU: 1, SandboxesActive: 2, CanAdmit: true, SupportedRuntimes: []string{"docker"},
		},
	}})
	if _, errOut, code := e.cliIn(token+"\n", "cluster", "add", "eu-west",
		"--admin-url", fake.URL(), "--endpoint-url", fake.URL(), "--admin-token-stdin"); code != 0 {
		t.Fatalf("cluster add: exit %d: %s", code, errOut)
	}
	if _, errOut, code := e.cli("cluster", "check", "eu-west"); code != 0 {
		t.Fatalf("cluster check: exit %d: %s", code, errOut)
	}

	e.same([]string{"cluster", "health", "eu-west"}, "GET", "/v0/clusters/eu-west/health?days=30", "", 200)
	e.same([]string{"cluster", "health", "eu-west", "--days", "7"}, "GET", "/v0/clusters/eu-west/health?days=7", "", 200)
	e.same([]string{"cluster", "metrics", "eu-west"}, "GET", "/v0/clusters/eu-west/metrics?range=24h", "", 200)
	e.same([]string{"cluster", "metrics", "eu-west", "--range", "7d"}, "GET", "/v0/clusters/eu-west/metrics?range=7d", "", 200)
	overview := e.same([]string{"overview"}, "GET", "/v0/overview", "", 200)
	if !strings.Contains(overview, `"customers"`) || !strings.Contains(overview, `"running"`) {
		t.Errorf("overview = %s", overview)
	}

	e.sameError([]string{"cluster", "metrics", "eu-west", "--range", "1h"},
		"GET", "/v0/clusters/eu-west/metrics?range=1h", "", 400)
	e.sameError([]string{"cluster", "health", "eu-west", "--days", "91"},
		"GET", "/v0/clusters/eu-west/health?days=91", "", 400)
}

func TestPlansSettingsAndSignup(t *testing.T) {
	e := newEnv(t)

	// Settings read and write the same either way.
	e.same([]string{"settings"}, "GET", "/v0/settings", "", 200)
	e.same([]string{"settings", "set", "--signup", "approval"},
		"PATCH", "/v0/settings", `{"signup":"approval"}`, 200)
	e.same([]string{"settings", "set", "--name", "Acme Sandboxes", "--default-plan", "free"},
		"PATCH", "/v0/settings", `{"instance_name":"Acme Sandboxes","default_plan":"free"}`, 200)
	e.same([]string{"settings", "set", "--default-region", "none"},
		"PATCH", "/v0/settings", `{"default_region":"none"}`, 200)
	e.same([]string{"settings", "set", "--default-plan", "none"},
		"PATCH", "/v0/settings", `{"default_plan":"none"}`, 200)
	e.same([]string{"settings", "set", "--default-plan", "free"},
		"PATCH", "/v0/settings", `{"default_plan":"free"}`, 200)

	// Sign-up creates only the account; its workspace waits for approval.
	status, body := e.httpWith("POST", "/v0/auth/signup",
		`{"email":"new@example.com","password":"new-password-1"}`, "")
	if status != http.StatusOK {
		t.Fatalf("signup: status %d body %s", status, body)
	}
	status, body = e.httpWith("POST", "/v0/auth/login",
		`{"email":"new@example.com","password":"new-password-1"}`, "")
	if status != http.StatusOK {
		t.Fatalf("new account login: status %d body %s", status, body)
	}
	var login service.LoginResponse
	if err := json.Unmarshal([]byte(body), &login); err != nil {
		t.Fatal(err)
	}
	status, body = e.httpWith("GET", "/v0/me", "", login.Token)
	var me service.Me
	if err := json.Unmarshal([]byte(body), &me); err != nil || status != http.StatusOK || len(me.Organizations) != 0 || me.Organization != nil {
		t.Fatalf("me after signup: status %d body %s (%v)", status, body, err)
	}
	status, body = e.httpWith("POST", "/v0/organizations", `{"name":"New Co"}`, login.Token)
	if status != http.StatusCreated {
		t.Fatalf("user workspace: status %d body %s", status, body)
	}
	var pendingOrg service.Organization
	if err := json.Unmarshal([]byte(body), &pendingOrg); err != nil {
		t.Fatal(err)
	}
	if pendingOrg.Status != "pending" || pendingOrg.Region != nil || pendingOrg.Role != "owner" {
		t.Fatalf("pending workspace = %+v", pendingOrg)
	}
	pending := e.same([]string{"org", "list", "--status", "pending"},
		"GET", "/v0/organizations?status=pending", "", 200)
	var list service.OrganizationList
	if err := json.Unmarshal([]byte(pending), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || list.Data[0].Status != "pending" || list.Data[0].Region != nil {
		t.Fatalf("pending = %s", pending)
	}
	e.same([]string{"org", "approve", list.Data[0].Slug},
		"PATCH", "/v0/organizations/"+list.Data[0].Slug, `{"status":"active"}`, 200)

	// Plans: create, show, list, update, clear a limit, delete.
	smallOut, errOut, code := e.cli("plan", "create", "small", "--name", "Small",
		"--max-sandboxes", "5", "--max-vcpus", "2", "--max-memory", "4GB")
	if code != 0 {
		t.Fatalf("plan create: exit %d: %s", code, errOut)
	}
	var small service.Plan
	if err := json.Unmarshal([]byte(smallOut), &small); err != nil {
		t.Fatal(err)
	}
	if small.MaxMemoryMB == nil || *small.MaxMemoryMB != 4096 || small.Default {
		t.Fatalf("plan = %+v", small)
	}
	if got := e.same([]string{"plan", "show", "small"}, "GET", "/v0/plans/small", "", 200); got != smallOut {
		t.Errorf("plan show = %s, want the create output %s", got, smallOut)
	}
	e.same([]string{"plan", "show", small.ID}, "GET", "/v0/plans/"+small.ID, "", 200)
	e.same([]string{"plan", "list"}, "GET", "/v0/plans", "", 200)
	cleared := e.same([]string{"plan", "update", "small", "--name", "Smaller", "--max-vcpus", "unlimited", "--max-memory", "1GB"},
		"PATCH", "/v0/plans/small", `{"name":"Smaller","max_vcpus":null,"max_memory_mb":1024}`, 200)
	if got, errOut, code := e.cli("plan", "delete", "small", "--yes"); code != 0 || got != cleared {
		t.Fatalf("plan delete: exit %d stdout %s stderr %s\nwant %s", code, got, errOut, cleared)
	}
	e.sameError([]string{"plan", "show", "small"}, "GET", "/v0/plans/small", "", 404)

	// An organization has no plan until the operator subscribes it.
	created, errOut, code := e.cli("org", "create", "acme")
	if code != 0 {
		t.Fatalf("org create: %s", errOut)
	}
	if got := e.same([]string{"org", "get", "acme"}, "GET", "/v0/organizations/acme", "", 200); got != created {
		t.Errorf("org get = %s, want the create output %s", got, created)
	}
	if strings.Contains(created, `"plan"`) || !strings.Contains(created, `"region":null`) {
		t.Errorf("new organization = %s", created)
	}
	e.same([]string{"org", "update", "acme", "--name", "Acme Inc"},
		"PATCH", "/v0/organizations/acme", `{"name":"Acme Inc"}`, 200)
	if _, errOut, code := e.cli("plan", "update", "small", "--max-sandboxes", "0"); code != 2 || !strings.Contains(errOut, "positive") {
		t.Errorf("plan update zero: exit %d stderr %s", code, errOut)
	}
	e.same([]string{"org", "list", "--status", "active"}, "GET", "/v0/organizations?status=active", "", 200)
}

// TestSubscriptions exercises per-region plans through the CLI and HTTP.
func TestSubscriptions(t *testing.T) {
	e := newEnv(t)
	fake := aerolvmtest.New(t)
	const token = "avm_admin_subscription_parity"
	fake.AdminToken = token
	fake.SetMembers([]aerolvm.Member{{
		NodeID: "node-a", NodeName: "node-a", APIURL: fake.URL(), Alive: true,
		Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
	}})
	if _, errOut, code := e.cliIn(token+"\n", "cluster", "add", "eu-west",
		"--admin-url", fake.URL(), "--endpoint-url", fake.URL(), "--admin-token-stdin"); code != 0 {
		t.Fatalf("cluster add: exit %d: %s", code, errOut)
	}
	if _, errOut, code := e.cli("org", "create", "acme"); code != 0 {
		t.Fatalf("org create: %s", errOut)
	}

	// With no subscription, a key there is refused.
	out, errOut, code := e.cli("key", "create", "laptop", "--org", "acme", "--region", "eu-west")
	if code != 1 || out != "" {
		t.Fatalf("key create without a subscription: exit %d stdout %q", code, out)
	}
	status, got := e.httpOrg("acme", "POST", "/v0/sandbox-keys", `{"name":"laptop","region":"eu-west"}`)
	if status != http.StatusNotFound || errOut != got {
		t.Fatalf("key create without a subscription: status %d error %q, want 404 %q", status, errOut, got)
	}
	if got := e.sameOrg("acme", []string{"subscription", "list", "--org", "acme"}, "GET", "/v0/subscriptions", 200); got != `{"data":[]}`+"\n" {
		t.Errorf("empty subscription list = %q", got)
	}

	created := e.sameOrgBody("acme", []string{"subscription", "set", "eu-west", "--plan", "free", "--max-sandboxes", "1", "--org", "acme"},
		"PUT", "/v0/subscriptions/eu-west", `{"plan":"free","max_sandboxes":1}`, 200)
	e.sameOrg("acme", []string{"subscription", "list", "--org", "acme"}, "GET", "/v0/subscriptions", 200)
	e.sameOrg("acme", []string{"limits", "--org", "acme"}, "GET", "/v0/limits", 200)
	regions := e.sameOrg("acme", []string{"region", "list", "--org", "acme"}, "GET", "/v0/regions", 200)
	if !strings.Contains(regions, `"slug":"eu-west"`) {
		t.Errorf("regions = %s", regions)
	}

	// A subscribed key works.
	if _, errOut, code := e.cli("key", "create", "laptop", "--org", "acme", "--region", "eu-west"); code != 0 {
		t.Fatalf("key create: %s", errOut)
	}

	// Removing the subscription returns the same object either way.
	out, errOut, code = e.cli("subscription", "remove", "eu-west", "--org", "acme", "--yes")
	if code != 0 {
		t.Fatalf("subscription remove: exit %d: %s", code, errOut)
	}
	if out != created {
		t.Fatalf("subscription remove = %s, want the set output %s", out, created)
	}
	if got := e.sameOrg("acme", []string{"subscription", "list", "--org", "acme"}, "GET", "/v0/subscriptions", 200); got != `{"data":[]}`+"\n" {
		t.Errorf("subscription list after remove = %q", got)
	}
}

func TestDNSProfiles(t *testing.T) {
	fake := cloudflaretest.New(t)
	const token = "cf_parity_token"
	fake.Token = token
	zoneID := fake.AddZone("example.com")
	t.Setenv("LEWBOX_CLOUDFLARE_API_URL", fake.URL)
	e := newEnvWith(t, service.WithCloudflareURL(fake.URL))

	// Create through the CLI with a token on stdin; HTTP sees the same
	// profile, without the token.
	out, errOut, code := e.cliIn(token+"\n", "dns-profile", "create", "cf-eu",
		"--zone", "example.com", "--api-token-stdin", "--acme-email", "ops@example.com")
	if code != 0 {
		t.Fatalf("dns-profile create: exit %d: %s", code, errOut)
	}
	var created service.DNSProfile
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "cf-eu" || created.ZoneID != zoneID || created.Version != 1 ||
		created.TestedAt == nil || created.TestError != "" || created.ACMEEmail != "ops@example.com" {
		t.Fatalf("created = %+v", created)
	}
	if strings.Contains(out, token) {
		t.Errorf("the create output carries the token: %s", out)
	}
	if got := e.same([]string{"dns-profile", "show", "cf-eu"}, "GET", "/v0/dns-profiles/cf-eu", "", 200); got != out {
		t.Errorf("show = %s, want the create output %s", got, out)
	}
	e.same([]string{"dns-profile", "show", created.ID}, "GET", "/v0/dns-profiles/"+created.ID, "", 200)
	e.same([]string{"dns-profile", "list"}, "GET", "/v0/dns-profiles", "", 200)

	// Create a second one over HTTP; the CLI's list sees it.
	status, body := e.http("POST", "/v0/dns-profiles",
		`{"name":"cf-us","zone":"example.com","api_token":"`+token+`"}`)
	if status != http.StatusCreated {
		t.Fatalf("POST dns-profiles: status %d body %s", status, body)
	}
	var remote service.DNSProfile
	if err := json.Unmarshal([]byte(body), &remote); err != nil {
		t.Fatal(err)
	}
	if remote.Name != "cf-us" || remote.ZoneID != zoneID || remote.TestedAt == nil {
		t.Errorf("remote profile = %+v", remote)
	}
	list := e.same([]string{"dns-profile", "list"}, "GET", "/v0/dns-profiles", "", 200)
	if !strings.Contains(list, `"name":"cf-eu"`) || !strings.Contains(list, `"token_last4"`) || strings.Contains(list, token) {
		t.Errorf("list = %s", list)
	}

	// Update through the CLI; the same row comes back from HTTP.
	updated, errOut, code := e.cli("dns-profile", "update", "cf-eu", "--name", "cf-eu-2")
	if code != 0 {
		t.Fatalf("dns-profile update: exit %d: %s", code, errOut)
	}
	if got := e.same([]string{"dns-profile", "show", "cf-eu-2"}, "GET", "/v0/dns-profiles/cf-eu-2", "", 200); got != updated {
		t.Errorf("show after update = %s, want the update output %s", got, updated)
	}
	if !strings.Contains(updated, `"version":1`) {
		t.Errorf("a name-only update bumped the version: %s", updated)
	}
	status, body = e.http("PATCH", "/v0/dns-profiles/cf-us", `{"acme_email":"dns@example.com"}`)
	if status != http.StatusOK {
		t.Fatalf("PATCH dns-profile: status %d body %s", status, body)
	}

	// Test: a failing test shows in test_error either way.
	fake.FailNext("GET", "/zones", 500, 1)
	tested, errOut, code := e.cli("dns-profile", "test", "cf-eu-2")
	if code != 0 {
		t.Fatalf("dns-profile test: exit %d: %s", code, errOut)
	}
	if !strings.Contains(tested, `"test_error"`) || strings.Contains(tested, `"test_error":""`) {
		t.Errorf("failed test = %s", tested)
	}
	status, body = e.http("POST", "/v0/dns-profiles/cf-us/test", "")
	if status != http.StatusOK {
		t.Fatalf("POST dns-profile test: status %d body %s", status, body)
	}
	e.same([]string{"dns-profile", "show", "cf-eu-2"}, "GET", "/v0/dns-profiles/cf-eu-2", "", 200)

	// Credentials are the only response with the token, and agree.
	creds := e.same([]string{"dns-profile", "credentials", "cf-eu-2"}, "POST", "/v0/dns-profiles/cf-eu-2/credentials", "", 200)
	if !strings.Contains(creds, token) {
		t.Errorf("credentials = %s", creds)
	}

	// Delete through the CLI, then through HTTP.
	deleted, errOut, code := e.cli("dns-profile", "delete", "cf-eu-2", "--yes")
	if code != 0 || !strings.Contains(deleted, `"name":"cf-eu-2"`) {
		t.Fatalf("dns-profile delete: exit %d stdout %s stderr %s", code, deleted, errOut)
	}
	e.sameError([]string{"dns-profile", "show", "cf-eu-2"}, "GET", "/v0/dns-profiles/cf-eu-2", "", 404)
	if status, body := e.http("DELETE", "/v0/dns-profiles/cf-us", ""); status != http.StatusOK {
		t.Fatalf("DELETE dns-profile: status %d body %s", status, body)
	}
	if got := e.same([]string{"dns-profile", "list"}, "GET", "/v0/dns-profiles", "", 200); got != `{"data":[]}`+"\n" {
		t.Errorf("list after delete = %q", got)
	}

	// A bad zone fails the test as a precondition, and nothing is saved.
	badOut, badErr, badCode := e.cliIn(token+"\n", "dns-profile", "create", "cf-bad",
		"--zone", "missing.example", "--api-token-stdin")
	if badCode != 1 || badOut != "" {
		t.Fatalf("dns-profile create with a bad zone: exit %d stdout %q", badCode, badOut)
	}
	status, body = e.http("POST", "/v0/dns-profiles",
		`{"name":"cf-bad","zone":"missing.example","api_token":"`+token+`"}`)
	if status != http.StatusPreconditionFailed || badErr != body {
		t.Errorf("bad zone error = %d %q, want 412 %q", status, badErr, body)
	}
}

func TestStorageProfiles(t *testing.T) {
	fake := objstoretest.New(t)
	fake.AddBucket("assets")
	const secret = "objstore_parity_secret"
	e := newEnv(t)

	// Create through the CLI with a secret on stdin; HTTP sees the same
	// profile, without the secret.
	out, errOut, code := e.cliIn(secret+"\n", "storage-profile", "create", "backups",
		"--endpoint", fake.URL, "--region", "us-east-1", "--bucket", "assets",
		"--prefix", "clusters", "--access-key-id", fake.AccessKeyID, "--secret-access-key-stdin")
	if code != 0 {
		t.Fatalf("storage-profile create: exit %d: %s", code, errOut)
	}
	var created service.StorageProfile
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "backups" || created.Endpoint != fake.URL || created.Bucket != "assets" ||
		created.Prefix != "clusters" || !created.PathStyle || created.Version != 1 ||
		created.TestedAt == nil || created.TestError != "" || created.SecretLast4 != "cret" {
		t.Fatalf("created = %+v", created)
	}
	if strings.Contains(out, secret) {
		t.Errorf("the create output carries the secret: %s", out)
	}
	if got := e.same([]string{"storage-profile", "show", "backups"}, "GET", "/v0/storage-profiles/backups", "", 200); got != out {
		t.Errorf("show = %s, want the create output %s", got, out)
	}
	e.same([]string{"storage-profile", "show", created.ID}, "GET", "/v0/storage-profiles/"+created.ID, "", 200)
	e.same([]string{"storage-profile", "list"}, "GET", "/v0/storage-profiles", "", 200)

	// Create a second one over HTTP; the CLI's list sees it.
	status, body := e.http("POST", "/v0/storage-profiles",
		`{"name":"backups-2","endpoint":"`+fake.URL+`","bucket":"assets","access_key_id":"`+
			fake.AccessKeyID+`","secret_access_key":"`+secret+`"}`)
	if status != http.StatusCreated {
		t.Fatalf("POST storage-profiles: status %d body %s", status, body)
	}
	var remote service.StorageProfile
	if err := json.Unmarshal([]byte(body), &remote); err != nil {
		t.Fatal(err)
	}
	if remote.Name != "backups-2" || remote.Region != "us-east-1" || !remote.PathStyle || remote.TestedAt == nil {
		t.Errorf("remote profile = %+v", remote)
	}
	list := e.same([]string{"storage-profile", "list"}, "GET", "/v0/storage-profiles", "", 200)
	if !strings.Contains(list, `"name":"backups"`) || !strings.Contains(list, `"secret_last4"`) || strings.Contains(list, secret) {
		t.Errorf("list = %s", list)
	}

	// Update through the CLI; the same row comes back from HTTP.
	updated, errOut, code := e.cli("storage-profile", "update", "backups", "--name", "backups-1")
	if code != 0 {
		t.Fatalf("storage-profile update: exit %d: %s", code, errOut)
	}
	if got := e.same([]string{"storage-profile", "show", "backups-1"}, "GET", "/v0/storage-profiles/backups-1", "", 200); got != updated {
		t.Errorf("show after update = %s, want the update output %s", got, updated)
	}
	if !strings.Contains(updated, `"version":1`) {
		t.Errorf("a name-only update bumped the version: %s", updated)
	}
	status, body = e.http("PATCH", "/v0/storage-profiles/backups-2", `{"prefix":"clusters/eu"}`)
	if status != http.StatusOK {
		t.Fatalf("PATCH storage-profile: status %d body %s", status, body)
	}
	if !strings.Contains(body, `"prefix":"clusters/eu"`) || !strings.Contains(body, `"version":2`) {
		t.Errorf("prefix update = %s", body)
	}

	// Test: a failing test shows in test_error either way.
	fake.FailNext("GET", "clusters/lewbox-test/", 500, "InternalError", 1)
	tested, errOut, code := e.cli("storage-profile", "test", "backups-1")
	if code != 0 {
		t.Fatalf("storage-profile test: exit %d: %s", code, errOut)
	}
	if !strings.Contains(tested, `"test_error"`) || strings.Contains(tested, `"test_error":""`) {
		t.Errorf("failed test = %s", tested)
	}
	status, body = e.http("POST", "/v0/storage-profiles/backups-2/test", "")
	if status != http.StatusOK {
		t.Fatalf("POST storage-profile test: status %d body %s", status, body)
	}
	e.same([]string{"storage-profile", "show", "backups-1"}, "GET", "/v0/storage-profiles/backups-1", "", 200)

	// Credentials are the only response with the secret, and agree.
	creds := e.same([]string{"storage-profile", "credentials", "backups-1"}, "POST", "/v0/storage-profiles/backups-1/credentials", "", 200)
	if !strings.Contains(creds, secret) {
		t.Errorf("credentials = %s", creds)
	}

	// Delete through the CLI, then through HTTP.
	deleted, errOut, code := e.cli("storage-profile", "delete", "backups-1", "--yes")
	if code != 0 || !strings.Contains(deleted, `"name":"backups-1"`) {
		t.Fatalf("storage-profile delete: exit %d stdout %s stderr %s", code, deleted, errOut)
	}
	e.sameError([]string{"storage-profile", "show", "backups-1"}, "GET", "/v0/storage-profiles/backups-1", "", 404)
	if status, body := e.http("DELETE", "/v0/storage-profiles/backups-2", ""); status != http.StatusOK {
		t.Fatalf("DELETE storage-profile: status %d body %s", status, body)
	}
	if got := e.same([]string{"storage-profile", "list"}, "GET", "/v0/storage-profiles", "", 200); got != `{"data":[]}`+"\n" {
		t.Errorf("list after delete = %q", got)
	}

	// A bucket that does not exist fails the test as a precondition, and
	// nothing is saved.
	badOut, badErr, badCode := e.cliIn(secret+"\n", "storage-profile", "create", "backups-bad",
		"--endpoint", fake.URL, "--bucket", "missing", "--region", "us-east-1",
		"--access-key-id", fake.AccessKeyID, "--secret-access-key-stdin")
	if badCode != 1 || badOut != "" {
		t.Fatalf("storage-profile create with a missing bucket: exit %d stdout %q", badCode, badOut)
	}
	status, body = e.http("POST", "/v0/storage-profiles",
		`{"name":"backups-bad","endpoint":"`+fake.URL+`","bucket":"missing","access_key_id":"`+
			fake.AccessKeyID+`","secret_access_key":"`+secret+`"}`)
	if status != http.StatusPreconditionFailed || badErr != body {
		t.Errorf("missing bucket error = %d %q, want 412 %q", status, badErr, body)
	}
}

func TestClusterStorageSettingsAndNodeSettings(t *testing.T) {
	storage := objstoretest.New(t)
	storage.AddBucket("assets")
	const secret = "objstore_node_settings_parity_secret"
	e := newEnv(t)

	if _, errOut, code := e.cliIn(secret+"\n", "storage-profile", "create", "s3-main",
		"--endpoint", storage.URL, "--region", "us-east-1", "--bucket", "assets",
		"--prefix", "lewbox", "--access-key-id", storage.AccessKeyID, "--secret-access-key-stdin"); code != 0 {
		t.Fatalf("storage-profile create: exit %d: %s", code, errOut)
	}

	fake := aerolvmtest.New(t)
	const token = "avm_admin_storage_parity_token"
	fake.AdminToken = token
	fake.SetMembers([]aerolvm.Member{
		{NodeID: "node-a", NodeName: "node-a", APIURL: fake.URL(), Alive: true, Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}}},
	})
	out, errOut, code := e.cliIn(token+"\n", "cluster", "add", "lab",
		"--admin-url", fake.URL(), "--endpoint-url", fake.URL(), "--admin-token-stdin")
	if code != 0 {
		t.Fatalf("cluster add: exit %d: %s", code, errOut)
	}
	var added service.ClusterDetail
	if err := json.Unmarshal([]byte(out), &added); err != nil {
		t.Fatal(err)
	}
	if added.StorageProfile != nil || added.StopPolicy != "keep" {
		t.Errorf("new cluster = %+v", added.Cluster)
	}

	updated := e.same([]string{"cluster", "set", "lab", "--storage-profile", "s3-main", "--stop-policy", "archive"},
		"PATCH", "/v0/clusters/lab", `{"storage_profile":"s3-main","stop_policy":"archive"}`, 200)
	if !strings.Contains(updated, `"storage_profile":"s3-main"`) || !strings.Contains(updated, `"stop_policy":"archive"`) {
		t.Errorf("cluster set = %s", updated)
	}
	e.same([]string{"cluster", "get", "lab"}, "GET", "/v0/clusters/lab", "", 200)
	e.same([]string{"cluster", "set", "lab", "--stop-policy", "keep"},
		"PATCH", "/v0/clusters/lab", `{"stop_policy":"keep"}`, 200)

	// The node settings carry the bucket secret and the cluster's prefix.
	settings := e.same([]string{"cluster", "node-settings", "lab"}, "GET", "/v0/clusters/lab/node-settings", "", 200)
	for _, want := range []string{
		`"cluster":"lab"`, `"stop_policy":"keep"`, `"profile":"s3-main"`,
		`"root_directory":"/lewbox/clusters/` + added.ID + `"`, secret,
	} {
		if !strings.Contains(settings, want) {
			t.Errorf("node settings %s is missing %q", settings, want)
		}
	}
	if !strings.Contains(settings, `"dns":null`) {
		t.Errorf("node settings have DNS without a profile: %s", settings)
	}

	// Clearing the profile gives a cluster with none.
	cleared := e.same([]string{"cluster", "set", "lab", "--storage-profile", "none"},
		"PATCH", "/v0/clusters/lab", `{"storage_profile":"none"}`, 200)
	if !strings.Contains(cleared, `"storage_profile":null`) {
		t.Errorf("cleared cluster = %s", cleared)
	}
	e.same([]string{"cluster", "node-settings", "lab"}, "GET", "/v0/clusters/lab/node-settings", "", 200)
}

func TestLoginWithAdminKey(t *testing.T) {
	e := newEnv(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, errOut, code := e.cli("org", "create", "acme"); code != 0 {
		t.Fatalf("org create: %s", errOut)
	}
	out, errOut, code := e.cli("admin-key", "create", "cli", "--org", "acme", "--never-expires")
	if code != 0 {
		t.Fatalf("admin-key create: %s", errOut)
	}
	var key service.NewAdminKey
	if err := json.Unmarshal([]byte(out), &key); err != nil {
		t.Fatal(err)
	}

	// Remote commands: no --context local, so the saved context is used.
	remote := func(input string, args ...string) (string, string, int) {
		var stdout, stderr bytes.Buffer
		code := runWithInput(context.Background(), append([]string{"--json"}, args...), strings.NewReader(input), &stdout, &stderr)
		return stdout.String(), stderr.String(), code
	}
	if out, errOut, code := remote(key.Token+"\n", "login", e.srv.URL, "--key-stdin", "--context-name", "acme-key"); code != 0 || !strings.Contains(out, `"via":"admin_key"`) {
		t.Fatalf("login --key-stdin: exit %d stdout %s stderr %s", code, out, errOut)
	}
	// The key's organization became the context's default.
	if out, errOut, code := remote("", "member", "list"); code != 0 || out != `{"data":[]}`+"\n" {
		t.Fatalf("member list: exit %d stdout %s stderr %s", code, out, errOut)
	}
	if _, errOut, code := remote("", "cluster", "list"); code != 1 || !strings.Contains(errOut, "admin key") {
		t.Errorf("cluster list with a key: exit %d stderr %s", code, errOut)
	}
	if _, errOut, code := remote("", "org", "use", "missing"); code != 1 || !strings.Contains(errOut, "not_found") {
		t.Errorf("org use missing: exit %d stderr %s", code, errOut)
	}
	if _, _, code := e.cli("org", "use", "acme"); code != 2 {
		t.Errorf("org use in local mode: exit %d, want 2", code)
	}
}

func TestAccessTokenParity(t *testing.T) {
	e := newEnv(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// Local mode has no account of its own, so it names one.
	if _, errOut, code := e.cli("token", "create", "ci"); code != 1 || !strings.Contains(errOut, "--user") {
		t.Errorf("token create without --user in local mode: exit %d stderr %s", code, errOut)
	}
	out, errOut, code := e.cli("token", "create", "ci", "--user", adminEmail, "--expires-in", "30d")
	if code != 0 {
		t.Fatalf("token create: %s", errOut)
	}
	var tok service.NewAccessToken
	if err := json.Unmarshal([]byte(out), &tok); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok.Token, "lbp_") || tok.ExpiresAt == nil {
		t.Fatalf("token = %+v", tok)
	}
	list := e.same([]string{"token", "list", "--user", adminEmail}, "GET", "/v0/access-tokens", "", 200)
	if strings.Contains(list, tok.Token) || strings.Contains(list, `"token"`) {
		t.Errorf("the token list shows the token: %s", list)
	}
	e.same([]string{"token", "list", "--user", adminEmail}, "GET", "/v0/access-tokens?user="+adminEmail, "", 200)
	e.sameError([]string{"token", "list", "--user", "nobody@example.com"}, "GET", "/v0/access-tokens?user=nobody@example.com", "", 404)
	e.sameError([]string{"token", "revoke", "missing", "--user", adminEmail, "--yes"}, "DELETE", "/v0/access-tokens/missing", "", 404)

	// The admin's token manages the instance over HTTP.
	if status, body := e.httpWith("GET", "/v0/nodes", "", tok.Token); status != 200 {
		t.Errorf("nodes with an admin's token: %d %s", status, body)
	}
	if status, body := e.httpWith("POST", "/v0/access-tokens", `{"name":"more"}`, tok.Token); status != 403 {
		t.Errorf("a token creates a token: %d %s", status, body)
	}

	if _, errOut, code := e.cli("token", "revoke", "ci", "--user", adminEmail, "--yes"); code != 0 {
		t.Fatalf("token revoke: %s", errOut)
	}
	if status, _ := e.httpWith("GET", "/v0/nodes", "", tok.Token); status != 401 {
		t.Errorf("revoked token: status %d", status)
	}
	e.same([]string{"token", "list", "--user", adminEmail}, "GET", "/v0/access-tokens", "", 200)
}

func TestLoginWithAccessToken(t *testing.T) {
	e := newEnv(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	out, errOut, code := e.cli("token", "create", "laptop", "--user", adminEmail, "--never-expires")
	if code != 0 {
		t.Fatalf("token create: %s", errOut)
	}
	var tok service.NewAccessToken
	if err := json.Unmarshal([]byte(out), &tok); err != nil {
		t.Fatal(err)
	}

	// Remote commands: no --context local, so the saved context is used.
	remote := func(input string, args ...string) (string, string, int) {
		var stdout, stderr bytes.Buffer
		code := runWithInput(context.Background(), append([]string{"--json"}, args...), strings.NewReader(input), &stdout, &stderr)
		return stdout.String(), stderr.String(), code
	}

	// --token alone reads stdin.
	out, errOut, code = remote(tok.Token+"\n", "login", e.srv.URL, "--token", "--context-name", "ops")
	if code != 0 || !strings.Contains(out, `"via":"access_token"`) || strings.Contains(out, tok.Token) {
		t.Fatalf("login --token: exit %d stdout %s stderr %s", code, out, errOut)
	}
	if strings.Contains(errOut, "warning") {
		t.Errorf("piped token warned: %s", errOut)
	}
	// An instance admin's token runs operator commands.
	if out, errOut, code := remote("", "cluster", "list"); code != 0 || out != `{"data":[]}`+"\n" {
		t.Errorf("cluster list with a token: exit %d stdout %s stderr %s", code, out, errOut)
	}
	if out, errOut, code := remote("", "whoami"); code != 0 || !strings.Contains(out, `"name":"laptop"`) {
		t.Errorf("whoami: exit %d stdout %s stderr %s", code, out, errOut)
	}
	if _, errOut, code := remote("", "token", "create", "more"); code != 1 || !strings.Contains(errOut, "cannot create another") {
		t.Errorf("token create with a token: exit %d stderr %s", code, errOut)
	}

	// Logout forgets the token and says it still works.
	if _, errOut, code := remote("", "logout"); code != 0 || !strings.Contains(errOut, "still works") {
		t.Errorf("logout: exit %d stderr %s", code, errOut)
	}
	if status, _ := e.httpWith("GET", "/v0/me", "", tok.Token); status != 200 {
		t.Errorf("token after logout: status %d", status)
	}

	// --token=<token> works, with a warning about shell history.
	out, errOut, code = remote("", "login", e.srv.URL, "--token="+tok.Token, "--context-name", "ops")
	if code != 0 || !strings.Contains(out, `"via":"access_token"`) || !strings.Contains(errOut, "shell history") {
		t.Fatalf("login --token=: exit %d stdout %s stderr %s", code, out, errOut)
	}

	// A token after a space would be taken as the URL: refuse it.
	for _, args := range [][]string{
		{"login", e.srv.URL, "--token", tok.Token},
		{"login", "--token", tok.Token},
	} {
		if _, errOut, code := remote("", args...); code != 2 || !strings.Contains(errOut, "looks like a token") {
			t.Errorf("lewbox %v: exit %d stderr %s", args[:2], code, errOut)
		}
	}
	for _, args := range [][]string{
		{"login", e.srv.URL, "--token", "--email", adminEmail},
		{"login", e.srv.URL, "--token", "--password-stdin"},
		{"login", e.srv.URL, "--token", "--key-stdin"},
	} {
		if _, _, code := remote(tok.Token, args...); code != 2 {
			t.Errorf("lewbox %v: exit %d, want 2", args, code)
		}
	}
	if _, errOut, code := remote("", "login", e.srv.URL, "--token"); code != 1 || !strings.Contains(errOut, "no token") {
		t.Errorf("login --token with empty stdin: exit %d stderr %s", code, errOut)
	}
	if _, errOut, code := remote("lbp_nope\n", "login", e.srv.URL, "--token"); code != 1 || !strings.Contains(errOut, "unauthorized") {
		t.Errorf("login with a bad token: exit %d stderr %s", code, errOut)
	}
}
