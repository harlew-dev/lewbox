package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/harlew-dev/lewbox/internal/machine"
	"github.com/harlew-dev/lewbox/internal/service"
)

// updateCheckOutput is printed by `lewbox update --check`.
type updateCheckOutput struct {
	Current          string `json:"current"`
	Latest           string `json:"latest"`
	AerolVMInstalled string `json:"aerolvm_installed"`
	AerolVMTarget    string `json:"aerolvm_target"`
	UpdateAvailable  bool   `json:"update_available"`
}

// updateOutput is printed by `lewbox update`.
type updateOutput struct {
	Version        string `json:"version"`
	Replaced       bool   `json:"replaced"`
	AerolVMVersion string `json:"aerolvm_version,omitempty"`
}

type updateOptions struct {
	version        string
	releaseFrom    string
	aerolvmVersion string
	from           string
	check          bool
	aerolvm        bool
	aerolvmOnly    bool
}

// newUpdateCmd updates the CLI and, with --aerolvm, AerolVM on this machine.
func newUpdateCmd(a *app) *cobra.Command {
	o := &updateOptions{}
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update the Lewbox CLI, and with --aerolvm AerolVM here",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runUpdate(cmd.Context(), o)
		},
	}
	cmd.Flags().StringVar(&o.version, "version", "", "a specific Lewbox release tag (default: the latest)")
	cmd.Flags().BoolVar(&o.check, "check", false, "show what would change, and change nothing")
	cmd.Flags().StringVar(&o.releaseFrom, "release-from", "", "read a Lewbox release from this directory (env LEWBOX_RELEASE_FROM)")
	cmd.Flags().BoolVar(&o.aerolvm, "aerolvm", false, "also update AerolVM on this machine")
	cmd.Flags().StringVar(&o.aerolvmVersion, "aerolvm-version", "", "the AerolVM version to move to (default: the release's tested version)")
	cmd.Flags().StringVar(&o.from, "from", "", "read AerolVM files from this mirror directory (env LEWBOX_AEROLVM_FROM)")
	cmd.Flags().BoolVar(&o.aerolvmOnly, "aerolvm-only", false, "only update AerolVM here")
	_ = cmd.Flags().MarkHidden("aerolvm-only")
	return cmd
}

func (a *app) runUpdate(ctx context.Context, o *updateOptions) error {
	if err := a.refuseRoot(); err != nil {
		return err
	}
	if o.aerolvmOnly {
		return a.updateAerolVM(ctx, o)
	}

	source := a.releaseSource(o.releaseFrom)
	release, err := source.Release(ctx, o.version)
	if err != nil {
		return preconditionError("%s", err)
	}
	target := o.aerolvmVersion
	if target == "" {
		target = release.AerolVMVersion
	}
	state, stateErr := machine.Load()

	if o.check {
		installed := ""
		if stateErr == nil {
			installed = state.AerolVMVersion
		}
		return a.write(updateCheckOutput{
			Current:          version,
			Latest:           release.Version,
			AerolVMInstalled: installed,
			AerolVMTarget:    target,
			UpdateAvailable:  release.Version != version,
		})
	}
	if err := a.confirm(fmt.Sprintf("Update lewbox from %s to %s?", version, release.Version)); err != nil {
		return err
	}

	out := updateOutput{Version: release.Version}
	path, err := executablePath(a.host().executable)
	if err != nil {
		return err
	}
	if release.Version != version {
		binary, err := source.Asset(ctx, o.version, source.BinaryName())
		if err != nil {
			return preconditionError("%s", err)
		}
		want, err := source.Checksum(ctx, o.version, source.BinaryName())
		if err != nil {
			return preconditionError("%s", err)
		}
		if err := machine.VerifySum(binary, want); err != nil {
			return preconditionError("%s does not match checksums.txt: %v", source.BinaryName(), err)
		}
		if err := replaceBinary(path, binary); err != nil {
			return err
		}
		out.Replaced = true
		a.say("  ✓ replaced %s with %s", path, release.Version)
	} else {
		a.say("already on %s", version)
	}

	if o.aerolvm {
		args := []string{"update", "--aerolvm-only", "--aerolvm-version", target}
		if o.from != "" {
			args = append(args, "--from", o.from)
		}
		if o.releaseFrom != "" {
			args = append(args, "--release-from", o.releaseFrom)
		}
		a.say("  → %s %s", path, strings.Join(args, " "))
		if err := a.host().execFn(path, append([]string{path}, args...), os.Environ()); err != nil {
			return err
		}
	}
	return a.write(out)
}

