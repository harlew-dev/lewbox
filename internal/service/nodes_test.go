package service

import "testing"

func TestNormalizeURL(t *testing.T) {
	ok := map[string]string{
		"https://n1.example.com":       "https://n1.example.com",
		"https://n1.example.com/":      "https://n1.example.com",
		" http://10.0.0.7:8080 ":       "http://10.0.0.7:8080",
		"https://n1.example.com/api//": "https://n1.example.com/api",
		"https://[2001:db8::1]:8443":   "https://[2001:db8::1]:8443",
	}
	for in, want := range ok {
		got, err := normalizeURL(in)
		if err != nil || got != want {
			t.Errorf("normalizeURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}

	for _, in := range []string{
		"",
		"n1.example.com",
		"n1.example.com:8443",
		"ftp://n1.example.com",
		"https://",
		"https://user:pw@n1.example.com",
		"https://n1.example.com?x=1",
		"https://n1.example.com#frag",
	} {
		if got, err := normalizeURL(in); err == nil {
			t.Errorf("normalizeURL(%q) = %q, want an error", in, got)
		}
	}
}
