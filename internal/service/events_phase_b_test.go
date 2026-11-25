package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func findEvent(t *testing.T, s *Service, action string) (Event, bool) {
	t.Helper()
	list, err := s.ListEvents(localCtx(), ListEventsRequest{Action: action})
	if err != nil {
		t.Fatalf("list events %s: %v", action, err)
	}
	if len(list.Data) == 0 {
		return Event{}, false
	}
	return list.Data[0], true
}

func TestClusterEvents(t *testing.T) {
	s := newClusterService(t)
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	detail := addTestCluster(t, s, fake, "eu-west")

	created, ok := findEvent(t, s, "cluster.created")
	if !ok || created.TargetType != "cluster" || created.TargetID != detail.ID || created.TargetName != "eu-west" {
		t.Fatalf("cluster.created = %+v", created)
	}

	if _, err := s.UpdateCluster(localCtx(), "eu-west", UpdateClusterRequest{ArchiveLocalRetention: strptr("1h")}); err != nil {
		t.Fatal(err)
	}
	updated, ok := findEvent(t, s, "cluster.updated")
	if !ok || updated.TargetID != detail.ID {
		t.Fatalf("cluster.updated = %+v", updated)
	}
	if change, ok := updated.Changes["archive_local_retention"]; !ok || change.From != "instant" || change.To != "1h" {
		t.Errorf("changes = %+v", updated.Changes)
	}

	if _, err := s.CreateJoinToken(localCtx(), "eu-west", JoinTokenRequest{Role: "worker"}); err != nil {
		t.Fatal(err)
	}
	join, ok := findEvent(t, s, "join_token.created")
	if !ok || join.TargetID != detail.ID || join.TargetName != "eu-west" {
		t.Errorf("join_token.created = %+v", join)
	}
	data, _ := json.Marshal(join)
	if strings.Contains(string(data), "join_token\":") || strings.Contains(string(data), "secret") {
		t.Errorf("the join token event carries a secret: %s", data)
	}

	if _, err := s.RemoveCluster(localCtx(), "eu-west", false); err != nil {
		t.Fatal(err)
	}
	if removed, ok := findEvent(t, s, "cluster.deleted"); !ok || removed.TargetID != detail.ID {
		t.Errorf("cluster.deleted = %+v", removed)
	}
}

func TestNodeEvents(t *testing.T) {
	s := newTestService(t)
	node, err := s.AddNode(localCtx(), AddNodeRequest{Name: "node-1", URL: "https://node-1.example.test:8443"})
	if err != nil {
		t.Fatal(err)
	}
	created, ok := findEvent(t, s, "node.created")
	if !ok || created.TargetID != node.ID || created.TargetName != "node-1" {
		t.Fatalf("node.created = %+v", created)
	}
	if _, err := s.RemoveNode(localCtx(), "node-1", RemoveNodeRequest{}); err != nil {
		t.Fatal(err)
	}
	if removed, ok := findEvent(t, s, "node.deleted"); !ok || removed.TargetID != node.ID {
		t.Errorf("node.deleted = %+v", removed)
	}
}

func TestDNSProfileEventsNeverRecordTheToken(t *testing.T) {
	s, _, fake := newDNSProfileFixture(t)
	fake.AddZone("Example.com")
	p := createDNSProfile(t, s, CreateDNSProfileRequest{
		Name: "cf-eu", Zone: "example.com", APIToken: dnsTestToken, ACMEEmail: "ops@example.com",
	})

	created, ok := findEvent(t, s, "dns_profile.created")
	if !ok || created.TargetID != p.ID || created.TargetName != "cf-eu" {
		t.Fatalf("dns_profile.created = %+v", created)
	}

	if _, err := s.UpdateDNSProfile(localCtx(), p.Name, UpdateDNSProfileRequest{Name: strptr("cf-eu-2")}); err != nil {
		t.Fatal(err)
	}
	updated, ok := findEvent(t, s, "dns_profile.updated")
	if !ok || updated.TargetName != "cf-eu-2" {
		t.Fatalf("dns_profile.updated = %+v", updated)
	}
	if change, ok := updated.Changes["name"]; !ok || change.From != "cf-eu" || change.To != "cf-eu-2" {
		t.Errorf("changes = %+v", updated.Changes)
	}

	if _, err := s.DNSProfileCredentials(localCtx(), p.ID); err != nil {
		t.Fatal(err)
	}
	if read, ok := findEvent(t, s, "dns_profile.credentials_read"); !ok || read.TargetID != p.ID {
		t.Errorf("credentials read = %+v", read)
	}

	if _, err := s.DeleteDNSProfile(localCtx(), p.ID); err != nil {
		t.Fatal(err)
	}
	if removed, ok := findEvent(t, s, "dns_profile.deleted"); !ok || removed.TargetID != p.ID {
		t.Errorf("dns_profile.deleted = %+v", removed)
	}

	list, err := s.ListEvents(localCtx(), ListEventsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(list.Data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), dnsTestToken) {
		t.Fatalf("the audit trail carries the DNS token: %s", data)
	}
}

