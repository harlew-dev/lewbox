package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/aerolvm/aerolvmtest"
	"github.com/harlew-dev/lewbox/internal/machine"
)

const firewallGuardSample = `*filter
:INPUT DROP [0:0]
-A AEROLVM-NODE-35381888-B -s 172.17.0.0/16 -p tcp -m tcp --dport 15080 -m comment --comment sbx-node-guard -j RETURN
-A AEROLVM-NODE-35381888-B -s 172.17.0.0/16 -m comment --comment sbx-node-guard -j DROP
COMMIT
`

type firewallHarness struct {
	h          *harness
	ran        []string
	guard      string
	sshd       string
	user4      string
	user6      string
	missingUfw bool
}

func newFirewallHarness(t *testing.T, localURL string) *firewallHarness {
	t.Helper()
	t.Setenv("SSH_CONNECTION", "")
	f := &firewallHarness{
		h:     newHarness(t, localURL),
		guard: firewallGuardSample,
		sshd:  "port 22\n",
	}
	f.h.runner.OutputFn = f.output
	previous := f.h.runner.RunFn
	f.h.runner.RunFn = func(ctx context.Context, cmd machine.Cmd) error {
		f.ran = append(f.ran, cmd.Display())
		return previous(ctx, cmd)
	}
	f.h.hd.interfaceAddrs = func() ([]netip.Prefix, error) {
		return []netip.Prefix{netip.MustParsePrefix("172.30.0.11/24")}, nil
	}
	return f
}

func (f *firewallHarness) output(ctx context.Context, cmd machine.Cmd) ([]byte, error) {
	switch cmd.Name {
	case "ufw":
		if len(cmd.Args) > 0 && cmd.Args[0] == "version" {
			if f.missingUfw {
				return nil, fmt.Errorf("ufw: not found")
			}
			return []byte("ufw 0.36\n"), nil
		}
	case "sshd":
		return []byte(f.sshd), nil
	case "iptables-save":
		return []byte(f.guard), nil
	case "cat":
		switch cmd.Args[len(cmd.Args)-1] {
		case "/etc/ufw/user.rules":
			if f.user4 == "" {
				return nil, os.ErrNotExist
			}
			return []byte(f.user4), nil
		case "/etc/ufw/user6.rules":
			if f.user6 == "" {
				return nil, os.ErrNotExist
			}
			return []byte(f.user6), nil
		}
	}
	return f.h.output(ctx, cmd)
}

func firewallMember(name, raft string) aerolvm.Member {
	return aerolvm.Member{
		NodeID:   name,
		NodeName: name,
		APIURL:   "http://" + name + ":21212",
		RaftAddr: raft,
		Alive:    true,
		Role:     "mixed",
		Version:  "v0.7.0",
		Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
	}
}

func saveFirewallState(t *testing.T, name, mode string) {
	t.Helper()
	saveState(t, machine.State{Name: name, Mode: mode, Runtime: "docker", AerolVMVersion: "v0.7.0"})
}

func decodeFirewallOutput(t *testing.T, out string) firewallOutput {
	t.Helper()
	var got firewallOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode output %q: %v", out, err)
	}
	return got
}

func publicHostRules(peer string) []machine.FirewallRule {
	return []machine.FirewallRule{
		{Port: "22", Proto: "tcp", From: "any", Purpose: "ssh"},
		{Port: "80", Proto: "tcp", From: "any", Purpose: "sandbox-http"},
		{Port: "21212", Proto: "tcp", From: "any", Purpose: "aerolvm-api"},
		{Port: "2220", Proto: "tcp", From: "any", Purpose: "ssh-gateway"},
		{Port: "22000:23000", Proto: "tcp", From: "any", Purpose: "sandbox-tcp"},
		{Port: "7000:7003", Proto: "tcp", From: peer, Purpose: "cluster"},
		{Port: "7001", Proto: "udp", From: peer, Purpose: "cluster"},
		{Port: "15080", Proto: "tcp", From: "172.17.0.0/16", Purpose: "sandbox-to-host"},
	}
}

