package backend_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/aerolvm/aerolvmtest"
	"github.com/harlew-dev/lewbox/internal/backend"
	"github.com/harlew-dev/lewbox/internal/client"
	"github.com/harlew-dev/lewbox/internal/db"
	"github.com/harlew-dev/lewbox/internal/httpapi"
	"github.com/harlew-dev/lewbox/internal/secrets"
	"github.com/harlew-dev/lewbox/internal/service"
)

const (
	adminEmail    = "admin@example.com"
	adminPassword = "admin-password-1"
	clusterToken  = "avm_admin_parity_token"
)

// harness has one database, one HTTP server and one fake cluster. Both
// backends see the same state, so a local write and a remote read must agree.
type harness struct {
	local  backend.Backend
	remote backend.Backend
	fake   *aerolvmtest.Server
	url    string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	conn, err := db.Open(ctx, filepath.Join(dir, "lewbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	keyring, err := secrets.LoadOrCreate(filepath.Join(dir, "keyring.key"))
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(db.New(conn),
		service.WithKeyring(keyring),
		service.WithAerolVMOptions(aerolvm.WithRetry(0)),
	)
	srv := httptest.NewServer(httpapi.NewRouter(svc, slog.New(slog.DiscardHandler)))
	t.Cleanup(srv.Close)

	local := backend.Local{Svc: svc}
	if _, err := local.CreateUser(ctx, service.CreateUserRequest{
		Email:         adminEmail,
		Password:      adminPassword,
		InstanceAdmin: true,
	}); err != nil {
		t.Fatal(err)
	}

	// Sign in through the remote client, so it covers Login too.
	anonymous := client.New(srv.URL, "", srv.Client())
	login, err := anonymous.Login(ctx, service.LoginRequest{Email: adminEmail, Password: adminPassword})
	if err != nil {
		t.Fatal(err)
	}
	if login.Token == "" {
		t.Fatal("login returned no token")
	}

	fake := aerolvmtest.New(t)
	fake.AdminToken = clusterToken
	fake.SetMembers([]aerolvm.Member{
		{NodeID: "node-a", NodeName: "node-a", APIURL: fake.URL(), Alive: true, Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}}},
		{NodeID: "node-b", NodeName: "node-b", APIURL: "http://node-b.test:21212", Alive: true, Role: "mixed", Version: "v0.7.0", Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}}},
	})

	return &harness{
		local:  local,
		remote: client.New(srv.URL, login.Token, srv.Client()),
		fake:   fake,
		url:    srv.URL,
	}
}

type callFunc func(context.Context, backend.Backend) (any, error)

type step struct {
	name    string
	local   callFunc
	remote  callFunc
	compare bool
	check   func(*testing.T, any)
}

