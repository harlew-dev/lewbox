package machine

import (
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
)

// FirewallRule is one port ufw should allow. From is "any" for anyone,
// otherwise a CIDR. Port is a single port ("22") or a ufw range
// ("22000:23000").
type FirewallRule struct {
	Port    string `json:"port"`
	Proto   string `json:"proto"`
	From    string `json:"from"`
	Purpose string `json:"purpose"`
}

// Cmd returns the ufw command that adds the rule. Every argument is a
// separate argv entry, so nothing is interpreted by a shell.
func (r FirewallRule) Cmd() Cmd {
	return Cmd{
		Name: "ufw",
		Args: []string{
			"allow", "proto", r.Proto,
			"from", r.From,
			"to", "any",
			"port", r.Port,
			"comment", "lewbox:" + r.Purpose,
		},
		Sudo: true,
	}
}

// UFWRule is one rule parsed from ufw's user.rules files.
type UFWRule struct {
	Action    string
	Proto     string
	Port      string
	To        string
	From      string
	App       string
	Direction string
}

// ParseUFWRules reads the "### tuple ###" lines ufw writes to
// /etc/ufw/user.rules and user6.rules. Lines it cannot parse are skipped.
func ParseUFWRules(content string) []UFWRule {
	const prefix = "### tuple ### "
	var out []UFWRule
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, prefix))
		kept := make([]string, 0, len(fields))
		for _, f := range fields {
			if strings.HasPrefix(f, "comment=") {
				continue
			}
			kept = append(kept, f)
		}
		var u UFWRule
		switch len(kept) {
		case 9:
			u = UFWRule{
				Action:    kept[0],
				Proto:     kept[1],
				Port:      kept[2],
				To:        kept[3],
				From:      kept[5],
				App:       appName(kept[6], kept[7]),
				Direction: kept[8],
			}
		case 7:
			u = UFWRule{
				Action:    kept[0],
				Proto:     kept[1],
				Port:      kept[2],
				To:        kept[3],
				From:      kept[5],
				Direction: kept[6],
			}
		default:
			continue
		}
		u.To = normalizeAnyAddr(u.To)
		u.From = normalizeAnyAddr(u.From)
		out = append(out, u)
	}
	return out
}

func appName(dapp, sapp string) string {
	if dapp != "" && dapp != "-" {
		return dapp
	}
	if sapp != "" && sapp != "-" {
		return sapp
	}
	return ""
}

// normalizeAnyAddr gives an address the form ufw stores it in: "any" for
// every address, and a single host without its /32 or /128 (ufw saves
// "from 10.0.0.5/32" as "10.0.0.5").
func normalizeAnyAddr(s string) string {
	switch s {
	case "0.0.0.0/0", "::/0", "any":
		return "any"
	}
	if prefix, err := netip.ParsePrefix(s); err == nil && prefix.IsSingleIP() {
		return prefix.Addr().String()
	}
	return s
}

// Matches reports whether u is already the rule r asks for.
func (u UFWRule) Matches(r FirewallRule) bool {
	return u.Action == "allow" &&
		u.Direction == "in" &&
		u.App == "" &&
		u.To == "any" &&
		u.Proto == r.Proto &&
		u.Port == r.Port &&
		u.From == normalizeAnyAddr(r.From)
}

// Open reports whether u keeps a port open to incoming traffic.
func (u UFWRule) Open() bool {
	return (u.Action == "allow" || u.Action == "limit") && u.Direction == "in"
}

// String is a short label for prompts and error messages.
func (u UFWRule) String() string {
	label := u.Port
	switch {
	case u.Port == "any":
		label = "all ports"
	case u.Proto != "" && u.Proto != "any":
		label = u.Port + "/" + u.Proto
	}
	if u.App != "" {
		label = u.App
	}
	if u.From != "" && u.From != "any" {
		label += " from " + u.From
	}
	if u.Action != "" && u.Action != "allow" {
		label = u.Action + " " + label
	}
	if u.Direction != "" && u.Direction != "in" && !strings.HasPrefix(u.Action, "route:") {
		label = u.Direction + " " + label
	}
	return label
}

