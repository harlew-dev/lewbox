package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/harlew-dev/lewbox/internal/db"
	"github.com/harlew-dev/lewbox/internal/dns/cloudflaretest"
	"github.com/harlew-dev/lewbox/internal/secrets"
)

const dnsTestToken = "cf-dns-profile-test-token"

// newDNSProfileFixture is a service with a fake Cloudflare API and the
// database handle, so a test can add a cluster row that uses a profile.
func newDNSProfileFixture(t *testing.T) (*Service, *sql.DB, *cloudflaretest.Fake) {
	t.Helper()
	conn, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "lewbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	keyring, err := secrets.LoadOrCreate(filepath.Join(t.TempDir(), "keyring.key"))
	if err != nil {
		t.Fatal(err)
	}
	fake := cloudflaretest.New(t)
	fake.Token = dnsTestToken
	s := New(db.New(conn), WithKeyring(keyring), WithCloudflareURL(fake.URL))
	return s, conn, fake
}

func createDNSProfile(t *testing.T, s *Service, req CreateDNSProfileRequest) DNSProfile {
	t.Helper()
	p, err := s.CreateDNSProfile(localCtx(), req)
	if err != nil {
		t.Fatalf("create DNS profile: %v", err)
	}
	return p
}

func TestCreateDNSProfile(t *testing.T) {
	s, _, fake := newDNSProfileFixture(t)
	zoneID := fake.AddZone("Example.com")

	p := createDNSProfile(t, s, CreateDNSProfileRequest{
		Name: "cf-eu", Zone: "Example.com.", APIToken: dnsTestToken, ACMEEmail: "ops@example.com",
	})
	if p.Name != "cf-eu" || p.Provider != "cloudflare" || p.Zone != "example.com" || p.ZoneID != zoneID {
		t.Errorf("profile = %+v", p)
	}
	if p.TokenLast4 != last4(dnsTestToken) || p.Version != 1 || p.ACMEEmail != "ops@example.com" {
		t.Errorf("profile = %+v", p)
	}
	if p.TestedAt == nil || p.TestError != "" || p.Clusters != 0 {
		t.Errorf("test result = %+v", p)
	}
	if records := fake.Records(zoneID); len(records) != 0 {
		t.Errorf("the test left records: %+v", records)
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), dnsTestToken) {
		t.Errorf("the profile JSON carries the token: %s", data)
	}
	if !strings.Contains(string(data), `"token_last4"`) {
		t.Errorf("the profile JSON has no token_last4: %s", data)
	}

	list, err := s.ListDNSProfiles(localCtx())
	if err != nil || len(list.Data) != 1 || list.Data[0].Name != "cf-eu" {
		t.Fatalf("list = %+v, %v", list.Data, err)
	}
	if got, err := s.GetDNSProfile(localCtx(), p.ID); err != nil || got.ID != p.ID {
		t.Errorf("get by ID = %+v, %v", got, err)
	}
}

func TestCreateDNSProfileFailures(t *testing.T) {
	s, _, fake := newDNSProfileFixture(t)
	zoneID := fake.AddZone("example.com")

	_, err := s.CreateDNSProfile(localCtx(), CreateDNSProfileRequest{
		Name: "cf-eu", Zone: "example.com", APIToken: "wrong-token",
	})
	se := wantCode(t, err, CodePrecondition)
	details, ok := se.Details.(map[string]string)
	if !ok || details["step"] != "zone" {
		t.Errorf("details = %v", se.Details)
	}
	if !strings.Contains(se.Message, "the token was refused") {
		t.Errorf("message = %q", se.Message)
	}

	_, err = s.CreateDNSProfile(localCtx(), CreateDNSProfileRequest{
		Name: "cf-eu", Zone: "missing.example", APIToken: dnsTestToken,
	})
	se = wantCode(t, err, CodePrecondition)
	details, _ = se.Details.(map[string]string)
	if details["step"] != "zone" || !strings.Contains(se.Message, "not found with this token") {
		t.Errorf("error = %v", se)
	}

	fake.FailNext("POST", "/zones/"+zoneID+"/dns_records", 500, 1)
	_, err = s.CreateDNSProfile(localCtx(), CreateDNSProfileRequest{
		Name: "cf-eu", Zone: "example.com", APIToken: dnsTestToken,
	})
	se = wantCode(t, err, CodePrecondition)
	details, _ = se.Details.(map[string]string)
	if details["step"] != "write" || !strings.Contains(se.Message, "can read the zone but not edit its records") {
		t.Errorf("error = %v", se)
	}

	if list, err := s.ListDNSProfiles(localCtx()); err != nil || len(list.Data) != 0 {
		t.Errorf("a failing creation saved a profile: %+v, %v", list.Data, err)
	}
}

