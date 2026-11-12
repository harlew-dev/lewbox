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
	"github.com/harlew-dev/lewbox/internal/httpapi"
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
	srv := httptest.NewServer(httpapi.NewRouter(service.New(db.New(conn), service.WithKeyring(keyring)), slog.New(slog.DiscardHandler)))
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
	if status, out := e.http("GET", "/v0/clusters/resolve/cluster/eu-west", ""); status != http.StatusOK {
		t.Fatalf("GET resolve cluster/x: status %d body %s", status, out)
	}
	if status, out := e.http("GET", "/v0/clusters/resolve/node/node-a", ""); status != http.StatusOK {
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

func TestPlansSettingsAndSignup(t *testing.T) {
	e := newEnv(t)

	// Settings read and write the same either way.
	e.same([]string{"settings"}, "GET", "/v0/settings", "", 200)
	e.same([]string{"settings", "set", "--signup", "approval"},
		"PATCH", "/v0/settings", `{"signup":"approval"}`, 200)
	e.same([]string{"settings", "set", "--name", "Acme Sandboxes", "--default-plan", "free"},
		"PATCH", "/v0/settings", `{"instance_name":"Acme Sandboxes","default_plan":"free"}`, 200)

	// A sign-up waits for approval, then the operator approves it.
	status, body := e.httpWith("POST", "/v0/auth/signup",
		`{"email":"new@example.com","password":"new-password-1","organization":"New Co"}`, "")
	if status != http.StatusOK {
		t.Fatalf("signup: status %d body %s", status, body)
	}
	pending := e.same([]string{"org", "list", "--status", "pending"},
		"GET", "/v0/organizations?status=pending", "", 200)
	var list service.OrganizationList
	if err := json.Unmarshal([]byte(pending), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || list.Data[0].Status != "pending" || list.Data[0].Plan == nil || list.Data[0].Plan.Slug != "free" {
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

	// A plan an organization uses, and an organization's limits.
	if _, errOut, code := e.cli("plan", "create", "pro", "--max-sandboxes", "50"); code != 0 {
		t.Fatalf("plan create pro: %s", errOut)
	}
	created, errOut, code := e.cli("org", "create", "acme", "--plan", "free")
	if code != 0 {
		t.Fatalf("org create: %s", errOut)
	}
	if got := e.same([]string{"org", "get", "acme"}, "GET", "/v0/organizations/acme", "", 200); got != created {
		t.Errorf("org get = %s, want the create output %s", got, created)
	}
	updated := e.same([]string{"org", "update", "acme", "--plan", "pro", "--max-sandboxes", "10"},
		"PATCH", "/v0/organizations/acme", `{"plan":"pro","max_sandboxes":10}`, 200)
	if !strings.Contains(updated, `"slug":"pro"`) || !strings.Contains(updated, `"sandboxes":10`) || !strings.Contains(updated, `"team":"lbx-`) {
		t.Errorf("updated organization = %s", updated)
	}
	e.same([]string{"org", "update", "acme", "--max-sandboxes", "plan"},
		"PATCH", "/v0/organizations/acme", `{"max_sandboxes":null}`, 200)
	if _, errOut, code := e.cli("org", "update", "acme", "--max-sandboxes", "unlimited"); code != 2 || !strings.Contains(errOut, "plan without that limit") {
		t.Errorf("org update unlimited: exit %d stderr %s", code, errOut)
	}
	if _, errOut, code := e.cli("plan", "update", "pro", "--max-sandboxes", "0"); code != 2 || !strings.Contains(errOut, "positive") {
		t.Errorf("plan update zero: exit %d stderr %s", code, errOut)
	}
	e.same([]string{"org", "list", "--status", "active"}, "GET", "/v0/organizations?status=active", "", 200)
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
