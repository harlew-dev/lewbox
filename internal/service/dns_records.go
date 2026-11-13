package service

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"
	"uuid"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/db"
	"github.com/harlew-dev/lewbox/internal/dns/cloudflare"
)

const (
	// dnsRecordsTimeout bounds one reconciliation: listing, adopting,
	// creating and deleting records in a cluster's zone.
	dnsRecordsTimeout = 30 * time.Second
	// dnsRecordsTTL is short so a member that moves is followed quickly.
	dnsRecordsTTL = 60
	// dnsRecordsCheck names the cluster check that keeps the records.
	dnsRecordsCheck = "dns records"
)

// desiredDNSRecord is one Cloudflare record Lewbox wants in a zone.
type desiredDNSRecord struct {
	name    string // <domain> or *.<domain>
	typ     string // A or AAAA
	content string // the IP
	node    string // the member behind it
}

// reconcileDNSRecords keeps a cluster's domain pointed at its live ingress
// members. It creates a record for every such member's public IP and deletes
// only the records Lewbox recorded, never another record in the zone. It
// returns a Check with an empty Name when the cluster has no domain or DNS
// profile, or when records are off and there are none to remove, so the
// caller can leave such clusters alone.
func (s *Service) reconcileDNSRecords(ctx context.Context, cluster db.Cluster, members []aerolvm.Member) Check {
	rows, err := s.q.ListDNSRecordsByCluster(ctx, cluster.ID)
	if err != nil {
		return Check{Name: dnsRecordsCheck, Detail: fmt.Sprintf("list the cluster's DNS records: %v", err)}
	}
	if cluster.Domain == "" || !cluster.DnsProfileID.Valid {
		// The operator removed the domain or the profile, so nothing Lewbox
		// keeps has a place any more. Drop the records quietly: there is no
		// DNS check to show.
		if len(rows) > 0 {
			s.removeDNSRecords(ctx, cluster)
		}
		return Check{}
	}
	profile, err := s.q.GetDNSProfileByID(ctx, cluster.DnsProfileID.String)
	if err != nil {
		return Check{Name: dnsRecordsCheck, Detail: fmt.Sprintf("get DNS profile: %v", err)}
	}
	auto := cluster.DnsRecords != "off"
	if !auto && len(rows) == 0 {
		return Check{}
	}

	var desired []desiredDNSRecord
	var skipped []string
	if auto {
		desired, skipped = desiredDNSRecords(cluster.Domain, members)
	}

	ctx, cancel := context.WithTimeout(ctx, dnsRecordsTimeout)
	defer cancel()
	clients := make(map[string]*cloudflare.Client)

	wanted := make(map[string]bool, len(desired))
	for _, want := range desired {
		wanted[dnsRecordKey(want.name, want.typ, want.content)] = true
	}

	// Rows that no longer match this cluster's domain, profile or members go
	// first: the table's unique key is (cluster, name, type, content), so a
	// stale row would block the record that replaces it.
	keep := make(map[string]bool, len(rows))
	blocked := make(map[string]bool)
	var failures []string
	for _, row := range rows {
		key := dnsRecordKey(row.Name, row.Type, row.Content)
		// A row from another profile or zone is stale even when it points at
		// the right name: its record lives somewhere else.
		if wanted[key] && row.ProfileID == profile.ID && row.ZoneID == profile.ZoneID {
			keep[key] = true
			continue
		}
		client, err := s.dnsClientFor(ctx, clients, row.ProfileID)
		if err != nil {
			failures = append(failures, fmt.Sprintf("record %s %s: %v", row.Type, row.Name, err))
			blocked[key] = true
			continue
		}
		if err := client.DeleteRecord(ctx, row.ZoneID, row.RecordID); err != nil {
			failures = append(failures, fmt.Sprintf("delete %s %s: %s", row.Type, row.Name, dnsAPIErrorMessage(err)))
			blocked[key] = true
			continue
		}
		if _, err := s.q.DeleteDNSRecord(ctx, row.ID); err != nil {
			failures = append(failures, fmt.Sprintf("forget %s %s: %v", row.Type, row.Name, err))
			blocked[key] = true
		}
	}

	for _, want := range desired {
		key := dnsRecordKey(want.name, want.typ, want.content)
		if keep[key] || blocked[key] {
			continue
		}
		recordID, err := s.ensureDNSRecord(ctx, clients, profile, cluster, want)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		if _, err := s.q.CreateDNSRecord(ctx, db.CreateDNSRecordParams{
			ID:        uuid.NewV7().String(),
			ClusterID: cluster.ID,
			ProfileID: profile.ID,
			ZoneID:    profile.ZoneID,
			Node:      want.node,
			Name:      want.name,
			Type:      want.typ,
			Content:   want.content,
			RecordID:  recordID,
			CreatedAt: s.now(),
		}); err != nil && !db.IsUniqueViolation(err) {
			failures = append(failures, fmt.Sprintf("record %s %s: %v", want.typ, want.name, err))
			continue
		}
		keep[key] = true
	}

	if len(failures) > 0 {
		return Check{Name: dnsRecordsCheck, Detail: strings.Join(failures, "; ")}
	}
	detail := "records are off"
	if auto {
		detail = dnsRecordsDetail(cluster.Domain, desired)
	}
	if len(skipped) > 0 {
		detail += "; " + strings.Join(skipped, "; ")
	}
	return Check{Name: dnsRecordsCheck, OK: true, Detail: detail}
}

