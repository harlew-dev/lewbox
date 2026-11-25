package service

import (
	"context"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	// Defaults for brute-force protection. Both budgets are measured over a
	// fixed one-minute window.
	defaultIPAttemptsPerMinute    = 20
	defaultEmailFailuresPerMinute = 5

	// Sign-up has its own budget, so its spam cannot exhaust sign-in and vice
	// versa.
	signupIPAttemptsPerHour = 5
	signupIPWindow          = time.Hour

	// loginLimiterMaxKeys bounds the memory one limiter may use. Each key is
	// pruned when its window ends; if the maps are still full, new keys are
	// rejected (fail closed) instead of growing without bound.
	loginLimiterMaxKeys = 10000
	loginWindow         = time.Minute

	// The API budget is api:per-user: every authenticated request, whichever
	// credential it used (session, access token or admin key), draws on one
	// budget. 1,800 requests in an aligned minute is 30/s: a busy console page
	// (roughly 10–20 requests) and a CLI looping a few times a second stay well
	// under it, while a runaway loop is still cut off.
	requestBudgetPerMinute = 1800
	requestBudgetWindow    = time.Minute
	requestBudgetMaxKeys   = 10000
)

// windowState is one key's count in the fixed window that ends at reset.
type windowState struct {
	count int
	reset time.Time
	// refused is set on the first refusal in this window, so the audit trail
	// records one event per key per window rather than one per flooded request.
	refused bool
}

// limiter is an in-memory, fixed-window rate limiter. Every window is aligned
// to a boundary of its length (floor(now / window)), so all keys share the
// same reset time at a given moment and Retry-After never reveals when a
// key's first hit happened. Expired entries are pruned opportunistically on
// every call, and the map is capped; a new key that cannot fit is rejected so
// an attacker cannot grow memory by spraying keys.
type limiter struct {
	mu      sync.Mutex
	limit   int // 0 disables the budget
	window  time.Duration
	maxKeys int
	keys    map[string]*windowState
	// nextPrune throttles pruning: the API budget runs on every request, and
	// a full sweep of a large map each time would cost more than the check.
	nextPrune time.Time
}

func newWindowLimiter(limit int, window time.Duration, maxKeys int) *limiter {
	return &limiter{
		limit:   limit,
		window:  window,
		maxKeys: maxKeys,
		keys:    make(map[string]*windowState),
	}
}

// setLimit changes the budget without dropping the counts already made.
func (l *limiter) setLimit(limit int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.limit = limit
}

// allow counts one hit for key and reports whether it fits the budget. The
// returned duration is how long the caller should wait when it does not.
// firstRefusal is true only for the first refusal of this key in the current
// aligned window, so a flood writes one audit event, not thousands.
func (l *limiter) allow(now time.Time, key string) (retry time.Duration, allowed, firstRefusal bool) {
	if l == nil || l.limit <= 0 || key == "" {
		return 0, true, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
	reset := windowReset(now, l.window)
	w, ok := l.keys[key]
	if !ok {
		if len(l.keys) >= l.maxKeys {
			return l.fullRetryLocked(now), false, false
		}
		w = &windowState{reset: reset}
		l.keys[key] = w
	} else if !w.reset.Equal(reset) {
		// The aligned window moved on: the old count does not carry over.
		w.count = 0
		w.refused = false
		w.reset = reset
	}
	if w.count >= l.limit {
		first := !w.refused
		w.refused = true
		return retryAfter(w.reset, now), false, first
	}
	w.count++
	return 0, true, false
}

// clear forgets key's count, so a success can free a budget before its window
// ends.
func (l *limiter) clear(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.keys, key)
}

// pruneLocked drops expired entries, at most once a second unless the map is
// full, so an idle server does not keep old keys forever. A key whose window
// has moved on is reset in allow, so a late prune never over-counts.
func (l *limiter) pruneLocked(now time.Time) {
	if now.Before(l.nextPrune) && len(l.keys) < l.maxKeys {
		return
	}
	l.nextPrune = now.Add(time.Second)
	for k, w := range l.keys {
		if !now.Before(w.reset) {
			delete(l.keys, k)
		}
	}
}

// fullRetryLocked reports how long until the earliest window ends when the map
// is full and a new key cannot be added.
func (l *limiter) fullRetryLocked(now time.Time) time.Duration {
	var soonest time.Time
	for _, w := range l.keys {
		if soonest.IsZero() || w.reset.Before(soonest) {
			soonest = w.reset
		}
	}
	if soonest.IsZero() {
		return l.window
	}
	return retryAfter(soonest, now)
}

// windowReset is the end of the aligned window that contains now. Windows
// start at a whole multiple of their length, so every key gets the same reset
// time at a given moment.
func windowReset(now time.Time, window time.Duration) time.Time {
	return now.Truncate(window).Add(window)
}

