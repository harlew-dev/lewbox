package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"uuid"

	"github.com/harlew-dev/lewbox/internal/db"
	"github.com/harlew-dev/lewbox/internal/objstore"
)

// storageProfileTestTimeout bounds one profile test: put, get, list and
// delete of a small test object.
const storageProfileTestTimeout = 30 * time.Second

// StorageProfile is a saved S3-compatible bucket and key (DECISIONS D35).
// The secret access key is never part of it.
type StorageProfile struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Provider    string     `json:"provider"`
	Endpoint    string     `json:"endpoint"`
	Region      string     `json:"region"`
	Bucket      string     `json:"bucket"`
	Prefix      string     `json:"prefix"`
	PathStyle   bool       `json:"path_style"`
	AccessKeyID string     `json:"access_key_id"`
	SecretLast4 string     `json:"secret_last4"`
	Version     int64      `json:"version"`
	Clusters    int        `json:"clusters"`
	TestedAt    *time.Time `json:"tested_at"`
	TestError   string     `json:"test_error"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// StorageProfileList is returned by ListStorageProfiles. Data is never null.
type StorageProfileList struct {
	Data []StorageProfile `json:"data"`
}

// CreateStorageProfileRequest is the input to CreateStorageProfile.
type CreateStorageProfileRequest struct {
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	Prefix          string `json:"prefix"`
	PathStyle       *bool  `json:"path_style"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

// UpdateStorageProfileRequest changes the fields that are set.
type UpdateStorageProfileRequest struct {
	Name            *string `json:"name,omitempty"`
	Endpoint        *string `json:"endpoint,omitempty"`
	Region          *string `json:"region,omitempty"`
	Bucket          *string `json:"bucket,omitempty"`
	Prefix          *string `json:"prefix,omitempty"`
	PathStyle       *bool   `json:"path_style,omitempty"`
	AccessKeyID     *string `json:"access_key_id,omitempty"`
	SecretAccessKey *string `json:"secret_access_key,omitempty"`
}

