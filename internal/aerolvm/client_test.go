package aerolvm_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/aerolvm/aerolvmtest"
)

const adminToken = "avm_admin_test_token"

func newFake(t *testing.T) *aerolvmtest.Server {
	t.Helper()
	fake := aerolvmtest.New(t)
	fake.AdminToken = adminToken
	return fake
}

func readFixture(t *testing.T, name string, v any) {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func TestFixturesDecode(t *testing.T) {
	var health aerolvm.Health
	readFixture(t, "health.json", &health)
	if health.Status != "ok" || health.Version != "v0.7.0" || health.ClusterNodes != 2 {
		t.Errorf("health = %+v", health)
	}

	var who aerolvm.WhoAmI
	readFixture(t, "whoami.json", &who)
	if who.Role != "admin" || who.Team != "*" || who.Principal.ID != "tok_H4IZwIoxL6t5" || who.ExpiresAt == nil {
		t.Errorf("whoami = %+v", who)
	}

	var members aerolvm.Members
	readFixture(t, "members.json", &members)
	if len(members.Members) != 2 {
		t.Fatalf("members = %+v", members.Members)
	}
	m := members.Members[0]
	if m.NodeID != "proto-a" || !m.Alive || m.Role != "mixed" || !m.Leader || !m.Voter ||
		m.APIURL != "http://172.30.0.11:21212" || m.PublicHost != "172.30.0.11" ||
		m.Version != "v0.7.0" || len(m.Capacity.SupportedRuntimes) != 1 || m.Capacity.SupportedRuntimes[0] != "docker" {
		t.Errorf("member = %+v", m)
	}

	var token aerolvm.Token
	readFixture(t, "token_create.json", &token)
	if token.ID != "tok_zc6jWAcUIWKB" || token.Role != "runner" || token.Team != "fixture" || token.Token == "" || token.ExpiresAt == nil {
		t.Errorf("token = %+v", token)
	}

	var tokens struct {
		Tokens []aerolvm.Token `json:"tokens"`
	}
	readFixture(t, "tokens_list.json", &tokens)
	if len(tokens.Tokens) != 1 || tokens.Tokens[0].Name != "lewbox" {
		t.Errorf("tokens = %+v", tokens.Tokens)
	}

	var join aerolvm.JoinToken
	readFixture(t, "join_token_create.json", &join)
	if join.ID != "8xr414" || join.Role != "server" || join.Reusable || join.CACertHash == "" || len(join.JoinURLs) != 2 || join.ExpiresAt.IsZero() {
		t.Errorf("join = %+v", join)
	}
}

func TestClientMethods(t *testing.T) {
	ctx := context.Background()
	fake := newFake(t)
	fake.SetMembers([]aerolvm.Member{
		{NodeID: "node-a", NodeName: "node-a", APIURL: fake.URL(), Alive: true, Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}}},
		{NodeID: "node-b", NodeName: "node-b", APIURL: fake.URL(), Alive: true, Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}}},
	})
	client := aerolvm.New(fake.URL(), adminToken, aerolvm.WithRetry(0))

	health, err := client.Health(ctx)
	if err != nil || health.Version != "v0.7.0" || health.ClusterNodes != 2 {
		t.Fatalf("Health = %+v, %v", health, err)
	}
	who, err := client.WhoAmI(ctx)
	if err != nil || who.Role != "admin" || who.Team != "*" || who.Principal.ID != "tok_admin" {
		t.Fatalf("WhoAmI = %+v, %v", who, err)
	}
	members, err := client.Members(ctx)
	if err != nil || len(members.Members) != 2 {
		t.Fatalf("Members = %+v, %v", members, err)
	}

	token, err := client.CreateToken(ctx, aerolvm.CreateTokenRequest{Name: "n", Role: "runner", Team: "t", ExpiresIn: "1h"})
	if err != nil || token.ID == "" || token.Token == "" || token.Role != "runner" {
		t.Fatalf("CreateToken = %+v, %v", token, err)
	}
	list, err := client.ListTokens(ctx)
	if err != nil || len(list) != 1 || list[0].ID != token.ID {
		t.Fatalf("ListTokens = %+v, %v", list, err)
	}
	if err := client.RevokeToken(ctx, token.ID); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if got := fake.Revoked(); len(got) != 1 || got[0] != token.ID {
		t.Errorf("revoked = %v", got)
	}

	join, err := client.CreateJoinToken(ctx, aerolvm.JoinTokenRequest{Role: "server", TTL: "15m", Reusable: false})
	if err != nil || join.Token == "" || join.Role != "server" || join.ExpiresAt.IsZero() || len(join.JoinURLs) == 0 {
		t.Fatalf("CreateJoinToken = %+v, %v", join, err)
	}
	if err := client.DrainNode(ctx, "node-a"); err != nil {
		t.Fatalf("DrainNode: %v", err)
	}
	if err := client.DeleteNode(ctx, "node-a", true); err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}
	if got := fake.Drained(); len(got) != 1 || got[0] != "node-a" {
		t.Errorf("drained = %v", got)
	}
	if got := fake.Deleted(); len(got) != 1 || got[0] != "node-a" {
		t.Errorf("deleted = %v", got)
	}
	err = client.DeleteNode(ctx, "missing", false)
	var apiErr *aerolvm.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Fatalf("DeleteNode missing = %v", err)
	}
}

