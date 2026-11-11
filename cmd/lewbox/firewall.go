package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/machine"
)

// firewallOutput is printed by `lewbox firewall enable`.
type firewallOutput struct {
	Enabled bool                   `json:"enabled"`
	Reset   bool                   `json:"reset"`
	DryRun  bool                   `json:"dry_run"`
	Rules   []machine.FirewallRule `json:"rules"`
	Kept    []string               `json:"kept"`
}

type firewallOptions struct {
	allow  []string
	peers  []string
	reset  bool
	dryRun bool
}

// newFirewallCmd manages this machine's firewall.
func newFirewallCmd(a *app) *cobra.Command {
	firewall := &cobra.Command{
		Use:   "firewall",
		Short: "Manage this machine's firewall (ufw)",
	}

	o := &firewallOptions{}
	enable := &cobra.Command{
		Use:   "enable",
		Short: "Turn on ufw with the ports this Lewbox node needs",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runFirewallEnable(cmd.Context(), cmd, o)
		},
	}
	enable.Flags().StringSliceVar(&o.allow, "allow", nil, "also open PORT[/PROTO] to anyone (repeatable)")
	enable.Flags().StringSliceVar(&o.peers, "peers", nil, "the cluster's private network CIDR (repeatable)")
	enable.Flags().BoolVar(&o.reset, "reset", false, "reset the firewall to only Lewbox's rules")
	enable.Flags().BoolVar(&o.dryRun, "dry-run", false, "print the commands and change nothing")

	firewall.AddCommand(enable)
	return firewall
}

func (a *app) runFirewallEnable(ctx context.Context, cmd *cobra.Command, o *firewallOptions) error {
	if err := a.refuseRoot(); err != nil {
		return err
	}
	if a.host().env.GOOS != "linux" {
		return preconditionError("lewbox firewall needs Linux and ufw")
	}

	var allow []machine.FirewallRule
	for _, spec := range o.allow {
		rule, err := machine.ParseFirewallPort(spec, "allow")
		if err != nil {
			return usagef("invalid --allow %q: %v", spec, err)
		}
		allow = append(allow, rule)
	}
	var givenPeers []netip.Prefix
	for _, spec := range o.peers {
		prefix, err := parsePeerPrefix(spec)
		if err != nil {
			return usagef("invalid --peers %q: %v", spec, err)
		}
		givenPeers = append(givenPeers, prefix)
	}

	state, err := a.loadState()
	if err != nil {
		return err
	}
	runner := a.host().runner
	// read runs a command that only reads, printing it first like every
	// other privileged step.
	read := func(c machine.Cmd) ([]byte, error) {
		a.say("  → %s", c.Display())
		return runner.Output(ctx, c)
	}

	a.say("Checking this machine")
	if _, err := read(machine.Cmd{Name: "ufw", Args: []string{"version"}, Sudo: true}); err != nil {
		return preconditionError("ufw isn't installed: sudo apt-get install ufw")
	}

	sshPorts := sshPorts(read)

	var guard []machine.FirewallRule
	if out, err := read(machine.Cmd{Name: "iptables-save", Args: []string{"-t", "filter"}, Sudo: true}); err == nil {
		guard = machine.ParseNodeGuard(string(out))
	}
	if len(guard) == 0 {
		return preconditionError("AerolVM's sandbox network rules aren't loaded on this machine; start sandboxd (lewbox node add) and run this again")
	}

	peers := givenPeers
	if state.Mode != machine.ModeLocal {
		peers, err = a.clusterPeers(ctx, read, state, givenPeers)
		if err != nil {
			return err
		}
	}

	wanted := machine.NodeFirewall{
		Mode:     state.Mode,
		SSHPorts: sshPorts,
		Peers:    peers,
		Guard:    guard,
		Allow:    allow,
	}.Rules()

	var existing []machine.UFWRule
	for _, path := range []string{"/etc/ufw/user.rules", "/etc/ufw/user6.rules"} {
		out, err := read(machine.Cmd{Name: "cat", Args: []string{path}, Sudo: true})
		if err != nil {
			continue
		}
		existing = append(existing, machine.ParseUFWRules(string(out))...)
	}
	extra := machine.ExtraUFWRules(existing, wanted)

	reset := false
	switch {
	case len(extra) == 0:
		reset = false
	case cmd.Flags().Changed("reset"):
		reset = o.reset
	case a.yes || o.dryRun:
		reset = true
	case a.jsonMode() || !a.terminal():
		return usagef("this firewall has rules Lewbox doesn't set (%s): pass --reset to remove them or --reset=false to keep them", strings.Join(firewallLabels(extra), ", "))
	default:
		answer, err := askYesDefault(a.stdin, a.stderr, resetQuestion(extra))
		if err != nil {
			return err
		}
		reset = answer
	}

	a.say("Firewall (ufw)")
	run := func(c machine.Cmd) error {
		a.say("  → %s", c.Display())
		if o.dryRun {
			return nil
		}
		return runner.Run(ctx, c)
	}
	if reset {
		if err := run(machine.Cmd{Name: "ufw", Args: []string{"--force", "reset"}, Sudo: true}); err != nil {
			return err
		}
	}
	if err := run(machine.Cmd{Name: "ufw", Args: []string{"default", "deny", "incoming"}, Sudo: true}); err != nil {
		return err
	}
	if reset {
		if err := run(machine.Cmd{Name: "ufw", Args: []string{"default", "allow", "outgoing"}, Sudo: true}); err != nil {
			return err
		}
	}
	for _, rule := range wanted {
		if err := run(rule.Cmd()); err != nil {
			return err
		}
	}
	if err := run(machine.Cmd{Name: "ufw", Args: []string{"--force", "enable"}, Sudo: true}); err != nil {
		return err
	}

	if o.dryRun {
		a.say("  ✓ dry run: nothing changed")
	} else {
		a.say("  ✓ ufw enabled with %d Lewbox rules", len(wanted))
	}
	kept := make([]string, 0)
	if !reset {
		kept = firewallLabels(extra)
	}
	return a.write(firewallOutput{
		Enabled: !o.dryRun,
		Reset:   reset,
		DryRun:  o.dryRun,
		Rules:   wanted,
		Kept:    kept,
	})
}