func TestCreateDNSProfileValidation(t *testing.T) {
	s, _, fake := newDNSProfileFixture(t)
	fake.AddZone("example.com")

	cases := []struct {
		name string
		req  CreateDNSProfileRequest
	}{
		{"uuid name", CreateDNSProfileRequest{Name: "0192f0c4-a3b2-7c6d-8e9f-0a1b2c3d4e5f", Zone: "example.com", APIToken: dnsTestToken}},
		{"bad name", CreateDNSProfileRequest{Name: "CF_EU", Zone: "example.com", APIToken: dnsTestToken}},
		{"provider", CreateDNSProfileRequest{Name: "cf", Provider: "route53", Zone: "example.com", APIToken: dnsTestToken}},
		{"no dot", CreateDNSProfileRequest{Name: "cf", Zone: "localhost", APIToken: dnsTestToken}},
		{"no token", CreateDNSProfileRequest{Name: "cf", Zone: "example.com", APIToken: "  "}},
		{"bad acme email", CreateDNSProfileRequest{Name: "cf", Zone: "example.com", APIToken: dnsTestToken, ACMEEmail: "ops@example.com@example.com"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.CreateDNSProfile(localCtx(), tc.req); err == nil {
				t.Fatal("creation succeeded")
			} else {
				wantCode(t, err, CodeInvalid)
			}
		})
	}
}