func TestSandboxAndSnapshotMethods(t *testing.T) {
	ctx := context.Background()
	fake := newFake(t)
	admin := aerolvm.New(fake.URL(), adminToken, aerolvm.WithRetry(0))

	token, err := admin.CreateToken(ctx, aerolvm.CreateTokenRequest{Name: "runner", Role: "runner", Team: "team-a", ExpiresIn: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	team := aerolvm.New(fake.URL(), token.Token, aerolvm.WithRetry(0))

	created, err := team.CreateSandbox(ctx, aerolvm.CreateSandboxRequest{
		Image: "ubuntu:22.04", Name: "box", Tags: map[string]string{"a": "b"},
		CPU: 1, MemoryMB: 512, DiskGB: 10, Runtime: "docker",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.ID, "sb-") || created.Status != "started" || created.Name != "box" ||
		created.Tags["a"] != "b" || created.CPU != 1 || created.MemoryMB != 512 || created.DiskGB != 10 || created.Runtime != "docker" {
		t.Fatalf("CreateSandbox = %+v", created)
	}
	got, err := team.GetSandbox(ctx, created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("GetSandbox = %+v, %v", got, err)
	}

	snapshot, err := team.CreateSnapshot(ctx, created.ID, "archive-"+created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Name != "archive-"+created.ID || snapshot.PushState != "pending" ||
		snapshot.SourceSandboxID != created.ID || !strings.HasPrefix(snapshot.Image, "aerolvm-snapshot/") {
		t.Fatalf("CreateSnapshot = %+v", snapshot)
	}
	if _, err := team.CreateSnapshot(ctx, created.ID, snapshot.Name); err == nil {
		t.Fatal("a second snapshot with the same name was accepted")
	} else if apiErr, ok := errors.AsType[*aerolvm.APIError](err); !ok || apiErr.Status != http.StatusConflict {
		t.Errorf("duplicate snapshot = %v", err)
	}
	fetched, err := team.GetSnapshot(ctx, snapshot.Name)
	if err != nil || fetched.Name != snapshot.Name {
		t.Fatalf("GetSnapshot = %+v, %v", fetched, err)
	}

	ref := "127.0.0.1:5000/cluster/c1/snapshots/tenant-x/" + snapshot.Name + ":latest"
	fake.SetSnapshotPush("team-a", snapshot.Name, "active", ref)
	other, err := admin.CreateToken(ctx, aerolvm.CreateTokenRequest{Name: "other", Role: "runner", Team: "team-b", ExpiresIn: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	otherClient := aerolvm.New(fake.URL(), other.Token, aerolvm.WithRetry(0))
	if _, err := otherClient.GetSandbox(ctx, created.ID); err == nil {
		t.Fatal("another team saw the sandbox")
	}
	fetched, err = team.GetSnapshot(ctx, snapshot.Name)
	if err != nil || fetched.ImageRegistryRef != ref {
		t.Fatalf("pushed snapshot = %+v, %v", fetched, err)
	}
	_, err = otherClient.CreateSandbox(ctx, aerolvm.CreateSandboxRequest{Image: ref})
	if err == nil {
		t.Fatal("another team restored from the snapshot ref")
	} else if apiErr, ok := errors.AsType[*aerolvm.APIError](err); !ok || apiErr.Status != http.StatusBadRequest || !strings.Contains(apiErr.Message, "sandbox not found") {
		t.Errorf("another team's restore = %v", err)
	}

	_, err = team.GetSnapshot(ctx, "missing")
	if apiErr, ok := errors.AsType[*aerolvm.APIError](err); !ok || apiErr.Status != http.StatusNotFound {
		t.Errorf("missing snapshot = %v", err)
	}
	_, err = team.GetSandbox(ctx, "missing")
	if apiErr, ok := errors.AsType[*aerolvm.APIError](err); !ok || apiErr.Status != http.StatusNotFound {
		t.Errorf("missing sandbox = %v", err)
	}
}

func TestNodeCertExpiry(t *testing.T) {
	ctx := context.Background()
	fake := newFake(t)
	client := aerolvm.New(fake.URL(), adminToken, aerolvm.WithRetry(0))

	expires := time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC)
	fake.SetCertExpiry(expires)
	at, ok, err := client.NodeCertExpiry(ctx)
	if err != nil || !ok || !at.Equal(expires) {
		t.Fatalf("NodeCertExpiry = %v, %v, %v", at, ok, err)
	}

	fake.SetCertExpiry(time.Time{})
	if _, ok, err := client.NodeCertExpiry(ctx); err != nil || ok {
		t.Fatalf("NodeCertExpiry without a certificate = %v, %v", ok, err)
	}

	// Prometheus writes large gauges in scientific notation.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "aerolvm_cluster_mtls_cert_not_after_unix 1.7648928e+09\n")
	}))
	defer srv.Close()
	at, ok, err = aerolvm.New(srv.URL, adminToken, aerolvm.WithRetry(0)).NodeCertExpiry(ctx)
	if err != nil || !ok || !at.Equal(time.Unix(1764892800, 0).UTC()) {
		t.Fatalf("NodeCertExpiry scientific = %v, %v, %v", at, ok, err)
	}
}

func TestErrorParsing(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantStatus int
		wantMsg    string
	}{
		{"reason wins", 401, `{"error":"unauthorized","reason":"malformed"}`, 401, "malformed"},
		{"error alone", 400, `{"error":"expires_in exceeds SB_TOKEN_MAX_TTL"}`, 400, "expires_in exceeds SB_TOKEN_MAX_TTL"},
		{"raw body", 500, "not json at all", 500, "not json at all"},
		{"empty body", 418, "", 418, "I'm a teapot"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFake(t)
			fake.FailNext("/v1/whoami", tc.status, tc.body, 1)
			_, err := aerolvm.New(fake.URL(), adminToken, aerolvm.WithRetry(0)).WhoAmI(context.Background())
			var apiErr *aerolvm.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want *APIError", err)
			}
			if apiErr.Status != tc.wantStatus || apiErr.Message != tc.wantMsg {
				t.Errorf("APIError = %+v, want status %d message %q", apiErr, tc.wantStatus, tc.wantMsg)
			}
		})
	}
}

