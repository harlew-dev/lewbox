package machine

import (
	"net/netip"
	"reflect"
	"testing"
)

const ufwSample = `### tuple ### allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in
### tuple ### allow tcp 22000:23000 0.0.0.0/0 any 0.0.0.0/0 in
### tuple ### allow tcp 7000:7003 0.0.0.0/0 any 172.30.0.0/24 in
### tuple ### allow udp 7001 0.0.0.0/0 any 172.30.0.0/24 in
### tuple ### allow tcp 22 ::/0 any ::/0 in
### tuple ### allow tcp 80 0.0.0.0/0 any 0.0.0.0/0 in comment=6c6577626f783a737368
### tuple ### allow any 22 0.0.0.0/0 any 0.0.0.0/0 OpenSSH - in
### tuple ### deny tcp 25 0.0.0.0/0 any 0.0.0.0/0 in
### tuple ### limit tcp 22 0.0.0.0/0 any 0.0.0.0/0 in
### tuple ### route:allow any any 0.0.0.0/0 any 0.0.0.0/0 in_docker0!out_eth0
### tuple ### allow tcp 8080 0.0.0.0/0 any 1.2.3.4 out
`

const guardSample = `*filter
:INPUT DROP [0:0]
-A AEROLVM-NODE-35381888-B -s 172.17.0.0/16 -p tcp -m tcp --dport 15080 -m comment --comment sbx-node-guard -j RETURN
-A AEROLVM-NODE-35381888-B -s 172.17.0.0/16 -p tcp -m tcp --dport 443 -m comment --comment sbx-node-guard -j RETURN
-A AEROLVM-NODE-35381888-B -s 172.17.0.0/16 -p tcp -m tcp --dport 53054 -m comment --comment sbx-node-guard -j RETURN
-A AEROLVM-NODE-35381888-B -s 172.17.0.0/16 -p tcp -m tcp --dport 80 -m comment --comment sbx-node-guard -j RETURN
-A AEROLVM-NODE-35381888-B -s 172.17.0.0/16 -p udp -m udp --dport 53054 -m comment --comment sbx-node-guard -j RETURN
-A AEROLVM-NODE-35381888-B -s 172.17.0.0/16 -m comment --comment sbx-node-guard -j DROP
COMMIT
`

func TestParseUFWRules(t *testing.T) {
	rules := ParseUFWRules(ufwSample)
	if len(rules) != 11 {
		t.Fatalf("parsed %d rules, want 11: %+v", len(rules), rules)
	}
	cases := []struct {
		i    int
		want UFWRule
		text string
	}{
		{0, UFWRule{Action: "allow", Proto: "tcp", Port: "22", To: "any", From: "any", Direction: "in"}, "22/tcp"},
		{1, UFWRule{Action: "allow", Proto: "tcp", Port: "22000:23000", To: "any", From: "any", Direction: "in"}, "22000:23000/tcp"},
		{2, UFWRule{Action: "allow", Proto: "tcp", Port: "7000:7003", To: "any", From: "172.30.0.0/24", Direction: "in"}, "7000:7003/tcp from 172.30.0.0/24"},
		{3, UFWRule{Action: "allow", Proto: "udp", Port: "7001", To: "any", From: "172.30.0.0/24", Direction: "in"}, "7001/udp from 172.30.0.0/24"},
		{4, UFWRule{Action: "allow", Proto: "tcp", Port: "22", To: "any", From: "any", Direction: "in"}, "22/tcp"},
		{5, UFWRule{Action: "allow", Proto: "tcp", Port: "80", To: "any", From: "any", Direction: "in"}, "80/tcp"},
		{6, UFWRule{Action: "allow", Proto: "any", Port: "22", To: "any", From: "any", App: "OpenSSH", Direction: "in"}, "OpenSSH"},
		{7, UFWRule{Action: "deny", Proto: "tcp", Port: "25", To: "any", From: "any", Direction: "in"}, "deny 25/tcp"},
		{8, UFWRule{Action: "limit", Proto: "tcp", Port: "22", To: "any", From: "any", Direction: "in"}, "limit 22/tcp"},
		{9, UFWRule{Action: "route:allow", Proto: "any", Port: "any", To: "any", From: "any", Direction: "in_docker0!out_eth0"}, "route:allow all ports"},
		{10, UFWRule{Action: "allow", Proto: "tcp", Port: "8080", To: "any", From: "1.2.3.4", Direction: "out"}, "out 8080/tcp from 1.2.3.4"},
	}
	for _, tc := range cases {
		if rules[tc.i] != tc.want {
			t.Errorf("rule %d = %+v, want %+v", tc.i, rules[tc.i], tc.want)
		}
		if got := rules[tc.i].String(); got != tc.text {
			t.Errorf("rule %d String() = %q, want %q", tc.i, got, tc.text)
		}
	}
}

