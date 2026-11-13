package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/harlew-dev/lewbox/internal/db"
	"github.com/harlew-dev/lewbox/internal/objstore/objstoretest"
	"github.com/harlew-dev/lewbox/internal/secrets"
)

const storageTestSecret = "objstore-profile-test-secret"

// newStorageProfileFixture is a service with a fake S3 server and the
// database handle, so a test can add a cluster row that uses a profile.
func newStorageProfileFixture(t *testing.T) (*Service, *sql.DB, *objstoretest.Fake) {
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
	fake := objstoretest.New(t)
	fake.AddBucket("assets")
	s := New(db.New(conn), WithKeyring(keyring))
	return s, conn, fake
}

func newStorageProfileRequest(fake *objstoretest.Fake, name string) CreateStorageProfileRequest {
	return CreateStorageProfileRequest{
		Name:            name,
		Endpoint:        fake.URL,
		Region:          "us-east-1",
		Bucket:          "assets",
		AccessKeyID:     fake.AccessKeyID,
		SecretAccessKey: storageTestSecret,
	}
}

func createStorageProfile(t *testing.T, s *Service, req CreateStorageProfileRequest) StorageProfile {
	t.Helper()
	p, err := s.CreateStorageProfile(localCtx(), req)
	if err != nil {
		t.Fatalf("create storage profile: %v", err)
	}
	return p
}

func TestCreateStorageProfile(t *testing.T) {
	s, _, fake := newStorageProfileFixture(t)
	req := newStorageProfileRequest(fake, "backups")
	req.Prefix = "/clusters/"

	p := createStorageProfile(t, s, req)
	if p.Name != "backups" || p.Provider != "s3" || p.Endpoint != fake.URL || p.Region != "us-east-1" {
		t.Errorf("profile = %+v", p)
	}
	if p.Bucket != "assets" || p.Prefix != "clusters" || !p.PathStyle || p.AccessKeyID != fake.AccessKeyID {
		t.Errorf("profile = %+v", p)
	}
	if p.SecretLast4 != last4(storageTestSecret) || p.Version != 1 {
		t.Errorf("profile = %+v", p)
	}
	if p.TestedAt == nil || p.TestError != "" || p.Clusters != 0 {
		t.Errorf("test result = %+v", p)
	}
	if objects := fake.Objects("assets"); len(objects) != 0 {
		t.Errorf("the test left objects: %+v", objects)
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), storageTestSecret) {
		t.Errorf("the profile JSON carries the secret: %s", data)
	}
	if !strings.Contains(string(data), `"secret_last4"`) {
		t.Errorf("the profile JSON has no secret_last4: %s", data)
	}

	list, err := s.ListStorageProfiles(localCtx())
	if err != nil || len(list.Data) != 1 || list.Data[0].Name != "backups" {
		t.Fatalf("list = %+v, %v", list.Data, err)
	}
	if got, err := s.GetStorageProfile(localCtx(), p.ID); err != nil || got.ID != p.ID {
		t.Errorf("get by ID = %+v, %v", got, err)
	}
}

func TestCreateStorageProfileDefaults(t *testing.T) {
	s, _, fake := newStorageProfileFixture(t)
	req := newStorageProfileRequest(fake, "backups")
	req.Region = ""
	req.PathStyle = nil

	p := createStorageProfile(t, s, req)
	if p.Region != "us-east-1" {
		t.Errorf("region = %q", p.Region)
	}
	if !p.PathStyle {
		t.Error("path_style did not default to true with an endpoint")
	}
}

func TestCreateStorageProfileFailures(t *testing.T) {
	s, _, fake := newStorageProfileFixture(t)

	wrong := newStorageProfileRequest(fake, "backups")
	wrong.SecretAccessKey = "wrong-secret"
	wrong.AccessKeyID = "wrong-key"
	_, err := s.CreateStorageProfile(localCtx(), wrong)
	se := wantCode(t, err, CodePrecondition)
	details, ok := se.Details.(map[string]string)
	if !ok || details["step"] != "write" {
		t.Errorf("details = %v", se.Details)
	}
	if !strings.Contains(se.Message, "the key was refused") {
		t.Errorf("message = %q", se.Message)
	}

	missing := newStorageProfileRequest(fake, "backups")
	missing.Bucket = "missing"
	_, err = s.CreateStorageProfile(localCtx(), missing)
	se = wantCode(t, err, CodePrecondition)
	details, _ = se.Details.(map[string]string)
	if details["step"] != "write" || !strings.Contains(se.Message, "bucket missing not found") {
		t.Errorf("error = %v", se)
	}

	fake.FailNext("GET", "lewbox-test/", 500, "InternalError", 1)
	_, err = s.CreateStorageProfile(localCtx(), newStorageProfileRequest(fake, "backups"))
	se = wantCode(t, err, CodePrecondition)
	details, _ = se.Details.(map[string]string)
	if details["step"] != "read" {
		t.Errorf("details = %v", details)
	}

	if list, err := s.ListStorageProfiles(localCtx()); err != nil || len(list.Data) != 0 {
		t.Errorf("a failing creation saved a profile: %+v, %v", list.Data, err)
	}
}

