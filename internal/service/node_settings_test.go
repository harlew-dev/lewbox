package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
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

// reportPolicy marks every node of a cluster as having applied its create
// policy, so tests about other checks do not trip over it.
func reportPolicy(t *testing.T, s *Service, clusterID, runtime string) {
	t.Helper()
	if _, err := s.database.ExecContext(context.Background(),
		`UPDATE nodes SET settings_policy_runtime = ? WHERE cluster_id = ?`, runtime, clusterID); err != nil {
		t.Fatal(err)
	}
}

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
	if settings.ClusterID != detail.ID || settings.Cluster != "lab" || settings.Runtime != "docker" || settings.StopPolicy != "archive" {
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

	// AerolVM pushes Firecracker snapshots since v0.6.11, so Firecracker
	// clusters archive too.
	fcFake := newClusterFake(t)
	fcFake.SetMembers([]aerolvm.Member{testMember(fcFake, "node-fc", "firecracker", true)})
	firecracker := clusterRequest(fcFake, "archive-fc")
	firecracker.Runtime = "firecracker"
	firecracker.StorageProfile = "s3-main"
	firecracker.StopPolicy = "archive"
	if detail, err := s.AddCluster(localCtx(), firecracker); err != nil {
		t.Errorf("archive on firecracker: %v", err)
	} else if detail.StopPolicy != "archive" {
		t.Errorf("firecracker stop policy = %q, want archive", detail.StopPolicy)
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
	detail, err := s.AddCluster(localCtx(), req)
	if err != nil {
		t.Fatal(err)
	}
	reportPolicy(t, s, detail.ID, detail.Runtime)

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
		StorageProfile: ptrString("s3-main"), StorageVersion: 1, PolicyRuntime: ptrString("docker"),
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
		StorageProfile: ptrString("s3-old"), StorageVersion: 1, PolicyRuntime: ptrString("docker"),
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
	detail, err := s.AddCluster(localCtx(), req)
	if err != nil {
		t.Fatal(err)
	}
	reportPolicy(t, s, detail.ID, detail.Runtime)

	// A node that never reported DNS is not judged on it.
	node, err := s.GetNode(localCtx(), "node-a")
	if err != nil {
		t.Fatal(err)
	}
	if node.Settings.Outdated {
		t.Errorf("a node with no DNS report is outdated: %v", node.Settings.Reasons)
	}

	node, err = s.ReportNodeSettings(localCtx(), "node-a", ReportNodeSettingsRequest{
		DNSProfile: ptrString("cf-prod"), DNSVersion: 1, PolicyRuntime: ptrString("docker"),
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
			StorageProfile: ptrString("s3-main"), StorageVersion: 1, PolicyRuntime: ptrString("docker"),
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

func TestNodeCreatePolicyOutdated(t *testing.T) {
	s, _ := nodeSettingsFixture(t)
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	if _, err := s.AddCluster(localCtx(), clusterRequest(fake, "lab")); err != nil {
		t.Fatal(err)
	}

	node, err := s.GetNode(localCtx(), "node-a")
	if err != nil {
		t.Fatal(err)
	}
	want := "create policy runtime is docker; this node applied none"
	if !node.Settings.Outdated || !containsString(node.Settings.Reasons, want) {
		t.Errorf("reasons = %v, want %q", node.Settings.Reasons, want)
	}

	node, err = s.ReportNodeSettings(localCtx(), "node-a", ReportNodeSettingsRequest{
		PolicyRuntime: ptrString("firecracker"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want = "create policy runtime is docker; this node applied firecracker"
	if !node.Settings.Outdated || !containsString(node.Settings.Reasons, want) {
		t.Errorf("reasons = %v, want %q", node.Settings.Reasons, want)
	}

	if _, err := s.ReportNodeSettings(localCtx(), "node-a", ReportNodeSettingsRequest{
		PolicyRuntime: ptrString("gvisor"),
	}); err == nil {
		t.Error("an unknown policy runtime was accepted")
	}

	node, err = s.ReportNodeSettings(localCtx(), "node-a", ReportNodeSettingsRequest{
		PolicyRuntime: ptrString("docker"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if node.Settings.Outdated {
		t.Errorf("a matching policy is outdated: %v", node.Settings.Reasons)
	}

	// The list query reads its own columns; it must carry the policy too.
	list, err := s.ListNodes(localCtx(), ListNodesRequest{Cluster: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || list.Data[0].Settings.Outdated {
		t.Errorf("listed node = %+v", list.Data)
	}
}

func TestCreatePolicyCheck(t *testing.T) {
	s, _ := nodeSettingsFixture(t)
	fake := newClusterFake(t)
	setMembers(fake, "node-a", "node-b")
	detail, err := s.AddCluster(localCtx(), clusterRequest(fake, "lab"))
	if err != nil {
		t.Fatal(err)
	}

	check := func() Check {
		t.Helper()
		checked, err := s.CheckCluster(localCtx(), "lab")
		if err != nil {
			t.Fatal(err)
		}
		c, ok := findCheck(checked.Checks, "create policy")
		if !ok {
			t.Fatalf("checks = %+v", checked.Checks)
		}
		return c
	}

	// No node has applied the policy yet.
	c := check()
	if c.OK {
		t.Fatalf("check = %+v", c)
	}
	for _, want := range []string{
		"node-a: create policy runtime is docker; this node applied none",
		"node-b: create policy runtime is docker; this node applied none",
		"run `lewbox node configure` on each",
	} {
		if !strings.Contains(c.Detail, want) {
			t.Errorf("detail %q is missing %q", c.Detail, want)
		}
	}

	for _, name := range []string{"node-a", "node-b"} {
		if _, err := s.ReportNodeSettings(localCtx(), name, ReportNodeSettingsRequest{
			PolicyRuntime: ptrString("docker"),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if c = check(); !c.OK || c.Detail != "2 node(s) enforce docker" {
		t.Errorf("up-to-date check = %+v", c)
	}

	// A sandbox of another runtime in the mirror is the live drift signal.
	// A destroyed one does not count, and a blank runtime is docker.
	for _, row := range []struct{ id, status, runtime string }{
		{"sb-drift", "started", "firecracker"},
		{"sb-destroyed", "destroyed", "firecracker"},
		{"sb-default", "started", ""},
	} {
		if _, err := s.database.ExecContext(context.Background(),
			`INSERT INTO sandboxes (cluster_id, id, node, name, image, status, status_reason, runtime, cpu, memory_mb, disk_gb, created_at, last_seen_at)
			 VALUES (?, ?, 'node-b', ?, 'alpine', ?, '', ?, 1, 1024, 10, ?, ?)`,
			detail.ID, row.id, row.id, row.status, row.runtime, s.now(), s.now()); err != nil {
			t.Fatal(err)
		}
	}
	c = check()
	if c.OK {
		t.Fatalf("drift check = %+v", c)
	}
	for _, want := range []string{
		"1 sandbox(es) on this docker cluster run another runtime",
		"sb-drift (firecracker) on node-b",
	} {
		if !strings.Contains(c.Detail, want) {
			t.Errorf("detail %q is missing %q", c.Detail, want)
		}
	}
	if strings.Contains(c.Detail, "sb-destroyed") || strings.Contains(c.Detail, "sb-default") {
		t.Errorf("detail names a destroyed or default-runtime sandbox: %q", c.Detail)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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