func TestFirewallEnablePublicHost(t *testing.T) {
	fake := aerolvmtest.New(t)
	fake.SetMembers([]aerolvm.Member{firewallMember("node-1", "172.30.0.11:7000")})
	f := newFirewallHarness(t, fake.URL())
	fake.AdminToken = f.h.pat
	saveFirewallState(t, "node-1", machine.ModePublicHost)

	out, errOut, code := f.h.runCLI("", "firewall", "enable")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}

	wantRules := publicHostRules("172.30.0.0/24")
	wantRan := []string{"sudo ufw default deny incoming"}
	for _, rule := range wantRules {
		wantRan = append(wantRan, rule.Cmd().Display())
	}
	wantRan = append(wantRan, "sudo ufw --force enable")
	if !reflect.DeepEqual(f.ran, wantRan) {
		t.Errorf("commands:\n%v\nwant:\n%v", f.ran, wantRan)
	}

	got := decodeFirewallOutput(t, out)
	if !got.Enabled || got.Reset || got.DryRun {
		t.Errorf("output = %+v", got)
	}
	if !reflect.DeepEqual(got.Rules, wantRules) {
		t.Errorf("rules = %+v\nwant %+v", got.Rules, wantRules)
	}
	if got.Kept == nil || len(got.Kept) != 0 {
		t.Errorf("kept = %v, want []", got.Kept)
	}
}

func TestFirewallEnableExtrasNeedsResetFlag(t *testing.T) {
	fake := aerolvmtest.New(t)
	fake.SetMembers([]aerolvm.Member{firewallMember("node-1", "172.30.0.11:7000")})
	f := newFirewallHarness(t, fake.URL())
	fake.AdminToken = f.h.pat
	saveFirewallState(t, "node-1", machine.ModePublicHost)
	f.user4 = "### tuple ### allow tcp 8080 0.0.0.0/0 any 0.0.0.0/0 in\n" +
		"### tuple ### allow tcp 5432 0.0.0.0/0 any 10.0.0.0/8 in\n"

	_, errOut, code := f.h.runCLI("", "firewall", "enable")
	if code != 2 {
		t.Fatalf("exit %d, want 2: %s", code, errOut)
	}
	for _, want := range []string{"8080/tcp, 5432/tcp from 10.0.0.0/8", "pass --reset"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr does not contain %q:\n%s", want, errOut)
		}
	}
	if len(f.ran) != 0 {
		t.Errorf("ran commands: %v", f.ran)
	}
}

func TestFirewallEnableReset(t *testing.T) {
	fake := aerolvmtest.New(t)
	fake.SetMembers([]aerolvm.Member{firewallMember("node-1", "172.30.0.11:7000")})
	f := newFirewallHarness(t, fake.URL())
	fake.AdminToken = f.h.pat
	saveFirewallState(t, "node-1", machine.ModePublicHost)
	f.user4 = "### tuple ### allow tcp 8080 0.0.0.0/0 any 0.0.0.0/0 in\n" +
		"### tuple ### allow tcp 5432 0.0.0.0/0 any 10.0.0.0/8 in\n"

	out, errOut, code := f.h.runCLI("", "firewall", "enable", "--reset")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if len(f.ran) == 0 || f.ran[0] != "sudo ufw --force reset" {
		t.Errorf("first command = %v", f.ran)
	}
	if !containsLine(f.ran, "sudo ufw default allow outgoing") {
		t.Errorf("default allow outgoing did not run: %v", f.ran)
	}
	got := decodeFirewallOutput(t, out)
	if !got.Reset || len(got.Kept) != 0 {
		t.Errorf("output = %+v", got)
	}
}

func TestFirewallEnableKeepExtras(t *testing.T) {
	fake := aerolvmtest.New(t)
	fake.SetMembers([]aerolvm.Member{firewallMember("node-1", "172.30.0.11:7000")})
	f := newFirewallHarness(t, fake.URL())
	fake.AdminToken = f.h.pat
	saveFirewallState(t, "node-1", machine.ModePublicHost)
	f.user4 = "### tuple ### allow tcp 8080 0.0.0.0/0 any 0.0.0.0/0 in\n" +
		"### tuple ### allow tcp 5432 0.0.0.0/0 any 10.0.0.0/8 in\n"

	out, errOut, code := f.h.runCLI("", "firewall", "enable", "--reset=false")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if containsLine(f.ran, "sudo ufw --force reset") || containsLine(f.ran, "sudo ufw default allow outgoing") {
		t.Errorf("reset ran anyway: %v", f.ran)
	}
	got := decodeFirewallOutput(t, out)
	wantKept := []string{"8080/tcp", "5432/tcp from 10.0.0.0/8"}
	if got.Reset || !reflect.DeepEqual(got.Kept, wantKept) {
		t.Errorf("output = %+v, want kept %v", got, wantKept)
	}
}