func TestCreateStorageProfileValidation(t *testing.T) {
	s, _, fake := newStorageProfileFixture(t)

	base := func() CreateStorageProfileRequest {
		return newStorageProfileRequest(fake, "backups")
	}
	cases := []struct {
		name string
		edit func(*CreateStorageProfileRequest)
	}{
		{"uuid name", func(r *CreateStorageProfileRequest) { r.Name = "0192f0c4-a3b2-7c6d-8e9f-0a1b2c3d4e5f" }},
		{"bad name", func(r *CreateStorageProfileRequest) { r.Name = "Backups_EU" }},
		{"provider", func(r *CreateStorageProfileRequest) { r.Provider = "gcs" }},
		{"endpoint scheme", func(r *CreateStorageProfileRequest) { r.Endpoint = "ftp://example.com" }},
		{"endpoint path", func(r *CreateStorageProfileRequest) { r.Endpoint = "https://example.com/storage" }},
		{"endpoint query", func(r *CreateStorageProfileRequest) { r.Endpoint = "https://example.com?x=1" }},
		{"endpoint user", func(r *CreateStorageProfileRequest) { r.Endpoint = "https://user@example.com" }},
		{"no region on AWS", func(r *CreateStorageProfileRequest) { r.Endpoint, r.Region = "", "" }},
		{"short bucket", func(r *CreateStorageProfileRequest) { r.Bucket = "ab" }},
		{"bucket upper", func(r *CreateStorageProfileRequest) { r.Bucket = "ass_ets" }},
		{"bucket ip", func(r *CreateStorageProfileRequest) { r.Bucket = "192.168.0.1" }},
		{"bucket hyphen", func(r *CreateStorageProfileRequest) { r.Bucket = "-assets" }},
		{"bucket dot end", func(r *CreateStorageProfileRequest) { r.Bucket = "assets." }},
		{"prefix empty segment", func(r *CreateStorageProfileRequest) { r.Prefix = "a//b" }},
		{"prefix dot", func(r *CreateStorageProfileRequest) { r.Prefix = "a/./b" }},
		{"prefix dot dot", func(r *CreateStorageProfileRequest) { r.Prefix = "a/../b" }},
		{"prefix space", func(r *CreateStorageProfileRequest) { r.Prefix = "a b" }},
		{"no access key", func(r *CreateStorageProfileRequest) { r.AccessKeyID = "  " }},
		{"no secret", func(r *CreateStorageProfileRequest) { r.SecretAccessKey = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := base()
			tc.edit(&req)
			if _, err := s.CreateStorageProfile(localCtx(), req); err == nil {
				t.Fatal("creation succeeded")
			} else {
				wantCode(t, err, CodeInvalid)
			}
		})
	}
}

