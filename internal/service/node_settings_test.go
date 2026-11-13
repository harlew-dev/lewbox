package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/harlew-dev/lewbox/internal/dns/cloudflaretest"
	"github.com/harlew-dev/lewbox/internal/objstore/objstoretest"
)

// nodeSettingsFixture is a service with a fake S3 bucket and a fake
// Cloudflare API, ready for a cluster that uses both profiles.
func nodeSettingsFixture(t *testing.T) (*Service, *objstoretest.Fake) {
	t.Helper()
	e, _, storage := newStorageProfileFixture(t)
	return e, storage
}

func ptrString(s string) *string { return &s }

func TestClusterNodeSettings(t *testing.T) {
	s, storage := nodeSettingsFixture(t)
	cf := cloudflaretest.New(t)
	cf.Token = dnsTestToken
	cf.AddZone("example.com")
	s.cloudflareURL = cf.URL
	profile := createStorageProfile(t, s, newStorageProfileRequest(storage, "s3-main"))
	profile.Prefix = "lewbox"
	// Set the prefix through an update, which also bumps the version.
	if _, err := s.UpdateStorageProfile(localCtx(), profile.ID, UpdateStorageProfileRequest{Prefix: ptrString("lewbox")}); err != nil {
		t.Fatal(err)
	}
	dns := createDNSProfile(t, s, CreateDNSProfileRequest{
		Name: "cf-prod", Zone: "example.com", APIToken: dnsTestToken, ACMEEmail: "ops@example.com",
	})

	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	req := clusterRequest(fake, "lab")
	req.Domain = "eu.example.com"
	req.DNSProfile = dns.Name
	req.StorageProfile = "s3-main"
	req.StopPolicy = "archive"
	detail, err := s.AddCluster(localCtx(), req)
	if err != nil {
		t.Fatal(err)
	}

	settings, err := s.ClusterNodeSettings(localCtx(), "lab")
	if err != nil {
		t.Fatal(err)
	}
	if settings.ClusterID != detail.ID || settings.Cluster != "lab" || settings.StopPolicy != "archive" {
		t.Errorf("settings = %+v", settings)
	}
	if settings.Storage == nil || settings.DNS == nil {
		t.Fatalf("settings = %+v", settings)
	}
	st := settings.Storage
	if st.Profile != "s3-main" || st.Version != 2 || st.Bucket != "assets" || st.Endpoint != storage.URL ||
		!st.PathStyle || st.AccessKeyID != storage.AccessKeyID || st.SecretAccessKey != storageTestSecret {
		t.Errorf("storage = %+v", st)
	}
	if want := "/lewbox/clusters/" + detail.ID; st.RootDirectory != want {
		t.Errorf("root_directory = %q, want %q", st.RootDirectory, want)
	}
	if d := settings.DNS; d.Profile != "cf-prod" || d.Version != 1 || d.Zone != "example.com" ||
		d.Domain != "eu.example.com" || d.APIToken != dnsTestToken || d.ACMEEmail != "ops@example.com" {
		t.Errorf("dns = %+v", d)
	}

	// JSON carries the secrets: it is the node's credential.
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), storageTestSecret) || !strings.Contains(string(data), dnsTestToken) {
		t.Errorf("settings JSON has no secrets: %s", data)
	}
	if !strings.Contains(string(data), `"root_directory":"/lewbox/clusters/`+detail.ID+`"`) {
		t.Errorf("settings JSON = %s", data)
	}

	// A cluster with no profiles has null storage and DNS.
	plain := newClusterFake(t)
	setMembers(plain, "node-b")
	if _, err := s.AddCluster(localCtx(), clusterRequest(plain, "plain")); err != nil {
		t.Fatal(err)
	}
	empty, err := s.ClusterNodeSettings(localCtx(), "plain")
	if err != nil {
		t.Fatal(err)
	}
	if empty.Storage != nil || empty.DNS != nil || empty.StopPolicy != "keep" {
		t.Errorf("plain settings = %+v", empty)
	}
}