// ExtraUFWRules returns the existing rules no wanted rule matches, with the
// IPv4 and IPv6 copies of one rule shown once.
func ExtraUFWRules(existing []UFWRule, wanted []FirewallRule) []UFWRule {
	var out []UFWRule
	seen := make(map[string]bool)
	for _, u := range existing {
		matched := false
		for _, r := range wanted {
			if u.Matches(r) {
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		label := u.String()
		if seen[label] {
			continue
		}
		seen[label] = true
		out = append(out, u)
	}
	return out
}

// ParseNodeGuard reads the sandbox-to-host allow rules out of
// `iptables-save -t filter`. AerolVM's node guard adds them so sandboxes can
// reach the egress proxy and DNS; ufw's default-deny INPUT would drop them.
func ParseNodeGuard(iptablesSave string) []FirewallRule {
	var out []FirewallRule
	seen := make(map[string]bool)
	for _, line := range strings.Split(iptablesSave, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		var hasComment, returns bool
		for i := 0; i+1 < len(fields); i++ {
			switch {
			case fields[i] == "--comment" && fields[i+1] == "sbx-node-guard":
				hasComment = true
			case fields[i] == "-j" && fields[i+1] == "RETURN":
				returns = true
			}
		}
		if !hasComment || !returns {
			continue
		}
		var src, proto, port string
		for i := 0; i+1 < len(fields); i++ {
			switch fields[i] {
			case "-s":
				src = fields[i+1]
			case "-p":
				proto = fields[i+1]
			case "--dport":
				port = fields[i+1]
			}
		}
		if src == "" || port == "" {
			continue
		}
		if proto != "tcp" && proto != "udp" {
			continue
		}
		rule := FirewallRule{Port: port, Proto: proto, From: src, Purpose: "sandbox-to-host"}
		key := rule.Port + "\x00" + rule.Proto + "\x00" + rule.From
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, rule)
	}
	return out
}

// ParseSSHPorts reads the port lines from `sshd -T`.
func ParseSSHPorts(sshdT string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(sshdT, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "port" {
			continue
		}
		port := fields[1]
		if seen[port] {
			continue
		}
		seen[port] = true
		out = append(out, port)
	}
	return out
}

// ParseFirewallPort reads a --allow spec: 8080, 8080/tcp, 53/udp or
// 60000:61000/udp.
func ParseFirewallPort(spec, purpose string) (FirewallRule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return FirewallRule{}, fmt.Errorf("missing port")
	}
	proto := "tcp"
	portSpec := spec
	if i := strings.IndexByte(spec, '/'); i >= 0 {
		portSpec = spec[:i]
		proto = spec[i+1:]
		if proto != "tcp" && proto != "udp" {
			return FirewallRule{}, fmt.Errorf("protocol %q must be tcp or udp", proto)
		}
	}
	port, err := parsePortSpec(portSpec)
	if err != nil {
		return FirewallRule{}, err
	}
	return FirewallRule{Port: port, Proto: proto, From: "any", Purpose: purpose}, nil
}

func parsePortSpec(spec string) (string, error) {
	if start, end, ok := strings.Cut(spec, ":"); ok {
		first, err := parsePortNumber(start)
		if err != nil {
			return "", err
		}
		last, err := parsePortNumber(end)
		if err != nil {
			return "", err
		}
		if first >= last {
			return "", fmt.Errorf("range %q must start below its end", spec)
		}
		return fmt.Sprintf("%d:%d", first, last), nil
	}
	port, err := parsePortNumber(spec)
	if err != nil {
		return "", err
	}
	return strconv.Itoa(port), nil
}

func parsePortNumber(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("port %q must be 1-65535", s)
	}
	return n, nil
}

// NodeFirewall describes the ports one Lewbox node needs.
type NodeFirewall struct {
	Mode     string
	SSHPorts []string
	Peers    []netip.Prefix
	Guard    []FirewallRule
	Allow    []FirewallRule
}

// Rules returns the node's ports in the order they are opened.
func (n NodeFirewall) Rules() []FirewallRule {
	var rules []FirewallRule
	add := func(port, proto, from, purpose string) {
		rules = append(rules, FirewallRule{Port: port, Proto: proto, From: from, Purpose: purpose})
	}

	for _, port := range n.SSHPorts {
		add(port, "tcp", "any", "ssh")
	}

	switch n.Mode {
	case ModePublicHost:
		add("80", "tcp", "any", "sandbox-http")
		add("21212", "tcp", "any", "aerolvm-api")
		add("2220", "tcp", "any", "ssh-gateway")
		add("22000:23000", "tcp", "any", "sandbox-tcp")
	case ModeDomain:
		add("443", "tcp", "any", "sandbox-https")
		add("2220", "tcp", "any", "ssh-gateway")
		add("22000:23000", "tcp", "any", "sandbox-tcp")
	}

	if n.Mode != ModeLocal {
		for _, peer := range n.Peers {
			from := peer.String()
			add("7000:7003", "tcp", from, "cluster")
			add("7001", "udp", from, "cluster")
			if n.Mode == ModeDomain {
				add("21212", "tcp", from, "cluster-api")
			}
		}
	}

	rules = append(rules, n.Guard...)
	rules = append(rules, n.Allow...)
	return dedupeFirewallRules(rules)
}

func dedupeFirewallRules(rules []FirewallRule) []FirewallRule {
	out := make([]FirewallRule, 0, len(rules))
	seen := make(map[string]bool)
	for _, r := range rules {
		key := r.Port + "\x00" + r.Proto + "\x00" + r.From
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}

var cgnatPrefix = netip.MustParsePrefix("100.64.0.0/10")

// PeerPrefix returns the masked interface prefix that contains self, when it
// is a private network (including Tailscale's CGNAT range).
func PeerPrefix(self netip.Addr, ifaces []netip.Prefix) (netip.Prefix, bool) {
	for _, iface := range ifaces {
		if !iface.IsValid() {
			continue
		}
		if !iface.Contains(self) && !iface.Contains(self.Unmap()) {
			continue
		}
		masked := iface.Masked()
		addr := masked.Addr()
		if addr.IsPrivate() || (addr.Is4() && cgnatPrefix.Contains(addr)) {
			return masked, true
		}
	}
	return netip.Prefix{}, false
}

// UncoveredPeers returns the sorted names of members no peer prefix contains.
func UncoveredPeers(peers []netip.Prefix, members map[string]netip.Addr) []string {
	var out []string
	for name, addr := range members {
		covered := false
		for _, peer := range peers {
			if peer.Contains(addr) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
