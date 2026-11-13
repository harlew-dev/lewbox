package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/harlew-dev/lewbox/internal/machine"
	"github.com/harlew-dev/lewbox/internal/service"
)

// installOutput is printed by `lewbox install`.
type installOutput struct {
	Name           string   `json:"name"`
	Mode           string   `json:"mode"`
	AerolVMVersion string   `json:"aerolvm_version"`
	APIURL         string   `json:"api_url"`
	Started        bool     `json:"started"`
	Next           []string `json:"next"`
}

type installOptions struct {
	name       string
	mode       string
	domain     string
	publicHost string
	runtime    string
	version    string
	from       string
	acmeEmail  string
	dnsProfile string
	dnsStdin   bool
	dryRun     bool
}

// newInstallCmd installs AerolVM on this machine and records the install.
func newInstallCmd(a *app) *cobra.Command {
	o := &installOptions{}
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install AerolVM on this machine",
		Long: "Install AerolVM on this machine through its own installer.\n\n" +
			"Everything after -- is passed to the installer. It prints every\n" +
			"privileged command before running it; --dry-run only prints them.",
		Args: usageArgs(cobra.ArbitraryArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runInstall(cmd, o, args)
		},
	}
	cmd.Flags().StringVar(&o.name, "name", "", "the node name (default: the short hostname, lowercased)")
	cmd.Flags().StringVar(&o.mode, "mode", "", "local, domain or public-host (default: local without --domain or --public-host)")
	cmd.Flags().StringVar(&o.domain, "domain", "", "the domain this node serves, with wildcard TLS")
	cmd.Flags().StringVar(&o.dnsProfile, "dns-profile", "", "the DNS profile whose Cloudflare token gets the certificate")
	cmd.Flags().BoolVar(&o.dnsStdin, "dns-api-token-stdin", false, "read the Cloudflare DNS token from stdin")
	cmd.Flags().StringVar(&o.acmeEmail, "acme-email", "", "the ACME account email")
	cmd.Flags().StringVar(&o.publicHost, "public-host", "", "the public host or IP, without TLS")
	cmd.Flags().StringVar(&o.runtime, "runtime", "docker", "runtime: docker or firecracker")
	cmd.Flags().StringVar(&o.version, "aerolvm-version", machine.PinnedVersion, "the AerolVM version to install")
	cmd.Flags().StringVar(&o.from, "from", "", "read AerolVM files from this mirror directory (env LEWBOX_AEROLVM_FROM)")
	cmd.Flags().BoolVar(&o.dryRun, "dry-run", false, "run the checks and print every command, but change nothing")
	return cmd
}

