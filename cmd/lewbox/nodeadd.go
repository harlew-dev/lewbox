package main

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/spf13/cobra"

	"github.com/harlew-dev/lewbox/internal/backend"
	"github.com/harlew-dev/lewbox/internal/machine"
	"github.com/harlew-dev/lewbox/internal/service"
)

// nodeAddOptions describe both forms of `lewbox node add`.
type nodeAddOptions struct {
	url            string
	cluster        string
	newCluster     string
	role           string
	advertise      string
	adminURL       string
	endpointURL    string
	storageProfile string
	stopPolicy     string
	retention      string
	snapshotName   string
}

// newClusterOnly reports whether any option that only applies to a new
// cluster was set.
func (o *nodeAddOptions) newClusterOnly() bool {
	return o.storageProfile != "" || o.stopPolicy != "" || o.retention != "" || o.snapshotName != ""
}

// newAddNodeCmd registers a node by URL, or starts or joins a cluster on this
// machine.
func newAddNodeCmd(a *app) *cobra.Command {
	o := &nodeAddOptions{}
	cmd := &cobra.Command{
		Use:     "add [<name>]",
		Aliases: []string{"create"},
		Short:   "Start a cluster here, join one, or register a node by URL",
		Long: "Without --url this starts a new cluster on this machine or joins one.\n" +
			"`node add <name> --url <url>` keeps the old behaviour and only calls\n" +
			"the orchestrator.",
		Example: "  lewbox node add node-1 --new-cluster lab\n" +
			"  lewbox node add node-2 --cluster node-1\n" +
			"  lewbox node add eu-west-1 --url https://eu-west-1.internal:8443",
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if o.url != "" {
				if len(args) != 1 {
					return usagef("node add <name> --url <url>")
				}
				if o.cluster != "" || o.newCluster != "" || o.advertise != "" || o.adminURL != "" || o.endpointURL != "" ||
					o.newClusterOnly() {
					return usagef("--url cannot be combined with --cluster, --new-cluster, --advertise, --admin-url, --endpoint-url, --storage-profile, --stop-policy, --archive-local-retention or --archive-snapshot-name")
				}
				return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
					return b.AddNode(ctx, service.AddNodeRequest{Name: args[0], URL: o.url})
				})(cmd, args)
			}
			return a.runNodeAdd(cmd.Context(), args, o)
		},
	}
	cmd.Flags().StringVar(&o.url, "url", "", "register an existing node by its AerolVM API URL")
	cmd.Flags().StringVar(&o.cluster, "cluster", "", "join this cluster, by slug or by one of its nodes")
	cmd.Flags().StringVar(&o.newCluster, "new-cluster", "", "start a new cluster with this slug (default: the node name)")
	cmd.Flags().StringVar(&o.role, "role", "mixed", "role for the new node: worker, ingress, server or mixed")
	cmd.Flags().StringVar(&o.advertise, "advertise", "", "the IP this node advertises to the cluster")
	cmd.Flags().StringVar(&o.adminURL, "admin-url", "", "the cluster's admin URL (default: this machine's AerolVM URL)")
	cmd.Flags().StringVar(&o.endpointURL, "endpoint-url", "", "the endpoint customers use (default: admin-url)")
	cmd.Flags().StringVar(&o.storageProfile, "storage-profile", "", "the storage profile for a new cluster, by name")
	cmd.Flags().StringVar(&o.stopPolicy, "stop-policy", "", "what happens to a stopped sandbox in a new cluster: keep or archive")
	cmd.Flags().StringVar(&o.retention, "archive-local-retention", "", "how long a new cluster's nodes keep local copies of archived sandboxes: instant, 1h, 1d, 7d or never")
	cmd.Flags().StringVar(&o.snapshotName, "archive-snapshot-name", "", "how a new cluster names archive snapshots: id or name")
	return cmd
}

func (a *app) runNodeAdd(ctx context.Context, args []string, o *nodeAddOptions) error {
	if err := a.refuseRoot(); err != nil {
		return err
	}
	if o.cluster != "" && o.newCluster != "" {
		return usagef("--cluster and --new-cluster cannot be used together")
	}
	if o.cluster != "" && o.newClusterOnly() {
		return usagef("--storage-profile, --stop-policy, --archive-local-retention and --archive-snapshot-name only apply to a new cluster: change them with lewbox cluster set")
	}
	state, err := a.loadState()
	if err != nil {
		return err
	}
	name := state.Name
	if len(args) == 1 {
		name = args[0]
	}
	b, _, err := a.resolve(ctx)
	if err != nil {
		return err
	}
	if o.cluster != "" {
		return a.joinNode(ctx, b, state, name, o)
	}
	return a.startCluster(ctx, b, state, name, o)
}

