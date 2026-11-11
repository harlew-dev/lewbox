package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/harlew-dev/lewbox/internal/cliconfig"
	"github.com/harlew-dev/lewbox/internal/client"
	"github.com/harlew-dev/lewbox/internal/service"
)

// loginOutput is printed by login. It never includes the session token.
type loginOutput struct {
	User      service.User `json:"user"`
	ExpiresAt time.Time    `json:"expires_at"`
	Context   string       `json:"context"`
}

// whoamiOutput is Me plus where it came from.
type whoamiOutput struct {
	service.Me
	Context string `json:"context"`
	APIURL  string `json:"api_url"`
}

// statusOutput is printed by status.
type statusOutput struct {
	Context string        `json:"context"`
	Mode    string        `json:"mode"`
	APIURL  string        `json:"api_url"`
	Healthy bool          `json:"healthy"`
	User    *service.User `json:"user"`
	Version string        `json:"version"`
	DB      string        `json:"db,omitempty"`
}

// contextEntry is one line of `context list`.
type contextEntry struct {
	Name    string `json:"name"`
	APIURL  string `json:"api_url,omitempty"`
	Current bool   `json:"current"`
}

// contextList is returned by `context list`.
type contextList struct {
	Data []contextEntry `json:"data"`
}

// contextUseOutput is returned by `context use`.
type contextUseOutput struct {
	CurrentContext string `json:"current_context"`
}

// contextRemoveOutput is returned by `context remove`.
type contextRemoveOutput struct {
	Name    string `json:"name"`
	Removed bool   `json:"removed"`
}

// newLoginCmd signs in to a server and saves the context.
func newLoginCmd(a *app) *cobra.Command {
	var (
		email         string
		passwordStdin bool
		keyStdin      bool
		contextName   string
	)
	cmd := &cobra.Command{
		Use:   "login [<url>]",
		Short: "Sign in to a Lewbox server and save the session",
		Args:  usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if passwordStdin && keyStdin {
				return usagef("--password-stdin and --key-stdin cannot be used together")
			}
			rawURL := a.apiURL
			if len(args) == 1 {
				rawURL = args[0]
			}
			rawURL = strings.TrimSpace(rawURL)
			if rawURL == "" {
				return usagef("a Lewbox URL is required: lewbox login https://lewbox.acme.dev")
			}
			apiURL := strings.TrimRight(rawURL, "/")

			name := strings.TrimSpace(contextName)
			if name == "" {
				name = contextNameForURL(apiURL)
			}
			if name == "" || name == cliconfig.LocalContext {
				return usagef("cannot use %q as a context name: pass --context-name", name)
			}

			store, err := a.store()
			if err != nil {
				return err
			}

			if keyStdin {
				token, err := readToken(a.stdin)
				if err != nil {
					return err
				}
				if token == "" {
					return errors.New("no key on stdin")
				}
				c := client.New(apiURL, token, nil)
				me, err := c.Me(cmd.Context())
				if err != nil {
					return err
				}
				// An admin key acts for one organization; make it the default.
				saved := cliconfig.Context{APIURL: apiURL}
				if me.Organization != nil {
					saved.Org = me.Organization.Slug
				}
				if err := store.Add(name, saved, token); err != nil {
					return err
				}
				if err := store.SetCurrent(name); err != nil {
					return err
				}
				if err := store.Save(); err != nil {
					return err
				}
				return a.write(whoamiOutput{Me: me, Context: name, APIURL: apiURL})
			}

			if email == "" {
				if !a.terminal() {
					return usagef("pass --email and --password-stdin")
				}
				email = promptLine(a, "Email: ")
			}
			password, err := a.readLoginPassword(cmd, passwordStdin)
			if err != nil {
				return err
			}

			c := client.New(apiURL, "", nil)
			resp, err := c.Login(cmd.Context(), service.LoginRequest{Email: email, Password: password})
			if err != nil {
				return err
			}
			// Signing in again keeps the context's default organization.
			saved := cliconfig.Context{APIURL: apiURL}
			if prev, ok := store.Context(name); ok && strings.TrimRight(prev.APIURL, "/") == apiURL {
				saved.Org = prev.Org
			}
			if err := store.Add(name, saved, resp.Token); err != nil {
				return err
			}
			if err := store.SetCurrent(name); err != nil {
				return err
			}
			if err := store.Save(); err != nil {
				return err
			}
			return a.write(loginOutput{User: resp.User, ExpiresAt: resp.ExpiresAt, Context: name})
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "account email")
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "read the password from stdin")
	cmd.Flags().BoolVar(&keyStdin, "key-stdin", false, "read an admin key from stdin")
	cmd.Flags().StringVar(&contextName, "context-name", "", "name to save this sign-in as (default: the URL's host)")
	return cmd
}

// contextNameForURL names a context after the URL's host, with ":" replaced
// by "-" so the name is usable on a command line.
func contextNameForURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ReplaceAll(u.Host, ":", "-")
}

// promptLine asks a free-text question on stderr and reads one line.
func promptLine(a *app, label string) string {
	fmt.Fprint(a.stderr, label)
	line, _ := bufio.NewReader(a.stdin).ReadString('\n')
	return strings.TrimSpace(line)
}