func (a *app) runInstall(cmd *cobra.Command, o *installOptions, passthrough []string) error {
	if err := a.refuseRoot(); err != nil {
		return err
	}
	mode, err := resolveInstallMode(o.mode, o.domain, o.publicHost)
	if err != nil {
		return err
	}
	if o.runtime != "docker" && o.runtime != "firecracker" {
		return usagef("--runtime must be docker or firecracker")
	}
	if o.name == "" {
		o.name = a.instanceName()
	}
	if o.dnsStdin && mode != machine.ModeDomain {
		return usagef("--dns-api-token-stdin only makes sense with --domain")
	}
	if o.dnsProfile != "" && mode != machine.ModeDomain {
		return usagef("--dns-profile only makes sense with --domain")
	}
	if o.dnsProfile != "" && o.dnsStdin {
		return usagef("--dns-profile and --dns-api-token-stdin cannot be used together")
	}
	dnsToken := ""
	if o.dnsStdin {
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return err
		}
		dnsToken = strings.TrimSpace(string(data))
		if dnsToken == "" {
			return usagef("--dns-api-token-stdin was given but stdin was empty")
		}
	}

	ctx := cmd.Context()
	// A profile supplies the token the installer writes for Caddy, so fetch
	// it (even for a dry run) and check its zone before anything else runs.
	var profile service.DNSProfileCredentials
	if o.dnsProfile != "" {
		b, _, err := a.resolve(ctx)
		if err != nil {
			return err
		}
		profile, err = b.DNSProfileCredentials(ctx, o.dnsProfile)
		if err != nil {
			return err
		}
		if !service.DomainInZone(o.domain, profile.Zone) {
			return usagef("--domain %s is not in DNS profile %q's zone %s", o.domain, profile.Name, profile.Zone)
		}
		if o.acmeEmail == "" {
			o.acmeEmail = profile.ACMEEmail
		}
		dnsToken = profile.APIToken
	}

	checks := machine.Check(ctx, a.host().env, machine.CheckOptions{
		Mode:          mode,
		Runtime:       o.runtime,
		Domain:        o.domain,
		DNSTokenStdin: o.dnsStdin,
		DNSProfile:    o.dnsProfile != "",
	})
	a.say("Checking this machine")
	a.printChecks(checks)
	if _, bad := machine.Failed(checks); bad {
		return checkFailure(checks, "this machine is not ready for AerolVM")
	}

	source := newInstallerSource(o.from)
	installer, err := source.Installer(ctx, o.version)
	if err != nil {
		return preconditionError("%s", err)
	}
	if installer.Warning != "" {
		a.say("  ! %s", installer.Warning)
	}
	a.say("AerolVM %s", installer.Version)
	a.say("  ✓ install.sh verified (sha256 checked)")
	if source.From != "" {
		a.say("  → using the local mirror; the installer downloads nothing")
	}

	args, err := mustInstallArgs(installer.Script, machine.InstallSpec{
		Version:     installer.Version,
		NodeName:    o.name,
		Local:       mode == machine.ModeLocal,
		Domain:      o.domain,
		DNSToken:    dnsToken,
		ACMEEmail:   o.acmeEmail,
		PublicHost:  o.publicHost,
		Firecracker: o.runtime == "firecracker",
		NoStart:     mode != machine.ModeLocal,
		PAScope:     mode != machine.ModeLocal,
		Options:     installer.Options,
		Passthrough: passthrough,
	})
	if err != nil {
		return err
	}

	state := machine.State{
		Name:              o.name,
		Mode:              mode,
		Domain:            o.domain,
		PublicHost:        o.publicHost,
		DNSProfile:        profile.Name,
		DNSProfileVersion: profile.Version,
		Runtime:           o.runtime,
		AerolVMVersion:    installer.Version,
		From:              source.From,
		InstalledAt:       a.host().now().UTC().Truncate(time.Millisecond),
	}
	state.StoredAPIURL = state.APIURL()

	run := installerCommand(a.host().env, installer.Path, args, dnsToken)
	if o.dryRun {
		a.runInstaller(ctx, a.host().runner, run, true)
		a.say("Dry run: nothing was changed")
		return a.write(installResult(state, false))
	}
	if err := a.confirm(fmt.Sprintf("Install AerolVM %s on this machine?", installer.Version)); err != nil {
		return err
	}
	if err := a.runInstaller(ctx, a.host().runner, run, false); err != nil {
		return err
	}
	if err := state.Save(); err != nil {
		return err
	}
	if mode == machine.ModeLocal {
		a.say("  ✓ installed; sandboxd is running at %s", state.APIURL())
	} else {
		a.say("  ✓ installed; sandboxd is stopped until this node starts or joins a cluster")
	}
	return a.write(installResult(state, mode == machine.ModeLocal))
}

func installResult(state machine.State, started bool) installOutput {
	next := []string{"lewbox node add"}
	if state.Mode != machine.ModeLocal {
		next = []string{
			"lewbox node add --new-cluster " + state.Name,
			"lewbox node add --cluster <node|cluster>",
		}
	}
	return installOutput{
		Name:           state.Name,
		Mode:           state.Mode,
		AerolVMVersion: state.AerolVMVersion,
		APIURL:         state.APIURL(),
		Started:        started,
		Next:           next,
	}
}

// resolveInstallMode picks the mode and rejects combinations that contradict
// each other.
func resolveInstallMode(mode, domain, publicHost string) (string, error) {
	mode = strings.TrimSpace(mode)
	if domain != "" && publicHost != "" {
		return "", usagef("--domain and --public-host cannot be used together")
	}
	if mode == "" {
		switch {
		case domain != "":
			mode = machine.ModeDomain
		case publicHost != "":
			mode = machine.ModePublicHost
		default:
			mode = machine.ModeLocal
		}
	}
	switch mode {
	case machine.ModeLocal:
		if domain != "" || publicHost != "" {
			return "", usagef("--mode local cannot be used with --domain or --public-host")
		}
	case machine.ModeDomain:
		if domain == "" {
			return "", usagef("--mode domain needs --domain")
		}
	case machine.ModePublicHost:
		if publicHost == "" {
			return "", usagef("--mode public-host needs --public-host")
		}
	default:
		return "", usagef("--mode must be local, domain or public-host")
	}
	return mode, nil
}
