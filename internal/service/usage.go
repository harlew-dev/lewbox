package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/harlew-dev/lewbox/internal/db"
)

// UsageAmounts is a quantity of metered compute. Hours may be fractional.
type UsageAmounts struct {
	SandboxHours   float64 `json:"sandbox_hours"`
	VcpuHours      float64 `json:"vcpu_hours"`
	MemoryGiBHours float64 `json:"memory_gib_hours"`
}

// UsageRegion is one region's share of a usage bucket.
type UsageRegion struct {
	VcpuHours float64 `json:"vcpu_hours"`
}

// UsageBucket is one hour or day of usage. Buckets with no usage are present
// with zeros so charts have a continuous axis.
type UsageBucket struct {
	Start time.Time `json:"start"`
	UsageAmounts
	PeakSandboxes int64                   `json:"peak_sandboxes"`
	Regions       map[string]UsageRegion  `json:"regions"`
	Members       map[string]UsageAmounts `json:"members,omitempty"`
}

// Usage is returned by Usage: a bucket series and its totals.
type Usage struct {
	From        time.Time     `json:"from"`
	To          time.Time     `json:"to"`
	Granularity string        `json:"granularity"`
	Totals      UsageAmounts  `json:"totals"`
	Data        []UsageBucket `json:"data"`
}

// UsageRequest selects a range, granularity and organization. Org is empty
// for an instance admin's whole fleet. Member is an email or membership ID.
type UsageRequest struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Granularity string `json:"granularity"`
	Org         string `json:"org"`
	Member      string `json:"member"`
}

// Usage returns metered usage per hour or day, zero-filled over the range.
func (s *Service) Usage(ctx context.Context, req UsageRequest) (Usage, error) {
	scope, err := s.scopeOrg(ctx, req.Org)
	if err != nil {
		return Usage{}, err
	}
	now := s.now()
	from, err := parseUsageTime(req.From, now.Add(-30*24*time.Hour))
	if err != nil {
		return Usage{}, invalid("from: %s", err)
	}
	to, err := parseUsageTime(req.To, now)
	if err != nil {
		return Usage{}, invalid("to: %s", err)
	}
	if !to.After(from) {
		return Usage{}, invalid("to must be after from")
	}
	if to.Sub(from) > 400*24*time.Hour {
		return Usage{}, invalid("the range must be at most 400 days")
	}
	granularity := strings.ToLower(strings.TrimSpace(req.Granularity))
	if granularity == "" {
		granularity = "day"
	}
	step := 24 * time.Hour
	switch granularity {
	case "day":
	case "hour":
		step = time.Hour
	default:
		return Usage{}, invalid("granularity must be hour or day")
	}

	start := from.UTC().Truncate(step)
	end := to.UTC().Truncate(step)
	orgID := ""
	if scope.org != nil {
		orgID = scope.org.ID
	}
	membershipID := ""
	switch {
	case scope.limited():
		if scope.membership != nil {
			membershipID = scope.membership.ID
		} else {
			membershipID = "none"
		}
	case strings.TrimSpace(req.Member) != "":
		if scope.org == nil {
			return Usage{}, invalid("choose a workspace to filter by member")
		}
		m, _, err := s.findMember(ctx, *scope.org, req.Member)
		if err != nil {
			return Usage{}, err
		}
		membershipID = m.ID
	}

	rows, err := s.q.ListUsageHourly(ctx, db.ListUsageHourlyParams{
		OrgID:        orgID,
		MembershipID: membershipID,
		FromHour:     start,
		ToHour:       end.Add(step),
	})
	if err != nil {
		return Usage{}, fmt.Errorf("list usage: %w", err)
	}
	clusters, err := s.q.ListClusters(ctx)
	if err != nil {
		return Usage{}, fmt.Errorf("list clusters: %w", err)
	}
	regionSlug := make(map[string]string, len(clusters))
	for _, cluster := range clusters {
		regionSlug[cluster.ID] = cluster.Slug
	}

	breakdown := scope.org != nil && !scope.limited() &&
		(scope.p.InstanceAdmin || scope.p.Kind == "user")
	emails := map[string]string{}
	if breakdown {
		members, err := s.q.ListMembers(ctx, scope.org.ID)
		if err != nil {
			return Usage{}, fmt.Errorf("list members: %w", err)
		}
		for _, member := range members {
			email := member.Email
			if member.InstanceAdmin != 0 && !scope.p.InstanceAdmin {
				email = "operator"
			}
			emails[member.Membership.ID] = email
		}
	}

	buckets := make(map[time.Time]*UsageBucket)
	order := make([]time.Time, 0)
	for at := start; !at.After(end); at = at.Add(step) {
		bucket := &UsageBucket{Start: at, Regions: map[string]UsageRegion{}}
		if breakdown {
			bucket.Members = map[string]UsageAmounts{}
		}
		buckets[at] = bucket
		order = append(order, at)
	}
	var totals UsageAmounts
	for _, row := range rows {
		at := row.Hour.UTC().Truncate(step)
		bucket, ok := buckets[at]
		if !ok {
			continue
		}
		sandboxHours := float64(row.SandboxSeconds) / 3600
		vcpuHours := row.VcpuSeconds / 3600
		memoryGiBHours := float64(row.MemoryMbSeconds) / 1024 / 3600
		bucket.SandboxHours += sandboxHours
		bucket.VcpuHours += vcpuHours
		bucket.MemoryGiBHours += memoryGiBHours
		if row.PeakSandboxes > bucket.PeakSandboxes {
			bucket.PeakSandboxes = row.PeakSandboxes
		}
		slug := regionSlug[row.ClusterID]
		if slug != "" {
			region := bucket.Regions[slug]
			region.VcpuHours += vcpuHours
			bucket.Regions[slug] = region
		}
		if breakdown && row.MembershipID != "" {
			if email, ok := emails[row.MembershipID]; ok {
				member := bucket.Members[email]
				member.SandboxHours += sandboxHours
				member.VcpuHours += vcpuHours
				member.MemoryGiBHours += memoryGiBHours
				bucket.Members[email] = member
			}
		}
		totals.SandboxHours += sandboxHours
		totals.VcpuHours += vcpuHours
		totals.MemoryGiBHours += memoryGiBHours
	}

	out := Usage{
		From:        from.UTC(),
		To:          to.UTC(),
		Granularity: granularity,
		Totals:      roundAmounts(totals),
		Data:        make([]UsageBucket, 0, len(order)),
	}
	for _, at := range order {
		bucket := buckets[at]
		bucket.SandboxHours = round3(bucket.SandboxHours)
		bucket.VcpuHours = round3(bucket.VcpuHours)
		bucket.MemoryGiBHours = round3(bucket.MemoryGiBHours)
		for slug, region := range bucket.Regions {
			region.VcpuHours = round3(region.VcpuHours)
			bucket.Regions[slug] = region
		}
		for email, member := range bucket.Members {
			bucket.Members[email] = roundAmounts(member)
		}
		out.Data = append(out.Data, *bucket)
	}
	return out, nil
}

