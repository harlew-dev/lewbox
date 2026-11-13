package service

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/aerolvm/aerolvmtest"
	"github.com/harlew-dev/lewbox/internal/dns/cloudflare"
	"github.com/harlew-dev/lewbox/internal/dns/cloudflaretest"
)

// dnsMember is a cluster member with a public host, as AerolVM reports one.
func dnsMember(name, role, host string) aerolvm.Member {
	return aerolvm.Member{
		NodeID:     name,
		NodeName:   name,
		APIURL:     "http://" + name + ".test:21212",
		PublicHost: host,
		Role:       role,
		Alive:      true,
		Version:    "v0.7.0",
		Capacity:   aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
	}
}

// newDNSRecordsFixture registers a cluster with a domain and DNS profile on a
// fake AerolVM and a fake Cloudflare API, and returns its zone ID.
func newDNSRecordsFixture(t *testing.T, members ...aerolvm.Member) (*Service, *cloudflaretest.Fake, *aerolvmtest.Server, string) {
	t.Helper()
	s, fake, clusterFake, zoneID, _ := newDNSRecordsFixtureBefore(t, nil, members...)
	return s, fake, clusterFake, zoneID
}

// newDNSRecordsFixtureBefore is newDNSRecordsFixture that runs before on the
// zone ahead of registering the cluster, and returns what AddCluster said.
func newDNSRecordsFixtureBefore(t *testing.T, before func(fake *cloudflaretest.Fake, zoneID string), members ...aerolvm.Member) (*Service, *cloudflaretest.Fake, *aerolvmtest.Server, string, ClusterDetail) {
	t.Helper()
	fake := cloudflaretest.New(t)
	fake.Token = dnsTestToken
	zoneID := fake.AddZone("example.com")
	clusterFake := newClusterFake(t)
	clusterFake.SetMembers(members)
	s := newClusterService(t, WithCloudflareURL(fake.URL))
	createDNSProfile(t, s, CreateDNSProfileRequest{
		Name: "cf", Zone: "example.com", APIToken: dnsTestToken,
	})
	req := clusterRequest(clusterFake, "eu-west")
	req.Domain = "eu.example.com"
	req.DNSProfile = "cf"
	if before != nil {
		before(fake, zoneID)
	}
	detail, err := s.AddCluster(localCtx(), req)
	if err != nil {
		t.Fatalf("AddCluster: %v", err)
	}
	return s, fake, clusterFake, zoneID, detail
}

func recordSet(records []cloudflare.Record) map[string]string {
	out := make(map[string]string, len(records))
	for _, record := range records {
		out[record.Name+" "+record.Content] = record.Type
	}
	return out
}

func TestClusterDNSRecords(t *testing.T) {
	s, fake, clusterFake, zoneID := newDNSRecordsFixture(t,
		dnsMember("node-a", "mixed", "203.0.113.10"),
		dnsMember("node-b", "ingress", "203.0.113.11"),
		dnsMember("worker-1", "worker", "203.0.113.12"),
	)

	detail, err := s.CheckCluster(localCtx(), "eu-west")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Domain != "eu.example.com" || detail.DNSProfile == nil || *detail.DNSProfile != "cf" || detail.DNSRecords != "auto" {
		t.Errorf("cluster DNS settings = domain %q, profile %v, records %q", detail.Domain, detail.DNSProfile, detail.DNSRecords)
	}

	records := fake.Records(zoneID)
	if len(records) != 4 {
		t.Fatalf("records = %+v", records)
	}
	want := map[string]string{
		"eu.example.com 203.0.113.10":   "A",
		"*.eu.example.com 203.0.113.10": "A",
		"eu.example.com 203.0.113.11":   "A",
		"*.eu.example.com 203.0.113.11": "A",
	}
	got := recordSet(records)
	for key, typ := range want {
		if got[key] != typ {
			t.Errorf("missing record %q (%s); got %v", key, typ, got)
		}
	}
	for _, record := range records {
		if record.TTL != 60 || record.Proxied {
			t.Errorf("record = %+v, want TTL 60 and not proxied", record)
		}
		if !strings.HasPrefix(record.Comment, "lewbox eu-west ") {
			t.Errorf("comment = %q", record.Comment)
		}
	}

	check, ok := findCheck(detail.Checks, "dns records")
	if !ok || !check.OK {
		t.Fatalf("dns records check = %+v", check)
	}
	if !strings.Contains(check.Detail, "4 records for eu.example.com and *.eu.example.com") ||
		!strings.Contains(check.Detail, "203.0.113.10") || !strings.Contains(check.Detail, "203.0.113.11") {
		t.Errorf("dns records detail = %q", check.Detail)
	}
	if detail.Status != "ready" {
		t.Errorf("status = %q", detail.Status)
	}

	// A member that leaves loses its records on the next check; the other
	// member's stay.
	clusterFake.SetMembers([]aerolvm.Member{dnsMember("node-a", "mixed", "203.0.113.10")})
	detail, err = s.CheckCluster(localCtx(), "eu-west")
	if err != nil {
		t.Fatal(err)
	}
	records = fake.Records(zoneID)
	if len(records) != 2 {
		t.Fatalf("after removal, records = %+v", records)
	}
	for _, record := range records {
		if record.Content != "203.0.113.10" {
			t.Errorf("leftover record = %+v", record)
		}
	}
}

