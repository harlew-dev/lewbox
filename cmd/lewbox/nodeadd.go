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
	// Advanced settings for a new cluster.
	dockerTimeout      string
	destroyIfIdleFor   string
	maxAge             string
	maxSandboxCPU      float64
	maxSandboxMemoryMB int64
	maxSandboxDiskGB   int64
	idleStopMinutes    int64
	firecrackerVMSlots int64
	// advancedChanged names the advanced flags the caller actually set, so
	// an explicit zero is still refused with --cluster or --url.
	advancedChanged map[string]bool
}

// newClusterOnly reports whether any option that only applies to a new
// cluster was set.
func (o *nodeAddOptions) newClusterOnly() bool {
	return o.storageProfile != "" || o.stopPolicy != "" || o.retention != "" || o.snapshotName != "" ||
		len(o.advancedChanged) > 0
}

// advancedSettings is the requested advanced tuning, with only the flags that
// were set. The service normalizes and validates it before files are written.
func (o *nodeAddOptions) advancedSettings() service.AdvancedSettings {
	settings := service.AdvancedSettings{
		DockerTimeout:    o.dockerTimeout,
		DestroyIfIdleFor: o.destroyIfIdleFor,
		MaxAge:           o.maxAge,
	}
	if o.advancedChanged["max-sandbox-cpu"] {
		value := o.maxSandboxCPU
		settings.MaxSandboxCPU = &value
	}
	if o.advancedChanged["max-sandbox-memory-mb"] {
		value := o.maxSandboxMemoryMB
		settings.MaxSandboxMemoryMB = &value
	}
	if o.advancedChanged["max-sandbox-disk-gb"] {
		value := o.maxSandboxDiskGB
		settings.MaxSandboxDiskGB = &value
	}
	if o.advancedChanged["idle-stop-minutes"] {
		value := o.idleStopMinutes
		settings.IdleStopMinutes = &value
	}
	if o.advancedChanged["firecracker-vm-slots"] {
		value := o.firecrackerVMSlots
		settings.FirecrackerVMSlots = &value
	}
	return settings
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
			o.advancedChanged = map[string]bool{}
			for _, name := range []string{
				"max-sandbox-cpu", "max-sandbox-memory-mb", "max-sandbox-disk-gb",
				"idle-stop-minutes", "firecracker-vm-slots",
			} {
				if cmd.Flags().Changed(name) {
					o.advancedChanged[name] = true
				}
			}
			if o.dockerTimeout != "" {
				o.advancedChanged["docker-timeout"] = true
			}
			if o.destroyIfIdleFor != "" {
				o.advancedChanged["destroy-if-idle-for"] = true
			}
			if o.maxAge != "" {
				o.advancedChanged["max-age"] = true
			}
			if o.url != "" {
				if len(args) != 1 {
					return usagef("node add <name> --url <url>")
				}
				if o.cluster != "" || o.newCluster != "" || o.advertise != "" || o.adminURL != "" || o.endpointURL != "" ||
					o.newClusterOnly() {
					return usagef("--url cannot be combined with --cluster, --new-cluster, --advertise, --admin-url, --endpoint-url, --storage-profile, --stop-policy, --archive-local-retention, --archive-snapshot-name or an advanced setting")
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
	cmd.Flags().StringVar(&o.dockerTimeout, "docker-timeout", "", "a new cluster's sandboxd HTTP client timeout, from 1m to 2h (default 15m)")
	cmd.Flags().Float64Var(&o.maxSandboxCPU, "max-sandbox-cpu", 0, "the most CPUs one sandbox may have in a new cluster (default: no cap)")
	cmd.Flags().Int64Var(&o.maxSandboxMemoryMB, "max-sandbox-memory-mb", 0, "the most memory one sandbox may have in a new cluster, in MB (default: no cap)")
	cmd.Flags().Int64Var(&o.maxSandboxDiskGB, "max-sandbox-disk-gb", 0, "the most disk one sandbox may have in a new cluster, in GB (default: no cap)")
	cmd.Flags().Int64Var(&o.idleStopMinutes, "idle-stop-minutes", 0, "stop a sandbox idle this many minutes in a new cluster (default: off)")
	cmd.Flags().StringVar(&o.destroyIfIdleFor, "destroy-if-idle-for", "", "delete a sandbox idle this long in a new cluster, at most 720h (default: off)")
	cmd.Flags().StringVar(&o.maxAge, "max-age", "", "delete a sandbox this old in a new cluster, at most 720h (default: off)")
	cmd.Flags().Int64Var(&o.firecrackerVMSlots, "firecracker-vm-slots", 0, "a new Firecracker cluster's TAP pool size, 256-1024 (default 256)")
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
		return usagef("--storage-profile, --stop-policy, --archive-local-retention, --archive-snapshot-name and the advanced settings only apply to a new cluster: change them with lewbox cluster set")
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

	// Validate the requested advanced settings before writing files: AddCluster
	// runs its checks only after this node is configured, so an invalid value
	// must not reach the policy or env file.
	requested, err := service.NormalizeAdvanced(o.advancedSettings(), state.Runtime)
	if err != nil {
		return err
	}
	files := machine.NodeFiles{Runtime: state.Runtime, Advanced: machineAdvanced(requested)}
	// The cluster record, when it exists, names the runtime its create policy
	// allows and its advanced settings; before the first registration only
	// this install knows them. Never write defaults over a registered
	// cluster's settings, and fail closed when the orchestrator can't answer.
	if _, err := b.GetCluster(ctx, slug); err == nil {
		settings, err := b.ClusterNodeSettings(ctx, slug)
		if err != nil {
			return err
		}
		files = machineNodeFiles(settings, state.Mode)
	} else if !isNotFound(err) {
		return err
	}
	if _, err := a.applySettings(ctx, a.host().runner, files); err != nil {
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
		DockerTimeout:         requested.DockerTimeout,
		MaxSandboxCPU:         requested.MaxSandboxCPU,
		MaxSandboxMemoryMB:    requested.MaxSandboxMemoryMB,
		MaxSandboxDiskGB:      requested.MaxSandboxDiskGB,
		IdleStopMinutes:       requested.IdleStopMinutes,
		DestroyIfIdleFor:      requested.DestroyIfIdleFor,
		MaxAge:                requested.MaxAge,
		FirecrackerVMSlots:    requested.FirecrackerVMSlots,
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
	// The cluster's settings decide the runtime, size caps, timers and push
	// lines, so a join does not drop snapshot push.
	settings, err := b.ClusterNodeSettings(ctx, resolved.Slug)
	if err != nil {
		return err
	}
	if _, err := a.applySettings(ctx, a.host().runner, machineNodeFiles(settings, state.Mode)); err != nil {
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

// isNotFound reports a service 404, such as a cluster that is not registered
// yet.
func isNotFound(err error) bool {
	se, ok := errors.AsType[*service.Error](err)
	return ok && se.Code == service.CodeNotFound
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