func TestUnauthorized(t *testing.T) {
	fake := newFake(t)
	client := aerolvm.New(fake.URL(), "wrong", aerolvm.WithRetry(0))
	_, err := client.WhoAmI(context.Background())
	var apiErr *aerolvm.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized || apiErr.Message != "unauthorized" {
		t.Fatalf("WhoAmI with a bad token = %v", err)
	}
	// Health is public.
	if _, err := client.Health(context.Background()); err != nil {
		t.Errorf("Health with a bad token: %v", err)
	}
}

func TestRetrySucceeds(t *testing.T) {
	fake := newFake(t)
	fake.FailNext("/v1/tokens", http.StatusServiceUnavailable,
		`{"error":"the token store did not confirm the write; not raft leader"}`, 2)

	var sleeps []time.Duration
	client := aerolvm.New(fake.URL(), adminToken,
		aerolvm.WithRetry(time.Minute),
		aerolvm.WithSleep(func(d time.Duration) { sleeps = append(sleeps, d) }))
	token, err := client.CreateToken(context.Background(), aerolvm.CreateTokenRequest{Name: "n", Role: "runner", Team: "t", ExpiresIn: "1h"})
	if err != nil || token.ID == "" {
		t.Fatalf("CreateToken = %+v, %v", token, err)
	}
	if len(sleeps) != 2 || sleeps[0] != 250*time.Millisecond || sleeps[1] != 500*time.Millisecond {
		t.Errorf("sleeps = %v", sleeps)
	}
}

