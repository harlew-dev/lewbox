package service

import (
	"context"
	"testing"
	"time"
)

func TestLimiterWindowsAreAligned(t *testing.T) {
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

	minute := newLoginLimiter(1, 0)
	if _, ok, _ := minute.allowIP(now, "192.0.2.1"); !ok {
		t.Fatal("the first attempt was rejected")
	}
	retry, ok, _ := minute.allowIP(now, "192.0.2.1")
	if ok {
		t.Fatal("the second attempt was allowed")
	}
	if retry != 53*time.Second {
		t.Errorf("minute retry = %v, want 53s (until 5:07:00)", retry)
	}

	hour := newLimiter(1, 0, time.Hour)
	if _, ok, _ := hour.allowIP(now, "192.0.2.1"); !ok {
		t.Fatal("the first hourly attempt was rejected")
	}
	retry, ok, _ = hour.allowIP(now, "192.0.2.1")
	if ok {
		t.Fatal("the second hourly attempt was allowed")
	}
	want := time.Date(2026, 3, 4, 6, 0, 0, 0, time.UTC)
	if reset := now.Add(retry); !reset.Equal(want) {
		t.Errorf("hourly reset = %v, want %v", reset, want)
	}

	// A late hit still resets at the boundary, so Retry-After reveals nothing
	// about when the first hit happened.
	late := time.Date(2026, 3, 4, 5, 6, 59, 0, time.UTC)
	lateLimiter := newLoginLimiter(1, 0)
	if _, ok, _ := lateLimiter.allowIP(late, "192.0.2.1"); !ok {
		t.Fatal("the late first attempt was rejected")
	}
	if retry, ok, _ := lateLimiter.allowIP(late, "192.0.2.1"); ok || retry != time.Second {
		t.Errorf("late retry = %v, allowed = %v, want 1s", retry, ok)
	}
}

func TestIPBudgetCountsWholeIPv6Slash64(t *testing.T) {
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	l := newLoginLimiter(2, 0)

	// Two addresses in one /64 share a budget.
	for _, ip := range []string{"2001:db8:1:2::1", "2001:db8:1:2:ffff::9"} {
		if _, ok, _ := l.allowIP(now, ip); !ok {
			t.Fatalf("allowIP(%q) was rejected before the limit", ip)
		}
	}
	if _, ok, _ := l.allowIP(now, "2001:db8:1:2::3"); ok {
		t.Error("a third address in the same /64 was allowed")
	}

	// A different /64 and an IPv4 address each have their own budget.
	if _, ok, _ := l.allowIP(now, "2001:db8:1:3::1"); !ok {
		t.Error("another /64 was refused")
	}
	if _, ok, _ := l.allowIP(now, "192.0.2.1"); !ok {
		t.Error("an IPv4 address was refused")
	}

	if got := rateIPKey("2001:db8:1:2:ffff::9"); got != "2001:db8:1:2::/64" {
		t.Errorf("rateIPKey = %q", got)
	}
	if got := rateIPKey("::ffff:192.0.2.1"); got != "192.0.2.1" {
		t.Errorf("IPv4-mapped IP key = %q", got)
	}
}

func TestRequestBudgetRefusesAndRecovers(t *testing.T) {
	base := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	cur := base
	s := newTestServiceWith(t, WithClock(func() time.Time { return cur }), WithRequestBudget(2))
	ctx := WithPrincipal(context.Background(), Principal{Kind: "user", UserID: "user-1"})

	for i := 0; i < 2; i++ {
		if err := s.AllowRequest(ctx); err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
	}
	se := wantCode(t, s.AllowRequest(ctx), CodeRateLimited)
	// The window is aligned to the minute, so 5:06:07 waits until 5:07:00.
	if secs := retryAfterDetail(t, se); secs != 53 {
		t.Errorf("retry_after_seconds = %d, want 53", secs)
	}
	if se.Message != "too many requests; try again in 53 seconds" {
		t.Errorf("message = %q", se.Message)
	}

	// Another account has its own budget.
	other := WithPrincipal(context.Background(), Principal{Kind: "user", UserID: "user-2"})
	if err := s.AllowRequest(other); err != nil {
		t.Errorf("another account was refused: %v", err)
	}

	// Still refused just before the boundary...
	cur = time.Date(2026, 3, 4, 5, 6, 59, 0, time.UTC)
	if err := s.AllowRequest(ctx); err == nil {
		t.Error("the budget recovered before the boundary")
	}
	// ...and allowed in the next aligned window.
	cur = time.Date(2026, 3, 4, 5, 7, 0, 0, time.UTC)
	if err := s.AllowRequest(ctx); err != nil {
		t.Errorf("the budget did not recover: %v", err)
	}
}

func TestRequestBudgetIsPerCallerNotPerCredential(t *testing.T) {
	s := newTestServiceWith(t, WithRequestBudget(1))
	session := WithPrincipal(context.Background(), Principal{Kind: "user", UserID: "user-1", SessionID: "session-1"})
	token := WithPrincipal(context.Background(), Principal{Kind: "user", UserID: "user-1", TokenID: "token-1"})

	if err := s.AllowRequest(session); err != nil {
		t.Fatalf("session request: %v", err)
	}
	// An access token for the same account spends the same budget, so swapping
	// credentials cannot dodge it.
	wantCode(t, s.AllowRequest(token), CodeRateLimited)

	// An admin key is a distinct caller.
	key := WithPrincipal(context.Background(), Principal{Kind: "key", KeyID: "key-1", OrgID: "org-1"})
	if err := s.AllowRequest(key); err != nil {
		t.Errorf("admin key request: %v", err)
	}

	// Local mode and anonymous requests are never metered.
	local := WithPrincipal(context.Background(), LocalPrincipal())
	for i := 0; i < 2; i++ {
		if err := s.AllowRequest(local); err != nil {
			t.Errorf("local request %d: %v", i+1, err)
		}
	}
	if err := s.AllowRequest(context.Background()); err != nil {
		t.Errorf("anonymous request: %v", err)
	}
}
