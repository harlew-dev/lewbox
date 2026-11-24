package logsafe

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestScrub(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"session lbs_abc123DEF456 expired", "session [redacted] expired"},
		{"token lbp_0011223344556677", "token [redacted]"},
		{"key lba_aabbccddeeff", "key [redacted]"},
		{"runner avm_deadbeef", "runner [redacted]"},
		{"Authorization: Bearer lbs_secret", "Authorization: [redacted]"},
		{"Cookie: lewbox_session=lbs_secret; other=1", "Cookie: [redacted]; other=1"},
		{"GET https://user:hunter2@example.com/path", "GET https://user:[redacted]@example.com/path"},
		{"?password=hunter2&name=ada", "?password=[redacted]&name=ada"},
		{"ordinary message", "ordinary message"},
	}
	for _, tc := range cases {
		if got := Scrub(tc.in); got != tc.want {
			t.Errorf("Scrub(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHandlerScrubsMessagesAndAttributes(t *testing.T) {
	capture := &captureHandler{}
	log := WrapLogger(slog.New(capture))
	log.Error("failed for lbs_message_token", "err", errString("password=hunter2"), "token", "lbp_attr_token")
	log.With("cookie", "Cookie: lewbox_session=lbs_group_token").Info("grouped")

	got := capture.text()
	for _, secret := range []string{"lbs_message_token", "hunter2", "lbp_attr_token", "lbs_group_token"} {
		if strings.Contains(got, secret) {
			t.Errorf("log output leaks %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "[redacted]") {
		t.Errorf("log output has no redaction: %s", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

type captureHandler struct {
	mu    sync.Mutex
	lines []string
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, record slog.Record) error {
	var b strings.Builder
	b.WriteString(record.Message)
	record.Attrs(func(attr slog.Attr) bool {
		b.WriteString(" ")
		b.WriteString(attr.Key)
		b.WriteString("=")
		b.WriteString(attr.Value.String())
		return true
	})
	h.mu.Lock()
	h.lines = append(h.lines, b.String())
	h.mu.Unlock()
	return nil
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var b strings.Builder
	for _, attr := range attrs {
		b.WriteString(" ")
		b.WriteString(attr.Key)
		b.WriteString("=")
		b.WriteString(attr.Value.String())
	}
	return &prefixHandler{parent: h, prefix: b.String()}
}

func (h *captureHandler) WithGroup(string) slog.Handler { return h }

func (h *captureHandler) text() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.Join(h.lines, "\n")
}

type prefixHandler struct {
	parent *captureHandler
	prefix string
}

func (h *prefixHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *prefixHandler) Handle(ctx context.Context, record slog.Record) error {
	record.AddAttrs(slog.String("prefix", h.prefix))
	return h.parent.Handle(ctx, record)
}

func (h *prefixHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *prefixHandler) WithGroup(string) slog.Handler            { return h }
