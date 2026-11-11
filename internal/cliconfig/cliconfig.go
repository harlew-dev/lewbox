// Package cliconfig stores the CLI's saved contexts and credentials in
// $XDG_CONFIG_HOME/lewbox (or ~/.config/lewbox).
package cliconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// LocalContext is the built-in local-mode context. It always exists and
	// cannot be removed.
	LocalContext = "local"

	configFile      = "config.json"
	credentialsFile = "credentials.json"
	dirMode         = 0o700
	fileMode        = 0o600
)

// Mode is how a command reaches Lewbox.
type Mode string

const (
	// ModeLocal calls the service in-process against the local database.
	ModeLocal Mode = "local"
	// ModeRemote calls a Lewbox server over HTTP.
	ModeRemote Mode = "remote"
)

// Context is one saved sign-in target.
type Context struct {
	APIURL string `json:"api_url"`
	Org    string `json:"org"`
}

// Credential is the token saved for a context.
type Credential struct {
	Token string `json:"token"`
}

// Store is the parsed config.json and credentials.json pair.
type Store struct {
	dir            string
	CurrentContext string
	Contexts       map[string]Context
	Credentials    map[string]Credential
}

// Selection is the backend a command should use.
type Selection struct {
	Mode    Mode
	Context string
	APIURL  string
	Token   string
}

// Options are the values that choose a backend, in precedence order.
type Options struct {
	Context string
	APIURL  string
	Token   string
}

type configFileData struct {
	CurrentContext string             `json:"current_context"`
	Contexts       map[string]Context `json:"contexts"`
}

// DefaultDir returns $XDG_CONFIG_HOME/lewbox, falling back to
// ~/.config/lewbox.
func DefaultDir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "lewbox"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".config", "lewbox"), nil
}

// Load reads dir's config and credentials. Missing files are empty config.
func Load(dir string) (*Store, error) {
	s := &Store{
		dir:         dir,
		Contexts:    make(map[string]Context),
		Credentials: make(map[string]Credential),
	}

	var cfg configFileData
	if err := readJSON(filepath.Join(dir, configFile), &cfg); err != nil {
		return nil, err
	}
	s.CurrentContext = cfg.CurrentContext
	if cfg.Contexts != nil {
		s.Contexts = cfg.Contexts
	}

	var creds map[string]Credential
	if err := readJSON(filepath.Join(dir, credentialsFile), &creds); err != nil {
		return nil, err
	}
	if creds != nil {
		s.Credentials = creds
	}
	return s, nil
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

// Save writes both files atomically with 0600 permissions. The directory is
// created with 0700 when needed.
func (s *Store) Save() error {
	if s.Contexts == nil {
		s.Contexts = make(map[string]Context)
	}
	if s.Credentials == nil {
		s.Credentials = make(map[string]Credential)
	}
	cfg, err := json.Marshal(configFileData{
		CurrentContext: s.CurrentContext,
		Contexts:       s.Contexts,
	})
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	creds, err := json.Marshal(s.Credentials)
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	if err := writeAtomic(filepath.Join(s.dir, configFile), append(cfg, '\n')); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.dir, credentialsFile), append(creds, '\n'))
}

// writeAtomic writes data through a temporary file in the same directory so a
// crash never leaves a partial config behind.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(fileMode); err != nil {
		f.Close()
		return fmt.Errorf("set config permissions: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync config: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// Names returns every context name, local first and the rest sorted.
func (s *Store) Names() []string {
	names := make([]string, 0, len(s.Contexts)+1)
	names = append(names, LocalContext)
	for name := range s.Contexts {
		names = append(names, name)
	}
	sort.Strings(names[1:])
	return names
}

// Context returns a saved context. The built-in local context has no API URL.
func (s *Store) Context(name string) (Context, bool) {
	if name == LocalContext {
		return Context{}, true
	}
	ctx, ok := s.Contexts[name]
	return ctx, ok
}

// Add saves a context and its token, replacing an existing one with the same
// name.
func (s *Store) Add(name string, ctx Context, token string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("a context name is required")
	}
	if name == LocalContext {
		return errors.New("local is a built-in context and cannot be changed")
	}
	if s.Contexts == nil {
		s.Contexts = make(map[string]Context)
	}
	if s.Credentials == nil {
		s.Credentials = make(map[string]Credential)
	}
	s.Contexts[name] = ctx
	s.Credentials[name] = Credential{Token: token}
	return nil
}