func TestBackendsAgree(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	health := func(ctx context.Context, b backend.Backend) (any, error) {
		v, err := b.Health(ctx)
		return v, err
	}
	listUsers := func(ctx context.Context, b backend.Backend) (any, error) {
		v, err := b.ListUsers(ctx)
		return v, err
	}
	getAdmin := func(ctx context.Context, b backend.Backend) (any, error) {
		v, err := b.GetUser(ctx, adminEmail)
		return v, err
	}
	getSecond := func(ctx context.Context, b backend.Backend) (any, error) {
		v, err := b.GetUser(ctx, "second@example.com")
		return v, err
	}
	listNodes := func(ctx context.Context, b backend.Backend) (any, error) {
		v, err := b.ListNodes(ctx, service.ListNodesRequest{})
		return v, err
	}
	listClusterNodes := func(ctx context.Context, b backend.Backend) (any, error) {
		v, err := b.ListNodes(ctx, service.ListNodesRequest{Cluster: "eu-west"})
		return v, err
	}
	getCluster := func(ctx context.Context, b backend.Backend) (any, error) {
		v, err := b.GetCluster(ctx, "eu-west")
		return v, err
	}
	listClusters := func(ctx context.Context, b backend.Backend) (any, error) {
		v, err := b.ListClusters(ctx)
		return v, err
	}
	resolveCluster := func(ctx context.Context, b backend.Backend) (any, error) {
		v, err := b.ResolveCluster(ctx, "eu-west")
		return v, err
	}
	getExtraNode := func(ctx context.Context, b backend.Backend) (any, error) {
		v, err := b.GetNode(ctx, "extra-node")
		return v, err
	}
	getNodeB := func(ctx context.Context, b backend.Backend) (any, error) {
		v, err := b.GetNode(ctx, "node-b")
		return v, err
	}
	missingNode := func(ctx context.Context, b backend.Backend) (any, error) {
		v, err := b.GetNode(ctx, "missing")
		return v, err
	}
	missingUser := func(ctx context.Context, b backend.Backend) (any, error) {
		v, err := b.GetUser(ctx, "missing@example.com")
		return v, err
	}

	steps := []step{
		{name: "health", local: health, remote: health, compare: true},
		{name: "list users", local: listUsers, remote: listUsers, compare: true},
		{name: "get user", local: getAdmin, remote: getAdmin, compare: true},
		{
			name: "create user",
			local: func(ctx context.Context, b backend.Backend) (any, error) {
				v, err := b.CreateUser(ctx, service.CreateUserRequest{Email: "second@example.com", Password: "second-password-1"})
				return v, err
			},
			remote:  getSecond,
			compare: true,
		},
		{name: "list users after create", local: listUsers, remote: listUsers, compare: true},
		{
			name: "set password",
			local: func(ctx context.Context, b backend.Backend) (any, error) {
				v, err := b.SetPassword(ctx, "second@example.com", service.SetPasswordRequest{Password: "second-password-2"})
				return v, err
			},
			remote:  getSecond,
			compare: true,
		},
		{
			name: "disable user",
			local: func(ctx context.Context, b backend.Backend) (any, error) {
				v, err := b.SetUserDisabled(ctx, "second@example.com", true)
				return v, err
			},
			remote:  getSecond,
			compare: true,
		},
		{
			name: "enable user",
			local: func(ctx context.Context, b backend.Backend) (any, error) {
				v, err := b.SetUserDisabled(ctx, "second@example.com", false)
				return v, err
			},
			remote:  getSecond,
			compare: true,
		},
		{
			name:  "remove user",
			local: getSecond,
			remote: func(ctx context.Context, b backend.Backend) (any, error) {
				v, err := b.RemoveUser(ctx, "second@example.com")
				return v, err
			},
			compare: true,
		},
		{
			name: "add node",
			local: func(ctx context.Context, b backend.Backend) (any, error) {
				v, err := b.AddNode(ctx, service.AddNodeRequest{Name: "extra-node", URL: "https://extra.example:8443/"})
				return v, err
			},
			remote:  getExtraNode,
			compare: true,
		},
		{name: "list nodes", local: listNodes, remote: listNodes, compare: true},
		{name: "get node", local: getExtraNode, remote: getExtraNode, compare: true},
		{
			name:  "remove node",
			local: getExtraNode,
			remote: func(ctx context.Context, b backend.Backend) (any, error) {
				v, err := b.RemoveNode(ctx, "extra-node", service.RemoveNodeRequest{})
				return v, err
			},
			compare: true,
		},
		{
			name: "add cluster",
			local: func(ctx context.Context, b backend.Backend) (any, error) {
				v, err := b.AddCluster(ctx, service.AddClusterRequest{
					Slug:        "eu-west",
					AdminURL:    h.fake.URL(),
					EndpointURL: h.fake.URL(),
					Runtime:     "docker",
					AdminToken:  clusterToken,
				})
				return v, err
			},
			remote:  getCluster,
			compare: true,
		},
		{name: "list clusters", local: listClusters, remote: listClusters, compare: true},
		{name: "get cluster", local: getCluster, remote: getCluster, compare: true},
		{
			name: "check cluster",
			local: func(ctx context.Context, b backend.Backend) (any, error) {
				v, err := b.CheckCluster(ctx, "eu-west")
				return v, err
			},
			remote:  getCluster,
			compare: true,
		},
		{name: "resolve cluster", local: resolveCluster, remote: resolveCluster, compare: true},
		{
			name: "resolve node ref",
			local: func(ctx context.Context, b backend.Backend) (any, error) {
				v, err := b.ResolveCluster(ctx, "node/node-a")
				return v, err
			},
			remote: func(ctx context.Context, b backend.Backend) (any, error) {
				v, err := b.ResolveCluster(ctx, "node/node-a")
				return v, err
			},
			compare: true,
		},
		{
			name: "create join token",
			local: func(ctx context.Context, b backend.Backend) (any, error) {
				v, err := b.CreateJoinToken(ctx, "eu-west", service.JoinTokenRequest{Role: "worker"})
				return v, err
			},
			remote: func(ctx context.Context, b backend.Backend) (any, error) {
				v, err := b.CreateJoinToken(ctx, "eu-west", service.JoinTokenRequest{Role: "worker"})
				return v, err
			},
			check: func(t *testing.T, v any) {
				join, ok := v.(service.JoinToken)
				if !ok {
					t.Fatalf("join token is %T", v)
				}
				if join.Token == "" || !strings.Contains(join.Command, join.Token) {
					t.Errorf("join token = %+v", join)
				}
			},
		},
		{name: "list cluster nodes", local: listClusterNodes, remote: listClusterNodes, compare: true},
		{
			name:  "remove cluster node",
			local: getNodeB,
			remote: func(ctx context.Context, b backend.Backend) (any, error) {
				v, err := b.RemoveNode(ctx, "node-b", service.RemoveNodeRequest{})
				return v, err
			},
			compare: true,
		},
		{
			name:  "remove cluster",
			local: resolveCluster,
			remote: func(ctx context.Context, b backend.Backend) (any, error) {
				v, err := b.RemoveCluster(ctx, "eu-west")
				return v, err
			},
			compare: true,
		},
		{name: "missing node", local: missingNode, remote: missingNode, compare: true},
		{name: "missing user", local: missingUser, remote: missingUser, compare: true},
		{name: "list clusters after remove", local: listClusters, remote: listClusters, compare: true},
	}

	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			localVal, localErr := s.local(ctx, h.local)
			remoteVal, remoteErr := s.remote(ctx, h.remote)
			if s.compare {
				localJSON := resultJSON(t, localVal, localErr)
				remoteJSON := resultJSON(t, remoteVal, remoteErr)
				if localJSON != remoteJSON {
					t.Fatalf("local and remote differ:\nlocal:  %s\nremote: %s", localJSON, remoteJSON)
				}
			} else {
				if localErr != nil {
					t.Fatalf("local: %v", localErr)
				}
				if remoteErr != nil {
					t.Fatalf("remote: %v", remoteErr)
				}
			}
			if s.check != nil {
				s.check(t, localVal)
				s.check(t, remoteVal)
			}
		})
	}
}