// startCluster runs install.sh init here, applies Lewbox's settings and
// registers the cluster.
func (a *app) startCluster(ctx context.Context, b backend.Backend, state machine.State, name string, o *nodeAddOptions) error {
	slug := o.newCluster
	if slug == "" {
		slug = name
	}

	if state.Mode != machine.ModeLocal {
		source := newInstallerSource(state.From)
		installer, err := source.Installer(ctx, state.AerolVMVersion)
		if err != nil {
			return preconditionError("%s", err)
		}
		if installer.Warning != "" {
			a.say("  ! %s", installer.Warning)
		}
		args, err := mustInstallArgs(installer.Script, machine.InstallSpec{
			Version: installer.Version,
			PAScope: true,
			Options: installer.Options,
		})
		if err != nil {
			return err
		}
		args = append(args, "init", "--role", o.role, "--node-id", name)
		if advertise := advertiseIP(state, o.advertise); advertise != "" {
			args = append(args, "--advertise", advertise)
		}
		a.say("Starting a cluster")
		run := installerCommand(a.host().env, installer.Path, args)
		if err := a.runInstaller(ctx, a.host().runner, run, false); err != nil {
			return err
		}
	} else {
		a.say("Local mode: registering this machine as a one-node cluster")
	}

	if _, err := a.applySettings(ctx, a.host().runner, machine.EnvFileContent); err != nil {
		return err
	}
	if _, err := b.GetCluster(ctx, slug); err == nil {
		// This cluster is already registered. Re-running init is safe, but
		// minting again would revoke the token the orchestrator stored, so
		// only check the cluster.
		detail, err := b.CheckCluster(ctx, slug)
		if err != nil {
			return err
		}
		state.Cluster = slug
		state.Name = name
		if err := state.Save(); err != nil {
			return err
		}
		a.say("  ✓ cluster %q is already registered", slug)
		if _, err := a.configureNode(ctx, b, &state, false); err != nil {
			return err
		}
		return a.write(detail)
	}
	token, err := a.mintAdminToken(ctx, a.host().runner, "lewbox-"+slug)
	if err != nil {
		return err
	}

	adminURL := o.adminURL
	if adminURL == "" {
		adminURL = state.APIURL()
	}
	endpointURL := o.endpointURL
	if endpointURL == "" {
		endpointURL = adminURL
	}
	// A new domain usually doesn't resolve yet: the orchestrator creates its
	// records once the cluster is registered. Tell it where this node is so
	// registration doesn't depend on DNS.
	var adminIP string
	if state.Mode == machine.ModeDomain && adminURL == state.APIURL() {
		ip, err := a.localPublicIP(ctx, token, name)
		if err != nil {
			a.say("  ! couldn't read this node's public IP (%v); the orchestrator will look up %s", err, state.Domain)
		} else {
			adminIP = ip
			a.say("  ✓ this node's public IP is %s", ip)
		}
	}
	detail, err := b.AddCluster(ctx, service.AddClusterRequest{
		Slug:                  slug,
		AdminURL:              adminURL,
		EndpointURL:           endpointURL,
		Runtime:               state.Runtime,
		Domain:                clusterDomainOf(state),
		DNSProfile:            state.DNSProfile,
		StorageProfile:        o.storageProfile,
		StopPolicy:            o.stopPolicy,
		ArchiveLocalRetention: o.retention,
		ArchiveSnapshotName:   o.snapshotName,
		AdminToken:            token,
		AdminIP:               adminIP,
	})
	if err != nil {
		if se, ok := errors.AsType[*service.Error](err); ok && se.Code == service.CodeConflict {
			a.say("  ✓ cluster %q is already registered; checking it instead", slug)
			detail, err = b.CheckCluster(ctx, slug)
		}
		if err != nil {
			return err
		}
	}
	state.Cluster = detail.Slug
	state.Name = name
	if err := state.Save(); err != nil {
		return err
	}
	if _, err := a.configureNode(ctx, b, &state, false); err != nil {
		return err
	}
	a.say("  ✓ cluster %q registered, node %q added", detail.Slug, name)
	return a.write(detail)
}

