// Package logsafe keeps credentials out of log lines. Handlers pass every
// record through Scrub, so a raw error or attribute that embeds a token,
// cookie or password cannot reach a log sink.
package logsafe

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
)

// secretPattern matches the prefixes Lewbox mints: sessions (lbs_), access
// tokens (lbp_), admin keys (lba_) and sandbox keys (avm_).
var secretPattern = regexp.MustCompile(`\b(?:lbs_|lbp_|lba_|avm_)[A-Za-z0-9]+`)

var (
	// headerPattern masks an Authorization or Cookie header value.
	headerPattern = regexp.MustCompile(`(?i)\b(authorization|cookie)\s*[:=]\s*[^\r\n,;]+`)
	// passwordPattern masks a password in a query string or form body.
	passwordPattern = regexp.MustCompile(`(?i)\b(password|passwd|pwd)\s*=\s*[^&\s]+`)
	// userinfoPattern masks the password in a URL, https://user:pass@host.
	userinfoPattern = regexp.MustCompile(`(?i)(://[^/\s:@]+:)[^/\s@]+@`)
)

// Scrub replaces credentials in s with [redacted].
func Scrub(s string) string {
	s = headerPattern.ReplaceAllString(s, "$1: [redacted]")
	s = userinfoPattern.ReplaceAllString(s, "$1[redacted]@")
	s = passwordPattern.ReplaceAllString(s, "$1=[redacted]")
	return secretPattern.ReplaceAllString(s, "[redacted]")
}

// ScrubError renders err with credentials masked. A nil error is "".
func ScrubError(err error) string {
	if err == nil {
		return ""
	}
	return Scrub(err.Error())
}

// WrapLogger returns a logger whose handler scrubs every record.
func WrapLogger(log *slog.Logger) *slog.Logger {
	if log == nil {
		return slog.New(Wrap(slog.DiscardHandler))
	}
	return slog.New(Wrap(log.Handler()))
}

// Wrap returns a handler that scrubs every message and string attribute
// before passing the record to next. Error values become scrubbed strings.
func Wrap(next slog.Handler) slog.Handler {
	if next == nil {
		return slog.DiscardHandler
	}
	return &handler{next: next}
}

type handler struct {
	next slog.Handler
}

func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *handler) Handle(ctx context.Context, record slog.Record) error {
	clean := slog.NewRecord(record.Time, record.Level, Scrub(record.Message), record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		clean.AddAttrs(scrubAttr(attr))
		return true
	})
	return h.next.Handle(ctx, clean)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		clean = append(clean, scrubAttr(attr))
	}
	return &handler{next: h.next.WithAttrs(clean)}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{next: h.next.WithGroup(name)}
}

func scrubAttr(attr slog.Attr) slog.Attr {
	value := attr.Value.Resolve()
	switch value.Kind() {
	case slog.KindString:
		return slog.String(attr.Key, Scrub(value.String()))
	case slog.KindGroup:
		group := value.Group()
		clean := make([]slog.Attr, 0, len(group))
		for _, item := range group {
			clean = append(clean, scrubAttr(item))
		}
		return slog.Group(attr.Key, attrsToAny(clean)...)
	case slog.KindAny:
		if err, ok := value.Any().(error); ok {
			return slog.String(attr.Key, Scrub(err.Error()))
		}
		if stringer, ok := value.Any().(fmt.Stringer); ok {
			return slog.String(attr.Key, Scrub(stringer.String()))
		}
	}
	return slog.Attr{Key: attr.Key, Value: value}
}

func attrsToAny(attrs []slog.Attr) []any {
	out := make([]any, len(attrs))
	for i, attr := range attrs {
		out[i] = attr
	}
	return out
}