func TestStorageProfileEventsNeverRecordTheSecret(t *testing.T) {
	s, _, fake := newStorageProfileFixture(t)
	p := createStorageProfile(t, s, newStorageProfileRequest(fake, "backups"))

	if created, ok := findEvent(t, s, "storage_profile.created"); !ok || created.TargetID != p.ID {
		t.Fatalf("storage_profile.created = %+v", created)
	}
	if _, err := s.UpdateStorageProfile(localCtx(), p.Name, UpdateStorageProfileRequest{Name: strptr("backups-2")}); err != nil {
		t.Fatal(err)
	}
	updated, ok := findEvent(t, s, "storage_profile.updated")
	if !ok || updated.TargetName != "backups-2" {
		t.Fatalf("storage_profile.updated = %+v", updated)
	}
	if _, err := s.StorageProfileCredentials(localCtx(), p.ID); err != nil {
		t.Fatal(err)
	}
	if read, ok := findEvent(t, s, "storage_profile.credentials_read"); !ok || read.TargetID != p.ID {
		t.Errorf("credentials read = %+v", read)
	}
	if _, err := s.DeleteStorageProfile(localCtx(), p.ID); err != nil {
		t.Fatal(err)
	}
	if removed, ok := findEvent(t, s, "storage_profile.deleted"); !ok || removed.TargetID != p.ID {
		t.Errorf("storage_profile.deleted = %+v", removed)
	}

	list, err := s.ListEvents(localCtx(), ListEventsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(list.Data)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{storageTestSecret, fake.AccessKeyID} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("the audit trail carries a storage credential: %s", data)
		}
	}
}

func TestSubscriptionEvents(t *testing.T) {
	f := newSandboxFixture(t)

	limit := int64(3)
	if _, err := f.s.SetSubscription(f.root, "acme", "eu-west", SetSubscriptionRequest{
		Plan:         "free",
		MaxSandboxes: OptionalInt{Set: true, Value: &limit},
	}); err != nil {
		t.Fatal(err)
	}
	updated, ok := findEvent(t, f.s, "subscription.updated")
	if !ok || updated.TargetID == "" || updated.OrganizationID == "" {
		t.Fatalf("subscription.updated = %+v", updated)
	}
	if change, ok := updated.Changes["max_sandboxes"]; !ok || change.To != float64(3) {
		t.Errorf("changes = %+v", updated.Changes)
	}

	if _, err := f.s.DeleteSubscription(f.root, "acme", "eu-west", false); err != nil {
		t.Fatal(err)
	}
	if removed, ok := findEvent(t, f.s, "subscription.deleted"); !ok || removed.OrganizationID != updated.OrganizationID {
		t.Errorf("subscription.deleted = %+v", removed)
	}
}

func TestFailedSubscriptionDeleteRecordsTheRestore(t *testing.T) {
	f := newSandboxFixture(t)
	if _, err := f.s.CreateSandboxKey(f.owner, "acme", CreateSandboxKeyRequest{Name: "laptop", Region: "eu-west"}); err != nil {
		t.Fatal(err)
	}
	f.fake.Stop()
	if _, err := f.s.DeleteSubscription(f.root, "acme", "eu-west", false); err == nil {
		t.Fatal("a failed key revocation removed the subscription")
	}
	if subs, err := f.s.ListSubscriptions(f.root, "acme"); err != nil || len(subs.Data) != 1 {
		t.Errorf("subscription after a failed remove = %+v, %v", subs.Data, err)
	}

	// The trail is append-only: the deletion and its undo both stay, newest
	// first, with the reason on the restoration.
	if _, ok := findEvent(t, f.s, "subscription.deleted"); !ok {
		t.Error("a failed removal has no subscription.deleted event")
	}
	restored, ok := findEvent(t, f.s, "subscription.restored")
	if !ok {
		t.Fatal("a failed removal has no subscription.restored event")
	}
	if restored.OrganizationID == "" {
		t.Errorf("subscription.restored = %+v", restored)
	}
	if change, ok := restored.Changes["reason"]; !ok || change.To != "key revocation failed" {
		t.Errorf("restore reason = %+v", restored.Changes)
	}
}