func TestUFWRuleMatchesAndOpen(t *testing.T) {
	rules := ParseUFWRules(ufwSample)
	want := FirewallRule{Port: "22", Proto: "tcp", From: "any", Purpose: "ssh"}
	if !rules[0].Matches(want) {
		t.Error("allow 22/tcp in did not match")
	}
	if !rules[4].Matches(want) {
		t.Error("IPv6 copy did not match")
	}
	if rules[6].Matches(want) {
		t.Error("an app rule matched")
	}
	if rules[8].Matches(want) {
		t.Error("a limit rule matched")
	}
	if rules[10].Matches(FirewallRule{Port: "8080", Proto: "tcp", From: "1.2.3.4"}) {
		t.Error("an out rule matched")
	}

	open := []bool{true, true, true, true, true, true, true, false, true, false, false}
	for i, want := range open {
		if got := rules[i].Open(); got != want {
			t.Errorf("rule %d Open() = %v, want %v", i, got, want)
		}
	}
}

// ufw saves a single-host source without its /32 or /128 (verified on ufw
// 0.36.2), so a --peers IP must still match the rule Lewbox added for it.
func TestUFWRuleMatchesSingleHost(t *testing.T) {
	rules := ParseUFWRules(`### tuple ### allow tcp 7000:7003 0.0.0.0/0 any 10.9.9.9 in comment=6c6577626f783a636c7573746572
### tuple ### allow tcp 7000:7003 ::/0 any fd00::5 in comment=6c6577626f783a636c7573746572
`)
	if len(rules) != 2 {
		t.Fatalf("parsed %d rules, want 2", len(rules))
	}
	if !rules[0].Matches(FirewallRule{Port: "7000:7003", Proto: "tcp", From: "10.9.9.9/32"}) {
		t.Error("10.9.9.9 did not match 10.9.9.9/32")
	}
	if !rules[1].Matches(FirewallRule{Port: "7000:7003", Proto: "tcp", From: "fd00::5/128"}) {
		t.Error("fd00::5 did not match fd00::5/128")
	}
	if rules[0].Matches(FirewallRule{Port: "7000:7003", Proto: "tcp", From: "10.9.9.0/24"}) {
		t.Error("a host matched a network")
	}
	if extra := ExtraUFWRules(rules, []FirewallRule{
		{Port: "7000:7003", Proto: "tcp", From: "10.9.9.9/32"},
		{Port: "7000:7003", Proto: "tcp", From: "fd00::5/128"},
	}); len(extra) != 0 {
		t.Errorf("Lewbox's own single-host rules reported as extra: %v", extra)
	}
}

func TestExtraUFWRules(t *testing.T) {
	rules := ParseUFWRules(`### tuple ### allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in
### tuple ### allow tcp 22 ::/0 any ::/0 in
### tuple ### allow tcp 8080 0.0.0.0/0 any 0.0.0.0/0 in
### tuple ### allow tcp 5432 0.0.0.0/0 any 10.0.0.0/8 in
`)
	extra := ExtraUFWRules(rules, []FirewallRule{{Port: "22", Proto: "tcp", From: "any", Purpose: "ssh"}})
	if len(extra) != 2 {
		t.Fatalf("extra = %+v, want 2", extra)
	}
	if extra[0].String() != "8080/tcp" || extra[1].String() != "5432/tcp from 10.0.0.0/8" {
		t.Errorf("extra labels = %q, %q", extra[0].String(), extra[1].String())
	}
}

func TestParseNodeGuard(t *testing.T) {
	rules := ParseNodeGuard(guardSample)
	want := []FirewallRule{
		{Port: "15080", Proto: "tcp", From: "172.17.0.0/16", Purpose: "sandbox-to-host"},
		{Port: "443", Proto: "tcp", From: "172.17.0.0/16", Purpose: "sandbox-to-host"},
		{Port: "53054", Proto: "tcp", From: "172.17.0.0/16", Purpose: "sandbox-to-host"},
		{Port: "80", Proto: "tcp", From: "172.17.0.0/16", Purpose: "sandbox-to-host"},
		{Port: "53054", Proto: "udp", From: "172.17.0.0/16", Purpose: "sandbox-to-host"},
	}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("guard = %+v, want %+v", rules, want)
	}
	if got := ParseNodeGuard(""); len(got) != 0 {
		t.Errorf("empty input = %+v", got)
	}
}

func TestParseSSHPorts(t *testing.T) {
	in := "port 22\naddressfamily any\nport 2222\npermitrootlogin without-password\nport 22\n"
	got := ParseSSHPorts(in)
	want := []string{"22", "2222"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ports = %v, want %v", got, want)
	}
	if got := ParseSSHPorts(""); len(got) != 0 {
		t.Errorf("empty input = %v", got)
	}
}

