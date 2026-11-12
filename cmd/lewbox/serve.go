package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	docsfs "github.com/harlew-dev/lewbox/docs"
	docssite "github.com/harlew-dev/lewbox/internal/docs"
	"github.com/harlew-dev/lewbox/internal/httpapi"
	"github.com/harlew-dev/lewbox/internal/service"
	"github.com/harlew-dev/lewbox/internal/webui"
	"github.com/harlew-dev/lewbox/web"
)

// serveOptions are the flags that describe where and how often to serve.
type serveOptions struct {
	port           int
	listen         string
	interval       time.Duration
	syncInterval   time.Duration
	cookieSecure   string
	trustedOrigins []string
}

func addServeFlags(cmd *cobra.Command, o *serveOptions) {
	cmd.Flags().IntVar(&o.port, "port", envInt("LEWBOX_PORT", 8080),
		"listen on all interfaces on this port (env LEWBOX_PORT)")
	cmd.Flags().StringVar(&o.listen, "listen", "",
		"the whole listen address, e.g. 127.0.0.1:8080 (env LEWBOX_LISTEN; default :8080)")
	cmd.Flags().DurationVar(&o.interval, "check-interval", envDuration("LEWBOX_CHECK_INTERVAL", 60*time.Second),
		"how often to check every cluster; 0 disables (env LEWBOX_CHECK_INTERVAL)")
	cmd.Flags().DurationVar(&o.syncInterval, "sync-interval", envDuration("LEWBOX_SYNC_INTERVAL", 15*time.Second),
		"how often to sync sandboxes and usage; 0 disables (env LEWBOX_SYNC_INTERVAL)")
	cmd.Flags().StringVar(&o.cookieSecure, "cookie-secure", envOr("LEWBOX_COOKIE_SECURE", "auto"),
		"session cookie security: auto or always (env LEWBOX_COOKIE_SECURE)")
	cmd.Flags().StringSliceVar(&o.trustedOrigins, "trusted-origin", envList("LEWBOX_TRUSTED_ORIGINS"),
		"extra origin allowed by the CSRF check (repeatable; env LEWBOX_TRUSTED_ORIGINS, comma-separated)")
}

// secureCookies turns --cookie-secure into a router option.
func (o *serveOptions) secureCookies() (bool, error) {
	switch strings.ToLower(strings.TrimSpace(o.cookieSecure)) {
	case "", "auto":
		return false, nil
	case "always":
		return true, nil
	default:
		return false, usagef("--cookie-secure must be auto or always")
	}
}

// addr resolves --listen, --port and their environment fallbacks. Passing
// both flags is an error.
func (o *serveOptions) addr(cmd *cobra.Command) (string, error) {
	portSet := cmd.Flags().Changed("port")
	listenSet := cmd.Flags().Changed("listen")
	if portSet && listenSet {
		return "", usagef("--port and --listen cannot be used together")
	}
	switch {
	case listenSet:
		return o.listen, nil
	case portSet:
		return fmt.Sprintf(":%d", o.port), nil
	case os.Getenv("LEWBOX_LISTEN") != "":
		return os.Getenv("LEWBOX_LISTEN"), nil
	default:
		return fmt.Sprintf(":%d", o.port), nil
	}
}

// newOrchestratorCmd serves when run bare and has serve/status subcommands.
func newOrchestratorCmd(a *app) *cobra.Command {
	o := &serveOptions{}
	cmd := &cobra.Command{
		Use:   "orchestrator",
		Short: "Run the Lewbox server",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(a.stderr, `Warning: "lewbox orchestrator" is now "lewbox orchestrator serve"`)
			return a.runServe(cmd, o)
		},
	}
	addServeFlags(cmd, o)
	cmd.AddCommand(newOrchestratorServeCmd(a), newOrchestratorStatusCmd(a))
	return cmd
}

// newServeCmd is the shortcut for `orchestrator serve`.
func newServeCmd(a *app) *cobra.Command {
	o := &serveOptions{}
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the Lewbox server (same as orchestrator serve)",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runServe(cmd, o)
		},
	}
	addServeFlags(cmd, o)
	return cmd
}

// newOrchestratorServeCmd runs the server.
func newOrchestratorServeCmd(a *app) *cobra.Command {
	o := &serveOptions{}
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the Lewbox server",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runServe(cmd, o)
		},
	}
	addServeFlags(cmd, o)
	return cmd
}

