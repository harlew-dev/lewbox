package cliconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Load(filepath.Join(t.TempDir(), "lewbox"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Add("prod", Context{APIURL: "https://prod.example"}, "lbs_prod"); err != nil {
		t.Fatal(err)
	}
	if err := s.Add("dev", Context{APIURL: "https://dev.example"}, "lbs_dev"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCurrent("prod"); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSelectPrecedence(t *testing.T) {
	s := newStore(t)
	cases := []struct {
		name string
		opts Options
		want Selection
	}{
		{
			name: "context beats api url",
			opts: Options{Context: "dev", APIURL: "https://other.example"},
			want: Selection{Mode: ModeRemote, Context: "dev", APIURL: "https://dev.example", Token: "lbs_dev"},
		},
		{
			name: "built-in local",
			opts: Options{Context: LocalContext, APIURL: "https://other.example"},
			want: Selection{Mode: ModeLocal, Context: LocalContext},
		},
		{
			name: "api url reuses the saved token",
			opts: Options{APIURL: "https://prod.example/"},
			want: Selection{Mode: ModeRemote, Context: "prod", APIURL: "https://prod.example", Token: "lbs_prod"},
		},
		{
			name: "api url with an explicit token",
			opts: Options{APIURL: "https://new.example", Token: "lbs_env"},
			want: Selection{Mode: ModeRemote, Context: "new.example", APIURL: "https://new.example", Token: "lbs_env"},
		},
		{
			name: "unknown api url has no token",
			opts: Options{APIURL: "https://new.example"},
			want: Selection{Mode: ModeRemote, Context: "new.example", APIURL: "https://new.example"},
		},
		{name: "current context", opts: Options{}, want: Selection{Mode: ModeRemote, Context: "prod", APIURL: "https://prod.example", Token: "lbs_prod"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.Select(tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("Select(%+v) = %+v, want %+v", tc.opts, got, tc.want)
			}
		})
	}

	t.Run("local when nothing is saved", func(t *testing.T) {
		empty, err := Load(filepath.Join(t.TempDir(), "lewbox"))
		if err != nil {
			t.Fatal(err)
		}
		got, err := empty.Select(Options{})
		if err != nil {
			t.Fatal(err)
		}
		if got.Mode != ModeLocal || got.Context != LocalContext {
			t.Errorf("Select = %+v, want local", got)
		}
	})

	t.Run("unknown context is an error", func(t *testing.T) {
		if _, err := s.Select(Options{Context: "missing"}); err == nil {
			t.Error("Select accepted an unknown context")
		}
	})
}

func TestLocalIsBuiltIn(t *testing.T) {
	s := newStore(t)
	names := s.Names()
	if len(names) != 3 || names[0] != LocalContext {
		t.Errorf("Names = %v", names)
	}
	if _, ok := s.Context(LocalContext); !ok {
		t.Error("local context is missing")
	}
	if err := s.Remove(LocalContext); err == nil {
		t.Error("local context was removed")
	}
	if err := s.SetCurrent(LocalContext); err != nil {
		t.Fatal(err)
	}
	if s.CurrentContext != LocalContext {
		t.Errorf("current = %q", s.CurrentContext)
	}
}

func TestSaveIsAtomicAndPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lewbox")
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Add("prod", Context{APIURL: "https://prod.example"}, "lbs_prod"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCurrent("prod"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %o, want 700", info.Mode().Perm())
	}
	for _, name := range []string{configFile, credentialsFile} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %o, want 600", name, info.Mode().Perm())
		}
	}

	// A second save replaces the files and leaves no temporary files behind.
	if err := s.Remove("prod"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Contexts["prod"]; ok {
		t.Error("removed context came back")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("config dir has %d files, want 2", len(entries))
	}

	data, err := os.ReadFile(filepath.Join(dir, configFile))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("config.json is not JSON: %v", err)
	}
}

func TestSetOrg(t *testing.T) {
	s := newStore(t)
	if err := s.SetOrg("prod", "acme"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if ctx, _ := reloaded.Context("prod"); ctx.Org != "acme" {
		t.Errorf("prod org = %q, want acme", ctx.Org)
	}
	if ctx, _ := reloaded.Context("dev"); ctx.Org != "" {
		t.Errorf("dev org = %q, want none", ctx.Org)
	}
	if err := s.SetOrg(LocalContext, "acme"); err == nil {
		t.Error("SetOrg(local) succeeded")
	}
	if err := s.SetOrg("missing", "acme"); err == nil {
		t.Error("SetOrg(missing) succeeded")
	}
}