// removeDNSRecords deletes every record Lewbox keeps for a cluster. It is
// best effort: Cloudflare must never block a cluster's removal.
func (s *Service) removeDNSRecords(ctx context.Context, cluster db.Cluster) {
	rows, err := s.q.ListDNSRecordsByCluster(ctx, cluster.ID)
	if err != nil {
		s.log.Warn("list DNS records failed", "cluster", cluster.Slug, "err", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, dnsRecordsTimeout)
	defer cancel()
	clients := make(map[string]*cloudflare.Client)
	for _, row := range rows {
		client, err := s.dnsClientFor(ctx, clients, row.ProfileID)
		if err != nil {
			s.log.Warn("open DNS profile failed", "cluster", cluster.Slug, "profile", row.ProfileID, "err", err)
			continue
		}
		if err := client.DeleteRecord(ctx, row.ZoneID, row.RecordID); err != nil {
			s.log.Warn("delete DNS record failed", "cluster", cluster.Slug, "name", row.Name, "err", err)
			continue
		}
		if _, err := s.q.DeleteDNSRecord(ctx, row.ID); err != nil {
			s.log.Warn("forget DNS record failed", "cluster", cluster.Slug, "name", row.Name, "err", err)
		}
	}
}

// ensureDNSRecord returns the Cloudflare ID of the record for want: the one
// already in the zone with the same content, adopted, or a new one.
func (s *Service) ensureDNSRecord(ctx context.Context, clients map[string]*cloudflare.Client, profile db.DnsProfile, cluster db.Cluster, want desiredDNSRecord) (string, error) {
	client, err := s.dnsClientFor(ctx, clients, profile.ID)
	if err != nil {
		return "", fmt.Errorf("record %s %s: %w", want.typ, want.name, err)
	}
	existing, err := client.ListRecords(ctx, profile.ZoneID, want.typ, want.name)
	if err != nil {
		return "", fmt.Errorf("list %s %s: %s", want.typ, want.name, dnsAPIErrorMessage(err))
	}
	for _, record := range existing {
		if record.Content == want.content {
			return record.ID, nil
		}
	}
	record, err := client.CreateRecord(ctx, profile.ZoneID, cloudflare.Record{
		Type:    want.typ,
		Name:    want.name,
		Content: want.content,
		TTL:     dnsRecordsTTL,
		Proxied: false,
		Comment: fmt.Sprintf("lewbox %s %s", cluster.Slug, want.node),
	})
	if err != nil {
		return "", fmt.Errorf("create %s %s: %s", want.typ, want.name, dnsAPIErrorMessage(err))
	}
	return record.ID, nil
}

// dnsClientFor returns a Cloudflare client for a stored profile, opening its
// sealed token once per reconciliation.
func (s *Service) dnsClientFor(ctx context.Context, clients map[string]*cloudflare.Client, profileID string) (*cloudflare.Client, error) {
	if client, ok := clients[profileID]; ok {
		return client, nil
	}
	row, err := s.q.GetDNSProfileByID(ctx, profileID)
	if err != nil {
		return nil, fmt.Errorf("get DNS profile: %w", err)
	}
	token, err := s.keyring.Open(row.ApiTokenEnc)
	if err != nil {
		return nil, fmt.Errorf("open DNS profile %q's token: %w", row.Name, err)
	}
	client := cloudflare.New(string(token), cloudflare.WithBaseURL(s.cloudflareURL))
	clients[profileID] = client
	return client, nil
}

// desiredDNSRecords lists the records a cluster's domain needs: an A or AAAA
// record for the domain and its wildcard, for every live member that can
// serve traffic and has a public IP. Members whose public_host is not a
// usable address are named in the second result.
func desiredDNSRecords(domain string, members []aerolvm.Member) ([]desiredDNSRecord, []string) {
	var out []desiredDNSRecord
	var skipped []string
	for _, member := range members {
		if !member.Alive || (member.Role != "mixed" && member.Role != "ingress") {
			continue
		}
		host := strings.TrimSpace(member.PublicHost)
		if host == "" && strings.TrimSpace(member.DataPlaneHost) == "" {
			continue
		}
		name := memberName(member)
		// In domain mode public_host is the domain; the IP is data_plane_host.
		addr, ok := member.PublicIP()
		if !ok {
			skipped = append(skipped, fmt.Sprintf("%s: %s is not an IP address, no record", name, host))
			continue
		}
		if reason := unusablePublicAddr(addr); reason != "" {
			skipped = append(skipped, fmt.Sprintf("%s: %s is %s, no record", name, addr, reason))
			continue
		}
		typ := "AAAA"
		if addr.Is4() {
			typ = "A"
		}
		for _, recordName := range []string{domain, "*." + domain} {
			out = append(out, desiredDNSRecord{name: recordName, typ: typ, content: addr.String(), node: name})
		}
	}
	return out, skipped
}

// unusablePublicAddr says why an address cannot be published, or "" when it
// can.
func unusablePublicAddr(addr netip.Addr) string {
	switch {
	case !addr.IsValid():
		return "not an IP address"
	case addr.IsUnspecified():
		return "an unspecified address"
	case addr.IsLoopback():
		return "a loopback address"
	case addr.IsPrivate():
		return "a private address"
	case addr.IsLinkLocalUnicast(), addr.IsLinkLocalMulticast():
		return "a link-local address"
	case addr.IsMulticast():
		return "a multicast address"
	}
	return ""
}

// dnsRecordsDetail says what the records point at.
func dnsRecordsDetail(domain string, desired []desiredDNSRecord) string {
	if len(desired) == 0 {
		return "no records: no live mixed or ingress member has a public IP address"
	}
	names := []string{domain, "*." + domain}
	seen := make(map[string]bool, len(desired))
	ips := make([]string, 0, len(desired))
	for _, record := range desired {
		if seen[record.content] {
			continue
		}
		seen[record.content] = true
		ips = append(ips, record.content)
	}
	noun := "records"
	if len(desired) == 1 {
		noun = "record"
	}
	return fmt.Sprintf("%d %s for %s → %s", len(desired), noun, strings.Join(names, " and "), strings.Join(ips, ", "))
}

// dnsRecordKey identifies a row by what it points at.
func dnsRecordKey(name, typ, content string) string {
	return name + "\x00" + typ + "\x00" + content
}