func TestUpdateDNSProfile(t *testing.T) {
	s, _, fake := newDNSProfileFixture(t)
	zoneID := fake.AddZone("example.com")
	p := createDNSProfile(t, s, CreateDNSProfileRequest{Name: "cf-eu", Zone: "example.com", APIToken: dnsTestToken})

	name := "cf-west"
	updated, err := s.UpdateDNSProfile(localCtx(), "cf-eu", UpdateDNSProfileRequest{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "cf-west" || updated.Version != p.Version || updated.ZoneID != zoneID {
		t.Errorf("name-only update = %+v", updated)
	}

	fake.AddZone("other.example")
	zone := "other.example"
	updated, err = s.UpdateDNSProfile(localCtx(), "cf-west", UpdateDNSProfileRequest{Zone: &zone})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Zone != "other.example" || updated.Version != p.Version+1 || updated.ZoneID == zoneID {
		t.Errorf("zone update = %+v", updated)
	}

	token := "cf-new-token"
	if _, err := s.UpdateDNSProfile(localCtx(), "cf-west", UpdateDNSProfileRequest{APIToken: &token}); err == nil {
		t.Fatal("a token the fake does not accept was saved")
	} else {
		wantCode(t, err, CodePrecondition)
	}
	fake.Token = token
	updated, err = s.UpdateDNSProfile(localCtx(), "cf-west", UpdateDNSProfileRequest{APIToken: &token})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != p.Version+2 || updated.TokenLast4 != last4(token) {
		t.Errorf("token update = %+v", updated)
	}

	if _, err := s.UpdateDNSProfile(localCtx(), "cf-west", UpdateDNSProfileRequest{}); err == nil {
		t.Fatal("an empty update succeeded")
	} else {
		wantCode(t, err, CodeInvalid)
	}
}

func TestTestDNSProfile(t *testing.T) {
	s, _, fake := newDNSProfileFixture(t)
	fake.AddZone("example.com")
	p := createDNSProfile(t, s, CreateDNSProfileRequest{Name: "cf-eu", Zone: "example.com", APIToken: dnsTestToken})

	fake.FailNext("GET", "/zones", 500, 1)
	tested, err := s.TestDNSProfile(localCtx(), "cf-eu")
	if err != nil {
		t.Fatal(err)
	}
	if tested.TestError == "" || tested.TestedAt == nil {
		t.Errorf("failed test = %+v", tested)
	}

	ok, err := s.TestDNSProfile(localCtx(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ok.TestError != "" {
		t.Errorf("test after the failure = %+v", ok)
	}
}

func TestDeleteDNSProfile(t *testing.T) {
	s, conn, fake := newDNSProfileFixture(t)
	fake.AddZone("example.com")
	p := createDNSProfile(t, s, CreateDNSProfileRequest{Name: "cf-eu", Zone: "example.com", APIToken: dnsTestToken})

	if _, err := conn.ExecContext(context.Background(), `
		INSERT INTO clusters (id, slug, name, admin_url, endpoint_url, runtime, admin_token_enc,
		                      admin_token_id, admin_token_last4, status, status_detail, version,
		                      checks, checked_at, created_at, dns_profile_id)
		VALUES ('cluster-1', 'eu-west', 'eu-west', 'http://127.0.0.1:1', 'http://127.0.0.1:1', 'docker',
		        x'00', 'token-id', 'abcd', 'ready', '', '', '[]', NULL, CURRENT_TIMESTAMP, ?)`,
		p.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetDNSProfile(localCtx(), "cf-eu")
	if err != nil || got.Clusters != 1 {
		t.Fatalf("clusters = %d, %v", got.Clusters, err)
	}
	if _, err := s.DeleteDNSProfile(localCtx(), "cf-eu"); err == nil {
		t.Fatal("deleted a profile a cluster uses")
	} else {
		se := wantCode(t, err, CodeConflict)
		if !strings.Contains(se.Message, "1 cluster(s)") {
			t.Errorf("message = %q", se.Message)
		}
	}

	if _, err := conn.ExecContext(context.Background(), `UPDATE clusters SET dns_profile_id = NULL`); err != nil {
		t.Fatal(err)
	}
	deleted, err := s.DeleteDNSProfile(localCtx(), "cf-eu")
	if err != nil || deleted.ID != p.ID {
		t.Fatalf("delete = %+v, %v", deleted, err)
	}
	if _, err := s.GetDNSProfile(localCtx(), "cf-eu"); err == nil {
		t.Fatal("the profile is still there")
	} else {
		wantCode(t, err, CodeNotFound)
	}
}

func TestDNSProfileCredentials(t *testing.T) {
	s, _, fake := newDNSProfileFixture(t)
	fake.AddZone("example.com")
	p := createDNSProfile(t, s, CreateDNSProfileRequest{Name: "cf-eu", Zone: "example.com", APIToken: dnsTestToken})

	creds, err := s.DNSProfileCredentials(localCtx(), "cf-eu")
	if err != nil {
		t.Fatal(err)
	}
	if creds.APIToken != dnsTestToken || creds.Zone != "example.com" || creds.ZoneID != p.ZoneID ||
		creds.Name != "cf-eu" || creds.Provider != "cloudflare" || creds.Version != p.Version {
		t.Errorf("credentials = %+v", creds)
	}
}

func TestDNSProfilePermissions(t *testing.T) {
	s, _, fake := newDNSProfileFixture(t)
	fake.AddZone("example.com")
	if _, err := s.ListDNSProfiles(context.Background()); err == nil {
		t.Fatal("an anonymous caller listed DNS profiles")
	} else {
		wantCode(t, err, CodeUnauthorized)
	}

	createUser(t, s, "user@example.com", false)
	user := userCtx(t, s, "user@example.com")
	if _, err := s.CreateDNSProfile(user, CreateDNSProfileRequest{
		Name: "cf-eu", Zone: "example.com", APIToken: dnsTestToken,
	}); err == nil {
		t.Fatal("a non-admin created a DNS profile")
	} else {
		wantCode(t, err, CodeForbidden)
	}
}