// SetOrg sets a saved context's default organization ("" clears it). The
// built-in local context has none: pass --org or set LEWBOX_ORG there.
func (s *Store) SetOrg(name, org string) error {
	if name == LocalContext {
		return errors.New("the local context has no default organization: pass --org or set LEWBOX_ORG")
	}
	ctx, ok := s.Contexts[name]
	if !ok {
		return fmt.Errorf("context %q not found", name)
	}
	ctx.Org = org
	s.Contexts[name] = ctx
	return nil
}

// SetCurrent makes name the current context. The name must exist.
func (s *Store) SetCurrent(name string) error {
	if _, ok := s.Context(name); !ok {
		return fmt.Errorf("context %q not found", name)
	}
	s.CurrentContext = name
	return nil
}

// Remove deletes a context and its token. The built-in local context cannot
// be removed.
func (s *Store) Remove(name string) error {
	if name == LocalContext {
		return errors.New("local is a built-in context and cannot be removed")
	}
	if _, ok := s.Contexts[name]; !ok {
		return fmt.Errorf("context %q not found", name)
	}
	delete(s.Contexts, name)
	delete(s.Credentials, name)
	if s.CurrentContext == name {
		s.CurrentContext = ""
	}
	return nil
}

// Token returns the token saved for name, if any.
func (s *Store) Token(name string) string {
	return s.Credentials[name].Token
}

// DeleteToken forgets one context's token but keeps the context.
func (s *Store) DeleteToken(name string) {
	delete(s.Credentials, name)
}

// DeleteAllTokens forgets every saved token.
func (s *Store) DeleteAllTokens() {
	s.Credentials = make(map[string]Credential)
}

// Select chooses a backend. The first match wins: a context, an API URL, the
// saved current context, then local mode.
func (s *Store) Select(opts Options) (Selection, error) {
	if name := strings.TrimSpace(opts.Context); name != "" {
		return s.selectionFor(name)
	}
	if raw := strings.TrimSpace(opts.APIURL); raw != "" {
		apiURL := strings.TrimRight(raw, "/")
		token := opts.Token
		name := ""
		if token == "" {
			name, token = s.contextForURL(apiURL)
		}
		if name == "" {
			name = contextName(apiURL)
		}
		return Selection{Mode: ModeRemote, Context: name, APIURL: apiURL, Token: token}, nil
	}
	if name := strings.TrimSpace(s.CurrentContext); name != "" {
		if sel, err := s.selectionFor(name); err == nil {
			return sel, nil
		}
	}
	return Selection{Mode: ModeLocal, Context: LocalContext}, nil
}

func (s *Store) selectionFor(name string) (Selection, error) {
	if name == LocalContext {
		return Selection{Mode: ModeLocal, Context: LocalContext}, nil
	}
	ctx, ok := s.Contexts[name]
	if !ok {
		return Selection{}, fmt.Errorf("context %q not found", name)
	}
	return Selection{
		Mode:    ModeRemote,
		Context: name,
		APIURL:  strings.TrimRight(ctx.APIURL, "/"),
		Token:   s.Token(name),
	}, nil
}

// contextForURL finds a saved context with the same API URL, preferring the
// current one when several match.
func (s *Store) contextForURL(apiURL string) (name, token string) {
	if ctx, ok := s.Contexts[s.CurrentContext]; ok && strings.TrimRight(ctx.APIURL, "/") == apiURL {
		return s.CurrentContext, s.Token(s.CurrentContext)
	}
	for _, n := range s.Names() {
		if n == LocalContext {
			continue
		}
		if strings.TrimRight(s.Contexts[n].APIURL, "/") == apiURL {
			return n, s.Token(n)
		}
	}
	return "", ""
}

// contextName turns a URL into a default context name: its host, with ":" for
// a port replaced by "-".
func contextName(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ReplaceAll(u.Host, ":", "-")
}
