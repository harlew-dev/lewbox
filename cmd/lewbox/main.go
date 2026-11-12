// Command lewbox is the Lewbox binary: the orchestrator server and the CLI.
//
// Every CLI command calls a backend.Backend method and prints the same JSON as
// the service, on stdout for success and as the API's {error: {code, message}}
// body on stderr (exit 1) for failures. On a terminal it prints text instead.
package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/harlew-dev/lewbox/internal/backend"
	"github.com/harlew-dev/lewbox/internal/cliconfig"
	"github.com/harlew-dev/lewbox/internal/client"
	"github.com/harlew-dev/lewbox/internal/db"
	"github.com/harlew-dev/lewbox/internal/httpapi"
	"github.com/harlew-dev/lewbox/internal/secrets"
	"github.com/harlew-dev/lewbox/internal/service"
)

// version is set at build time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the CLI and returns the process exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runWithInput(ctx, args, os.Stdin, stdout, stderr)
}

// runWithInput is run with an injectable stdin, so tests can feed passwords
// through --password-stdin and answer confirmation prompts.
func runWithInput(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runWithApp(ctx, args, stdin, stdout, stderr, nil)
}

// runWithApp is runWithInput with the machine dependencies replaced, which is
// how the install, node add and update tests avoid sudo and the network.
func runWithApp(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, hd *hostDeps) int {
	a := &app{stdin: stdin, stdout: stdout, stderr: stderr, hd: hd}
	defer a.close()

	root := newRootCmd(a)
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)

	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	if errors.Is(err, errCanceled) {
		return 0
	}
	if exit, ok := errors.AsType[*exitError](err); ok {
		return exit.code
	}
	if usage, ok := errors.AsType[*usageError](err); ok {
		fmt.Fprintln(stderr, "Error:", usage.err)
		return 2
	}
	if ctx.Err() != nil && errors.Is(err, context.Canceled) {
		return 130
	}
	a.printError(err)
	return 1
}

// usageError marks a bad invocation: unknown flag, wrong arguments or two
// conflicting flags. It exits 2.
type usageError struct {
	err error
}

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

func usagef(format string, a ...any) error {
	return &usageError{err: fmt.Errorf(format, a...)}
}

// usageArgs wraps cobra's argument validators so their errors exit 2.
func usageArgs(fn cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := fn(cmd, args); err != nil {
			return &usageError{err: err}
		}
		return nil
	}
}

// exitError returns a specific process exit code without printing an error.
type exitError struct {
	code int
}

func (e *exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

// errCanceled means the user declined a confirmation prompt; it exits 0.
var errCanceled = errors.New("canceled")

// app holds what commands share. The database is opened on first use, so
// commands that don't need it (version, help, remote calls) never touch it.
type app struct {
	dbPath      string
	keyringPath string
	conn        *sql.DB
	svc         *service.Service

	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer

	jsonFlag    bool
	yes         bool
	contextName string
	apiURL      string
	org         string

	cfg *cliconfig.Store
	hd  *hostDeps
}

func (a *app) service(ctx context.Context) (*service.Service, error) {
	if a.svc != nil {
		return a.svc, nil
	}
	conn, err := db.Open(ctx, a.dbPath)
	if err != nil {
		return nil, err
	}
	a.conn = conn
	keyring, err := secrets.LoadOrCreate(a.keyringFile())
	if err != nil {
		return nil, err
	}
	a.svc = service.New(db.New(conn), service.WithKeyring(keyring))
	return a.svc, nil
}

// keyringFile is the --keyring flag or, by default, keyring.key next to the
// database so one database always uses one keyring.
func (a *app) keyringFile() string {
	if a.keyringPath != "" {
		return a.keyringPath
	}
	return filepath.Join(filepath.Dir(a.dbPath), "keyring.key")
}

func (a *app) close() {
	if a.conn != nil {
		a.conn.Close()
	}
}

// store loads the saved contexts once per process.
func (a *app) store() (*cliconfig.Store, error) {
	if a.cfg != nil {
		return a.cfg, nil
	}
	dir, err := cliconfig.DefaultDir()
	if err != nil {
		return nil, err
	}
	a.cfg, err = cliconfig.Load(dir)
	if err != nil {
		return nil, err
	}
	return a.cfg, nil
}

// resolve picks the backend a command should use and returns the selection it
// described.
func (a *app) resolve(ctx context.Context) (backend.Backend, cliconfig.Selection, error) {
	store, err := a.store()
	if err != nil {
		return nil, cliconfig.Selection{}, err
	}
	sel, err := store.Select(cliconfig.Options{
		Context: a.contextName,
		APIURL:  a.apiURL,
		Token:   os.Getenv("LEWBOX_TOKEN"),
	})
	if err != nil {
		return nil, cliconfig.Selection{}, err
	}
	if sel.Mode == cliconfig.ModeLocal {
		svc, err := a.service(ctx)
		if err != nil {
			return nil, cliconfig.Selection{}, err
		}
		return backend.Local{Svc: svc}, sel, nil
	}
	return client.New(sel.APIURL, sel.Token, nil), sel, nil
}

// call adapts a backend call into a cobra RunE that prints the result.
func (a *app) call(fn func(ctx context.Context, b backend.Backend, args []string) (any, error)) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		b, _, err := a.resolve(cmd.Context())
		if err != nil {
			return err
		}
		resp, err := fn(cmd.Context(), b, args)
		if err != nil {
			return err
		}
		return a.write(resp)
	}
}