func TestClusterDNSRecordsSkipsUnusableAddresses(t *testing.T) {
	s, fake, _, zoneID := newDNSRecordsFixture(t,
		dnsMember("node-a", "mixed", "10.0.0.5"),
		dnsMember("node-b", "ingress", "node-b.example"),
	)

	detail, err := s.CheckCluster(localCtx(), "eu-west")
	if err != nil {
		t.Fatal(err)
	}
	if records := fake.Records(zoneID); len(records) != 0 {
		t.Errorf("records = %+v", records)
	}
	check, ok := findCheck(detail.Checks, "dns records")
	if !ok || !check.OK {
		t.Fatalf("dns records check = %+v", check)
	}
	for _, want := range []string{
		"node-a: 10.0.0.5 is a private address, no record",
		"node-b: node-b.example is not an IP address, no record",
	} {
		if !strings.Contains(check.Detail, want) {
			t.Errorf("detail %q does not contain %q", check.Detail, want)
		}
	}
}

func TestClusterDNSRecordsAdoptsExisting(t *testing.T) {
	// The operator already made the apex record before registering.
	s, fake, _, zoneID, _ := newDNSRecordsFixtureBefore(t, func(fake *cloudflaretest.Fake, zoneID string) {
		client := cloudflare.New(dnsTestToken, cloudflare.WithBaseURL(fake.URL))
		if _, err := client.CreateRecord(context.Background(), zoneID, cloudflare.Record{
			Type: "A", Name: "eu.example.com", Content: "203.0.113.10", TTL: 300,
		}); err != nil {
			t.Fatal(err)
		}
	}, dnsMember("node-a", "mixed", "203.0.113.10"))

	// Registering adopts it; a later check keeps it.
	detail, err := s.CheckCluster(localCtx(), "eu-west")
	if err != nil {
		t.Fatal(err)
	}
	records := fake.Records(zoneID)
	if len(records) != 2 {
		t.Fatalf("records = %+v, want the wildcard added to the existing one", records)
	}
	rows, err := s.q.ListDNSRecordsByCluster(localCtx(), detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Errorf("rows = %d, want 2 (both records adopted or created)", len(rows))
	}
	for _, record := range records {
		if record.Name == "eu.example.com" && record.TTL != 300 {
			t.Errorf("the existing record was replaced: %+v", record)
		}
	}
}

func TestClusterDNSRecordsOff(t *testing.T) {
	s, fake, _, zoneID := newDNSRecordsFixture(t, dnsMember("node-a", "mixed", "203.0.113.10"))
	if _, err := s.CheckCluster(localCtx(), "eu-west"); err != nil {
		t.Fatal(err)
	}
	if records := fake.Records(zoneID); len(records) != 2 {
		t.Fatalf("records = %+v", records)
	}

	off := "off"
	updated, err := s.UpdateCluster(localCtx(), "eu-west", UpdateClusterRequest{DNSRecords: &off})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DNSRecords != "off" {
		t.Errorf("dns_records = %q", updated.DNSRecords)
	}

	detail, err := s.CheckCluster(localCtx(), "eu-west")
	if err != nil {
		t.Fatal(err)
	}
	if records := fake.Records(zoneID); len(records) != 0 {
		t.Errorf("records after off = %+v", records)
	}
	check, ok := findCheck(detail.Checks, "dns records")
	if !ok || !check.OK || check.Detail != "records are off" {
		t.Errorf("dns records check = %+v", check)
	}

	// With no rows left, an off cluster gets no DNS check at all.
	detail, err = s.CheckCluster(localCtx(), "eu-west")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findCheck(detail.Checks, "dns records"); ok {
		t.Errorf("an off cluster with no records has a DNS check: %+v", detail.Checks)
	}
}