func TestNoRetryWhenNotEnabled(t *testing.T) {
	fake := newFake(t)
	fake.SetClusterMode(false)
	var sleeps int
	client := aerolvm.New(fake.URL(), adminToken,
		aerolvm.WithRetry(time.Minute),
		aerolvm.WithSleep(func(time.Duration) { sleeps++ }))
	_, err := client.Members(context.Background())
	var apiErr *aerolvm.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusServiceUnavailable || !strings.Contains(apiErr.Message, "not enabled") {
		t.Fatalf("Members = %v", err)
	}
	if sleeps != 0 {
		t.Errorf("retried %d times, want 0", sleeps)
	}
}

func TestRetryBudget(t *testing.T) {
	fake := newFake(t)
	fake.FailNext("/v1/tokens", http.StatusServiceUnavailable, `{"error":"no raft leader"}`, 100)
	var sleeps []time.Duration
	client := aerolvm.New(fake.URL(), adminToken,
		aerolvm.WithRetry(time.Second),
		aerolvm.WithSleep(func(d time.Duration) { sleeps = append(sleeps, d) }))
	_, err := client.CreateToken(context.Background(), aerolvm.CreateTokenRequest{Name: "n", Role: "runner", Team: "t", ExpiresIn: "1h"})
	var apiErr *aerolvm.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("CreateToken = %v", err)
	}
	// 250ms then 500ms fit in one second; the next 1s wait does not.
	if len(sleeps) != 2 {
		t.Errorf("sleeps = %v, want 2 waits", sleeps)
	}
}

func TestTransportErrorIsWrapped(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	client := aerolvm.New(url, "", aerolvm.WithRetry(0))
	_, err := client.Health(context.Background())
	if err == nil || !strings.Contains(err.Error(), "aerolvm GET /health:") {
		t.Fatalf("Health = %v, want a wrapped transport error", err)
	}
	if strings.Contains(err.Error(), "Bearer") {
		t.Errorf("error mentions the token: %v", err)
	}
}

// A Firecracker stop writes the VM's memory to disk and can take longer than
// the default call timeout, so a caller's own deadline replaces it.
func TestCallTimeoutDefaultsOnlyWithoutDeadline(t *testing.T) {
	fake := newFake(t)
	fake.AddSandbox("t1", aerolvm.Sandbox{ID: "sb-1", Status: "started"})
	fake.Delay("/v1/sandboxes/sb-1/stop", 300*time.Millisecond)
	client := aerolvm.New(fake.URL(), adminToken, aerolvm.WithRetry(0), aerolvm.WithCallTimeout(50*time.Millisecond))

	if err := client.StopSandbox(context.Background(), "sb-1"); err == nil {
		t.Fatal("a slow call with no deadline was not cut at the default timeout")
	}
	if sb, _ := fake.Sandbox("sb-1"); sb.Status != "started" {
		t.Fatalf("the abandoned stop was applied: status %q", sb.Status)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.StopSandbox(ctx, "sb-1"); err != nil {
		t.Fatalf("a slow call within the caller's deadline failed: %v", err)
	}
	if sb, _ := fake.Sandbox("sb-1"); sb.Status != "stopped" {
		t.Fatalf("status after the stop = %q, want stopped", sb.Status)
	}
}