// jsonMode reports whether output is JSON: --json, LEWBOX_OUTPUT=json, or a
// stdout that isn't a terminal.
func (a *app) jsonMode() bool {
	if a.jsonFlag {
		return true
	}
	return !isTerminal(a.stdout)
}

// terminal reports whether both ends are terminals, so prompts make sense.
func (a *app) terminal() bool {
	return isTerminal(a.stdout) && isTerminalReader(a.stdin)
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func isTerminalReader(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// confirm asks before a destructive command. In JSON mode or without a
// terminal it requires --yes instead.
func (a *app) confirm(question string) error {
	if a.yes {
		return nil
	}
	if a.jsonMode() || !a.terminal() {
		return usagef("pass --yes to confirm")
	}
	fmt.Fprintf(a.stderr, "%s [y/N] ", question)
	line, err := bufio.NewReader(a.stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	fmt.Fprintln(a.stderr)
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "y" || answer == "yes" {
		return nil
	}
	return errCanceled
}

// write prints a result as JSON or text.
func (a *app) write(v any) error {
	if a.jsonMode() {
		return httpapi.Encode(a.stdout, v)
	}
	return renderAt(a.stdout, v, time.Now())
}

// printError prints a failure the way the current output mode wants it.
func (a *app) printError(err error) {
	if se, ok := errors.AsType[*service.Error](err); ok {
		if a.jsonMode() {
			_, body := httpapi.ErrorFrom(err)
			_ = httpapi.Encode(a.stderr, body)
			return
		}
		fmt.Fprintln(a.stderr, "Error:", se.Message)
		for _, c := range failedChecks(se.Details) {
			if c.OK {
				continue
			}
			fmt.Fprintf(a.stderr, "  %s: %s\n", c.Name, c.Detail)
		}
		return
	}
	fmt.Fprintln(a.stderr, "Error:", err)
}

func newRootCmd(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:           "lewbox",
		Short:         "Lewbox: run a sandbox business on your own AerolVM clusters",
		SilenceUsage:  true,
		SilenceErrors: true, // run prints errors
	}
	root.PersistentFlags().StringVar(&a.dbPath, "db", envOr("LEWBOX_DB", defaultDBPath()),
		"path to the SQLite database (env LEWBOX_DB)")
	root.PersistentFlags().StringVar(&a.keyringPath, "keyring", envOr("LEWBOX_KEYRING_FILE", ""),
		"path to the keyring file (env LEWBOX_KEYRING_FILE; default keyring.key next to the database)")
	root.PersistentFlags().StringVar(&a.contextName, "context", envOr("LEWBOX_CONTEXT", ""),
		"which saved sign-in to use (env LEWBOX_CONTEXT)")
	root.PersistentFlags().StringVar(&a.apiURL, "api-url", envOr("LEWBOX_API_URL", ""),
		"talk to this Lewbox server (env LEWBOX_API_URL)")
	root.PersistentFlags().StringVar(&a.org, "org", envOr("LEWBOX_ORG", ""),
		"organization ID or slug for organization commands (env LEWBOX_ORG; default: lewbox org use)")
	root.PersistentFlags().BoolVar(&a.jsonFlag, "json", os.Getenv("LEWBOX_OUTPUT") == "json",
		"print JSON, byte-identical to the API (env LEWBOX_OUTPUT=json)")
	root.PersistentFlags().BoolVarP(&a.yes, "yes", "y", false, "skip confirmations")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &usageError{err: err}
	})

	root.AddCommand(
		newServeCmd(a),
		newOrchestratorCmd(a),
		newHealthCmd(a),
		newLoginCmd(a),
		newLogoutCmd(a),
		newWhoamiCmd(a),
		newContextCmd(a),
		newStatusCmd(a),
		newInstallCmd(a),
		newNodeCmd(a),
		newClusterCmd(a),
		newOverviewCmd(a),
		newFirewallCmd(a),
		newUserCmd(a),
		newOrgCmd(a),
		newMemberCmd(a),
		newAdminKeyCmd(a),
		newSandboxKeyCmd(a),
		newSandboxCmd(a),
		newUsageCmd(a),
		newLimitsCmd(a),
		newRegionCmd(a),
		newPlanCmd(a),
		newSubscriptionCmd(a),
		newSettingsCmd(a),
		newUpdateCmd(a),
		newVersionCmd(),
	)
	return root
}