func TestClusterStorageValidation(t *testing.T) {
	s, storage := nodeSettingsFixture(t)
	createStorageProfile(t, s, newStorageProfileRequest(storage, "s3-main"))
	fake := newClusterFake(t)
	setMembers(fake, "node-a")

	noProfile := clusterRequest(fake, "archive-none")
	noProfile.StopPolicy = "archive"
	if _, err := s.AddCluster(localCtx(), noProfile); err == nil || !strings.Contains(err.Error(), "needs a storage profile") {
		t.Errorf("archive without a profile: %v", err)
	}

	firecracker := clusterRequest(fake, "archive-fc")
	firecracker.Runtime = "firecracker"
	firecracker.StorageProfile = "s3-main"
	firecracker.StopPolicy = "archive"
	if _, err := s.AddCluster(localCtx(), firecracker); err == nil || !strings.Contains(err.Error(), "only tested on Docker") {
		t.Errorf("archive on firecracker: %v", err)
	}

	badPolicy := clusterRequest(fake, "bad-policy")
	badPolicy.StopPolicy = "delete"
	if _, err := s.AddCluster(localCtx(), badPolicy); err == nil || !strings.Contains(err.Error(), "keep or archive") {
		t.Errorf("bad stop policy: %v", err)
	}

	req := clusterRequest(fake, "lab")
	req.StorageProfile = "s3-main"
	req.StopPolicy = "archive"
	detail, err := s.AddCluster(localCtx(), req)
	if err != nil {
		t.Fatal(err)
	}
	if detail.StorageProfile == nil || *detail.StorageProfile != "s3-main" || detail.StopPolicy != "archive" {
		t.Errorf("cluster = %+v", detail.Cluster)
	}

	// Clearing the profile while archiving is refused; keeping stopped
	// sandboxes first lets it go.
	if _, err := s.UpdateCluster(localCtx(), "lab", UpdateClusterRequest{StorageProfile: ptrString("none")}); err == nil ||
		!strings.Contains(err.Error(), "needs a storage profile") {
		t.Errorf("clearing while archiving: %v", err)
	}
	if _, err := s.UpdateCluster(localCtx(), "lab", UpdateClusterRequest{StopPolicy: ptrString("keep")}); err != nil {
		t.Fatal(err)
	}
	cleared, err := s.UpdateCluster(localCtx(), "lab", UpdateClusterRequest{StorageProfile: ptrString("none")})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.StorageProfile != nil || cleared.StopPolicy != "keep" {
		t.Errorf("cleared cluster = %+v", cleared.Cluster)
	}
	if _, err := s.UpdateCluster(localCtx(), "lab", UpdateClusterRequest{StopPolicy: ptrString("archive")}); err == nil ||
		!strings.Contains(err.Error(), "needs a storage profile") {
		t.Errorf("archiving without a profile: %v", err)
	}
}

