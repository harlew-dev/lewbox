package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/harlew-dev/lewbox/internal/cliconfig"
	"github.com/harlew-dev/lewbox/internal/db"
	"github.com/harlew-dev/lewbox/internal/httpapi"
	"github.com/harlew-dev/lewbox/internal/secrets"
	"github.com/harlew-dev/lewbox/internal/service"
)

// cliTest isolates the CLI's config directory and runs one command.
func cliTest(t *testing.T, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LEWBOX_CONTEXT", "")
	t.Setenv("LEWBOX_API_URL", "")
	t.Setenv("LEWBOX_TOKEN", "")
	t.Setenv("LEWBOX_OUTPUT", "")
	var out, errOut bytes.Buffer
	code = runWithInput(context.Background(), args, strings.NewReader(stdin), &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestLoginWhoamiNodeListLogout(t *testing.T) {
	ctx := context.Background()
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("LEWBOX_CONTEXT", "")
	t.Setenv("LEWBOX_API_URL", "")
	t.Setenv("LEWBOX_TOKEN", "")
	t.Setenv("LEWBOX_OUTPUT", "")

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "lewbox.db")
	conn, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	keyring, err := secrets.LoadOrCreate(filepath.Join(dir, "keyring.key"))
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(db.New(conn), service.WithKeyring(keyring))
	srv := httptest.NewServer(httpapi.NewRouter(svc, slog.New(slog.DiscardHandler)))
	t.Cleanup(srv.Close)

	if _, err := svc.BootstrapAdmin(ctx, adminEmail, adminPassword); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddNode(service.WithPrincipal(ctx, service.LocalPrincipal()), service.AddNodeRequest{
		Name: "lab-1",
		URL:  "https://lab-1.example:8443",
	}); err != nil {
		t.Fatal(err)
	}

	run := func(stdin string, args ...string) (string, string, int) {
		var out, errOut bytes.Buffer
		code := runWithInput(ctx, args, strings.NewReader(stdin), &out, &errOut)
		return out.String(), errOut.String(), code
	}

	out, errOut, code := run(adminPassword+"\n", "--db", dbPath,
		"login", srv.URL, "--email", adminEmail, "--password-stdin", "--context-name", "dev", "--json")
	if code != 0 {
		t.Fatalf("login: exit %d: %s", code, errOut)
	}
	var login map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &login); err != nil {
		t.Fatal(err)
	}
	if _, ok := login["token"]; ok {
		t.Errorf("login printed the token: %s", out)
	}
	if string(login["context"]) != `"dev"` {
		t.Errorf("login context = %s", login["context"])
	}

	out, errOut, code = run("", "--db", dbPath, "whoami", "--json")
	if code != 0 {
		t.Fatalf("whoami: exit %d: %s", code, errOut)
	}
	var who whoamiOutput
	if err := json.Unmarshal([]byte(out), &who); err != nil {
		t.Fatal(err)
	}
	if who.Context != "dev" || who.APIURL != srv.URL || who.User == nil || who.User.Email != adminEmail {
		t.Errorf("whoami = %+v", who)
	}

	out, errOut, code = run("", "--db", dbPath, "node", "list", "--json")
	if code != 0 {
		t.Fatalf("node list: exit %d: %s", code, errOut)
	}
	var nodes service.NodeList
	if err := json.Unmarshal([]byte(out), &nodes); err != nil {
		t.Fatal(err)
	}
	if len(nodes.Data) != 1 || nodes.Data[0].Name != "lab-1" {
		t.Errorf("nodes = %+v", nodes.Data)
	}

	out, errOut, code = run("", "--db", dbPath, "logout", "--json")
	if code != 0 {
		t.Fatalf("logout: exit %d: %s", code, errOut)
	}
	store, err := cliconfig.Load(filepath.Join(cfgDir, "lewbox"))
	if err != nil {
		t.Fatal(err)
	}
	if store.Token("dev") != "" {
		t.Errorf("logout kept the token")
	}
	if _, err := store.Context("dev"); !err {
		t.Error("logout removed the context itself")
	}

	if _, errOut, code = run("", "--db", dbPath, "whoami", "--json"); code != 1 {
		t.Fatalf("whoami after logout: exit %d: %s", code, errOut)
	}
}

func TestUsageErrors(t *testing.T) {
	t.Run("unknown flag", func(t *testing.T) {
		_, errOut, code := cliTest(t, "", "--nope")
		if code != 2 || !strings.Contains(errOut, "unknown flag") {
			t.Errorf("exit %d stderr %q", code, errOut)
		}
	})
	t.Run("port and listen", func(t *testing.T) {
		_, errOut, code := cliTest(t, "", "serve", "--port", "1", "--listen", ":2")
		if code != 2 || !strings.Contains(errOut, "--port and --listen") {
			t.Errorf("exit %d stderr %q", code, errOut)
		}
	})
	t.Run("remove without yes", func(t *testing.T) {
		e := newEnv(t)
		if _, errOut, code := e.cli("node", "add", "lab-1", "--url", "https://lab-1.example:8443"); code != 0 {
			t.Fatalf("node add: exit %d: %s", code, errOut)
		}
		_, errOut, code := e.cli("node", "remove", "lab-1")
		if code != 2 || !strings.Contains(errOut, "pass --yes to confirm") {
			t.Errorf("exit %d stderr %q", code, errOut)
		}
	})
}

func TestTextRendering(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	alive := true
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{
			name: "nodes",
			value: service.NodeList{Data: []service.Node{{
				Name: "lab-1", Cluster: "lab", Role: "mixed", Version: "v0.7.0",
				Alive: &alive, URL: "https://lab-1.example:8443",
			}}},
			want: "NAME   CLUSTER  ROLE   VERSION  ALIVE  URL\n" +
				"lab-1  lab      mixed  v0.7.0   yes    https://lab-1.example:8443\n",
		},
		{
			name: "clusters",
			value: service.ClusterList{Data: []service.Cluster{{
				Slug: "eu-west", Runtime: "docker", Status: "ready", Nodes: 2,
				Version: "v0.7.0", EndpointURL: "https://eu.example",
			}}},
			want: "SLUG     RUNTIME  STATUS  NODES  VERSION  ENDPOINT\n" +
				"eu-west  docker   ready   2      v0.7.0   https://eu.example\n",
		},
		{
			name: "users",
			value: service.UserList{Data: []service.User{{
				Email: "admin@example.com", InstanceAdmin: true, Disabled: false,
				CreatedAt: now.Add(-5 * time.Minute),
			}}},
			want: "EMAIL              ADMIN  DISABLED  CREATED\n" +
				"admin@example.com  yes    no        5m ago\n",
		},
		{
			name: "contexts",
			value: contextList{Data: []contextEntry{
				{Name: "local", Current: false},
				{Name: "dev", APIURL: "https://dev.example", Current: true},
			}},
			want: "NAME   API_URL              CURRENT\n" +
				"local  -                    \n" +
				"dev    https://dev.example  *\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := renderAt(&buf, tc.value, now); err != nil {
				t.Fatal(err)
			}
			if got := buf.String(); got != tc.want {
				t.Errorf("render =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}