// sshPorts reads the ports sshd listens on and the port this session came in
// on. A node always keeps SSH open.
func sshPorts(read func(machine.Cmd) ([]byte, error)) []string {
	var ports []string
	if out, err := read(machine.Cmd{Name: "sshd", Args: []string{"-T"}, Sudo: true}); err == nil {
		ports = machine.ParseSSHPorts(string(out))
	}
	if conn := os.Getenv("SSH_CONNECTION"); conn != "" {
		fields := strings.Fields(conn)
		if len(fields) >= 4 {
			if _, err := strconv.Atoi(fields[3]); err == nil {
				ports = append(ports, fields[3])
			}
		}
	}
	ports = uniqueStrings(ports)
	if len(ports) == 0 {
		return []string{"22"}
	}
	return ports
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// clusterPeers works out the private network this cluster's nodes use, and
// refuses when a node would fall outside it.
func (a *app) clusterPeers(ctx context.Context, read func(machine.Cmd) ([]byte, error), state machine.State, given []netip.Prefix) ([]netip.Prefix, error) {
	// LocalPAT runs this read itself; print it the same way.
	a.say("  → sudo cat /etc/sandboxd/sandboxd.env")
	pat, err := machine.LocalPAT(ctx, a.host().runner)
	if err != nil {
		return nil, preconditionError("can't read this cluster's members from AerolVM here (%v); is sandboxd running? Run lewbox node add first", err)
	}
	client := aerolvm.New(a.host().localAPIURL, pat, aerolvm.WithRetry(15*time.Second))
	members, err := client.Members(ctx)
	if err != nil {
		apiErr, ok := errors.AsType[*aerolvm.APIError](err)
		if !(ok && apiErr.Status == http.StatusServiceUnavailable && strings.Contains(apiErr.Message, "not enabled")) {
			return nil, preconditionError("can't read this cluster's members from AerolVM here (%v); is sandboxd running? Run lewbox node add first", err)
		}
	}

	addrs := make(map[string]netip.Addr)
	for _, member := range members.Members {
		addrPort, err := netip.ParseAddrPort(member.RaftAddr)
		if err != nil {
			continue
		}
		addrs[member.NodeName] = addrPort.Addr()
	}

	peers := given
	if len(peers) == 0 {
		self, ok := addrs[state.Name]
		if !ok {
			return nil, cantTellPeers()
		}
		ifaces, err := a.host().interfaceAddrs()
		if err != nil {
			return nil, cantTellPeers()
		}
		prefix, ok := machine.PeerPrefix(self, ifaces)
		if !ok {
			return nil, cantTellPeers()
		}
		peers = []netip.Prefix{prefix}
	}

	if outside := machine.UncoveredPeers(peers, addrs); len(outside) > 0 {
		formatted := make([]string, 0, len(peers))
		for _, p := range peers {
			formatted = append(formatted, p.String())
		}
		return nil, preconditionError("cluster nodes %s are outside --peers %s; the firewall would cut them off. Pass --peers with a network that covers every node", strings.Join(outside, ", "), strings.Join(formatted, ", "))
	}
	return peers, nil
}

func cantTellPeers() error {
	return preconditionError("can't tell which private network this cluster's nodes use; pass --peers <cidr> (for example --peers 10.0.0.0/24)")
}

func parsePeerPrefix(spec string) (netip.Prefix, error) {
	spec = strings.TrimSpace(spec)
	if prefix, err := netip.ParsePrefix(spec); err == nil {
		return prefix.Masked(), nil
	}
	addr, err := netip.ParseAddr(spec)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("want a CIDR or an IP address")
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

// resetQuestion is the prompt shown when ufw has rules Lewbox would not add.
func resetQuestion(extra []machine.UFWRule) string {
	var open, other []string
	for _, rule := range extra {
		if rule.Open() {
			open = append(open, rule.String())
		} else {
			other = append(other, rule.String())
		}
	}
	var first string
	if len(open) > 0 {
		first = fmt.Sprintf("You have %s open that Lewbox doesn't use.", strings.Join(open, ", "))
		if len(other) > 0 {
			first = fmt.Sprintf("You have %s open that Lewbox doesn't use (and %d other rules).", strings.Join(open, ", "), len(other))
		}
	} else {
		first = fmt.Sprintf("Your firewall has %d rules that Lewbox doesn't set (%s).", len(other), strings.Join(other, ", "))
	}
	return first + "\n" +
		"Resetting removes them and keeps only Lewbox's rules (ufw saves a backup in /etc/ufw). Answering no keeps them and adds Lewbox's.\n" +
		"Do you want to reset your firewall?"
}

// askYesDefault asks a yes/no question whose default answer is yes.
func askYesDefault(r io.Reader, w io.Writer, question string) (bool, error) {
	fmt.Fprintf(w, "%s [Y/n] ", question)
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	fmt.Fprintln(w)
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "" || answer == "y" || answer == "yes", nil
}

func firewallLabels(rules []machine.UFWRule) []string {
	labels := make([]string, 0, len(rules))
	for _, rule := range rules {
		labels = append(labels, rule.String())
	}
	return labels
}