func TestClusterDNSRecordsFailureDegrades(t *testing.T) {
	// Cloudflare fails while the cluster is registered: the cluster is
	// still stored, degraded, so the next check can retry.
	s, _, _, _, detail := newDNSRecordsFixtureBefore(t, func(fake *cloudflaretest.Fake, zoneID string) {
		fake.FailNext("POST", "/zones/"+zoneID+"/dns_records", 500, 1)
	}, dnsMember("node-a", "mixed", "203.0.113.10"))

	check, ok := findCheck(detail.Checks, "dns records")
	if !ok || check.OK {
		t.Fatalf("dns records check = %+v", check)
	}
	if !strings.Contains(check.Detail, "cloudflare") && !strings.Contains(check.Detail, "create A") {
		t.Errorf("detail = %q", check.Detail)
	}
	if detail.Status != "degraded" {
		t.Errorf("status = %q, want degraded", detail.Status)
	}

	// The next check retries and settles.
	detail, err := s.CheckCluster(localCtx(), "eu-west")
	if err != nil {
		t.Fatal(err)
	}
	if check, ok := findCheck(detail.Checks, "dns records"); !ok || !check.OK || detail.Status != "ready" {
		t.Errorf("retry: check %+v, status %q", check, detail.Status)
	}
}

func TestClusterDNSSettingsValidation(t *testing.T) {
	fake := cloudflaretest.New(t)
	fake.Token = dnsTestToken
	fake.AddZone("example.com")
	s := newClusterService(t, WithCloudflareURL(fake.URL))
	createDNSProfile(t, s, CreateDNSProfileRequest{Name: "cf", Zone: "example.com", APIToken: dnsTestToken})
	clusterFake := newClusterFake(t)
	setMembers(clusterFake, "node-a")

	cases := []struct {
		name   string
		change func(*AddClusterRequest)
		want   string
	}{
		{
			name:   "outside the zone",
			change: func(req *AddClusterRequest) { req.Domain = "eu.other.org"; req.DNSProfile = "cf" },
			want:   "domain eu.other.org is not in the profile's zone example.com",
		},
		{
			name:   "a profile without a domain",
			change: func(req *AddClusterRequest) { req.DNSProfile = "cf" },
			want:   "a domain is required when a DNS profile is set",
		},
		{
			name:   "a bad domain",
			change: func(req *AddClusterRequest) { req.Domain = "not_a_domain" },
			want:   "domain must be a domain name with a dot",
		},
		{
			name:   "a bad record mode",
			change: func(req *AddClusterRequest) { req.DNSRecords = "sometimes" },
			want:   "dns_records must be auto or off",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := clusterRequest(clusterFake, "eu-west")
			tc.change(&req)
			_, err := s.AddCluster(localCtx(), req)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("AddCluster error = %v, want %q", err, tc.want)
			}
			if se, ok := errors.AsType[*Error](err); !ok || se.Code != CodeInvalid {
				t.Errorf("error code = %v, want invalid", err)
			}
		})
	}
}