func TestFirewallEnableYesResets(t *testing.T) {
	fake := aerolvmtest.New(t)
	fake.SetMembers([]aerolvm.Member{firewallMember("node-1", "172.30.0.11:7000")})
	f := newFirewallHarness(t, fake.URL())
	fake.AdminToken = f.h.pat
	saveFirewallState(t, "node-1", machine.ModePublicHost)
	f.user4 = "### tuple ### allow tcp 8080 0.0.0.0/0 any 0.0.0.0/0 in\n"

	out, errOut, code := f.h.runCLI("", "firewall", "enable", "--yes")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	got := decodeFirewallOutput(t, out)
	if !got.Reset {
		t.Errorf("output = %+v, want reset", got)
	}
	if len(f.ran) == 0 || f.ran[0] != "sudo ufw --force reset" {
		t.Errorf("commands = %v", f.ran)
	}
}

func TestFirewallEnableDryRun(t *testing.T) {
	fake := aerolvmtest.New(t)
	fake.SetMembers([]aerolvm.Member{firewallMember("node-1", "172.30.0.11:7000")})
	f := newFirewallHarness(t, fake.URL())
	fake.AdminToken = f.h.pat
	saveFirewallState(t, "node-1", machine.ModePublicHost)
	f.user4 = "### tuple ### allow tcp 8080 0.0.0.0/0 any 0.0.0.0/0 in\n"

	out, errOut, code := f.h.runCLI("", "firewall", "enable", "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if len(f.ran) != 0 {
		t.Errorf("dry run ran commands: %v", f.ran)
	}
	got := decodeFirewallOutput(t, out)
	if got.Enabled || !got.DryRun || !got.Reset {
		t.Errorf("output = %+v", got)
	}
}

