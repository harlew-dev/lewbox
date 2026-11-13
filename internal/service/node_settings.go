package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/harlew-dev/lewbox/internal/db"
)

// NodeSettings is everything a node of a cluster must apply: its storage
// profile (the registry in front of the cluster's bucket) and its DNS profile.
// It is the only response besides the profiles' own credentials that carries
// secrets. Instance admins only.
type NodeSettings struct {
	ClusterID  string `json:"cluster_id"`
	Cluster    string `json:"cluster"` // slug
	StopPolicy string `json:"stop_policy"`
	// ArchiveLocalRetention is how long the node keeps local copies of
	// archived sandboxes before its cleanup timer removes them: instant, 1h,
	// 1d, 7d or never (no cleanup timer).
	ArchiveLocalRetention string               `json:"archive_local_retention"`
	Storage               *NodeStorageSettings `json:"storage"` // null: no storage profile
	DNS                   *NodeDNSSettings     `json:"dns"`     // null: no domain or no DNS profile
}

// NodeStorageSettings is the bucket a node's registry stores snapshots in.
type NodeStorageSettings struct {
	Profile         string `json:"profile"`
	Version         int64  `json:"version"`
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	PathStyle       bool   `json:"path_style"`
	RootDirectory   string `json:"root_directory"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

// NodeDNSSettings is the Cloudflare token a domain-mode node installs its
// certificate with.
type NodeDNSSettings struct {
	Profile   string `json:"profile"`
	Version   int64  `json:"version"`
	Zone      string `json:"zone"`
	Domain    string `json:"domain"`
	APIToken  string `json:"api_token"`
	ACMEEmail string `json:"acme_email"`
}

// ReportNodeSettingsRequest is what a node sends after it applied its cluster's
// node settings. A null profile means the node applied none.
type ReportNodeSettingsRequest struct {
	StorageProfile *string `json:"storage_profile"`
	StorageVersion int64   `json:"storage_version"`
	DNSProfile     *string `json:"dns_profile"`
	DNSVersion     int64   `json:"dns_version"`
	// ArchiveRetention is the archive_local_retention the node applied with
	// its storage settings; null with no storage profile.
	ArchiveRetention *string `json:"archive_retention"`
}

// NodeSettingsState is what a node last reported. Reasons is never null.
type NodeSettingsState struct {
	AppliedAt *time.Time `json:"applied_at"`
	Outdated  bool       `json:"outdated"`
	Reasons   []string   `json:"reasons"`
}

// registryRoot is machine.RegistryRoot, copied because machine imports this
// package. It is the bucket path a cluster's registry stores under.
func registryRoot(prefix, clusterID string) string {
	parts := []string{}
	if p := strings.Trim(prefix, "/"); p != "" {
		parts = append(parts, p)
	}
	parts = append(parts, "clusters", strings.ToLower(clusterID))
	return "/" + strings.Join(parts, "/")
}

// ClusterNodeSettings returns what every node of a cluster must apply,
// including the bucket's secret key and the DNS token. Instance admins only.
func (s *Service) ClusterNodeSettings(ctx context.Context, ref string) (NodeSettings, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return NodeSettings{}, err
	}
	if s.keyring == nil {
		return NodeSettings{}, errors.New("no keyring configured")
	}
	row, err := s.findCluster(ctx, ref)
	if err != nil {
		return NodeSettings{}, err
	}
	out := NodeSettings{
		ClusterID:             row.ID,
		Cluster:               row.Slug,
		StopPolicy:            row.StopPolicy,
		ArchiveLocalRetention: row.ArchiveLocalRetention,
	}

	if row.StorageProfileID.Valid {
		profile, err := s.q.GetStorageProfileByID(ctx, row.StorageProfileID.String)
		if err != nil {
			return NodeSettings{}, fmt.Errorf("get storage profile: %w", err)
		}
		secret, err := s.keyring.Open(profile.SecretKeyEnc)
		if err != nil {
			return NodeSettings{}, err
		}
		out.Storage = &NodeStorageSettings{
			Profile:         profile.Name,
			Version:         profile.Version,
			Endpoint:        profile.Endpoint,
			Region:          profile.Region,
			Bucket:          profile.Bucket,
			PathStyle:       profile.PathStyle != 0,
			RootDirectory:   registryRoot(profile.Prefix, row.ID),
			AccessKeyID:     profile.AccessKeyID,
			SecretAccessKey: string(secret),
		}
	}

	if row.DnsProfileID.Valid && row.Domain != "" {
		profile, err := s.q.GetDNSProfileByID(ctx, row.DnsProfileID.String)
		if err != nil {
			return NodeSettings{}, fmt.Errorf("get DNS profile: %w", err)
		}
		token, err := s.keyring.Open(profile.ApiTokenEnc)
		if err != nil {
			return NodeSettings{}, err
		}
		out.DNS = &NodeDNSSettings{
			Profile:   profile.Name,
			Version:   profile.Version,
			Zone:      profile.Zone,
			Domain:    row.Domain,
			APIToken:  string(token),
			ACMEEmail: profile.AcmeEmail,
		}
	}
	return out, nil
}

// ReportNodeSettings records the profile versions a node applied. Instance
// admins only.
func (s *Service) ReportNodeSettings(ctx context.Context, ref string, req ReportNodeSettingsRequest) (Node, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return Node{}, err
	}
	row, err := s.findNode(ctx, ref)
	if err != nil {
		return Node{}, err
	}
	if !row.ClusterID.Valid {
		return Node{}, precondition(nil, "node %q is in no cluster: report settings only for a node in a cluster", row.Name)
	}
	storageID, storageVersion, err := s.reportedStorageProfile(ctx, req)
	if err != nil {
		return Node{}, err
	}
	dnsID, dnsVersion, err := s.reportedDNSProfile(ctx, req)
	if err != nil {
		return Node{}, err
	}
	var retention sql.NullString
	if req.ArchiveRetention != nil && storageID.Valid {
		value, err := clusterArchiveRetention(*req.ArchiveRetention)
		if err != nil {
			return Node{}, err
		}
		retention = sql.NullString{String: value, Valid: true}
	}
	updated, err := s.q.ReportNodeSettings(ctx, db.ReportNodeSettingsParams{
		SettingsAppliedAt:        sql.NullTime{Time: s.now(), Valid: true},
		SettingsStorageProfileID: storageID,
		SettingsStorageVersion:   storageVersion,
		SettingsDnsProfileID:     dnsID,
		SettingsDnsVersion:       dnsVersion,
		SettingsArchiveRetention: retention,
		ID:                       row.ID,
	})
	if err != nil {
		return Node{}, fmt.Errorf("report node settings: %w", err)
	}
	return s.decorateNode(ctx, updated)
}

// reportedStorageProfile resolves the reported storage profile to its ID, or
// keeps the name when the profile is gone.
func (s *Service) reportedStorageProfile(ctx context.Context, req ReportNodeSettingsRequest) (sql.NullString, sql.NullInt64, error) {
	if req.StorageProfile == nil || strings.TrimSpace(*req.StorageProfile) == "" {
		return sql.NullString{}, sql.NullInt64{}, nil
	}
	if req.StorageVersion < 1 {
		return sql.NullString{}, sql.NullInt64{}, invalid("storage_version must be at least 1 when a storage profile is reported")
	}
	row, err := s.findStorageProfile(ctx, *req.StorageProfile)
	if err == nil {
		return sql.NullString{String: row.ID, Valid: true}, sql.NullInt64{Int64: req.StorageVersion, Valid: true}, nil
	}
	if se, ok := errors.AsType[*Error](err); ok && se.Code == CodeNotFound {
		return sql.NullString{String: strings.TrimSpace(*req.StorageProfile), Valid: true},
			sql.NullInt64{Int64: req.StorageVersion, Valid: true}, nil
	}
	return sql.NullString{}, sql.NullInt64{}, err
}

// reportedDNSProfile resolves the reported DNS profile to its ID, or keeps the
// name when the profile is gone.
func (s *Service) reportedDNSProfile(ctx context.Context, req ReportNodeSettingsRequest) (sql.NullString, sql.NullInt64, error) {
	if req.DNSProfile == nil || strings.TrimSpace(*req.DNSProfile) == "" {
		return sql.NullString{}, sql.NullInt64{}, nil
	}
	if req.DNSVersion < 1 {
		return sql.NullString{}, sql.NullInt64{}, invalid("dns_version must be at least 1 when a DNS profile is reported")
	}
	row, err := s.findDNSProfile(ctx, *req.DNSProfile)
	if err == nil {
		return sql.NullString{String: row.ID, Valid: true}, sql.NullInt64{Int64: req.DNSVersion, Valid: true}, nil
	}
	if se, ok := errors.AsType[*Error](err); ok && se.Code == CodeNotFound {
		return sql.NullString{String: strings.TrimSpace(*req.DNSProfile), Valid: true},
			sql.NullInt64{Int64: req.DNSVersion, Valid: true}, nil
	}
	return sql.NullString{}, sql.NullInt64{}, err
}

// nodeSettingsState reads a node's reported settings. It is null for a node in
// no cluster.
func nodeSettingsState(r db.Node) *NodeSettingsState {
	if !r.ClusterID.Valid {
		return nil
	}
	state := &NodeSettingsState{Reasons: []string{}}
	if r.SettingsAppliedAt.Valid {
		at := r.SettingsAppliedAt.Time.UTC()
		state.AppliedAt = &at
	}
	return state
}

// applyNodeOutdated fills a node's settings state with why it is outdated
// against its cluster's profiles.
func (s *Service) applyNodeOutdated(ctx context.Context, n *Node, r db.Node, cluster db.Cluster) error {
	if n.Settings == nil {
		return nil
	}
	reasons := make([]string, 0)

	var storageName string
	var storageVersion int64
	hasStorage := false
	if cluster.StorageProfileID.Valid {
		row, err := s.q.GetStorageProfileByID(ctx, cluster.StorageProfileID.String)
		switch {
		case err == nil:
			hasStorage = true
			storageName, storageVersion = row.Name, row.Version
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("get storage profile: %w", err)
		}
	}
	nodeStorage, err := s.storageProfileLabel(ctx, r.SettingsStorageProfileID)
	if err != nil {
		return err
	}
	switch {
	case hasStorage && !r.SettingsStorageProfileID.Valid:
		reasons = append(reasons, "has no storage settings yet")
	case hasStorage && r.SettingsStorageProfileID.String != cluster.StorageProfileID.String:
		reasons = append(reasons, fmt.Sprintf("storage profile %s is at version %d; this node applied %s", storageName, storageVersion, nodeStorage))
	case hasStorage && r.SettingsStorageVersion.Int64 < storageVersion:
		reasons = append(reasons, fmt.Sprintf("storage profile %s is at version %d; this node has version %d", storageName, storageVersion, r.SettingsStorageVersion.Int64))
	case !hasStorage && r.SettingsStorageProfileID.Valid:
		reasons = append(reasons, fmt.Sprintf("the cluster no longer uses storage profile %s", nodeStorage))
	}
	// A node that applied storage before this setting existed reported no
	// retention: it removes local copies at once, as instant does.
	if hasStorage && r.SettingsStorageProfileID.Valid {
		applied := RetentionInstant
		if r.SettingsArchiveRetention.Valid {
			applied = r.SettingsArchiveRetention.String
		}
		if want := cluster.ArchiveLocalRetention; applied != want {
			reasons = append(reasons, fmt.Sprintf("archive_local_retention is %s; this node applied %s", want, applied))
		}
	}

	var dnsName string
	var dnsVersion int64
	hasDNS := false
	if cluster.DnsProfileID.Valid {
		row, err := s.q.GetDNSProfileByID(ctx, cluster.DnsProfileID.String)
		switch {
		case err == nil:
			hasDNS = true
			dnsName, dnsVersion = row.Name, row.Version
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("get DNS profile: %w", err)
		}
	}
	// A node that never reported a DNS profile was installed with a token on
	// stdin, so it is not judged on DNS.
	if r.SettingsDnsProfileID.Valid {
		nodeDNS, err := s.dnsProfileLabel(ctx, r.SettingsDnsProfileID)
		if err != nil {
			return err
		}
		switch {
		case !hasDNS:
			reasons = append(reasons, fmt.Sprintf("the cluster no longer uses DNS profile %s", nodeDNS))
		case r.SettingsDnsProfileID.String != cluster.DnsProfileID.String:
			reasons = append(reasons, fmt.Sprintf("DNS profile %s is at version %d; this node applied %s", dnsName, dnsVersion, nodeDNS))
		case r.SettingsDnsVersion.Int64 < dnsVersion:
			reasons = append(reasons, fmt.Sprintf("DNS profile %s is at version %d; this node has version %d", dnsName, dnsVersion, r.SettingsDnsVersion.Int64))
		}
	}

	n.Settings.Reasons = reasons
	n.Settings.Outdated = len(reasons) > 0
	return nil
}

// storageProfileLabel names the profile a node applied, keeping the stored
// value when the profile is gone.
func (s *Service) storageProfileLabel(ctx context.Context, id sql.NullString) (string, error) {
	if !id.Valid {
		return "", nil
	}
	row, err := s.q.GetStorageProfileByID(ctx, id.String)
	if err == nil {
		return row.Name, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return id.String, nil
	}
	return "", fmt.Errorf("get storage profile: %w", err)
}

// dnsProfileLabel names the DNS profile a node applied, keeping the stored
// value when the profile is gone.
func (s *Service) dnsProfileLabel(ctx context.Context, id sql.NullString) (string, error) {
	if !id.Valid {
		return "", nil
	}
	row, err := s.q.GetDNSProfileByID(ctx, id.String)
	if err == nil {
		return row.Name, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return id.String, nil
	}
	return "", fmt.Errorf("get DNS profile: %w", err)
}

// nodeSettingsCheck reports nodes whose applied settings are behind their
// cluster's profiles. It is omitted when the cluster has no storage profile
// and no node has reported storage settings.
func (s *Service) nodeSettingsCheck(ctx context.Context, cluster db.Cluster) (Check, error) {
	rows, err := s.q.ListNodesByCluster(ctx, sql.NullString{String: cluster.ID, Valid: true})
	if err != nil {
		return Check{}, fmt.Errorf("list cluster nodes: %w", err)
	}
	reportedStorage := false
	for _, r := range rows {
		if r.SettingsStorageProfileID.Valid {
			reportedStorage = true
			break
		}
	}
	if !cluster.StorageProfileID.Valid && !reportedStorage {
		return Check{}, nil
	}

	outdated := make([]string, 0)
	for _, r := range rows {
		n := toNode(r)
		if err := s.applyNodeOutdated(ctx, &n, r, cluster); err != nil {
			return Check{}, err
		}
		if n.Settings != nil && n.Settings.Outdated {
			outdated = append(outdated, fmt.Sprintf("%s: %s", n.Name, n.Settings.Reasons[0]))
		}
	}
	if len(outdated) == 0 {
		return Check{Name: "node settings", OK: true, Detail: fmt.Sprintf("%d node(s) up to date", len(rows))}, nil
	}
	return Check{
		Name:   "node settings",
		Detail: strings.Join(outdated, "; ") + "; run `lewbox node configure` on each",
	}, nil
}