func TestDeleteDNSProfileRefusedWhileRecordsExist(t *testing.T) {
	s, _, _, _ := newDNSRecordsFixture(t, dnsMember("node-a", "mixed", "203.0.113.10"))
	if _, err := s.CheckCluster(localCtx(), "eu-west"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteDNSProfile(localCtx(), "cf"); err == nil {
		t.Fatal("a profile a cluster uses was deleted")
	}

	none := "none"
	if _, err := s.UpdateCluster(localCtx(), "eu-west", UpdateClusterRequest{DNSProfile: &none}); err != nil {
		t.Fatal(err)
	}
	_, err := s.DeleteDNSProfile(localCtx(), "cf")
	if err == nil || !strings.Contains(err.Error(), "records") {
		t.Fatalf("delete with records: %v", err)
	}

	// The next check drops the records, and then the profile can go.
	if _, err := s.CheckCluster(localCtx(), "eu-west"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteDNSProfile(localCtx(), "cf"); err != nil {
		t.Fatalf("delete after the records went: %v", err)
	}
}

func TestRemoveClusterDeletesDNSRecords(t *testing.T) {
	s, fake, _, zoneID := newDNSRecordsFixture(t, dnsMember("node-a", "mixed", "203.0.113.10"))
	if _, err := s.CheckCluster(localCtx(), "eu-west"); err != nil {
		t.Fatal(err)
	}
	if records := fake.Records(zoneID); len(records) != 2 {
		t.Fatalf("records = %+v", records)
	}
	if _, err := s.RemoveCluster(localCtx(), "eu-west", false); err != nil {
		t.Fatal(err)
	}
	if records := fake.Records(zoneID); len(records) != 0 {
		t.Errorf("records after removal = %+v", records)
	}
}

// A new cluster gets its records when it is registered, not at the first
// check: until then nothing can reach its domain.
func TestAddClusterCreatesDNSRecords(t *testing.T) {
	_, fake, _, zoneID, detail := newDNSRecordsFixtureBefore(t, nil, dnsMember("node-a", "mixed", "203.0.113.10"))
	got := recordSet(fake.Records(zoneID))
	if len(got) != 2 || got["eu.example.com 203.0.113.10"] != "A" || got["*.eu.example.com 203.0.113.10"] != "A" {
		t.Errorf("records after AddCluster = %v", got)
	}
	if check, ok := findCheck(detail.Checks, "dns records"); !ok || !check.OK {
		t.Errorf("dns records check = %+v (found %v)", check, ok)
	}
	if detail.Status != "ready" {
		t.Errorf("status = %q", detail.Status)
	}
}

// A new domain doesn't resolve yet, so registration reaches the cluster at
// admin_ip while keeping the domain as its admin URL.
func TestAddClusterAdminIP(t *testing.T) {
	clusterFake := newClusterFake(t)
	_, port, err := net.SplitHostPort(strings.TrimPrefix(clusterFake.URL(), "http://"))
	if err != nil {
		t.Fatal(err)
	}
	// .invalid never resolves (RFC 2606).
	unresolved := "http://eu.lewbox-test.invalid:" + port
	s := newClusterService(t)

	req := clusterRequest(clusterFake, "eu-west")
	req.AdminURL, req.EndpointURL = unresolved, unresolved
	_, err = s.AddCluster(localCtx(), req)
	wantCode(t, err, CodePrecondition)

	req.AdminIP = "127.0.0.1"
	detail, err := s.AddCluster(localCtx(), req)
	if err != nil {
		t.Fatalf("AddCluster with admin_ip: %v", err)
	}
	if detail.AdminURL != unresolved || detail.EndpointURL != unresolved {
		t.Errorf("urls = %q, %q; want the host name kept", detail.AdminURL, detail.EndpointURL)
	}
	if check, ok := findCheck(detail.Checks, "health"); !ok || !check.OK {
		t.Errorf("health check = %+v", check)
	}

	for _, tc := range []struct{ adminURL, adminIP string }{
		{unresolved, "eu.example.com"},
		{unresolved, "fe80::1%eth0"},
		{clusterFake.URL(), "127.0.0.1"}, // admin_url is already an IP
	} {
		req := clusterRequest(clusterFake, "eu-bad")
		req.AdminURL, req.AdminIP = tc.adminURL, tc.adminIP
		_, err := s.AddCluster(localCtx(), req)
		wantCode(t, err, CodeInvalid)
	}
}

// In domain mode AerolVM reports the domain as public_host and the machine's
// IP as data_plane_host (testdata/members_domain.json). The records point at
// that IP; before, a domain-mode cluster never got any.
func TestClusterDNSRecordsDomainMode(t *testing.T) {
	m := dnsMember("node-1", "mixed", "eu.example.com")
	m.DataPlaneHost = "203.0.113.10"
	private := dnsMember("node-2", "mixed", "eu.example.com")
	private.DataPlaneHost = "10.0.0.5"
	_, fake, _, zoneID, detail := newDNSRecordsFixtureBefore(t, nil, m, private)

	got := recordSet(fake.Records(zoneID))
	if len(got) != 2 || got["eu.example.com 203.0.113.10"] != "A" || got["*.eu.example.com 203.0.113.10"] != "A" {
		t.Errorf("records = %v, want both names on node-1's data_plane_host", got)
	}
	check, ok := findCheck(detail.Checks, "dns records")
	if !ok || !check.OK || !strings.Contains(check.Detail, "node-2: 10.0.0.5 is a private address") {
		t.Errorf("dns records check = %+v", check)
	}
}