// readToken reads a whole secret from stdin and trims surrounding space.
func readToken(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// readLoginPassword reads a password from stdin or prompts without echo.
func (a *app) readLoginPassword(cmd *cobra.Command, fromStdin bool) (string, error) {
	if fromStdin {
		b, err := io.ReadAll(a.stdin)
		if err != nil {
			return "", err
		}
		s := strings.TrimSuffix(string(b), "\n")
		return strings.TrimSuffix(s, "\r"), nil
	}
	f, ok := a.stdin.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return "", usagef("stdin is not a terminal: pass --password-stdin")
	}
	return promptPassword(cmd, f, "Password: ")
}

// newLogoutCmd ends sessions and forgets their tokens.
func newLogoutCmd(a *app) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Sign out and delete the saved session",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := a.store()
			if err != nil {
				return err
			}
			if all {
				var firstErr error
				for name := range store.Credentials {
					if err := a.logoutRemote(cmd.Context(), store.Contexts[name].APIURL, store.Token(name)); err != nil && firstErr == nil {
						firstErr = err
					}
				}
				store.DeleteAllTokens()
				if err := store.Save(); err != nil {
					return err
				}
				if firstErr != nil {
					return firstErr
				}
				return a.write(service.LogoutResponse{OK: true})
			}
			sel, err := store.Select(cliconfig.Options{
				Context: a.contextName,
				APIURL:  a.apiURL,
				Token:   os.Getenv("LEWBOX_TOKEN"),
			})
			if err != nil {
				return err
			}
			if sel.Mode == cliconfig.ModeRemote {
				logoutErr := a.logoutRemote(cmd.Context(), sel.APIURL, sel.Token)
				store.DeleteToken(sel.Context)
				if err := store.Save(); err != nil {
					return err
				}
				if logoutErr != nil {
					return logoutErr
				}
				return a.write(service.LogoutResponse{OK: true})
			}
			if err := store.Save(); err != nil {
				return err
			}
			return a.write(service.LogoutResponse{OK: true})
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "sign out of every saved context")
	return cmd
}

// logoutRemote ends one session, ignoring an already-invalid token.
func (a *app) logoutRemote(ctx context.Context, apiURL, token string) error {
	if apiURL == "" || token == "" {
		return nil
	}
	_, err := client.New(apiURL, token, nil).Logout(ctx)
	if se, ok := errors.AsType[*service.Error](err); ok && se.Code == service.CodeUnauthorized {
		return nil
	}
	return err
}

// newWhoamiCmd mirrors GET /v0/me and adds the selected context.
func newWhoamiCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the signed-in account and context",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			b, sel, err := a.resolve(cmd.Context())
			if err != nil {
				return err
			}
			me, err := b.Me(cmd.Context())
			if err != nil {
				return err
			}
			return a.write(whoamiOutput{Me: me, Context: sel.Context, APIURL: sel.APIURL})
		},
	}
}

// newContextCmd manages the saved sign-in contexts.
func newContextCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "context",
		Aliases: []string{"ctx"},
		Short:   "List and switch saved sign-ins",
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List contexts",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := a.store()
			if err != nil {
				return err
			}
			out := contextList{Data: make([]contextEntry, 0, len(store.Contexts)+1)}
			for _, name := range store.Names() {
				ctx, _ := store.Context(name)
				current := store.CurrentContext == name
				if store.CurrentContext == "" && name == cliconfig.LocalContext {
					current = true
				}
				out.Data = append(out.Data, contextEntry{
					Name:    name,
					APIURL:  ctx.APIURL,
					Current: current,
				})
			}
			return a.write(out)
		},
	}

	use := &cobra.Command{
		Use:   "use <name>",
		Short: "Make a context current",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := a.store()
			if err != nil {
				return err
			}
			if err := store.SetCurrent(args[0]); err != nil {
				return err
			}
			if err := store.Save(); err != nil {
				return err
			}
			return a.write(contextUseOutput{CurrentContext: args[0]})
		},
	}

	remove := &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm", "delete"},
		Short:   "Forget a context and its token",
		Args:    usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := a.store()
			if err != nil {
				return err
			}
			if err := store.Remove(args[0]); err != nil {
				return err
			}
			if err := store.Save(); err != nil {
				return err
			}
			return a.write(contextRemoveOutput{Name: args[0], Removed: true})
		},
	}

	cmd.AddCommand(list, use, remove)
	return cmd
}

// newStatusCmd shows the selected mode, health and account.
func newStatusCmd(a *app) *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the context, health and account",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			b, sel, err := a.resolve(cmd.Context())
			if err != nil {
				return err
			}
			out := statusOutput{
				Context: sel.Context,
				Mode:    string(sel.Mode),
				APIURL:  sel.APIURL,
				Version: version,
			}
			if sel.Mode == cliconfig.ModeLocal {
				out.DB = a.dbPath
			}
			_, healthErr := b.Health(cmd.Context())
			out.Healthy = healthErr == nil
			signedIn := false
			if me, err := b.Me(cmd.Context()); err == nil {
				out.User = me.User
				signedIn = true
			}
			if err := a.write(out); err != nil {
				return err
			}
			if check && (!out.Healthy || (sel.Mode == cliconfig.ModeRemote && !signedIn)) {
				return &exitError{code: 1}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "exit 1 when unhealthy or not signed in")
	return cmd
}

// failedChecks turns precondition details into checks for text output.
func failedChecks(details any) []service.Check {
	if details == nil {
		return nil
	}
	data, err := json.Marshal(details)
	if err != nil {
		return nil
	}
	var checks []service.Check
	if err := json.Unmarshal(data, &checks); err != nil {
		return nil
	}
	return checks
}
