package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/backend"
	"github.com/harlew-dev/lewbox/internal/machine"
	"github.com/harlew-dev/lewbox/internal/service"
)

// configureOutput is printed by `lewbox node configure` and by `node add`
// after it registered the node.
type configureOutput struct {
	Node            string `json:"node"`
	Cluster         string `json:"cluster"`
	Storage         string `json:"storage"` // applied, removed or none
	RegistryChanged bool   `json:"registry_changed"`
	EnvChanged      bool   `json:"env_changed"`
	DNS             string `json:"dns"` // updated, unchanged or none
	CleanupTimer    bool   `json:"cleanup_timer"`
}

// newConfigureNodeCmd applies a cluster's node settings on this machine.
func newConfigureNodeCmd(a *app) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Apply this cluster's storage and DNS settings on this machine",
		Long: "Fetch the cluster's node settings and apply them here: the snapshot\n" +
			"registry and its archive cleanup timer (none when the cluster keeps\n" +
			"local archive copies forever), Lewbox's env file, and a\n" +
			"changed DNS profile's token. Run it on every node whose cluster\n" +
			"changed a profile.",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := a.refuseRoot(); err != nil {
				return err
			}
			state, err := a.loadState()
			if err != nil {
				return err
			}
			if state.Cluster == "" {
				return preconditionError("this machine is in no cluster yet: run lewbox node add first")
			}
			b, _, err := a.resolve(cmd.Context())
			if err != nil {
				return err
			}
			out, err := a.configureNode(cmd.Context(), b, &state, dryRun)
			if err != nil {
				return err
			}
			return a.write(out)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would change, and change nothing")
	return cmd
}

// newCleanupNodeCmd runs one archive cleanup pass on this node.
func newCleanupNodeCmd(a *app) *cobra.Command {
	var olderThan time.Duration
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Remove this node's local copies of archived sandboxes",
		Long: "Remove the local images of snapshots that are archived in the cluster's\n" +
			"bucket. The lewbox-archive-cleanup.timer runs this as root every five\n" +
			"minutes on a cluster with storage, with --older-than set from the\n" +
			"cluster's archive_local_retention.",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if olderThan < 0 {
				return usagef("--older-than must not be negative")
			}
			ctx := cmd.Context()
			sudo := a.host().euid() != 0
			if sudo {
				a.say("  → sudo cat /etc/sandboxd/sandboxd.env")
			} else {
				a.say("  → cat /etc/sandboxd/sandboxd.env")
			}
			pat, err := machine.LocalPATWithSudo(ctx, a.host().runner, sudo)
			if err != nil {
				return err
			}
			client := aerolvm.New(a.host().localAPIURL, pat)
			result, err := machine.CleanupArchives(ctx, a.host().runner, client, sudo, olderThan, a.host().now())
			if err != nil {
				return err
			}
			return a.write(result)
		},
	}
	cmd.Flags().DurationVar(&olderThan, "older-than", 0, "only remove copies at least this old, such as 24h (default: all)")
	return cmd
}