// StorageProfileCredentials is what a node needs to reach the bucket.
// SecretAccessKey is the only place the secret is returned.
type StorageProfileCredentials struct {
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	Prefix          string `json:"prefix"`
	PathStyle       bool   `json:"path_style"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	Version         int64  `json:"version"`
}

// storageProfileTest is the outcome of testing one profile: the step that
// failed and why, or an empty step when every step passed.
type storageProfileTest struct {
	step    string
	message string
}

// ListStorageProfiles returns every profile, ordered by name. Instance
// admins only.
func (s *Service) ListStorageProfiles(ctx context.Context) (StorageProfileList, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return StorageProfileList{}, err
	}
	rows, err := s.q.ListStorageProfiles(ctx)
	if err != nil {
		return StorageProfileList{}, fmt.Errorf("list storage profiles: %w", err)
	}
	counts, err := s.storageProfileClusterCounts(ctx)
	if err != nil {
		return StorageProfileList{}, err
	}
	out := StorageProfileList{Data: make([]StorageProfile, 0, len(rows))}
	for _, row := range rows {
		out.Data = append(out.Data, toStorageProfile(row, counts[row.ID]))
	}
	return out, nil
}

// GetStorageProfile returns a profile by ID or name.
func (s *Service) GetStorageProfile(ctx context.Context, ref string) (StorageProfile, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return StorageProfile{}, err
	}
	row, err := s.findStorageProfile(ctx, ref)
	if err != nil {
		return StorageProfile{}, err
	}
	counts, err := s.storageProfileClusterCounts(ctx)
	if err != nil {
		return StorageProfile{}, err
	}
	return toStorageProfile(row, counts[row.ID]), nil
}

// CreateStorageProfile saves a profile after testing it. A profile whose test
// fails is not saved. Instance admins only.
func (s *Service) CreateStorageProfile(ctx context.Context, req CreateStorageProfileRequest) (StorageProfile, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return StorageProfile{}, err
	}
	name, err := planSlug(req.Name)
	if err != nil {
		return StorageProfile{}, err
	}
	provider, err := storageProfileProvider(req.Provider)
	if err != nil {
		return StorageProfile{}, err
	}
	endpoint, err := storageProfileEndpoint(req.Endpoint)
	if err != nil {
		return StorageProfile{}, err
	}
	region, err := storageProfileRegion(req.Region, endpoint)
	if err != nil {
		return StorageProfile{}, err
	}
	bucket, err := storageProfileBucket(req.Bucket)
	if err != nil {
		return StorageProfile{}, err
	}
	prefix, err := storageProfilePrefix(req.Prefix)
	if err != nil {
		return StorageProfile{}, err
	}
	accessKey, err := storageProfileRequired(req.AccessKeyID, "access_key_id")
	if err != nil {
		return StorageProfile{}, err
	}
	secret, err := storageProfileRequired(req.SecretAccessKey, "secret_access_key")
	if err != nil {
		return StorageProfile{}, err
	}

	cfg := objstore.Config{
		Endpoint:        endpoint,
		Region:          region,
		Bucket:          bucket,
		AccessKeyID:     accessKey,
		SecretAccessKey: secret,
		PathStyle:       storageProfilePathStyle(req.PathStyle, endpoint),
	}
	if result := s.testStorageProfile(ctx, cfg, prefix); result.step != "" {
		return StorageProfile{}, precondition(map[string]string{"step": result.step}, "storage profile test failed: %s", result.message)
	}

	sealed, err := s.keyring.Seal([]byte(secret))
	if err != nil {
		return StorageProfile{}, err
	}
	now := s.now()
	row, err := s.q.CreateStorageProfile(ctx, db.CreateStorageProfileParams{
		ID:           uuid.NewV7().String(),
		Name:         name,
		Provider:     provider,
		Endpoint:     endpoint,
		Region:       region,
		Bucket:       bucket,
		Prefix:       prefix,
		PathStyle:    boolToInt(cfg.PathStyle),
		AccessKeyID:  accessKey,
		SecretKeyEnc: sealed,
		SecretLast4:  last4(secret),
		Version:      1,
		TestedAt:     sql.NullTime{Time: now, Valid: true},
		TestError:    "",
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if db.IsUniqueViolation(err) {
		return StorageProfile{}, conflict("a storage profile named %q already exists", name)
	}
	if err != nil {
		return StorageProfile{}, fmt.Errorf("create storage profile: %w", err)
	}
	return toStorageProfile(row, 0), nil
}

// UpdateStorageProfile changes a profile by ID or name. A changed endpoint,
// region, bucket, prefix, path style, key or secret is tested before it is
// saved, and bumps the version. Instance admins only.
func (s *Service) UpdateStorageProfile(ctx context.Context, ref string, req UpdateStorageProfileRequest) (StorageProfile, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return StorageProfile{}, err
	}
	if req.Name == nil && req.Endpoint == nil && req.Region == nil && req.Bucket == nil &&
		req.Prefix == nil && req.PathStyle == nil && req.AccessKeyID == nil && req.SecretAccessKey == nil {
		return StorageProfile{}, invalid("nothing to change: set name, endpoint, region, bucket, prefix, path_style, access_key_id or secret_access_key")
	}
	row, err := s.findStorageProfile(ctx, ref)
	if err != nil {
		return StorageProfile{}, err
	}

	if req.Name != nil {
		row.Name, err = planSlug(*req.Name)
		if err != nil {
			return StorageProfile{}, err
		}
	}

	endpoint := row.Endpoint
	if req.Endpoint != nil {
		endpoint, err = storageProfileEndpoint(*req.Endpoint)
		if err != nil {
			return StorageProfile{}, err
		}
	}
	region := row.Region
	if req.Region != nil {
		region = *req.Region
	}
	region, err = storageProfileRegion(region, endpoint)
	if err != nil {
		return StorageProfile{}, err
	}
	bucket := row.Bucket
	if req.Bucket != nil {
		bucket, err = storageProfileBucket(*req.Bucket)
		if err != nil {
			return StorageProfile{}, err
		}
	}
	prefix := row.Prefix
	if req.Prefix != nil {
		prefix, err = storageProfilePrefix(*req.Prefix)
		if err != nil {
			return StorageProfile{}, err
		}
	}
	accessKey := row.AccessKeyID
	if req.AccessKeyID != nil {
		accessKey, err = storageProfileRequired(*req.AccessKeyID, "access_key_id")
		if err != nil {
			return StorageProfile{}, err
		}
	}
	pathStyle := row.PathStyle != 0
	switch {
	case req.PathStyle != nil:
		pathStyle = *req.PathStyle
	case req.Endpoint != nil:
		pathStyle = endpoint != ""
	}

	changed := endpoint != row.Endpoint || region != row.Region || bucket != row.Bucket ||
		prefix != row.Prefix || boolToInt(pathStyle) != row.PathStyle || accessKey != row.AccessKeyID
	secret, err := s.keyring.Open(row.SecretKeyEnc)
	if err != nil {
		return StorageProfile{}, err
	}
	if req.SecretAccessKey != nil {
		value, err := storageProfileRequired(*req.SecretAccessKey, "secret_access_key")
		if err != nil {
			return StorageProfile{}, err
		}
		secret = []byte(value)
		changed = true
	}

	now := s.now()
	if changed {
		cfg := objstore.Config{
			Endpoint:        endpoint,
			Region:          region,
			Bucket:          bucket,
			AccessKeyID:     accessKey,
			SecretAccessKey: string(secret),
			PathStyle:       pathStyle,
		}
		if result := s.testStorageProfile(ctx, cfg, prefix); result.step != "" {
			return StorageProfile{}, precondition(map[string]string{"step": result.step}, "storage profile test failed: %s", result.message)
		}
		if req.SecretAccessKey != nil {
			sealed, err := s.keyring.Seal(secret)
			if err != nil {
				return StorageProfile{}, err
			}
			row.SecretKeyEnc = sealed
			row.SecretLast4 = last4(string(secret))
		}
		row.TestedAt = sql.NullTime{Time: now, Valid: true}
		row.TestError = ""
		row.Version++
	}
	row.Endpoint = endpoint
	row.Region = region
	row.Bucket = bucket
	row.Prefix = prefix
	row.PathStyle = boolToInt(pathStyle)
	row.AccessKeyID = accessKey
	row.UpdatedAt = now

	updated, err := s.q.UpdateStorageProfile(ctx, db.UpdateStorageProfileParams{
		Name:         row.Name,
		Endpoint:     row.Endpoint,
		Region:       row.Region,
		Bucket:       row.Bucket,
		Prefix:       row.Prefix,
		PathStyle:    row.PathStyle,
		AccessKeyID:  row.AccessKeyID,
		SecretKeyEnc: row.SecretKeyEnc,
		SecretLast4:  row.SecretLast4,
		Version:      row.Version,
		TestedAt:     row.TestedAt,
		TestError:    row.TestError,
		UpdatedAt:    row.UpdatedAt,
		ID:           row.ID,
	})
	if db.IsUniqueViolation(err) {
		return StorageProfile{}, conflict("a storage profile named %q already exists", row.Name)
	}
	if err != nil {
		return StorageProfile{}, fmt.Errorf("update storage profile: %w", err)
	}
	counts, err := s.storageProfileClusterCounts(ctx)
	if err != nil {
		return StorageProfile{}, err
	}
	return toStorageProfile(updated, counts[updated.ID]), nil
}

// DeleteStorageProfile removes a profile no cluster uses. Instance admins
// only. The guard is in the DELETE, so a cluster that starts using the
// profile between the lookup and the delete keeps it.
func (s *Service) DeleteStorageProfile(ctx context.Context, ref string) (StorageProfile, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return StorageProfile{}, err
	}
	row, err := s.findStorageProfile(ctx, ref)
	if err != nil {
		return StorageProfile{}, err
	}
	n, err := s.q.DeleteStorageProfile(ctx, row.ID)
	if err != nil {
		return StorageProfile{}, fmt.Errorf("delete storage profile: %w", err)
	}
	if n == 0 {
		counts, err := s.storageProfileClusterCounts(ctx)
		if err != nil {
			return StorageProfile{}, err
		}
		return StorageProfile{}, conflict("storage profile %q is used by %d cluster(s); stop using it in those clusters first", row.Name, counts[row.ID])
	}
	return toStorageProfile(row, 0), nil
}

// TestStorageProfile tests a profile's bucket and key now and records the
// result. A failing test is not an error: it shows in test_error. Instance
// admins only.
func (s *Service) TestStorageProfile(ctx context.Context, ref string) (StorageProfile, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return StorageProfile{}, err
	}
	row, err := s.findStorageProfile(ctx, ref)
	if err != nil {
		return StorageProfile{}, err
	}
	secret, err := s.keyring.Open(row.SecretKeyEnc)
	if err != nil {
		return StorageProfile{}, err
	}
	result := s.testStorageProfile(ctx, storageProfileConfig(row, string(secret)), row.Prefix)
	row.TestedAt = sql.NullTime{Time: s.now(), Valid: true}
	if result.step != "" {
		row.TestError = result.message
	} else {
		row.TestError = ""
	}
	if err := s.q.RecordStorageProfileTest(ctx, db.RecordStorageProfileTestParams{
		TestedAt:  row.TestedAt,
		TestError: row.TestError,
		ID:        row.ID,
	}); err != nil {
		return StorageProfile{}, fmt.Errorf("record storage profile test: %w", err)
	}
	counts, err := s.storageProfileClusterCounts(ctx)
	if err != nil {
		return StorageProfile{}, err
	}
	return toStorageProfile(row, counts[row.ID]), nil
}

// StorageProfileCredentials returns the profile's secret access key, which
// nodes need to reach the bucket. It is the only method that returns the
// secret. Instance admins only.
func (s *Service) StorageProfileCredentials(ctx context.Context, ref string) (StorageProfileCredentials, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return StorageProfileCredentials{}, err
	}
	row, err := s.findStorageProfile(ctx, ref)
	if err != nil {
		return StorageProfileCredentials{}, err
	}
	secret, err := s.keyring.Open(row.SecretKeyEnc)
	if err != nil {
		return StorageProfileCredentials{}, err
	}
	return StorageProfileCredentials{
		Name:            row.Name,
		Provider:        row.Provider,
		Endpoint:        row.Endpoint,
		Region:          row.Region,
		Bucket:          row.Bucket,
		Prefix:          row.Prefix,
		PathStyle:       row.PathStyle != 0,
		AccessKeyID:     row.AccessKeyID,
		SecretAccessKey: string(secret),
		Version:         row.Version,
	}, nil
}

// testStorageProfile puts a small object, reads it back, lists it and deletes
// it, and reports the step that failed.
func (s *Service) testStorageProfile(ctx context.Context, cfg objstore.Config, prefix string) storageProfileTest {
	ctx, cancel := context.WithTimeout(ctx, storageProfileTestTimeout)
	defer cancel()

	client, err := objstore.New(cfg)
	if err != nil {
		return storageProfileTest{step: "write", message: err.Error()}
	}
	key, err := storageTestKey(prefix)
	if err != nil {
		return storageProfileTest{step: "write", message: err.Error()}
	}
	content := []byte("lewbox test object " + key)

	if err := client.Put(ctx, key, content); err != nil {
		return storageProfileTest{step: "write", message: storageTestMessage(cfg, err)}
	}
	got, err := client.Get(ctx, key)
	if err != nil {
		return storageProfileTest{step: "read", message: storageTestMessage(cfg, err)}
	}
	if !bytes.Equal(got, content) {
		return storageProfileTest{step: "read", message: "the object read back differs from the one written"}
	}

	objs, _, err := client.List(ctx, storageTestPrefix(prefix), "", 0)
	if err != nil {
		return storageProfileTest{step: "list", message: storageTestMessage(cfg, err)}
	}
	found := false
	for _, obj := range objs {
		if obj.Key == key {
			found = true
			break
		}
	}
	if !found {
		return storageProfileTest{step: "list", message: fmt.Sprintf("the test object %s was not listed under its prefix", key)}
	}

	if err := client.Delete(ctx, key); err != nil {
		return storageProfileTest{step: "delete", message: storageTestMessage(cfg, err)}
	}
	return storageProfileTest{}
}

// storageTestPrefix is the part of the bucket a test lists.
func storageTestPrefix(prefix string) string {
	if prefix == "" {
		return "lewbox-test/"
	}
	return prefix + "/lewbox-test/"
}

// storageTestKey is the object a test uses, never with a leading slash.
func storageTestKey(prefix string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("make a test object name: %w", err)
	}
	return storageTestPrefix(prefix) + hex.EncodeToString(b[:]), nil
}

// storageTestMessage reports why a step failed, in terms an operator can act
// on. The secret is never part of it.
func storageTestMessage(cfg objstore.Config, err error) string {
	var apiErr *objstore.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.Code
		if code == "" {
			code = "error"
		}
		message := apiErr.Message
		if message == "" {
			message = http.StatusText(apiErr.Status)
		}
		switch {
		case apiErr.Status == http.StatusForbidden:
			return fmt.Sprintf("the key was refused: %s: %s", code, message)
		case apiErr.Code == "NoSuchBucket":
			return fmt.Sprintf("bucket %s not found", cfg.Bucket)
		default:
			return fmt.Sprintf("%s: %s", code, message)
		}
	}
	if errors.Is(err, objstore.ErrNotFound) {
		return "the bucket returned NoSuchKey for the test object"
	}
	return fmt.Sprintf("could not reach %s: %v", storageTarget(cfg), err)
}

// storageTarget names the store in a network error: the endpoint, or AWS.
func storageTarget(cfg objstore.Config) string {
	if cfg.Endpoint != "" {
		return cfg.Endpoint
	}
	return "AWS"
}

// storageProfileConfig builds the store client config for a saved row.
func storageProfileConfig(row db.StorageProfile, secret string) objstore.Config {
	return objstore.Config{
		Endpoint:        row.Endpoint,
		Region:          row.Region,
		Bucket:          row.Bucket,
		AccessKeyID:     row.AccessKeyID,
		SecretAccessKey: secret,
		PathStyle:       row.PathStyle != 0,
	}
}

// findStorageProfile looks a profile up by ID or name.
func (s *Service) findStorageProfile(ctx context.Context, ref string) (db.StorageProfile, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return db.StorageProfile{}, invalid("a storage profile ID or name is required")
	}
	var (
		row db.StorageProfile
		err error
	)
	if id, perr := uuid.Parse(ref); perr == nil {
		row, err = s.q.GetStorageProfileByID(ctx, id.String())
	} else {
		row, err = s.q.GetStorageProfileByName(ctx, strings.ToLower(ref))
	}
	if errors.Is(err, sql.ErrNoRows) {
		return db.StorageProfile{}, notFound("storage profile %q not found", ref)
	}
	if err != nil {
		return db.StorageProfile{}, fmt.Errorf("get storage profile: %w", err)
	}
	return row, nil
}

// storageProfileClusterCounts maps profile IDs to how many clusters use them.
func (s *Service) storageProfileClusterCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.q.CountStorageProfileClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("count clusters by storage profile: %w", err)
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		if row.ProfileID.Valid {
			counts[row.ProfileID.String] = int(row.Clusters)
		}
	}
	return counts, nil
}

func toStorageProfile(row db.StorageProfile, clusters int) StorageProfile {
	out := StorageProfile{
		ID:          row.ID,
		Name:        row.Name,
		Provider:    row.Provider,
		Endpoint:    row.Endpoint,
		Region:      row.Region,
		Bucket:      row.Bucket,
		Prefix:      row.Prefix,
		PathStyle:   row.PathStyle != 0,
		AccessKeyID: row.AccessKeyID,
		SecretLast4: row.SecretLast4,
		Version:     row.Version,
		Clusters:    clusters,
		TestError:   row.TestError,
		CreatedAt:   row.CreatedAt.UTC(),
		UpdatedAt:   row.UpdatedAt.UTC(),
	}
	if row.TestedAt.Valid {
		tested := row.TestedAt.Time.UTC()
		out.TestedAt = &tested
	}
	return out
}

// storageProfileProvider checks the storage provider. S3's API is the only
// one Lewbox speaks.
func storageProfileProvider(raw string) (string, error) {
	provider := strings.ToLower(strings.TrimSpace(raw))
	if provider == "" {
		provider = "s3"
	}
	if provider != "s3" {
		return "", invalid("only S3-compatible storage is supported")
	}
	return provider, nil
}

// storageProfileEndpoint checks and normalizes an endpoint URL. An empty URL
// means AWS.
func storageProfileEndpoint(raw string) (string, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(raw), "/")
	if endpoint == "" {
		return "", nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil || u.Fragment != "" {
		return "", invalid("endpoint must be an http:// or https:// URL with a host and no path, such as https://s3.example.com")
	}
	return endpoint, nil
}

// storageProfileRegion checks the region. AWS needs one; a profile with an
// endpoint defaults to us-east-1.
func storageProfileRegion(raw, endpoint string) (string, error) {
	region := strings.TrimSpace(raw)
	if region == "" {
		if endpoint == "" {
			return "", invalid("region is required for AWS S3; use auto for Cloudflare R2")
		}
		return "us-east-1", nil
	}
	return region, nil
}

// storageProfileBucket checks a bucket name against S3's rules.
func storageProfileBucket(raw string) (string, error) {
	bucket := strings.ToLower(strings.TrimSpace(raw))
	if len(bucket) < 3 || len(bucket) > 63 {
		return "", invalid("bucket must be 3-63 characters")
	}
	first := bucket[0]
	if !((first >= 'a' && first <= 'z') || (first >= '0' && first <= '9')) || net.ParseIP(bucket) != nil {
		return "", invalid("bucket must start with a letter or digit and must not be an IP address")
	}
	for i := 0; i < len(bucket); i++ {
		ch := bucket[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '.' || ch == '-' {
			continue
		}
		return "", invalid("bucket may only contain lowercase letters, digits, dots and hyphens")
	}
	last := bucket[len(bucket)-1]
	if !((last >= 'a' && last <= 'z') || (last >= '0' && last <= '9')) {
		return "", invalid("bucket must end with a letter or digit")
	}
	return bucket, nil
}

// storageProfilePrefix checks and normalizes a key prefix: no leading or
// trailing slash, and no empty, "." or ".." segment.
func storageProfilePrefix(raw string) (string, error) {
	prefix := strings.Trim(strings.TrimSpace(raw), "/")
	if prefix == "" {
		return "", nil
	}
	for _, segment := range strings.Split(prefix, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", invalid("prefix must not have an empty, %q or %q segment", ".", "..")
		}
		for i := 0; i < len(segment); i++ {
			ch := segment[i]
			if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') ||
				ch == '.' || ch == '_' || ch == '-' {
				continue
			}
			return "", invalid("prefix may only contain letters, digits, dots, underscores, hyphens and slashes")
		}
	}
	return prefix, nil
}

// storageProfileRequired checks a credential that must be present.
func storageProfileRequired(raw, field string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", invalid("%s is required", field)
	}
	return value, nil
}

// storageProfilePathStyle applies the default: path style when an endpoint is
// set, virtual-hosted when it is AWS.
func storageProfilePathStyle(pathStyle *bool, endpoint string) bool {
	if pathStyle != nil {
		return *pathStyle
	}
	return endpoint != ""
}

// boolToInt stores a boolean in SQLite's INTEGER column.
func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
