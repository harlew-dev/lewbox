package service

import (
	"sync"
	"time"
)

const (
	// Defaults for brute-force protection. Both budgets are measured over a
	// fixed one-minute window.
	defaultIPAttemptsPerMinute    = 20
	defaultEmailFailuresPerMinute = 5

	// loginLimiterMaxKeys bounds the memory one limiter may use. Each key is
	// pruned when its window ends; if the maps are still full, new keys are
	// rejected (fail closed) instead of growing without bound.
	loginLimiterMaxKeys = 10000
	loginWindow         = time.Minute
)

// loginLimiter is an in-memory, fixed-window rate limiter for sign-in. A key
// gets a fresh count every loginWindow, starting with its first attempt.
// Expired entries are pruned opportunistically on every call, and both maps
// are capped at loginLimiterMaxKeys; a new key that cannot fit is rejected so
// an attacker cannot grow memory by spraying unknown addresses or emails.
//
// The limiter is shared by Login and WebLogin, so the two entry points have
// the same budget.
type loginLimiter struct {
	mu       sync.Mutex
	perIP    int // 0 disables the per-client budget
	perEmail int // 0 disables the per-email budget
	ips      map[string]*loginWindowState
	emails   map[string]*loginWindowState
}

type loginWindowState struct {
	count int
	reset time.Time
}

func newLoginLimiter(perIP, perEmail int) *loginLimiter {
	return &loginLimiter{
		perIP:    perIP,
		perEmail: perEmail,
		ips:      make(map[string]*loginWindowState),
		emails:   make(map[string]*loginWindowState),
	}
}

// allowIP counts one attempt from ip and reports whether it fits the budget.
// The returned duration is how long the caller should wait when it does not.
func (l *loginLimiter) allowIP(now time.Time, ip string) (time.Duration, bool) {
	if l == nil || l.perIP <= 0 || ip == "" {
		return 0, true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(l.ips, now)
	w := l.windowLocked(l.ips, ip, now)
	if w == nil {
		return l.fullRetryLocked(l.ips, now), false
	}
	if w.count >= l.perIP {
		return retryAfter(w.reset, now), false
	}
	w.count++
	return 0, true
}

// allowEmail reserves one attempt in email's budget and reports whether it
// fits. The slot is taken before the password is checked, so concurrent
// guesses against one account can't all slip past the check while the hash
// runs. A successful sign-in calls clearEmail, so in effect only failures
// count.
func (l *loginLimiter) allowEmail(now time.Time, email string) (time.Duration, bool) {
	if l == nil || l.perEmail <= 0 || email == "" {
		return 0, true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(l.emails, now)
	w := l.windowLocked(l.emails, email, now)
	if w == nil {
		return l.fullRetryLocked(l.emails, now), false
	}
	if w.count >= l.perEmail {
		return retryAfter(w.reset, now), false
	}
	w.count++
	return 0, true
}

// clearEmail forgets email's failures after a successful sign-in.
func (l *loginLimiter) clearEmail(email string) {
	if l == nil || l.perEmail <= 0 || email == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.emails, email)
}

// pruneLocked drops expired entries. It runs on every call so an idle server
// does not keep old keys forever.
func (l *loginLimiter) pruneLocked(m map[string]*loginWindowState, now time.Time) {
	for k, w := range m {
		if !now.Before(w.reset) {
			delete(m, k)
		}
	}
}

// windowLocked returns the live window for key, starting one if there is
// room. A nil result means the map is full.
func (l *loginLimiter) windowLocked(m map[string]*loginWindowState, key string, now time.Time) *loginWindowState {
	if w, ok := m[key]; ok {
		return w
	}
	if len(m) >= loginLimiterMaxKeys {
		return nil
	}
	w := &loginWindowState{reset: now.Add(loginWindow)}
	m[key] = w
	return w
}

// fullRetryLocked reports how long until the earliest window ends when a map
// is full and a new key cannot be added.
func (l *loginLimiter) fullRetryLocked(m map[string]*loginWindowState, now time.Time) time.Duration {
	var soonest time.Time
	for _, w := range m {
		if soonest.IsZero() || w.reset.Before(soonest) {
			soonest = w.reset
		}
	}
	if soonest.IsZero() {
		return loginWindow
	}
	return retryAfter(soonest, now)
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
	seconds := int((retry + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return rateLimited(retry, "too many sign-in attempts; try again in %d seconds", seconds)
}