// newHealthCmd mirrors GET /v0/health.
func newHealthCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Report whether Lewbox is up (GET /v0/health)",
		Args:  usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			return b.Health(ctx)
		}),
	}
}

// newNodeCmd mirrors /v0/nodes.
func newNodeCmd(a *app) *cobra.Command {
	node := &cobra.Command{
		Use:     "node",
		Aliases: []string{"nodes"},
		Short:   "Manage AerolVM nodes",
	}

	add := newAddNodeCmd(a)

	var nodeCluster string
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List nodes (GET /v0/nodes)",
		Args:    usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			return b.ListNodes(ctx, service.ListNodesRequest{Cluster: nodeCluster})
		}),
	}
	list.Flags().StringVar(&nodeCluster, "cluster", "", "only nodes in this cluster ID or slug")

	get := &cobra.Command{
		Use:   "get <id|name>",
		Short: "Show a node (GET /v0/nodes/{id|name})",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.GetNode(ctx, args[0])
		}),
	}

	var force bool
	remove := &cobra.Command{
		Use:     "remove <id|name>",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a node (DELETE /v0/nodes/{id|name})",
		Args:    usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(fmt.Sprintf("Remove node %q?", args[0])); err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.RemoveNode(ctx, args[0], service.RemoveNodeRequest{Force: force})
			})(cmd, args)
		},
	}
	remove.Flags().BoolVar(&force, "force", false, "delete it even if it still owns sandboxes")

	node.AddCommand(add, list, get, remove)
	return node
}