// runSteps runs each step against both backends and compares or checks the
// results.
func runSteps(t *testing.T, h *harness, steps []step) {
	t.Helper()
	ctx := context.Background()
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			localVal, localErr := s.local(ctx, h.local)
			remoteVal, remoteErr := s.remote(ctx, h.remote)
			if s.compare {
				localJSON := resultJSON(t, localVal, localErr)
				remoteJSON := resultJSON(t, remoteVal, remoteErr)
				if localJSON != remoteJSON {
					t.Fatalf("local and remote differ:\nlocal:  %s\nremote: %s", localJSON, remoteJSON)
				}
			} else {
				if localErr != nil {
					t.Fatalf("local: %v", localErr)
				}
				if remoteErr != nil {
					t.Fatalf("remote: %v", remoteErr)
				}
			}
			if s.check != nil {
				s.check(t, localVal)
				s.check(t, remoteVal)
			}
		})
	}
}

// both is a step that makes the same call on each backend.
func both(name string, fn callFunc) step {
	return step{name: name, local: fn, remote: fn, compare: true}
}

func TestOrganizationBackendsAgree(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for _, email := range []string{"owner@example.com", "member@example.com"} {
		if _, err := h.local.CreateUser(ctx, service.CreateUserRequest{Email: email, Password: "member-password-1"}); err != nil {
			t.Fatal(err)
		}
	}

	getOrg := func(ctx context.Context, b backend.Backend) (any, error) { return b.GetOrganization(ctx, "acme") }
	listMembers := func(ctx context.Context, b backend.Backend) (any, error) { return b.ListMembers(ctx, "acme") }
	listKeys := func(ctx context.Context, b backend.Backend) (any, error) { return b.ListAdminKeys(ctx, "acme") }
	setMemberRole := func(role string) callFunc {
		return func(ctx context.Context, b backend.Backend) (any, error) {
			return b.SetMemberRole(ctx, "acme", "member@example.com", service.SetMemberRoleRequest{Role: role})
		}
	}
	var token string

	runSteps(t, h, []step{
		{
			name: "create organization",
			local: func(ctx context.Context, b backend.Backend) (any, error) {
				return b.CreateOrganization(ctx, service.CreateOrganizationRequest{Slug: "acme", Name: "Acme", Owner: "owner@example.com"})
			},
			remote:  getOrg,
			compare: true,
		},
		both("list organizations", func(ctx context.Context, b backend.Backend) (any, error) {
			return b.ListOrganizations(ctx, service.ListOrganizationsRequest{})
		}),
		both("list an account's organizations", func(ctx context.Context, b backend.Backend) (any, error) {
			return b.ListOrganizations(ctx, service.ListOrganizationsRequest{User: "owner@example.com"})
		}),
		{
			name: "add member",
			local: func(ctx context.Context, b backend.Backend) (any, error) {
				return b.AddMember(ctx, "acme", service.AddMemberRequest{Email: "member@example.com", Role: "viewer"})
			},
			remote:  setMemberRole("viewer"), // unchanged, so it returns the same member
			compare: true,
		},
		both("list members", listMembers),
		{name: "set member role", local: setMemberRole("admin"), remote: setMemberRole("admin"), compare: true},
		{
			name: "create admin key",
			local: func(ctx context.Context, b backend.Backend) (any, error) {
				k, err := b.CreateAdminKey(ctx, "acme", service.CreateAdminKeyRequest{Name: "backend", ExpiresIn: "30d"})
				token = k.Token
				return k, err
			},
			remote: func(ctx context.Context, b backend.Backend) (any, error) {
				return b.CreateAdminKey(ctx, "acme", service.CreateAdminKeyRequest{Name: "ci"})
			},
			check: func(t *testing.T, v any) {
				k, ok := v.(service.NewAdminKey)
				if !ok || !strings.HasPrefix(k.Token, "lba_") || k.Status != "active" {
					t.Errorf("new key = %+v", v)
				}
			},
		},
		both("list admin keys", listKeys),
		{
			name:  "revoke admin key",
			local: listKeys,
			remote: func(ctx context.Context, b backend.Backend) (any, error) {
				return b.RevokeAdminKey(ctx, "acme", "ci")
			},
			check: func(t *testing.T, v any) {
				if k, ok := v.(service.AdminKey); ok && k.Status != "revoked" {
					t.Errorf("revoked key = %+v", k)
				}
			},
		},
		both("list admin keys after revoke", listKeys),
		{
			name: "rename organization",
			local: func(ctx context.Context, b backend.Backend) (any, error) {
				name := "Acme Inc."
				return b.UpdateOrganization(ctx, "acme", service.UpdateOrganizationRequest{Name: &name})
			},
			remote:  getOrg,
			compare: true,
		},
		{
			name:  "remove member",
			local: listMembers,
			remote: func(ctx context.Context, b backend.Backend) (any, error) {
				return b.RemoveMember(ctx, "acme", "member@example.com")
			},
		},
		both("list members after remove", listMembers),
		both("no organization chosen", func(ctx context.Context, b backend.Backend) (any, error) {
			return b.ListMembers(ctx, "")
		}),
		both("missing organization", func(ctx context.Context, b backend.Backend) (any, error) {
			return b.GetOrganization(ctx, "missing")
		}),
	})

	// The key works over HTTP for its own organization, and only there.
	keyed := client.New(h.url, token, nil)
	me, err := keyed.Me(ctx)
	if err != nil || me.Via != "admin_key" || me.Organization == nil || me.Organization.Slug != "acme" {
		t.Fatalf("me with key = %+v, %v", me, err)
	}
	if org, err := keyed.GetOrganization(ctx, ""); err != nil || org.Slug != "acme" {
		t.Fatalf("own organization with key = %+v, %v", org, err)
	}
	if members, err := keyed.ListMembers(ctx, ""); err != nil || len(members.Data) != 1 {
		t.Fatalf("members with key = %+v, %v", members, err)
	}
	if _, err := keyed.ListClusters(ctx); err == nil || !strings.Contains(err.Error(), "admin key") {
		t.Errorf("clusters with key: %v", err)
	}
	if _, err := keyed.CreateAdminKey(ctx, "", service.CreateAdminKeyRequest{Name: "more"}); err == nil {
		t.Error("an admin key created another key")
	}
}

