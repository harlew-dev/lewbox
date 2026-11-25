package main

import (
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestServeCreateLimitFlags(t *testing.T) {
	o := &serveOptions{}
	cmd := &cobra.Command{}
	addServeFlags(cmd, o)
	if o.limitWorkspacesPerHour != 5 || o.limitSandboxKeysPerHour != 100 || o.limitAdminKeysPerHour != 100 {
		t.Fatalf("defaults = %d, %d, %d, want 5, 100, 100",
			o.limitWorkspacesPerHour, o.limitSandboxKeysPerHour, o.limitAdminKeysPerHour)
	}
	for _, name := range []string{"limit-workspaces-per-hour", "limit-sandbox-keys-per-hour", "limit-admin-keys-per-hour"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s is missing", name)
		}
	}
	if err := cmd.Flags().Parse([]string{"--limit-workspaces-per-hour=3", "--limit-admin-keys-per-hour=0"}); err != nil {
		t.Fatal(err)
	}
	if o.limitWorkspacesPerHour != 3 || o.limitAdminKeysPerHour != 0 {
		t.Errorf("parsed limits = %d, %d", o.limitWorkspacesPerHour, o.limitAdminKeysPerHour)
	}
}

func TestServeCreateLimitEnv(t *testing.T) {
	t.Setenv("LEWBOX_LIMIT_WORKSPACES_PER_HOUR", "9")
	t.Setenv("LEWBOX_LIMIT_SANDBOX_KEYS_PER_HOUR", "0")
	t.Setenv("LEWBOX_LIMIT_ADMIN_KEYS_PER_HOUR", "soon")
	o := &serveOptions{}
	addServeFlags(&cobra.Command{}, o)
	if o.limitWorkspacesPerHour != 9 || o.limitSandboxKeysPerHour != 0 || o.limitAdminKeysPerHour != 100 {
		t.Errorf("env limits = %d, %d, %d, want 9, 0, 100 (an unreadable value keeps the default)",
			o.limitWorkspacesPerHour, o.limitSandboxKeysPerHour, o.limitAdminKeysPerHour)
	}
}

func TestParseAuditRetention(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"90d", 90 * 24 * time.Hour},
		{"12h", 12 * time.Hour},
		{"0", 0},
		{"forever", 0},
	}
	for _, c := range cases {
		got, err := parseAuditRetention(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q = %s, want %s", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "-1d", "soon", "-1h"} {
		if _, err := parseAuditRetention(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}