func TestReportNodeSettingsAndOutdated(t *testing.T) {
	s, storage := nodeSettingsFixture(t)
	createStorageProfile(t, s, newStorageProfileRequest(storage, "s3-main"))
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	req := clusterRequest(fake, "lab")
	req.StorageProfile = "s3-main"
	if _, err := s.AddCluster(localCtx(), req); err != nil {
		t.Fatal(err)
	}

	// Nothing reported yet: the node has no storage settings.
	node, err := s.GetNode(localCtx(), "node-a")
	if err != nil {
		t.Fatal(err)
	}
	if node.Settings == nil || node.Settings.AppliedAt != nil || !node.Settings.Outdated {
		t.Fatalf("node settings = %+v", node.Settings)
	}
	if len(node.Settings.Reasons) != 1 || node.Settings.Reasons[0] != "has no storage settings yet" {
		t.Errorf("reasons = %v", node.Settings.Reasons)
	}

	reported, err := s.ReportNodeSettings(localCtx(), "node-a", ReportNodeSettingsRequest{
		StorageProfile: ptrString("s3-main"), StorageVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reported.Settings == nil || reported.Settings.AppliedAt == nil || reported.Settings.Outdated {
		t.Errorf("reported settings = %+v", reported.Settings)
	}
	if reported.Settings.Reasons == nil || len(reported.Settings.Reasons) != 0 {
		t.Errorf("reasons = %v", reported.Settings.Reasons)
	}

	// A newer profile version makes it outdated.
	if _, err := s.UpdateStorageProfile(localCtx(), "s3-main", UpdateStorageProfileRequest{Prefix: ptrString("moved")}); err != nil {
		t.Fatal(err)
	}
	node, err = s.GetNode(localCtx(), "node-a")
	if err != nil {
		t.Fatal(err)
	}
	want := "storage profile s3-main is at version 2; this node has version 1"
	if len(node.Settings.Reasons) != 1 || node.Settings.Reasons[0] != want {
		t.Errorf("reasons = %v, want %q", node.Settings.Reasons, want)
	}

	// Another profile is outdated too.
	createStorageProfile(t, s, newStorageProfileRequest(storage, "s3-old"))
	node, err = s.ReportNodeSettings(localCtx(), "node-a", ReportNodeSettingsRequest{
		StorageProfile: ptrString("s3-old"), StorageVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(node.Settings.Reasons) != 1 || !strings.Contains(node.Settings.Reasons[0], "this node applied s3-old") {
		t.Errorf("reasons = %v", node.Settings.Reasons)
	}

	// Clearing the cluster's profile makes the node's one stale.
	if _, err := s.UpdateCluster(localCtx(), "lab", UpdateClusterRequest{StorageProfile: ptrString("none")}); err != nil {
		t.Fatal(err)
	}
	node, err = s.GetNode(localCtx(), "node-a")
	if err != nil {
		t.Fatal(err)
	}
	want = "the cluster no longer uses storage profile s3-old"
	if len(node.Settings.Reasons) != 1 || node.Settings.Reasons[0] != want {
		t.Errorf("reasons = %v, want %q", node.Settings.Reasons, want)
	}

	// A node in no cluster has no settings at all.
	legacy, err := s.AddNode(localCtx(), AddNodeRequest{Name: "legacy", URL: "http://legacy.test:21212"})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Settings != nil {
		t.Errorf("legacy node settings = %+v", legacy.Settings)
	}
	if _, err := s.ReportNodeSettings(localCtx(), "legacy", ReportNodeSettingsRequest{}); err == nil {
		t.Error("reported settings for a node in no cluster")
	}
}

func TestNodeSettingsDNSErrors(t *testing.T) {
	s, _ := nodeSettingsFixture(t)
	cf := cloudflaretest.New(t)
	cf.Token = dnsTestToken
	cf.AddZone("example.com")
	s.cloudflareURL = cf.URL
	dns := createDNSProfile(t, s, CreateDNSProfileRequest{Name: "cf-prod", Zone: "example.com", APIToken: dnsTestToken})
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	req := clusterRequest(fake, "lab")
	req.Domain = "eu.example.com"
	req.DNSProfile = dns.Name
	if _, err := s.AddCluster(localCtx(), req); err != nil {
		t.Fatal(err)
	}

	// A node that never reported DNS is not judged on it.
	node, err := s.GetNode(localCtx(), "node-a")
	if err != nil {
		t.Fatal(err)
	}
	if node.Settings.Outdated {
		t.Errorf("a node with no DNS report is outdated: %v", node.Settings.Reasons)
	}

	node, err = s.ReportNodeSettings(localCtx(), "node-a", ReportNodeSettingsRequest{
		DNSProfile: ptrString("cf-prod"), DNSVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if node.Settings.Outdated {
		t.Errorf("reported DNS is outdated: %v", node.Settings.Reasons)
	}

	// A new token bumps the profile's version.
	token := dnsTestToken
	if _, err := s.UpdateDNSProfile(localCtx(), "cf-prod", UpdateDNSProfileRequest{APIToken: &token}); err != nil {
		t.Fatal(err)
	}
	node, err = s.GetNode(localCtx(), "node-a")
	if err != nil {
		t.Fatal(err)
	}
	want := "DNS profile cf-prod is at version 2; this node has version 1"
	if len(node.Settings.Reasons) != 1 || node.Settings.Reasons[0] != want {
		t.Errorf("reasons = %v, want %q", node.Settings.Reasons, want)
	}

	// Clearing the cluster's DNS profile makes the node's one stale.
	if _, err := s.UpdateCluster(localCtx(), "lab", UpdateClusterRequest{DNSProfile: ptrString("none"), Domain: ptrString("")}); err != nil {
		t.Fatal(err)
	}
	node, err = s.GetNode(localCtx(), "node-a")
	if err != nil {
		t.Fatal(err)
	}
	want = "the cluster no longer uses DNS profile cf-prod"
	if len(node.Settings.Reasons) != 1 || node.Settings.Reasons[0] != want {
		t.Errorf("reasons = %v, want %q", node.Settings.Reasons, want)
	}
}

func TestNodeSettingsCheck(t *testing.T) {
	s, storage := nodeSettingsFixture(t)
	createStorageProfile(t, s, newStorageProfileRequest(storage, "s3-main"))
	fake := newClusterFake(t)
	setMembers(fake, "node-a", "node-b")
	req := clusterRequest(fake, "lab")
	req.StorageProfile = "s3-main"
	if _, err := s.AddCluster(localCtx(), req); err != nil {
		t.Fatal(err)
	}

	detail, err := s.CheckCluster(localCtx(), "lab")
	if err != nil {
		t.Fatal(err)
	}
	check, ok := findCheck(detail.Checks, "node settings")
	if !ok || check.OK {
		t.Fatalf("check = %+v, found %v", check, ok)
	}
	for _, want := range []string{"node-a: has no storage settings yet", "node-b: has no storage settings yet", "run `lewbox node configure` on each"} {
		if !strings.Contains(check.Detail, want) {
			t.Errorf("check detail %q is missing %q", check.Detail, want)
		}
	}

	for _, name := range []string{"node-a", "node-b"} {
		if _, err := s.ReportNodeSettings(localCtx(), name, ReportNodeSettingsRequest{
			StorageProfile: ptrString("s3-main"), StorageVersion: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	detail, err = s.CheckCluster(localCtx(), "lab")
	if err != nil {
		t.Fatal(err)
	}
	check, ok = findCheck(detail.Checks, "node settings")
	if !ok || !check.OK {
		t.Errorf("up-to-date check = %+v, found %v", check, ok)
	}
}

func TestNodeSettingsCheckOmittedWithoutStorage(t *testing.T) {
	s, _ := nodeSettingsFixture(t)
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	if _, err := s.AddCluster(localCtx(), clusterRequest(fake, "lab")); err != nil {
		t.Fatal(err)
	}
	detail, err := s.CheckCluster(localCtx(), "lab")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findCheck(detail.Checks, "node settings"); ok {
		t.Error("the node settings check is present without storage")
	}
}