// resultJSON renders a result the way the CLI would. Error bodies drop
// details, whose field order differs between a service value and decoded JSON.
func resultJSON(t *testing.T, v any, err error) string {
	t.Helper()
	if err != nil {
		se, ok := errors.AsType[*service.Error](err)
		if !ok {
			t.Fatalf("unexpected error: %v", err)
		}
		data, mErr := json.Marshal(map[string]string{"code": string(se.Code), "message": se.Message})
		if mErr != nil {
			t.Fatal(mErr)
		}
		return string(data)
	}
	data, mErr := json.Marshal(v)
	if mErr != nil {
		t.Fatal(mErr)
	}
	return string(data)
}

func TestLocalAuthHasNoSession(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if _, err := h.local.Login(ctx, service.LoginRequest{Email: adminEmail, Password: adminPassword}); err == nil {
		t.Fatal("local login succeeded")
	} else if se, ok := errors.AsType[*service.Error](err); !ok || se.Code != service.CodeInvalid {
		t.Fatalf("local login error = %v", err)
	}
	if _, err := h.local.Logout(ctx); err == nil {
		t.Fatal("local logout succeeded")
	} else if se, ok := errors.AsType[*service.Error](err); !ok || se.Code != service.CodeInvalid {
		t.Fatalf("local logout error = %v", err)
	}
	me, err := h.local.Me(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if me.Via != "local" || me.User != nil {
		t.Errorf("local Me = %+v", me)
	}
}

func TestRemoteSessionAndLogout(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	me, err := h.remote.Me(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if me.Via != "session" || me.User == nil || me.User.Email != adminEmail {
		t.Fatalf("remote Me = %+v", me)
	}
	if _, err := h.remote.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := h.remote.Me(ctx); err == nil {
		t.Fatal("Me still works after logout")
	}
}

// TestPreconditionErrorBytes proves remote errors keep the server's details
// bytes, so local and remote CLIs print the same body.
func TestPreconditionErrorBytes(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	req := service.AddClusterRequest{
		Slug:       "unreachable",
		AdminURL:   "http://127.0.0.1:1",
		Runtime:    "docker",
		AdminToken: "avm_test",
	}
	_, localErr := h.local.AddCluster(ctx, req)
	_, remoteErr := h.remote.AddCluster(ctx, req)
	if localErr == nil || remoteErr == nil {
		t.Fatalf("expected both calls to fail: local %v, remote %v", localErr, remoteErr)
	}
	localBody := errorBody(t, localErr)
	remoteBody := errorBody(t, remoteErr)
	if localBody != remoteBody {
		t.Fatalf("error bodies differ:\nlocal:  %s\nremote: %s", localBody, remoteBody)
	}
}

func errorBody(t *testing.T, err error) string {
	t.Helper()
	var buf bytes.Buffer
	_, body := httpapi.ErrorFrom(err)
	if err := httpapi.Encode(&buf, body); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