// newClusterCmd mirrors /v0/clusters.
func newClusterCmd(a *app) *cobra.Command {
	cluster := &cobra.Command{
		Use:     "cluster",
		Aliases: []string{"clusters"},
		Short:   "Manage AerolVM clusters",
	}

	var (
		adminURL, endpointURL, runtime, clusterName string
		tokenStdin                                  bool
	)
	add := &cobra.Command{
		Use:     "add <slug>",
		Aliases: []string{"create"},
		Short:   "Register and validate a cluster (POST /v0/clusters)",
		Example: "  printf '%s' \"$TOKEN\" | lewbox cluster add eu-west --admin-url https://eu-west.api.acme.dev --admin-token-stdin",
		Args:    usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := readSecret(cmd, tokenStdin, "--admin-token-stdin")
			if err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.AddCluster(ctx, service.AddClusterRequest{
					Slug:        args[0],
					Name:        clusterName,
					AdminURL:    adminURL,
					EndpointURL: endpointURL,
					Runtime:     runtime,
					AdminToken:  token,
				})
			})(cmd, args)
		},
	}
	add.Flags().StringVar(&adminURL, "admin-url", "", "the cluster's admin API URL (required)")
	add.Flags().StringVar(&endpointURL, "endpoint-url", "", "the endpoint customers use (default: admin-url)")
	add.Flags().StringVar(&runtime, "runtime", "docker", "runtime: docker or firecracker")
	add.Flags().StringVar(&clusterName, "name", "", "a display name (default: the slug)")
	add.Flags().BoolVar(&tokenStdin, "admin-token-stdin", false, "read the admin token from stdin (required)")
	_ = add.MarkFlagRequired("admin-url")
	_ = add.MarkFlagRequired("admin-token-stdin")

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List clusters (GET /v0/clusters)",
		Args:    usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			return b.ListClusters(ctx)
		}),
	}

	get := &cobra.Command{
		Use:   "get <id|slug>",
		Short: "Show a cluster (GET /v0/clusters/{id|slug})",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.GetCluster(ctx, args[0])
		}),
	}

	check := &cobra.Command{
		Use:   "check <id|slug>",
		Short: "Run the cluster checks now (POST /v0/clusters/{id|slug}/check)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.CheckCluster(ctx, args[0])
		}),
	}

	var removeForce bool
	remove := &cobra.Command{
		Use:     "remove <id|slug>",
		Aliases: []string{"rm", "delete"},
		Short:   "Unregister a cluster (DELETE /v0/clusters/{id|slug})",
		Args:    usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(fmt.Sprintf("Remove cluster %q?", args[0])); err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.RemoveCluster(ctx, args[0], removeForce)
			})(cmd, args)
		},
	}
	remove.Flags().BoolVar(&removeForce, "force", false, "remove it even if its sandbox keys cannot be revoked")

	var joinRole, joinTTL string
	joinCommand := &cobra.Command{
		Use:   "join-command <id|slug>",
		Short: "Mint a join token (POST /v0/clusters/{id|slug}/join-tokens)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.CreateJoinToken(ctx, args[0], service.JoinTokenRequest{
				Role: joinRole,
				TTL:  joinTTL,
			})
		}),
	}
	joinCommand.Flags().StringVar(&joinRole, "role", "mixed", "role for the new node: worker, ingress, server or mixed")
	joinCommand.Flags().StringVar(&joinTTL, "ttl", "15m", "how long the token is valid")

	var healthDays int
	health := &cobra.Command{
		Use:   "health <id|slug>",
		Short: "Show uptime history (GET /v0/clusters/{id|slug}/health)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.ClusterHealth(ctx, args[0], healthDays)
		}),
	}
	health.Flags().IntVar(&healthDays, "days", 30, "how many UTC days to show (1-90)")

	var metricsRange string
	metrics := &cobra.Command{
		Use:   "metrics <id|slug>",
		Short: "Show node metrics (GET /v0/clusters/{id|slug}/metrics)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.ClusterMetrics(ctx, args[0], metricsRange)
		}),
	}
	metrics.Flags().StringVar(&metricsRange, "range", "24h", "24h, 7d or 30d")

	cluster.AddCommand(add, list, get, check, health, metrics, remove, joinCommand)
	return cluster
}

// newOverviewCmd mirrors GET /v0/overview.
func newOverviewCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "overview",
		Short: "Show the fleet at a glance (GET /v0/overview)",
		Args:  usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			return b.Overview(ctx)
		}),
	}
}