// configureNode applies a cluster's node settings: the snapshot registry and
// its cleanup timer, Lewbox's env file, and a changed DNS profile. It reports
// what it applied. With dryRun it prints what it would do and changes nothing.
func (a *app) configureNode(ctx context.Context, b backend.Backend, state *machine.State, dryRun bool) (configureOutput, error) {
	settings, err := b.ClusterNodeSettings(ctx, state.Cluster)
	if err != nil {
		return configureOutput{}, err
	}
	out := configureOutput{Node: state.Name, Cluster: settings.Cluster, Storage: "none", DNS: "none"}
	runner := a.host().runner
	report := func(cmd machine.Cmd) { a.say("  → %s", cmd.Display()) }

	// Local mode has no cluster to archive to: storage is skipped entirely.
	if state.Mode != machine.ModeLocal {
		switch {
		case settings.Storage != nil:
			age, cleanup := service.ArchiveRetentionAge(settings.ArchiveLocalRetention)
			if dryRun {
				if cleanup {
					a.say("  → would install the registry for storage profile %q and the archive cleanup timer", settings.Storage.Profile)
				} else {
					a.say("  → would install the registry for storage profile %q, with no archive cleanup timer (retention never)", settings.Storage.Profile)
				}
				out.Storage, out.RegistryChanged, out.CleanupTimer = "applied", true, true
				break
			}
			binary, err := newRegistrySource(state.From).Binary(ctx)
			if err != nil {
				return out, preconditionError("%s", err)
			}
			a.say("Registry for storage profile %q", settings.Storage.Profile)
			changed, err := machine.ApplyRegistry(ctx, runner, binary, registryStorage(*settings.Storage), report)
			if err != nil {
				return out, err
			}
			out.RegistryChanged = changed
			if err := a.host().waitRegistry(ctx); err != nil {
				return out, err
			}
			var timer bool
			if cleanup {
				timer, err = a.applyCleanupTimer(ctx, runner, age, report)
			} else {
				timer, err = a.removeCleanupTimer(ctx, runner, report)
			}
			if err != nil {
				return out, err
			}
			out.CleanupTimer = timer
			out.Storage = "applied"
		case settings.Storage == nil:
			if dryRun {
				a.say("  → would remove the registry and the archive cleanup timer")
				out.Storage = "removed"
				break
			}
			changed, err := machine.RemoveRegistry(ctx, runner, report)
			if err != nil {
				return out, err
			}
			timer, err := a.removeCleanupTimer(ctx, runner, report)
			if err != nil {
				return out, err
			}
			out.RegistryChanged = changed
			out.CleanupTimer = timer
			if changed || timer {
				out.Storage = "removed"
			}
		}
	}

	// Lewbox's env file carries the push settings when there is storage.
	envContent := machine.EnvFileContent
	if settings.Storage != nil && state.Mode != machine.ModeLocal {
		envContent += machine.PushEnv(settings.ClusterID)
	}
	if dryRun {
		a.say("  → would write %s", machine.EnvironmentFile)
	} else {
		changed, err := a.applySettings(ctx, runner, envContent)
		if err != nil {
			return out, err
		}
		out.EnvChanged = changed
	}

	if state.Mode == machine.ModeDomain && settings.DNS != nil {
		switch {
		case state.DNSProfile == settings.DNS.Profile && state.DNSProfileVersion == settings.DNS.Version:
			out.DNS = "unchanged"
		case dryRun:
			a.say("  → would re-run the installer with DNS profile %q", settings.DNS.Profile)
			out.DNS = "updated"
		default:
			if err := a.reinstallDNS(ctx, *state, settings.DNS); err != nil {
				return out, err
			}
			state.DNSProfile = settings.DNS.Profile
			state.DNSProfileVersion = settings.DNS.Version
			out.DNS = "updated"
		}
	}

	if dryRun {
		a.say("Dry run: nothing was changed")
		return out, nil
	}

	req := service.ReportNodeSettingsRequest{}
	if settings.Storage != nil && state.Mode != machine.ModeLocal {
		name := settings.Storage.Profile
		req.StorageProfile = &name
		req.StorageVersion = settings.Storage.Version
		retention := settings.ArchiveLocalRetention
		req.ArchiveRetention = &retention
	}
	if state.DNSProfile != "" && state.DNSProfileVersion >= 1 {
		name := state.DNSProfile
		req.DNSProfile = &name
		req.DNSVersion = state.DNSProfileVersion
	}
	reportAs, err := reportNodeName(ctx, b, state)
	if err != nil {
		return out, err
	}
	if _, err := b.ReportNodeSettings(ctx, reportAs, req); err != nil {
		return out, err
	}
	if err := state.Save(); err != nil {
		return out, err
	}
	return out, nil
}

// reportNodeName is the orchestrator's name for this machine. It is the
// state's name, except in local mode: AerolVM outside cluster mode names its
// only member "standalone", so the one-node cluster's single node is this
// machine whatever it is called.
func reportNodeName(ctx context.Context, b backend.Backend, state *machine.State) (string, error) {
	if state.Mode != machine.ModeLocal {
		return state.Name, nil
	}
	detail, err := b.GetCluster(ctx, state.Cluster)
	if err != nil {
		return "", err
	}
	if len(detail.NodeList) != 1 {
		return "", preconditionError("local-mode cluster %q has %d nodes, want 1: run lewbox cluster check %s", state.Cluster, len(detail.NodeList), state.Cluster)
	}
	return detail.NodeList[0].Name, nil
}

// registryStorage turns a cluster's storage settings into the registry's
// bucket config.
func registryStorage(s service.NodeStorageSettings) machine.RegistryStorage {
	return machine.RegistryStorage{
		Endpoint:        s.Endpoint,
		Region:          s.Region,
		Bucket:          s.Bucket,
		RootDirectory:   s.RootDirectory,
		PathStyle:       s.PathStyle,
		AccessKeyID:     s.AccessKeyID,
		SecretAccessKey: s.SecretAccessKey,
	}
}

