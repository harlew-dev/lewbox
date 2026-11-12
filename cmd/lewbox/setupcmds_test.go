package main

import "testing"

func TestSetupLinkHours(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
		bad  bool
	}{
		{"", 0, false},
		{"1h", 1, false},
		{"48h", 48, false},
		{"7d", 168, false},
		{"30d", 720, false},
		{"720h", 720, false},
		{"59m", 0, true},
		{"721h", 0, true},
		{"1.5h", 0, true},
		{"soon", 0, true},
	} {
		got, err := setupLinkHours(tc.in)
		if tc.bad {
			if err == nil {
				t.Errorf("setupLinkHours(%q) = %d, want an error", tc.in, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("setupLinkHours(%q) = %d, %v, want %d", tc.in, got, err, tc.want)
		}
	}
}