// rateIPKey normalises a client address for a per-IP budget. A whole IPv6 /64
// counts as one client, because one host usually controls the low bits and
// could otherwise rotate them for a fresh budget every request. An IPv4
// address (including an IPv4-mapped IPv6 one) stays a single client.
func rateIPKey(raw string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	addr = addr.Unmap()
	if addr.Is6() {
		if prefix, err := addr.Prefix(64); err == nil {
			return prefix.Masked().String()
		}
	}
	return addr.String()
}

// emailKey normalises an address just as sign-in does, so spellings of the
// same email share a budget.
func emailKey(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// loginLimiter is the pair of budgets that guard sign-in: one per client IP
// and one per email. The limiter is shared by Login and WebLogin, so the two
// entry points have the same budget. Sign-up uses its own instance with a
// longer window.
type loginLimiter struct {
	ips    *limiter
	emails *limiter
}

func newLoginLimiter(perIP, perEmail int) *loginLimiter {
	return newLimiter(perIP, perEmail, loginWindow)
}

func newLimiter(perIP, perEmail int, window time.Duration) *loginLimiter {
	return &loginLimiter{
		ips:    newWindowLimiter(perIP, window, loginLimiterMaxKeys),
		emails: newWindowLimiter(perEmail, window, loginLimiterMaxKeys),
	}
}

// setLimits changes both budgets. Zero disables the matching budget.
func (l *loginLimiter) setLimits(perIP, perEmail int) {
	l.ips.setLimit(perIP)
	l.emails.setLimit(perEmail)
}

// allowIP counts one attempt from ip and reports whether it fits the budget.
// The returned duration is how long the caller should wait when it does not;
// firstRefusal marks the first refusal for this key in this window.
func (l *loginLimiter) allowIP(now time.Time, ip string) (time.Duration, bool, bool) {
	if l == nil || ip == "" {
		return 0, true, false
	}
	return l.ips.allow(now, rateIPKey(ip))
}

// allowEmail reserves one attempt in email's budget and reports whether it
// fits. The slot is taken before the password is checked, so concurrent
// guesses against one account can't all slip past the check while the hash
// runs. A successful sign-in calls clearEmail, so in effect only failures
// count.
func (l *loginLimiter) allowEmail(now time.Time, email string) (time.Duration, bool, bool) {
	if l == nil || email == "" {
		return 0, true, false
	}
	return l.emails.allow(now, emailKey(email))
}

// clearEmail forgets email's failures after a successful sign-in.
func (l *loginLimiter) clearEmail(email string) {
	if l == nil || email == "" {
		return
	}
	l.emails.clear(emailKey(email))
}

func retryAfter(reset, now time.Time) time.Duration {
	d := reset.Sub(now)
	if d < time.Second {
		return time.Second
	}
	return d
}

// loginRateLimited builds the shared rate_limited error and its message.
func loginRateLimited(retry time.Duration) error {
	return rateLimitedAfter(retry, "too many sign-in attempts; try again in %d seconds")
}

// signupRateLimited is the sign-up budget's version, kept separate so its
// message names sign-up.
func signupRateLimited(retry time.Duration) error {
	return rateLimitedAfter(retry, "too many sign-up attempts; try again in %d seconds")
}

// requestRateLimited is the API budget's version (api:per-user).
func requestRateLimited(retry time.Duration) error {
	return rateLimitedAfter(retry, "too many requests; try again in %d seconds")
}

func rateLimitedAfter(retry time.Duration, format string) error {
	seconds := int((retry + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return rateLimited(retry, format, seconds)
}

// AllowRequest charges one request to the caller's budget (api:per-user). The
// HTTP middleware calls it after authentication, so every authenticated
// request is metered without each route opting in, and the remote CLI goes
// through the same check. Local mode owns the instance, and anonymous
// requests are refused or limited by their own methods, so neither is counted
// here.
//
// Sessions and access tokens for one account share a key, so switching
// credential cannot buy a fresh budget; an admin key is a separate caller.
func (s *Service) AllowRequest(ctx context.Context) error {
	p, ok := PrincipalFrom(ctx)
	if !ok {
		return nil
	}
	var key string
	switch p.Kind {
	case "user":
		if p.UserID == "" {
			return nil
		}
		key = "user:" + p.UserID
	case "key":
		if p.KeyID == "" {
			return nil
		}
		key = "key:" + p.KeyID
	default:
		return nil
	}
	retry, allowed, first := s.budget.allow(s.now(), key)
	if !allowed {
		if first {
			s.auditRateLimit(ctx, "api:per-user")
		}
		return requestRateLimited(retry)
	}
	return nil
}

// auditRateLimit records the first refusal for a budget in a window. Only the
// budget's fixed name and the context's client IP are recorded; never the
// typed email, route or any other caller-supplied value. A failure to write
// the event is logged, not shown: the caller is refused either way.
func (s *Service) auditRateLimit(ctx context.Context, budget string) {
	if err := s.record(ctx, Event{
		Name:       "rate_limit.refused",
		TargetType: "rate_limit",
		TargetID:   budget,
	}); err != nil {
		s.log.Warn("audit rate-limit refusal failed", "budget", budget, "err", err)
	}
}