// joinNode mints a join token, runs install.sh join here and waits for the
// node to appear among the cluster's members.
func (a *app) joinNode(ctx context.Context, b backend.Backend, state machine.State, name string, o *nodeAddOptions) error {
	if state.Mode == machine.ModeLocal {
		return preconditionError("a local-mode install can't join a cluster: reinstall with --public-host or --domain")
	}
	resolved, err := b.ResolveCluster(ctx, o.cluster)
	if err != nil {
		return err
	}
	if resolved.Runtime != state.Runtime {
		return preconditionError("cluster %q runs %s, but this install uses %s: install --runtime %s here first",
			resolved.Slug, resolved.Runtime, state.Runtime, resolved.Runtime)
	}
	if detail, err := b.GetCluster(ctx, resolved.Slug); err == nil {
		if node, ok := findNode(detail.NodeList, name); ok && node.Alive != nil && *node.Alive {
			a.say("%q is already an alive member of cluster %q", name, detail.Slug)
			state.Cluster = resolved.Slug
			state.Name = name
			if err := state.Save(); err != nil {
				return err
			}
			return a.write(node)
		}
	}

	join, err := b.CreateJoinToken(ctx, o.cluster, service.JoinTokenRequest{Role: o.role, TTL: "15m"})
	if err != nil {
		return err
	}
	a.say("  ✓ join token from the orchestrator (single use, expires in 15m)")
	if len(join.JoinURLs) == 0 {
		return preconditionError("cluster %q returned no join URL", resolved.Slug)
	}
	addr, err := dialAddressForJoin(join.JoinURLs[0])
	if err != nil {
		return preconditionError("%s", err)
	}
	if err := a.host().probe(ctx, addr); err != nil {
		return preconditionError("cannot reach %s: %v; open ports 7000-7003 between the cluster's nodes", addr, err)
	}
	a.say("  ✓ reached %s", addr)

	source := newInstallerSource(state.From)
	installer, err := source.Installer(ctx, join.Version)
	if err != nil {
		return preconditionError("%s", err)
	}
	if installer.Warning != "" {
		a.say("  ! %s", installer.Warning)
	}
	spec := machine.InstallSpec{}
	if installer.Options != nil {
		// A mirror has to be passed on, and with it the cluster's version:
		// re-running without --version would upgrade to the latest release.
		spec.Version = installer.Version
		spec.Options = installer.Options
	}
	args, err := mustInstallArgs(installer.Script, spec)
	if err != nil {
		return err
	}
	args = append(args, "join", join.JoinURLs[0],
		"--token", join.Token,
		"--ca-cert-hash", join.CACertHash,
		"--node-id", name,
		"--role", o.role,
	)
	a.say("Joining cluster %q", resolved.Slug)
	run := installerCommand(a.host().env, installer.Path, args, join.Token)
	if err := a.runInstaller(ctx, a.host().runner, run, false); err != nil {
		return err
	}
	if _, err := a.applySettings(ctx, a.host().runner, machine.EnvFileContent); err != nil {
		return err
	}

	node, err := a.waitForMember(ctx, b, resolved.Slug, name)
	if err != nil {
		return err
	}
	state.Cluster = resolved.Slug
	state.Name = name
	state.AerolVMVersion = join.Version
	if err := state.Save(); err != nil {
		return err
	}
	if _, err := a.configureNode(ctx, b, &state, false); err != nil {
		return err
	}
	a.say("  ✓ node %q joined cluster %q", name, resolved.Slug)
	return a.write(node)
}

// waitForMember polls the cluster's checks until name is an alive member.
func (a *app) waitForMember(ctx context.Context, b backend.Backend, slug, name string) (service.Node, error) {
	deadline := time.Now().Add(3 * time.Minute)
	a.say("  → waiting for %q to appear in cluster %q", name, slug)
	for {
		detail, err := b.CheckCluster(ctx, slug)
		if err == nil {
			if node, ok := findNode(detail.NodeList, name); ok && node.Alive != nil && *node.Alive {
				return node, nil
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				return service.Node{}, preconditionError("node %q did not join cluster %q within 3m: %v", name, slug, err)
			}
			return service.Node{}, preconditionError("node %q did not join cluster %q within 3m", name, slug)
		}
		a.host().sleep(ctx, 3*time.Second)
	}
}

// findNode looks up nodes by name in order.
func findNode(nodes []service.Node, names ...string) (service.Node, bool) {
	for _, name := range names {
		for _, node := range nodes {
			if node.Name == name {
				return node, true
			}
		}
	}
	return service.Node{}, false
}

// clusterDomainOf is the domain a new cluster takes from this install. Only a
// domain-mode install has one: a public-host install has no TLS.
func clusterDomainOf(state machine.State) string {
	if state.Mode == machine.ModeDomain {
		return state.Domain
	}
	return ""
}

// advertiseIP picks the address a public-host install should advertise.
func advertiseIP(state machine.State, explicit string) string {
	if explicit != "" {
		return explicit
	}
	if state.Mode == machine.ModePublicHost && net.ParseIP(state.PublicHost) != nil {
		return state.PublicHost
	}
	return ""
}