// LimitsUsed is live use, counted from the mirror like AerolVM counts a
// team's sandboxes: stopped ones still count.
type LimitsUsed struct {
	Sandboxes int64   `json:"sandboxes"`
	Vcpus     float64 `json:"vcpus"`
	MemoryMB  int64   `json:"memory_mb"`
}

// RegionLimits is one subscribed region's plan, live use and each member's
// share.
type RegionLimits struct {
	Slug    string         `json:"slug"`
	Name    string         `json:"name"`
	Plan    PlanRef        `json:"plan"`
	Limits  Limits         `json:"limits"`
	Used    LimitsUsed     `json:"used"`
	Members []MemberLimits `json:"members"`
}

// LimitsReport is returned by Limits: one entry per subscribed region.
type LimitsReport struct {
	Regions []RegionLimits `json:"regions"`
}

// Limits returns an organization's subscribed regions with their plan, live
// use and member shares. A member sees only their own row.
func (s *Service) Limits(ctx context.Context, orgRef string) (LimitsReport, error) {
	c, err := s.orgFor(ctx, orgRef, accessRead)
	if err != nil {
		return LimitsReport{}, err
	}
	data, err := s.quotaData(ctx, c.org)
	if err != nil {
		return LimitsReport{}, err
	}
	rows, err := s.q.SumSandboxesByOrg(ctx, sql.NullString{String: c.org.ID, Valid: true})
	if err != nil {
		return LimitsReport{}, fmt.Errorf("sum sandboxes: %w", err)
	}
	usedByCluster := make(map[string]LimitsUsed, len(rows))
	for _, row := range rows {
		usedByCluster[row.ClusterID] = LimitsUsed{Sandboxes: row.Sandboxes, Vcpus: row.Vcpus, MemoryMB: row.MemoryMb}
	}
	clusters, err := s.q.ListClusters(ctx)
	if err != nil {
		return LimitsReport{}, fmt.Errorf("list clusters: %w", err)
	}
	limited := !c.p.InstanceAdmin && c.p.Kind == "user" && c.role != roleOwner && c.role != roleAdmin
	var ownMembership string
	if limited {
		m, err := s.q.GetMembership(ctx, db.GetMembershipParams{OrgID: c.org.ID, UserID: c.p.UserID})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return LimitsReport{}, fmt.Errorf("get membership: %w", err)
		}
		if err == nil {
			ownMembership = m.ID
		}
	}
	out := LimitsReport{Regions: make([]RegionLimits, 0, len(data.subs))}
	for _, cluster := range clusters {
		limits, ok := data.limitsFor(cluster.ID)
		if !ok {
			continue
		}
		region := RegionLimits{
			Slug:    cluster.Slug,
			Name:    cluster.Name,
			Plan:    data.planRef(cluster.ID),
			Limits:  limits,
			Used:    usedByCluster[cluster.ID],
			Members: make([]MemberLimits, 0, len(data.members)),
		}
		for _, member := range data.members {
			if limited && member.Membership.ID != ownMembership {
				continue
			}
			region.Members = append(region.Members, data.memberLimits(cluster, member.Membership.ID, c.p.InstanceAdmin))
		}
		sort.SliceStable(region.Members, func(i, j int) bool { return region.Members[i].Email < region.Members[j].Email })
		out.Regions = append(out.Regions, region)
	}
	return out, nil
}

// planRef is the plan on one subscribed region.
func (d quotaData) planRef(clusterID string) PlanRef {
	sub, ok := d.subs[clusterID]
	if !ok {
		return PlanRef{}
	}
	plan, ok := d.plans[sub.PlanID]
	if !ok {
		return PlanRef{}
	}
	return PlanRef{Slug: plan.Slug, Name: plan.Name}
}

// parseUsageTime accepts an RFC3339 timestamp or a date, which is midnight
// UTC.
func parseUsageTime(raw string, def time.Time) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("use a date (2026-11-01) or an RFC3339 timestamp")
}

func roundAmounts(a UsageAmounts) UsageAmounts {
	return UsageAmounts{
		SandboxHours:   round3(a.SandboxHours),
		VcpuHours:      round3(a.VcpuHours),
		MemoryGiBHours: round3(a.MemoryGiBHours),
	}
}

func round3(f float64) float64 {
	return math.Round(f*1000) / 1000
}