// updateAerolVM re-runs the installer for a new version on this machine.
func (a *app) updateAerolVM(ctx context.Context, o *updateOptions) error {
	state, err := machine.Load()
	if machine.IsNotInstalled(err) {
		a.say("no AerolVM on this machine")
		return a.write(updateOutput{Version: version})
	}
	if err != nil {
		return err
	}
	target := o.aerolvmVersion
	if target == "" {
		target = machine.PinnedVersion
	}

	// Upgrading one Raft member on its own leaves the cluster on mixed
	// versions, so a cluster of more than one node needs the rolling upgrade.
	// Ask this machine's AerolVM, not the orchestrator: it is the authority,
	// and an orchestrator that can't be reached must not let the upgrade
	// through.
	members, err := a.localClusterSize(ctx, a.host().runner)
	if err != nil {
		return preconditionError("can't tell how many nodes this machine's cluster has, so not upgrading it alone: %v", err)
	}
	if members > 1 {
		slug := state.Cluster
		if slug == "" {
			slug = "<cluster>"
		}
		return preconditionError("this machine is one of %d nodes in a cluster: run lewbox cluster upgrade %s instead", members, slug)
	}

	from := o.from
	if from == "" {
		from = os.Getenv("LEWBOX_AEROLVM_FROM")
	}
	if from == "" {
		from = state.From
	}
	source := newInstallerSource(from)
	installer, err := source.Installer(ctx, target)
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
	a.say("Updating AerolVM to %s", installer.Version)
	run := installerCommand(a.host().env, installer.Path, args)
	if err := a.runInstaller(ctx, a.host().runner, run, false); err != nil {
		return err
	}
	if _, err := a.applySettings(ctx, a.host().runner, machine.EnvFileContent); err != nil {
		return err
	}
	if _, err := machine.WaitHealth(ctx, a.host().localAPIURL, installer.Version, a.host().sleep); err != nil {
		return preconditionError("%s", err)
	}
	state.AerolVMVersion = installer.Version
	if err := state.Save(); err != nil {
		return err
	}
	a.say("  ✓ AerolVM is now %s", state.AerolVMVersion)
	return a.write(updateOutput{Version: version, AerolVMVersion: state.AerolVMVersion})
}

// releaseSource builds the Lewbox release source for this command.
func (a *app) releaseSource(dir string) *machine.ReleaseSource {
	source := machine.NewReleaseSource(dir)
	source.BaseURL = a.host().releaseURL
	source.GOOS = runtime.GOOS
	source.GOARCH = runtime.GOARCH
	return source
}

// replaceBinary writes data next to target and renames it over target. When
// the directory is not writable it returns a precondition naming the exact
// sudo command to run.
func replaceBinary(target string, data []byte) error {
	dir := filepath.Dir(target)
	f, err := os.CreateTemp(dir, ".lewbox-update-*")
	if err != nil {
		staged, stageErr := stageBinary(data)
		if stageErr != nil {
			return err
		}
		return &service.Error{
			Code:    service.CodePrecondition,
			Message: fmt.Sprintf("%s is not writable: run: sudo install -m 0755 %s %s", dir, staged, target),
		}
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o755); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("replace %s: %w", target, err)
	}
	return nil
}

// stageBinary keeps the verified binary somewhere the user can point a sudo
// install at.
func stageBinary(data []byte) (string, error) {
	f, err := os.CreateTemp("", "lewbox-update-*")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return "", err
	}
	if err := f.Chmod(0o755); err != nil {
		return "", err
	}
	return f.Name(), nil
}