func TestParseFirewallPort(t *testing.T) {
	valid := []struct {
		spec string
		want FirewallRule
	}{
		{"8080", FirewallRule{Port: "8080", Proto: "tcp", From: "any", Purpose: "allow"}},
		{"8080/tcp", FirewallRule{Port: "8080", Proto: "tcp", From: "any", Purpose: "allow"}},
		{"53/udp", FirewallRule{Port: "53", Proto: "udp", From: "any", Purpose: "allow"}},
		{"60000:61000/udp", FirewallRule{Port: "60000:61000", Proto: "udp", From: "any", Purpose: "allow"}},
	}
	for _, tc := range valid {
		got, err := ParseFirewallPort(tc.spec, "allow")
		if err != nil {
			t.Errorf("%q: %v", tc.spec, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q = %+v, want %+v", tc.spec, got, tc.want)
		}
	}

	invalid := []string{"", "tcp", "8080/icmp", "8080/tcp/x", "0", "65536", "7000:7000", "7001:7000", "abc", "1:2:3"}
	for _, spec := range invalid {
		if got, err := ParseFirewallPort(spec, "allow"); err == nil {
			t.Errorf("%q parsed as %+v, want an error", spec, got)
		}
	}
}

func TestPeerPrefix(t *testing.T) {
	cases := []struct {
		name   string
		self   string
		ifaces []string
		want   string
		ok     bool
	}{
		{"private interface", "172.30.0.11", []string{"172.30.0.11/24", "10.0.0.5/8"}, "172.30.0.0/24", true},
		{"cgnat", "100.64.0.7", []string{"100.64.0.7/32"}, "100.64.0.7/32", true},
		{"ipv6 private", "fd00::1", []string{"fd00::1/64"}, "fd00::/64", true},
		{"public", "203.0.113.5", []string{"203.0.113.5/24"}, "", false},
		{"not on an interface", "172.30.0.11", []string{"10.0.0.1/8"}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ifaces []netip.Prefix
			for _, s := range tc.ifaces {
				ifaces = append(ifaces, netip.MustParsePrefix(s))
			}
			got, ok := PeerPrefix(netip.MustParseAddr(tc.self), ifaces)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && got.String() != tc.want {
				t.Errorf("prefix = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestUncoveredPeers(t *testing.T) {
	peers := []netip.Prefix{netip.MustParsePrefix("172.30.0.0/24")}
	members := map[string]netip.Addr{
		"node-1": netip.MustParseAddr("172.30.0.11"),
		"node-2": netip.MustParseAddr("172.30.5.12"),
	}
	if got := UncoveredPeers(peers, members); !reflect.DeepEqual(got, []string{"node-2"}) {
		t.Errorf("uncovered = %v, want [node-2]", got)
	}
	delete(members, "node-2")
	if got := UncoveredPeers(peers, members); len(got) != 0 {
		t.Errorf("uncovered = %v, want none", got)
	}
}

func TestNodeFirewallRules(t *testing.T) {
	ssh := FirewallRule{Port: "22", Proto: "tcp", From: "any", Purpose: "ssh"}
	guard := FirewallRule{Port: "15080", Proto: "tcp", From: "172.17.0.0/16", Purpose: "sandbox-to-host"}
	allow := FirewallRule{Port: "8080", Proto: "tcp", From: "any", Purpose: "allow"}
	peer := netip.MustParsePrefix("172.30.0.0/24")

	cases := []struct {
		mode string
		want []FirewallRule
	}{
		{
			mode: ModeLocal,
			want: []FirewallRule{ssh, guard, allow},
		},
		{
			mode: ModePublicHost,
			want: []FirewallRule{
				ssh,
				{Port: "80", Proto: "tcp", From: "any", Purpose: "sandbox-http"},
				{Port: "21212", Proto: "tcp", From: "any", Purpose: "aerolvm-api"},
				{Port: "2220", Proto: "tcp", From: "any", Purpose: "ssh-gateway"},
				{Port: "22000:23000", Proto: "tcp", From: "any", Purpose: "sandbox-tcp"},
				{Port: "7000:7003", Proto: "tcp", From: "172.30.0.0/24", Purpose: "cluster"},
				{Port: "7001", Proto: "udp", From: "172.30.0.0/24", Purpose: "cluster"},
				guard,
				allow,
			},
		},
		{
			mode: ModeDomain,
			want: []FirewallRule{
				ssh,
				{Port: "443", Proto: "tcp", From: "any", Purpose: "sandbox-https"},
				{Port: "2220", Proto: "tcp", From: "any", Purpose: "ssh-gateway"},
				{Port: "22000:23000", Proto: "tcp", From: "any", Purpose: "sandbox-tcp"},
				{Port: "7000:7003", Proto: "tcp", From: "172.30.0.0/24", Purpose: "cluster"},
				{Port: "7001", Proto: "udp", From: "172.30.0.0/24", Purpose: "cluster"},
				{Port: "21212", Proto: "tcp", From: "172.30.0.0/24", Purpose: "cluster-api"},
				guard,
				allow,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			fw := NodeFirewall{
				Mode:     tc.mode,
				SSHPorts: []string{"22", "22"},
				Peers:    []netip.Prefix{peer},
				Guard:    []FirewallRule{guard, guard},
				Allow:    []FirewallRule{allow, allow},
			}
			if got := fw.Rules(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("rules = %+v, want %+v", got, tc.want)
			}
		})
	}
}
