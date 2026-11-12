package service

import (
	"testing"
)

func TestDeleteSubscription(t *testing.T) {
	f := newSandboxFixture(t)
	key, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "laptop", Region: "eu-west"})
	if err != nil {
		t.Fatal(err)
	}
	upstream, ok := sandboxToken(f.fake, key.Token)
	if !ok {
		t.Fatalf("upstream token %q not found", key.Token)
	}
	f.fake.AddSandbox(f.org.Team, startedSandbox("sb-1", 1, 512))
	if err := f.s.Sync(localCtx()); err != nil {
		t.Fatal(err)
	}

	if _, err := f.s.DeleteSubscription(f.root, "acme", "eu-west", false); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, revoked := range f.fake.Revoked() {
		found = found || revoked == upstream.ID
	}
	if !found {
		t.Errorf("revoked tokens = %v, want the key's", f.fake.Revoked())
	}
	keys, err := f.s.ListSandboxKeys(f.owner, "acme")
	if err != nil || len(keys.Data) != 1 || keys.Data[0].RevokedAt == nil {
		t.Errorf("keys after remove = %+v, %v; want the key marked revoked", keys.Data, err)
	}
	subs, err := f.s.ListSubscriptions(f.root, "acme")
	if err != nil || len(subs.Data) != 0 {
		t.Errorf("subscriptions after remove = %+v, %v", subs, err)
	}
	pushes := f.fake.QuotaPushes(f.org.Team)
	last := pushes[len(pushes)-1]
	if last.MaxCPU != blockingCPU || last.MaxMemoryMB != blockingMemoryMB {
		t.Errorf("quota after remove = %+v", last)
	}
	list, err := f.s.ListSandboxes(localCtx(), ListSandboxesRequest{Org: "acme"})
	if err != nil || len(list.Data) != 1 || list.Data[0].Status != "stopped" {
		t.Errorf("sandboxes after remove = %+v, %v", list.Data, err)
	}
}

func TestDeleteSubscriptionFailsClosed(t *testing.T) {
	f := newSandboxFixture(t)
	if _, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "laptop", Region: "eu-west"}); err != nil {
		t.Fatal(err)
	}
	f.fake.Stop()
	if _, err := f.s.DeleteSubscription(f.root, "acme", "eu-west", false); err != nil {
		wantCode(t, err, CodeUnavailable)
	}
	if subs, err := f.s.ListSubscriptions(f.root, "acme"); err != nil || len(subs.Data) != 1 {
		t.Errorf("subscription was removed without force: %+v, %v", subs, err)
	}
	if _, err := f.s.DeleteSubscription(f.root, "acme", "eu-west", true); err != nil {
		t.Fatalf("forced remove: %v", err)
	}
}
