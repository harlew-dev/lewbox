// Package service holds Lewbox's operations. Each one is plain Go and is
// reached two ways: over HTTP (internal/httpapi) and from the CLI
// (cmd/lewbox). Both entry points call the same method and encode the same
// response, so `lewbox health` and `GET /v0/health` always agree.
package service

import (
	"context"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/db"
	"github.com/harlew-dev/lewbox/internal/secrets"
)

// Service is the shared implementation behind the HTTP API and the CLI.
type Service struct {
	q   *db.Queries
	now func() time.Time

	keyring        *secrets.Keyring
	aerolvmOptions []aerolvm.Option

	limiter *loginLimiter
}

// Option changes a Service built by New.
type Option func(*Service)

// WithKeyring lets the service seal and open cluster admin tokens.
func WithKeyring(k *secrets.Keyring) Option {
	return func(s *Service) { s.keyring = k }
}

// WithAerolVMOptions passes options to every AerolVM client the service
// creates, such as a short retry budget in tests.
func WithAerolVMOptions(opts ...aerolvm.Option) Option {
	return func(s *Service) { s.aerolvmOptions = append(s.aerolvmOptions, opts...) }
}

// WithClock replaces the service's clock, which tests use for fixed times.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// WithLoginLimits changes the sign-in budgets: attempts per client IP per
// minute, and failed attempts per email per minute. Zero disables that budget.
func WithLoginLimits(perIPPerMinute, failuresPerEmailPerMinute int) Option {
	return func(s *Service) {
		s.limiter.perIP = perIPPerMinute
		s.limiter.perEmail = failuresPerEmailPerMinute
	}
}

// New returns a Service backed by q.
func New(q *db.Queries, opts ...Option) *Service {
	s := &Service{
		q:       q,
		now:     func() time.Time { return time.Now().UTC().Truncate(time.Millisecond) },
		limiter: newLoginLimiter(defaultIPAttemptsPerMinute, defaultEmailFailuresPerMinute),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// HealthResponse is returned by Health.
type HealthResponse struct {
	OK bool `json:"ok"`
}

// Health reports whether this Lewbox instance is up.
func (s *Service) Health(ctx context.Context) (HealthResponse, error) {
	return HealthResponse{OK: true}, nil
}