func TestFirewallEnableNoGuard(t *testing.T) {
	fake := aerolvmtest.New(t)
	fake.SetMembers([]aerolvm.Member{firewallMember("node-1", "172.30.0.11:7000")})
	f := newFirewallHarness(t, fake.URL())
	fake.AdminToken = f.h.pat
	saveFirewallState(t, "node-1", machine.ModePublicHost)
	f.guard = "*filter\nCOMMIT\n"

	_, errOut, code := f.h.runCLI("", "firewall", "enable")
	if code != 1 || !strings.Contains(errOut, "sandbox network rules aren't loaded") {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
	if len(f.ran) != 0 {
		t.Errorf("ran commands: %v", f.ran)
	}
}

func TestFirewallEnablePeerOutsideNetwork(t *testing.T) {
	fake := aerolvmtest.New(t)
	fake.SetMembers([]aerolvm.Member{
		firewallMember("node-1", "172.30.0.11:7000"),
		firewallMember("node-2", "172.30.5.12:7000"),
	})
	f := newFirewallHarness(t, fake.URL())
	fake.AdminToken = f.h.pat
	saveFirewallState(t, "node-1", machine.ModePublicHost)

	_, errOut, code := f.h.runCLI("", "firewall", "enable")
	if code != 1 || !strings.Contains(errOut, "node-2") {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}

	out, errOut, code := f.h.runCLI("", "firewall", "enable", "--peers", "172.30.0.0/16")
	if code != 0 {
		t.Fatalf("with --peers: exit %d: %s", code, errOut)
	}
	got := decodeFirewallOutput(t, out)
	want := publicHostRules("172.30.0.0/16")
	if !reflect.DeepEqual(got.Rules, want) {
		t.Errorf("rules = %+v\nwant %+v", got.Rules, want)
	}
}

func TestFirewallEnablePublicSelf(t *testing.T) {
	fake := aerolvmtest.New(t)
	fake.SetMembers([]aerolvm.Member{firewallMember("node-1", "203.0.113.5:7000")})
	f := newFirewallHarness(t, fake.URL())
	fake.AdminToken = f.h.pat
	saveFirewallState(t, "node-1", machine.ModePublicHost)
	f.h.hd.interfaceAddrs = func() ([]netip.Prefix, error) {
		return []netip.Prefix{netip.MustParsePrefix("203.0.113.5/24")}, nil
	}

	_, errOut, code := f.h.runCLI("", "firewall", "enable")
	if code != 1 || !strings.Contains(errOut, "pass --peers") {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
	if len(f.ran) != 0 {
		t.Errorf("ran commands: %v", f.ran)
	}
}

func TestFirewallEnableLocalMode(t *testing.T) {
	f := newFirewallHarness(t, "http://127.0.0.1:1")
	saveFirewallState(t, "node-1", machine.ModeLocal)

	out, errOut, code := f.h.runCLI("", "firewall", "enable")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	got := decodeFirewallOutput(t, out)
	want := []machine.FirewallRule{
		{Port: "22", Proto: "tcp", From: "any", Purpose: "ssh"},
		{Port: "15080", Proto: "tcp", From: "172.17.0.0/16", Purpose: "sandbox-to-host"},
	}
	if !reflect.DeepEqual(got.Rules, want) {
		t.Errorf("rules = %+v\nwant %+v", got.Rules, want)
	}
	for _, cmd := range f.h.runner.Commands() {
		if cmd.Name == "cat" && cmd.Args[len(cmd.Args)-1] == "/etc/sandboxd/sandboxd.env" {
			t.Error("local mode read the cluster's members")
		}
	}
}

func TestFirewallEnableAllowFlag(t *testing.T) {
	fake := aerolvmtest.New(t)
	fake.SetMembers([]aerolvm.Member{firewallMember("node-1", "172.30.0.11:7000")})
	f := newFirewallHarness(t, fake.URL())
	fake.AdminToken = f.h.pat
	saveFirewallState(t, "node-1", machine.ModePublicHost)

	out, errOut, code := f.h.runCLI("", "firewall", "enable", "--allow", "8080/tcp")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	got := decodeFirewallOutput(t, out)
	last := got.Rules[len(got.Rules)-1]
	want := machine.FirewallRule{Port: "8080", Proto: "tcp", From: "any", Purpose: "allow"}
	if last != want {
		t.Errorf("last rule = %+v, want %+v", last, want)
	}
	if !containsLine(f.ran, want.Cmd().Display()) {
		t.Errorf("allow command missing: %v", f.ran)
	}

	f2 := newFirewallHarness(t, fake.URL())
	f2.h.pat = f.h.pat
	saveFirewallState(t, "node-1", machine.ModePublicHost)
	_, errOut, code = f2.h.runCLI("", "firewall", "enable", "--allow", "nonsense")
	if code != 2 || !strings.Contains(errOut, "invalid --allow") {
		t.Errorf("exit %d stderr %q", code, errOut)
	}
	if len(f2.ran) != 0 {
		t.Errorf("ran commands: %v", f2.ran)
	}
}

func TestFirewallEnableSSHConnectionPort(t *testing.T) {
	fake := aerolvmtest.New(t)
	fake.SetMembers([]aerolvm.Member{firewallMember("node-1", "172.30.0.11:7000")})
	f := newFirewallHarness(t, fake.URL())
	fake.AdminToken = f.h.pat
	saveFirewallState(t, "node-1", machine.ModePublicHost)
	f.sshd = "port 22\n"
	t.Setenv("SSH_CONNECTION", "10.0.0.9 51234 10.0.0.5 2222")

	out, errOut, code := f.h.runCLI("", "firewall", "enable")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	got := decodeFirewallOutput(t, out)
	var ports []string
	for _, rule := range got.Rules {
		if rule.Purpose == "ssh" {
			ports = append(ports, rule.Port)
		}
	}
	if !reflect.DeepEqual(ports, []string{"22", "2222"}) {
		t.Errorf("ssh ports = %v, want [22 2222]", ports)
	}
}

func TestAskYesDefault(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"y\n", true},
		{"YES\n", true},
		{"n\n", false},
		{"nope\n", false},
	}
	for _, tc := range cases {
		var out strings.Builder
		got, err := askYesDefault(strings.NewReader(tc.in), &out, "Reset?")
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("%q = %v, want %v", tc.in, got, tc.want)
		}
		if !strings.Contains(out.String(), "Reset? [Y/n]") {
			t.Errorf("prompt = %q", out.String())
		}
	}
}

func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}