// applyCleanupTimer installs the root-owned lewbox copy the timer runs, its
// service and timer units, and enables the timer. The service removes local
// copies at least olderThan old. It reports whether anything changed.
func (a *app) applyCleanupTimer(ctx context.Context, runner machine.Runner, olderThan time.Duration, report func(machine.Cmd)) (bool, error) {
	run := func(cmd machine.Cmd) error {
		if report != nil {
			report(cmd)
		}
		return runner.Run(ctx, cmd)
	}
	output := func(cmd machine.Cmd) ([]byte, error) {
		if report != nil {
			report(cmd)
		}
		return runner.Output(ctx, cmd)
	}
	changed := false

	exe, err := executablePath(a.host().executable)
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", exe, err)
	}
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])
	have := ""
	if out, err := output(machine.Cmd{Name: "sha256sum", Args: []string{machine.CleanupBinary}, Sudo: true}); err == nil {
		if fields := strings.Fields(string(out)); len(fields) > 0 {
			have = fields[0]
		}
	}
	if have != want {
		if err := run(machine.Cmd{
			Name: "install",
			Args: []string{"-D", "-m", "0755", "-o", "root", "-g", "root", exe, machine.CleanupBinary},
			Sudo: true,
		}); err != nil {
			return false, err
		}
		changed = true
	}

	wantUnit := machine.CleanupServiceUnit(olderThan)
	unit, _ := readRootFile(output, machine.CleanupService)
	if unit != wantUnit {
		if err := writeRootFile(run, machine.CleanupService, "0644", wantUnit); err != nil {
			return false, err
		}
		changed = true
	}
	timer, _ := readRootFile(output, machine.CleanupTimer)
	if timer != machine.CleanupTimerUnit {
		if err := writeRootFile(run, machine.CleanupTimer, "0644", machine.CleanupTimerUnit); err != nil {
			return false, err
		}
		changed = true
	}
	if changed {
		if err := run(machine.Cmd{Name: "systemctl", Args: []string{"daemon-reload"}, Sudo: true}); err != nil {
			return false, err
		}
	}
	if err := run(machine.Cmd{Name: "systemctl", Args: []string{"enable", "--now", machine.CleanupTimerName}, Sudo: true}); err != nil {
		return false, err
	}
	return changed, nil
}

// removeCleanupTimer stops and removes the archive cleanup timer, its service
// and the root-owned binary copy, when they are present.
func (a *app) removeCleanupTimer(ctx context.Context, runner machine.Runner, report func(machine.Cmd)) (bool, error) {
	run := func(cmd machine.Cmd) error {
		if report != nil {
			report(cmd)
		}
		return runner.Run(ctx, cmd)
	}
	output := func(cmd machine.Cmd) ([]byte, error) {
		if report != nil {
			report(cmd)
		}
		return runner.Output(ctx, cmd)
	}
	exists := func(path string) bool {
		_, err := output(machine.Cmd{Name: "test", Args: []string{"-e", path}, Sudo: true})
		return err == nil
	}
	if !exists(machine.CleanupTimer) && !exists(machine.CleanupService) && !exists(machine.CleanupBinary) {
		return false, nil
	}
	steps := []machine.Cmd{
		{Name: "systemctl", Args: []string{"disable", "--now", machine.CleanupTimerName}, Sudo: true},
		{Name: "rm", Args: []string{"-f", machine.CleanupService, machine.CleanupTimer, machine.CleanupBinary}, Sudo: true},
		{Name: "systemctl", Args: []string{"daemon-reload"}, Sudo: true},
	}
	for _, step := range steps {
		if err := run(step); err != nil {
			return true, err
		}
	}
	return true, nil
}

// reinstallDNS re-runs the verified installer with the DNS profile's token.
// The installer keeps its other saved options.
func (a *app) reinstallDNS(ctx context.Context, state machine.State, dns *service.NodeDNSSettings) error {
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
		Options: installer.Options,
	})
	if err != nil {
		return err
	}
	args = append(args, "--dns-provider", "cloudflare", "--dns-api-token", dns.APIToken)
	a.say("Updating DNS for %s with profile %q", dns.Domain, dns.Profile)
	run := installerCommand(a.host().env, installer.Path, args, dns.APIToken)
	return a.runInstaller(ctx, a.host().runner, run, false)
}

func readRootFile(output func(machine.Cmd) ([]byte, error), path string) (string, error) {
	out, err := output(machine.Cmd{Name: "cat", Args: []string{path}, Sudo: true})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func writeRootFile(run func(machine.Cmd) error, path, mode, content string) error {
	return run(machine.Cmd{
		Name:  "install",
		Args:  []string{"-m", mode, "/dev/stdin", path},
		Sudo:  true,
		Stdin: strings.NewReader(content),
	})
}