// runServe opens the local database, bootstraps the first admin if asked and
// serves the API. The server always runs in local mode; it owns the database.
func (a *app) runServe(cmd *cobra.Command, o *serveOptions) error {
	addr, err := o.addr(cmd)
	if err != nil {
		return err
	}
	secureCookies, err := o.secureCookies()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(a.stderr, nil))
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	svc, err := a.service(ctx)
	if err != nil {
		return err
	}
	if err := bootstrapAdmin(ctx, svc, log); err != nil {
		return err
	}
	log.Info("database ready", "path", a.dbPath)
	svc.SetSyncInterval(o.syncInterval)
	startClusterChecks(ctx, svc, o.interval, log)
	startSync(ctx, svc, o.syncInterval, log)
	site, err := loadDocs()
	if err != nil {
		return err
	}
	dist := webDist()
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		log.Info("web console not built", "hint", "npm --prefix web ci && npm --prefix web run build")
	} else {
		log.Info("web console ready")
	}
	return serve(ctx, addr, webui.Handler(httpapi.NewRouter(svc, log,
		httpapi.WithCookieSecure(secureCookies),
		httpapi.WithTrustedOrigins(o.trustedOrigins),
		httpapi.WithDocs(site),
	), dist), log)
}

// loadDocs parses the embedded documentation, or the folder named by
// LEWBOX_DOCS_DIR when it is set (development: edits need a restart).
func loadDocs() (*docssite.Site, error) {
	if dir := strings.TrimSpace(os.Getenv("LEWBOX_DOCS_DIR")); dir != "" {
		site, err := docssite.Load(os.DirFS(dir))
		if err != nil {
			return nil, fmt.Errorf("load docs from %s: %w", dir, err)
		}
		return site, nil
	}
	site, err := docssite.Load(docsfs.FS)
	if err != nil {
		return nil, fmt.Errorf("load embedded docs: %w", err)
	}
	return site, nil
}

// webDist returns the built web console, or the folder named by
// LEWBOX_WEB_DIR when it is set (development).
func webDist() fs.FS {
	if dir := strings.TrimSpace(os.Getenv("LEWBOX_WEB_DIR")); dir != "" {
		return os.DirFS(dir)
	}
	return web.DistFS()
}

// bootstrapAdmin creates the first instance admin when the bootstrap
// environment is set. It never logs the password.
func bootstrapAdmin(ctx context.Context, svc *service.Service, log *slog.Logger) error {
	email := strings.TrimSpace(os.Getenv("LEWBOX_BOOTSTRAP_ADMIN_EMAIL"))
	password := os.Getenv("LEWBOX_BOOTSTRAP_ADMIN_PASSWORD")
	if file := strings.TrimSpace(os.Getenv("LEWBOX_BOOTSTRAP_ADMIN_PASSWORD_FILE")); file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read bootstrap password file: %w", err)
		}
		password = strings.TrimRight(string(b), "\r\n")
	}
	if email == "" || password == "" {
		return nil
	}
	created, err := svc.BootstrapAdmin(ctx, email, password)
	if err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}
	if created {
		log.Info("bootstrap admin created", "email", email)
	} else {
		log.Info("bootstrap admin already exists", "email", email)
	}
	return nil
}

// orchestratorStatus is printed by `orchestrator status`.
type orchestratorStatus struct {
	Listening bool   `json:"listening"`
	Healthy   bool   `json:"healthy"`
	Version   string `json:"version"`
}

// newOrchestratorStatusCmd asks the local server's health route.
func newOrchestratorStatusCmd(a *app) *cobra.Command {
	o := &serveOptions{}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check the server on this machine (GET /v0/health)",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			addr, err := o.addr(cmd)
			if err != nil {
				return err
			}
			out := orchestratorStatus{Version: version}
			c := &http.Client{Timeout: 5 * time.Second}
			res, err := c.Get(healthURL(addr))
			if err == nil {
				defer res.Body.Close()
				out.Listening = true
				var health service.HealthResponse
				if res.StatusCode == http.StatusOK && json.NewDecoder(res.Body).Decode(&health) == nil {
					out.Healthy = health.OK
				}
			}
			return a.write(out)
		},
	}
	addServeFlags(cmd, o)
	return cmd
}

// healthURL turns a listen address into the loopback health URL.
func healthURL(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		if rest, ok := strings.CutPrefix(addr, ":"); ok {
			port = rest
		} else {
			port = addr
		}
	}
	return "http://" + net.JoinHostPort("127.0.0.1", port) + httpapi.Prefix + "/health"
}

// envInt reads an integer from the environment, falling back to def.
func envInt(key string, def int) int {
	v := envOr(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// envList reads a comma-separated list from the environment. Blank entries
// are dropped.
func envList(key string) []string {
	v := strings.TrimSpace(envOr(key, ""))
	if v == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
