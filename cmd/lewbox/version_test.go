package main

import (
	"runtime/debug"
	"testing"
)

func TestModuleVersion(t *testing.T) {
	vcs := []debug.BuildSetting{{Key: "vcs", Value: "git"}, {Key: "vcs.revision", Value: "0123456789ab"}}
	for _, tc := range []struct {
		name     string
		set      string
		main     string
		settings []debug.BuildSetting
		want     string
	}{
		{"go install of a tag", "dev", "v0.2.0", nil, "v0.2.0"},
		{"go install of a branch", "dev", "v0.2.1-0.20261112211700-0123456789ab", nil, "v0.2.1-0.20261112211700-0123456789ab"},
		{"ldflags win", "v0.3.0", "v0.2.0", nil, "v0.3.0"},
		{"build from a checkout", "dev", "v0.2.1-0.20261112211700-0123456789ab", vcs, "dev"},
		{"dirty checkout", "dev", "v0.2.1-0.20261112211700-0123456789ab+dirty", nil, "dev"},
		{"tests and go run", "dev", "(devel)", nil, "dev"},
		{"not a tag", "dev", "v1.0.0/../../x", nil, "dev"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &debug.BuildInfo{Main: debug.Module{Version: tc.main}, Settings: tc.settings}
			if got := moduleVersion(tc.set, info); got != tc.want {
				t.Errorf("moduleVersion(%q, %q) = %q, want %q", tc.set, tc.main, got, tc.want)
			}
		})
	}
}