// readSecret reads a secret from stdin when the matching flag is set. The
// flag exists so the secret never shows up in ps.
func readSecret(cmd *cobra.Command, fromStdin bool, flag string) (string, error) {
	if !fromStdin {
		return "", errors.New("pass " + flag + " and pipe the secret on stdin")
	}
	b, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// newUserCmd mirrors /v0/users.
func newUserCmd(a *app) *cobra.Command {
	user := &cobra.Command{
		Use:     "user",
		Aliases: []string{"users"},
		Short:   "Manage accounts",
	}

	var createAdmin, createPasswordStdin bool
	create := &cobra.Command{
		Use:   "create <email>",
		Short: "Create an account (POST /v0/users)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			password, err := readNewPassword(cmd, createPasswordStdin, true)
			if err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.CreateUser(ctx, service.CreateUserRequest{
					Email:         args[0],
					Password:      password,
					InstanceAdmin: createAdmin,
				})
			})(cmd, args)
		},
	}
	create.Flags().BoolVar(&createAdmin, "instance-admin", false, "make the account an instance admin")
	create.Flags().BoolVar(&createPasswordStdin, "password-stdin", false, "read the password from stdin")

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List accounts (GET /v0/users)",
		Args:    usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			return b.ListUsers(ctx)
		}),
	}

	get := &cobra.Command{
		Use:   "get <id|email>",
		Short: "Show an account (GET /v0/users/{id|email})",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.GetUser(ctx, args[0])
		}),
	}

	remove := &cobra.Command{
		Use:     "remove <id|email>",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove an account (DELETE /v0/users/{id|email})",
		Args:    usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(fmt.Sprintf("Remove user %q?", args[0])); err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.RemoveUser(ctx, args[0])
			})(cmd, args)
		},
	}

	disable := &cobra.Command{
		Use:   "disable <id|email>",
		Short: "Block sign-in and end sessions (POST /v0/users/{id|email}/disable)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(fmt.Sprintf("Disable user %q?", args[0])); err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.SetUserDisabled(ctx, args[0], true)
			})(cmd, args)
		},
	}

	enable := &cobra.Command{
		Use:   "enable <id|email>",
		Short: "Allow sign-in again (POST /v0/users/{id|email}/enable)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.SetUserDisabled(ctx, args[0], false)
		}),
	}

	var setPasswordStdin bool
	setPassword := &cobra.Command{
		Use:   "set-password <id|email>",
		Short: "Change a password (PUT /v0/users/{id|email}/password)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			password, err := readNewPassword(cmd, setPasswordStdin, false)
			if err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.SetPassword(ctx, args[0], service.SetPasswordRequest{Password: password})
			})(cmd, args)
		},
	}
	setPassword.Flags().BoolVar(&setPasswordStdin, "password-stdin", false, "read the password from stdin")

	user.AddCommand(create, list, get, remove, disable, enable, setPassword)
	return user
}

// readNewPassword reads a password from stdin. With --password-stdin it reads
// one line; otherwise it prompts without echo, and only when stdin is a
// terminal, so scripts must be explicit.
func readNewPassword(cmd *cobra.Command, fromStdin, confirm bool) (string, error) {
	if fromStdin {
		b, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", err
		}
		s := strings.TrimSuffix(string(b), "\n")
		return strings.TrimSuffix(s, "\r"), nil
	}
	f, ok := cmd.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return "", errors.New("stdin is not a terminal: pass --password-stdin")
	}
	password, err := promptPassword(cmd, f, "Password: ")
	if err != nil {
		return "", err
	}
	if confirm {
		again, err := promptPassword(cmd, f, "Repeat password: ")
		if err != nil {
			return "", err
		}
		if password != again {
			return "", errors.New("passwords do not match")
		}
	}
	return password, nil
}

func promptPassword(cmd *cobra.Command, f *os.File, label string) (string, error) {
	fmt.Fprint(cmd.ErrOrStderr(), label)
	b, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the Lewbox version",
		Args:  usageArgs(cobra.NoArgs),
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), version)
		},
	}
}

// startSync mirrors sandboxes and meters usage in the background until ctx
// ends.
func startSync(ctx context.Context, svc *service.Service, every time.Duration, log *slog.Logger) {
	if every <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := svc.Sync(ctx); err != nil {
					log.Warn("sync failed", "err", err)
				}
			}
		}
	}()
}

// startClusterChecks checks every cluster in the background until ctx ends.
func startClusterChecks(ctx context.Context, svc *service.Service, every time.Duration, log *slog.Logger) {
	if every <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := svc.CheckAllClusters(ctx); err != nil {
					log.Warn("cluster check failed", "err", err)
				}
			}
		}
	}()
}

func serve(ctx context.Context, addr string, h http.Handler, log *slog.Logger) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("orchestrator listening", "addr", addr, "version", version)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// defaultDBPath is $XDG_DATA_HOME/lewbox/lewbox.db, falling back to
// ~/.local/share/lewbox/lewbox.db.
func defaultDBPath() string {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "lewbox.db"
		}
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "lewbox", "lewbox.db")
}

func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// envDuration reads a duration from the environment, falling back to def
// when it is missing or invalid.
func envDuration(key string, def time.Duration) time.Duration {
	v := envOr(key, "")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
