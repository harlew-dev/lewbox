package machine

import (
	"fmt"
	"strings"
)

// InstallSpec is the installer invocation Lewbox wants to build.
type InstallSpec struct {
	Version     string // empty omits --version
	NodeName    string
	Local       bool
	Domain      string
	DNSToken    string
	ACMEEmail   string
	PublicHost  string
	Firecracker bool
	NoStart     bool
	PAScope     bool
	Options     []DownloadOption
	Passthrough []string
}

// InstallArgs maps a spec to install.sh arguments. Optional flags the script
// does not know are skipped so an older installer still works; a missing
// required flag is an error.
func InstallArgs(script []byte, spec InstallSpec) ([]string, error) {
	var args []string
	add := func(flag string, values ...string) {
		args = append(args, flag)
		args = append(args, values...)
	}
	required := func(flag string, ok bool, values ...string) error {
		if !ok {
			return fmt.Errorf("this install.sh does not support %s; use a newer AerolVM", flag)
		}
		add(flag, values...)
		return nil
	}

	if spec.Version != "" {
		if err := required("--version", supportsFlag(script, "--version"), spec.Version); err != nil {
			return nil, err
		}
	}
	for _, opt := range spec.Options {
		if supportsFlag(script, opt.Flag) {
			add(opt.Flag, opt.Value)
		}
	}
	if spec.PAScope && supportsFlag(script, "--pat-scope") {
		add("--pat-scope", "host")
	}
	if spec.NodeName != "" && supportsFlag(script, "--node-name") {
		add("--node-name", spec.NodeName)
	}
	if spec.Local {
		if err := required("--local", supportsFlag(script, "--local")); err != nil {
			return nil, err
		}
	}
	if spec.Domain != "" {
		if err := required("--domain", supportsFlag(script, "--domain"), spec.Domain); err != nil {
			return nil, err
		}
		if err := required("--dns-provider", supportsFlag(script, "--dns-provider"), "cloudflare"); err != nil {
			return nil, err
		}
		if err := required("--dns-api-token", supportsFlag(script, "--dns-api-token"), spec.DNSToken); err != nil {
			return nil, err
		}
		if spec.ACMEEmail != "" && supportsFlag(script, "--acme-email") {
			add("--acme-email", spec.ACMEEmail)
		}
	}
	if spec.PublicHost != "" {
		if err := required("--public-host", supportsFlag(script, "--public-host"), spec.PublicHost); err != nil {
			return nil, err
		}
	}
	if spec.Firecracker && supportsFlag(script, "--with-firecracker") {
		add("--with-firecracker")
	}
	if spec.NoStart && supportsFlag(script, "--no-start") {
		add("--no-start")
	}
	args = append(args, spec.Passthrough...)
	return args, nil
}

// supportsFlag looks for the flag in the script's usage text, where options
// are listed as "  --flag ...".
func supportsFlag(script []byte, flag string) bool {
	text := string(script)
	needle := "  " + flag
	for {
		i := strings.Index(text, needle)
		if i < 0 {
			return false
		}
		rest := text[i+len(needle):]
		if rest == "" {
			return true
		}
		r := rest[0]
		if r == ' ' || r == '\t' || r == '\n' || r == '=' || r == '<' || r == '[' || r == '\r' {
			return true
		}
		text = rest
	}
}
