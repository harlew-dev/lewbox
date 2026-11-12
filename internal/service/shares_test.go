package service

import (
	"testing"
	"time"
)

func intPtr(n int64) *int64       { return &n }
func floatPtr(n float64) *float64 { return &n }

func shareMembers(ids ...string) []memberShareInput {
	members := make([]memberShareInput, 0, len(ids))
	for i, id := range ids {
		members = append(members, memberShareInput{
			ID:        id,
			CreatedAt: time.Date(2026, 11, 1, 0, i, 0, 0, time.UTC),
			Role:      roleMember,
		})
	}
	return members
}

func wantShare(t *testing.T, got shares, sandboxes int64, vcpus float64, memoryMB int64) {
	t.Helper()
	if got.Sandboxes == nil || *got.Sandboxes != sandboxes {
		t.Errorf("sandboxes = %v, want %d", got.Sandboxes, sandboxes)
	}
	if got.Vcpus == nil || *got.Vcpus != vcpus {
		t.Errorf("vcpus = %v, want %g", got.Vcpus, vcpus)
	}
	if got.MemoryMB == nil || *got.MemoryMB != memoryMB {
		t.Errorf("memory = %v, want %d", got.MemoryMB, memoryMB)
	}
}

func TestMemberSharesEqualSplit(t *testing.T) {
	members := shareMembers("a", "b", "c")
	got := memberShares(Limits{Sandboxes: intPtr(5), Vcpus: intPtr(2), MemoryMB: intPtr(4096)}, members)
	wantShare(t, got["a"], 2, 0.66, 1366)
	wantShare(t, got["b"], 2, 0.66, 1365)
	wantShare(t, got["c"], 1, 0.66, 1365)
}

func TestMemberSharesExplicit(t *testing.T) {
	members := shareMembers("a", "b", "c")
	members[0].Explicit = shares{Sandboxes: intPtr(4), Vcpus: floatPtr(1.5)}
	got := memberShares(Limits{Sandboxes: intPtr(10), Vcpus: intPtr(4), MemoryMB: intPtr(8192)}, members)
	// Memory is all automatic, so it splits equally.
	wantShare(t, got["a"], 4, 1.5, 2731)
	wantShare(t, got["b"], 3, 1.25, 2731)
	wantShare(t, got["c"], 3, 1.25, 2730)
}

func TestMemberSharesUnlimited(t *testing.T) {
	members := shareMembers("a", "b")
	members[0].Explicit = shares{Sandboxes: intPtr(3), MemoryMB: intPtr(1024)}
	got := memberShares(Limits{}, members)
	if got["a"].Sandboxes == nil || *got["a"].Sandboxes != 3 || got["a"].Vcpus != nil ||
		got["a"].MemoryMB == nil || *got["a"].MemoryMB != 1024 {
		t.Errorf("explicit cap on an unlimited plan = %+v", got["a"])
	}
	if got["b"].Sandboxes != nil || got["b"].Vcpus != nil || got["b"].MemoryMB != nil {
		t.Errorf("automatic unlimited share = %+v", got["b"])
	}
}

func TestMemberSharesZeroBlocks(t *testing.T) {
	members := shareMembers("a", "b", "c", "d")
	got := memberShares(Limits{Sandboxes: intPtr(2), Vcpus: intPtr(4), MemoryMB: intPtr(4096)}, members)
	if *got["a"].Sandboxes != 1 || *got["b"].Sandboxes != 1 || *got["c"].Sandboxes != 0 || *got["d"].Sandboxes != 0 {
		t.Errorf("sandbox split = %+v", got)
	}
	if !shareBlocked(got["c"]) || !shareBlocked(got["d"]) || shareBlocked(got["a"]) {
		t.Errorf("blocking = %+v", got)
	}
}

func TestMemberSharesScaleDown(t *testing.T) {
	members := shareMembers("a", "b")
	members[0].Explicit = shares{Sandboxes: intPtr(6), Vcpus: floatPtr(0.6)}
	members[1].Explicit = shares{Sandboxes: intPtr(3), Vcpus: floatPtr(0.6)}
	got := memberShares(Limits{Sandboxes: intPtr(4), Vcpus: intPtr(1)}, members)
	if *got["a"].Sandboxes != 2 || *got["b"].Sandboxes != 1 {
		t.Errorf("scaled sandboxes = %+v", got)
	}
	if *got["a"].Vcpus != 0.5 || *got["b"].Vcpus != 0.5 {
		t.Errorf("scaled vcpus = %+v", got)
	}
}

func TestMemberSharesFractionalAndViewers(t *testing.T) {
	members := shareMembers("a", "b", "viewer")
	members[0].Explicit = shares{Vcpus: floatPtr(0.555)}
	members[2].Role = roleViewer
	got := memberShares(Limits{Vcpus: intPtr(1), Sandboxes: intPtr(1)}, members)
	if *got["a"].Vcpus != 0.55 {
		t.Errorf("explicit vcpus = %v, want 0.55", *got["a"].Vcpus)
	}
	if _, ok := got["viewer"]; ok {
		t.Error("a viewer got a share")
	}
}

func TestShareBlockedOnlyLimitedKinds(t *testing.T) {
	if shareBlocked(shares{}) {
		t.Error("an unlimited share is blocked")
	}
	zero := int64(0)
	if !shareBlocked(shares{Sandboxes: &zero}) {
		t.Error("a zero sandbox share is not blocked")
	}
}
