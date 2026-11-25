package main

import (
	"testing"
	"time"
)

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