func TestUpdateStorageProfile(t *testing.T) {
	s, _, fake := newStorageProfileFixture(t)
	fake.AddBucket("other")
	p := createStorageProfile(t, s, newStorageProfileRequest(fake, "backups"))

	name := "backups-2"
	updated, err := s.UpdateStorageProfile(localCtx(), "backups", UpdateStorageProfileRequest{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "backups-2" || updated.Version != p.Version || updated.Bucket != "assets" {
		t.Errorf("name-only update = %+v", updated)
	}

	bucket := "other"
	updated, err = s.UpdateStorageProfile(localCtx(), "backups-2", UpdateStorageProfileRequest{Bucket: &bucket})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Bucket != "other" || updated.Version != p.Version+1 || updated.TestedAt == nil {
		t.Errorf("bucket update = %+v", updated)
	}

	missing := "missing"
	if _, err := s.UpdateStorageProfile(localCtx(), "backups-2", UpdateStorageProfileRequest{Bucket: &missing}); err == nil {
		t.Fatal("a failing profile was saved")
	} else {
		se := wantCode(t, err, CodePrecondition)
		details, _ := se.Details.(map[string]string)
		if details["step"] != "write" || !strings.Contains(se.Message, "bucket missing not found") {
			t.Errorf("error = %v", se)
		}
	}

	secret := "objstore-another-secret"
	updated, err = s.UpdateStorageProfile(localCtx(), "backups-2", UpdateStorageProfileRequest{SecretAccessKey: &secret})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != p.Version+2 || updated.SecretLast4 != last4(secret) {
		t.Errorf("secret update = %+v", updated)
	}

	prefix := "clusters/"
	updated, err = s.UpdateStorageProfile(localCtx(), "backups-2", UpdateStorageProfileRequest{Prefix: &prefix})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Prefix != "clusters" || updated.Version != p.Version+3 {
		t.Errorf("prefix update = %+v", updated)
	}

	if _, err := s.UpdateStorageProfile(localCtx(), "backups-2", UpdateStorageProfileRequest{}); err == nil {
		t.Fatal("an empty update succeeded")
	} else {
		wantCode(t, err, CodeInvalid)
	}
}

func TestTestStorageProfile(t *testing.T) {
	s, _, fake := newStorageProfileFixture(t)
	p := createStorageProfile(t, s, newStorageProfileRequest(fake, "backups"))

	fake.FailNext("GET", "lewbox-test/", 500, "InternalError", 1)
	tested, err := s.TestStorageProfile(localCtx(), "backups")
	if err != nil {
		t.Fatal(err)
	}
	if tested.TestError == "" || tested.TestedAt == nil || tested.Version != p.Version {
		t.Errorf("failed test = %+v", tested)
	}

	ok, err := s.TestStorageProfile(localCtx(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ok.TestError != "" {
		t.Errorf("test after the failure = %+v", ok)
	}
}

func TestDeleteStorageProfile(t *testing.T) {
	s, conn, fake := newStorageProfileFixture(t)
	p := createStorageProfile(t, s, newStorageProfileRequest(fake, "backups"))

	if _, err := conn.ExecContext(context.Background(), `
		INSERT INTO clusters (id, slug, name, admin_url, endpoint_url, runtime, admin_token_enc,
		                      admin_token_id, admin_token_last4, status, status_detail, version,
		                      checks, checked_at, created_at, storage_profile_id)
		VALUES ('cluster-1', 'eu-west', 'eu-west', 'http://127.0.0.1:1', 'http://127.0.0.1:1', 'docker',
		        x'00', 'token-id', 'abcd', 'ready', '', '', '[]', NULL, CURRENT_TIMESTAMP, ?)`,
		p.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetStorageProfile(localCtx(), "backups")
	if err != nil || got.Clusters != 1 {
		t.Fatalf("clusters = %d, %v", got.Clusters, err)
	}
	if _, err := s.DeleteStorageProfile(localCtx(), "backups"); err == nil {
		t.Fatal("deleted a profile a cluster uses")
	} else {
		se := wantCode(t, err, CodeConflict)
		if !strings.Contains(se.Message, "1 cluster(s)") {
			t.Errorf("message = %q", se.Message)
		}
	}

	if _, err := conn.ExecContext(context.Background(), `UPDATE clusters SET storage_profile_id = NULL`); err != nil {
		t.Fatal(err)
	}
	deleted, err := s.DeleteStorageProfile(localCtx(), "backups")
	if err != nil || deleted.ID != p.ID {
		t.Fatalf("delete = %+v, %v", deleted, err)
	}
	if _, err := s.GetStorageProfile(localCtx(), "backups"); err == nil {
		t.Fatal("the profile is still there")
	} else {
		wantCode(t, err, CodeNotFound)
	}
}

func TestStorageProfileCredentials(t *testing.T) {
	s, _, fake := newStorageProfileFixture(t)
	p := createStorageProfile(t, s, newStorageProfileRequest(fake, "backups"))

	creds, err := s.StorageProfileCredentials(localCtx(), "backups")
	if err != nil {
		t.Fatal(err)
	}
	if creds.SecretAccessKey != storageTestSecret || creds.AccessKeyID != fake.AccessKeyID ||
		creds.Bucket != "assets" || creds.Endpoint != fake.URL || creds.Region != "us-east-1" ||
		!creds.PathStyle || creds.Name != "backups" || creds.Provider != "s3" || creds.Version != p.Version {
		t.Errorf("credentials = %+v", creds)
	}
}

func TestStorageProfilePermissions(t *testing.T) {
	s, _, fake := newStorageProfileFixture(t)
	if _, err := s.ListStorageProfiles(context.Background()); err == nil {
		t.Fatal("an anonymous caller listed storage profiles")
	} else {
		wantCode(t, err, CodeUnauthorized)
	}

	createUser(t, s, "user@example.com", false)
	user := userCtx(t, s, "user@example.com")
	if _, err := s.CreateStorageProfile(user, newStorageProfileRequest(fake, "backups")); err == nil {
		t.Fatal("a non-admin created a storage profile")
	} else {
		wantCode(t, err, CodeForbidden)
	}
}
