package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"uuid"

	"github.com/harlew-dev/lewbox/internal/db"
	"github.com/harlew-dev/lewbox/internal/dns/cloudflare"
)

// dnsProfileTestTimeout bounds one profile test: finding the zone, then
// writing and deleting a TXT record.
const dnsProfileTestTimeout = 30 * time.Second

// dnsZoneName is a domain name with at least one dot.
var dnsZoneName = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// DNSProfile is a saved Cloudflare API token and the zone it reaches
// (DECISIONS D34). The token is never part of it.
type DNSProfile struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Provider   string     `json:"provider"`
	Zone       string     `json:"zone"`
	ZoneID     string     `json:"zone_id"`
	TokenLast4 string     `json:"token_last4"`
	ACMEEmail  string     `json:"acme_email"`
	Version    int64      `json:"version"`
	Clusters   int        `json:"clusters"`
	TestedAt   *time.Time `json:"tested_at"`
	TestError  string     `json:"test_error"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// DNSProfileList is returned by ListDNSProfiles. Data is never null.
type DNSProfileList struct {
	Data []DNSProfile `json:"data"`
}

// CreateDNSProfileRequest is the input to CreateDNSProfile.
type CreateDNSProfileRequest struct {
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Zone      string `json:"zone"`
	APIToken  string `json:"api_token"`
	ACMEEmail string `json:"acme_email"`
}

// UpdateDNSProfileRequest changes the fields that are set.
type UpdateDNSProfileRequest struct {
	Name      *string `json:"name,omitempty"`
	Zone      *string `json:"zone,omitempty"`
	APIToken  *string `json:"api_token,omitempty"`
	ACMEEmail *string `json:"acme_email,omitempty"`
}

// DNSProfileCredentials is what a node needs to configure DNS. APIToken is
// the only place the token is returned.
type DNSProfileCredentials struct {
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Zone      string `json:"zone"`
	ZoneID    string `json:"zone_id"`
	APIToken  string `json:"api_token"`
	ACMEEmail string `json:"acme_email"`
	Version   int64  `json:"version"`
}

// dnsProfileTest is the outcome of testing one token: a zone ID, or the step
// that failed and why.
type dnsProfileTest struct {
	zoneID  string
	step    string
	message string
}

// ListDNSProfiles returns every profile, ordered by name. Instance admins
// only.
func (s *Service) ListDNSProfiles(ctx context.Context) (DNSProfileList, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return DNSProfileList{}, err
	}
	rows, err := s.q.ListDNSProfiles(ctx)
	if err != nil {
		return DNSProfileList{}, fmt.Errorf("list DNS profiles: %w", err)
	}
	counts, err := s.dnsProfileClusterCounts(ctx)
	if err != nil {
		return DNSProfileList{}, err
	}
	out := DNSProfileList{Data: make([]DNSProfile, 0, len(rows))}
	for _, row := range rows {
		out.Data = append(out.Data, toDNSProfile(row, counts[row.ID]))
	}
	return out, nil
}

// GetDNSProfile returns a profile by ID or name.
func (s *Service) GetDNSProfile(ctx context.Context, ref string) (DNSProfile, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return DNSProfile{}, err
	}
	row, err := s.findDNSProfile(ctx, ref)
	if err != nil {
		return DNSProfile{}, err
	}
	counts, err := s.dnsProfileClusterCounts(ctx)
	if err != nil {
		return DNSProfile{}, err
	}
	return toDNSProfile(row, counts[row.ID]), nil
}

// CreateDNSProfile saves a profile after testing it. A profile whose test
// fails is not saved. Instance admins only.
func (s *Service) CreateDNSProfile(ctx context.Context, req CreateDNSProfileRequest) (DNSProfile, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return DNSProfile{}, err
	}
	name, err := planSlug(req.Name)
	if err != nil {
		return DNSProfile{}, err
	}
	provider, err := dnsProfileProvider(req.Provider)
	if err != nil {
		return DNSProfile{}, err
	}
	zone, err := dnsProfileZone(req.Zone)
	if err != nil {
		return DNSProfile{}, err
	}
	token, err := dnsProfileToken(req.APIToken)
	if err != nil {
		return DNSProfile{}, err
	}
	acme, err := dnsProfileACMEEmail(req.ACMEEmail)
	if err != nil {
		return DNSProfile{}, err
	}

	result := s.testDNSProfile(ctx, zone, token)
	if result.step != "" {
		return DNSProfile{}, precondition(map[string]string{"step": result.step}, "DNS profile test failed: %s", result.message)
	}

	sealed, err := s.keyring.Seal([]byte(token))
	if err != nil {
		return DNSProfile{}, err
	}
	now := s.now()
	row, err := s.q.CreateDNSProfile(ctx, db.CreateDNSProfileParams{
		ID:          uuid.NewV7().String(),
		Name:        name,
		Provider:    provider,
		Zone:        zone,
		ZoneID:      result.zoneID,
		ApiTokenEnc: sealed,
		TokenLast4:  last4(token),
		AcmeEmail:   acme,
		Version:     1,
		TestedAt:    sql.NullTime{Time: now, Valid: true},
		TestError:   "",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if db.IsUniqueViolation(err) {
		return DNSProfile{}, conflict("a DNS profile named %q already exists", name)
	}
	if err != nil {
		return DNSProfile{}, fmt.Errorf("create DNS profile: %w", err)
	}
	return toDNSProfile(row, 0), nil
}

// UpdateDNSProfile changes a profile by ID or name. A new zone or token is
// tested before it is saved, and bumps the version. Instance admins only.
func (s *Service) UpdateDNSProfile(ctx context.Context, ref string, req UpdateDNSProfileRequest) (DNSProfile, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return DNSProfile{}, err
	}
	if req.Name == nil && req.Zone == nil && req.APIToken == nil && req.ACMEEmail == nil {
		return DNSProfile{}, invalid("nothing to change: set name, zone, api_token or acme_email")
	}
	row, err := s.findDNSProfile(ctx, ref)
	if err != nil {
		return DNSProfile{}, err
	}

	if req.Name != nil {
		row.Name, err = planSlug(*req.Name)
		if err != nil {
			return DNSProfile{}, err
		}
	}
	if req.ACMEEmail != nil {
		row.AcmeEmail, err = dnsProfileACMEEmail(*req.ACMEEmail)
		if err != nil {
			return DNSProfile{}, err
		}
	}

	changed := false
	if req.Zone != nil {
		zone, err := dnsProfileZone(*req.Zone)
		if err != nil {
			return DNSProfile{}, err
		}
		if zone != row.Zone {
			row.Zone = zone
			changed = true
		}
	}
	if req.APIToken != nil {
		token, err := dnsProfileToken(*req.APIToken)
		if err != nil {
			return DNSProfile{}, err
		}
		sealed, err := s.keyring.Seal([]byte(token))
		if err != nil {
			return DNSProfile{}, err
		}
		row.ApiTokenEnc = sealed
		row.TokenLast4 = last4(token)
		changed = true
	}

	now := s.now()
	if changed {
		token, err := s.keyring.Open(row.ApiTokenEnc)
		if err != nil {
			return DNSProfile{}, err
		}
		result := s.testDNSProfile(ctx, row.Zone, string(token))
		if result.step != "" {
			return DNSProfile{}, precondition(map[string]string{"step": result.step}, "DNS profile test failed: %s", result.message)
		}
		row.ZoneID = result.zoneID
		row.TestedAt = sql.NullTime{Time: now, Valid: true}
		row.TestError = ""
		row.Version++
	}
	row.UpdatedAt = now

	updated, err := s.q.UpdateDNSProfile(ctx, db.UpdateDNSProfileParams{
		Name:        row.Name,
		Zone:        row.Zone,
		ZoneID:      row.ZoneID,
		ApiTokenEnc: row.ApiTokenEnc,
		TokenLast4:  row.TokenLast4,
		AcmeEmail:   row.AcmeEmail,
		Version:     row.Version,
		TestedAt:    row.TestedAt,
		TestError:   row.TestError,
		UpdatedAt:   row.UpdatedAt,
		ID:          row.ID,
	})
	if db.IsUniqueViolation(err) {
		return DNSProfile{}, conflict("a DNS profile named %q already exists", row.Name)
	}
	if err != nil {
		return DNSProfile{}, fmt.Errorf("update DNS profile: %w", err)
	}
	counts, err := s.dnsProfileClusterCounts(ctx)
	if err != nil {
		return DNSProfile{}, err
	}
	return toDNSProfile(updated, counts[updated.ID]), nil
}

// DeleteDNSProfile removes a profile no cluster uses. Instance admins only.
// The guard is in the DELETE, so a cluster that starts using the profile
// between the lookup and the delete keeps it.
func (s *Service) DeleteDNSProfile(ctx context.Context, ref string) (DNSProfile, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return DNSProfile{}, err
	}
	row, err := s.findDNSProfile(ctx, ref)
	if err != nil {
		return DNSProfile{}, err
	}
	n, err := s.q.DeleteDNSProfile(ctx, row.ID)
	if err != nil {
		return DNSProfile{}, fmt.Errorf("delete DNS profile: %w", err)
	}
	if n == 0 {
		counts, err := s.dnsProfileClusterCounts(ctx)
		if err != nil {
			return DNSProfile{}, err
		}
		return DNSProfile{}, conflict("DNS profile %q is used by %d cluster(s); stop using it in those clusters first", row.Name, counts[row.ID])
	}
	return toDNSProfile(row, 0), nil
}

// TestDNSProfile tests a profile's token now and records the result. A
// failing test is not an error: it shows in test_error. Instance admins only.
func (s *Service) TestDNSProfile(ctx context.Context, ref string) (DNSProfile, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return DNSProfile{}, err
	}
	row, err := s.findDNSProfile(ctx, ref)
	if err != nil {
		return DNSProfile{}, err
	}
	token, err := s.keyring.Open(row.ApiTokenEnc)
	if err != nil {
		return DNSProfile{}, err
	}
	result := s.testDNSProfile(ctx, row.Zone, string(token))
	testedAt := sql.NullTime{Time: s.now(), Valid: true}
	if result.step != "" {
		row.TestError = result.message
	} else {
		row.ZoneID = result.zoneID
		row.TestError = ""
	}
	row.TestedAt = testedAt
	if err := s.q.RecordDNSProfileTest(ctx, db.RecordDNSProfileTestParams{
		TestedAt:  row.TestedAt,
		TestError: row.TestError,
		ZoneID:    row.ZoneID,
		ID:        row.ID,
	}); err != nil {
		return DNSProfile{}, fmt.Errorf("record DNS profile test: %w", err)
	}
	counts, err := s.dnsProfileClusterCounts(ctx)
	if err != nil {
		return DNSProfile{}, err
	}
	return toDNSProfile(row, counts[row.ID]), nil
}

// DNSProfileCredentials returns the profile's token, which nodes need to
// configure TLS. It is the only method that returns the secret. Instance
// admins only.
func (s *Service) DNSProfileCredentials(ctx context.Context, ref string) (DNSProfileCredentials, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return DNSProfileCredentials{}, err
	}
	row, err := s.findDNSProfile(ctx, ref)
	if err != nil {
		return DNSProfileCredentials{}, err
	}
	token, err := s.keyring.Open(row.ApiTokenEnc)
	if err != nil {
		return DNSProfileCredentials{}, err
	}
	return DNSProfileCredentials{
		Name:      row.Name,
		Provider:  row.Provider,
		Zone:      row.Zone,
		ZoneID:    row.ZoneID,
		APIToken:  string(token),
		ACMEEmail: row.AcmeEmail,
		Version:   row.Version,
	}, nil
}

// testDNSProfile finds the zone, writes and deletes a TXT record, and reports
// the step that failed.
func (s *Service) testDNSProfile(ctx context.Context, zone, token string) dnsProfileTest {
	ctx, cancel := context.WithTimeout(ctx, dnsProfileTestTimeout)
	defer cancel()

	client := cloudflare.New(token, cloudflare.WithBaseURL(s.cloudflareURL))
	found, err := client.FindZone(ctx, zone)
	if errors.Is(err, cloudflare.ErrZoneNotFound) {
		return dnsProfileTest{step: "zone", message: fmt.Sprintf("zone %s not found with this token", zone)}
	}
	if err != nil {
		return dnsProfileTest{step: "zone", message: dnsZoneTestMessage(err)}
	}

	content, err := dnsTestContent()
	if err != nil {
		return dnsProfileTest{step: "write", message: err.Error()}
	}
	record, err := client.CreateRecord(ctx, found.ID, cloudflare.Record{
		Type:    "TXT",
		Name:    "_lewbox-test." + zone,
		Content: content,
		TTL:     60,
	})
	if err != nil {
		return dnsProfileTest{step: "write", message: fmt.Sprintf("the token can read the zone but not edit its records: %s", dnsAPIErrorMessage(err))}
	}
	if err := client.DeleteRecord(ctx, found.ID, record.ID); err != nil {
		return dnsProfileTest{step: "write", message: fmt.Sprintf("the token can read the zone but not edit its records: %s", dnsAPIErrorMessage(err))}
	}
	return dnsProfileTest{zoneID: found.ID}
}

// dnsZoneTestMessage reports why finding the zone failed. A refused token
// says so rather than repeating Cloudflare's authentication text.
func dnsZoneTestMessage(err error) string {
	if apiErr, ok := errors.AsType[*cloudflare.APIError](err); ok && (apiErr.Status == 401 || apiErr.Status == 403) {
		return "the token was refused"
	}
	return dnsAPIErrorMessage(err)
}

func dnsAPIErrorMessage(err error) string {
	if apiErr, ok := errors.AsType[*cloudflare.APIError](err); ok && apiErr.Message != "" {
		return apiErr.Message
	}
	return err.Error()
}

func dnsTestContent() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("make a test record: %w", err)
	}
	return "lewbox-test-" + hex.EncodeToString(b[:]), nil
}

// findDNSProfile looks a profile up by ID or name.
func (s *Service) findDNSProfile(ctx context.Context, ref string) (db.DnsProfile, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return db.DnsProfile{}, invalid("a DNS profile ID or name is required")
	}
	var (
		row db.DnsProfile
		err error
	)
	if id, perr := uuid.Parse(ref); perr == nil {
		row, err = s.q.GetDNSProfileByID(ctx, id.String())
	} else {
		row, err = s.q.GetDNSProfileByName(ctx, strings.ToLower(ref))
	}
	if errors.Is(err, sql.ErrNoRows) {
		return db.DnsProfile{}, notFound("DNS profile %q not found", ref)
	}
	if err != nil {
		return db.DnsProfile{}, fmt.Errorf("get DNS profile: %w", err)
	}
	return row, nil
}

// dnsProfileClusterCounts maps profile IDs to how many clusters use them.
func (s *Service) dnsProfileClusterCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.q.CountDNSProfileClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("count clusters by DNS profile: %w", err)
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		if row.ProfileID.Valid {
			counts[row.ProfileID.String] = int(row.Clusters)
		}
	}
	return counts, nil
}

func toDNSProfile(row db.DnsProfile, clusters int) DNSProfile {
	out := DNSProfile{
		ID:         row.ID,
		Name:       row.Name,
		Provider:   row.Provider,
		Zone:       row.Zone,
		ZoneID:     row.ZoneID,
		TokenLast4: row.TokenLast4,
		ACMEEmail:  row.AcmeEmail,
		Version:    row.Version,
		Clusters:   clusters,
		TestError:  row.TestError,
		CreatedAt:  row.CreatedAt.UTC(),
		UpdatedAt:  row.UpdatedAt.UTC(),
	}
	if row.TestedAt.Valid {
		tested := row.TestedAt.Time.UTC()
		out.TestedAt = &tested
	}
	return out
}

// dnsProfileProvider checks the DNS provider. Cloudflare is the only one
// AerolVM's installer supports.
func dnsProfileProvider(raw string) (string, error) {
	provider := strings.ToLower(strings.TrimSpace(raw))
	if provider == "" {
		provider = "cloudflare"
	}
	if provider != "cloudflare" {
		return "", invalid("only cloudflare is supported: AerolVM's installer supports no other DNS provider")
	}
	return provider, nil
}

// dnsProfileZone checks and normalizes a zone name.
func dnsProfileZone(raw string) (string, error) {
	zone := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
	if !dnsZoneName.MatchString(zone) {
		return "", invalid("zone must be a domain name with a dot, such as example.com")
	}
	return zone, nil
}

// dnsProfileToken checks an API token.
func dnsProfileToken(raw string) (string, error) {
	token := strings.TrimSpace(raw)
	if token == "" {
		return "", invalid("api_token is required")
	}
	return token, nil
}

// dnsProfileACMEEmail checks the optional ACME account email.
func dnsProfileACMEEmail(raw string) (string, error) {
	email := strings.TrimSpace(raw)
	if email == "" {
		return "", nil
	}
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return "", invalid("acme_email must be an email address such as ops@example.com")
	}
	return email, nil
}
