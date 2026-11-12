package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
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
	PeakSandboxes int64                  `json:"peak_sandboxes"`
	Regions       map[string]UsageRegion `json:"regions"`
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
// for an instance admin's whole fleet.
type UsageRequest struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Granularity string `json:"granularity"`
	Org         string `json:"org"`
}

// Usage returns metered usage per hour or day, zero-filled over the range.
func (s *Service) Usage(ctx context.Context, req UsageRequest) (Usage, error) {
	_, org, fleet, err := s.scopeOrg(ctx, req.Org)
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
	if !fleet && org != nil {
		orgID = org.ID
	}
	rows, err := s.q.ListUsageHourly(ctx, db.ListUsageHourlyParams{
		OrgID:    orgID,
		FromHour: start,
		ToHour:   end.Add(step),
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

	buckets := make(map[time.Time]*UsageBucket)
	order := make([]time.Time, 0)
	for at := start; !at.After(end); at = at.Add(step) {
		buckets[at] = &UsageBucket{Start: at, Regions: map[string]UsageRegion{}}
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

// RegionLimits is one region's use and the quota Lewbox last pushed there.
type RegionLimits struct {
	Slug     string             `json:"slug"`
	Used     LimitsUsed         `json:"used"`
	Quota    *aerolvm.TeamQuota `json:"quota"`
	Blocked  bool               `json:"blocked"`
	PushedAt *time.Time         `json:"pushed_at"`
	Error    string             `json:"error"`
}

// LimitsReport is returned by Limits: effective limits, live use and the
// per-region quotas.
type LimitsReport struct {
	Limits  Limits         `json:"limits"`
	Used    LimitsUsed     `json:"used"`
	Regions []RegionLimits `json:"regions"`
}

// Limits returns an organization's limits and live use, in total and per
// region.
func (s *Service) Limits(ctx context.Context, orgRef string) (LimitsReport, error) {
	c, err := s.orgFor(ctx, orgRef, accessRead)
	if err != nil {
		return LimitsReport{}, err
	}
	plans, err := s.planRows(ctx)
	if err != nil {
		return LimitsReport{}, err
	}
	rows, err := s.q.SumSandboxesByOrg(ctx, sql.NullString{String: c.org.ID, Valid: true})
	if err != nil {
		return LimitsReport{}, fmt.Errorf("sum sandboxes: %w", err)
	}
	usedByCluster := make(map[string]LimitsUsed, len(rows))
	var total LimitsUsed
	for _, row := range rows {
		used := LimitsUsed{Sandboxes: row.Sandboxes, Vcpus: row.Vcpus, MemoryMB: row.MemoryMb}
		usedByCluster[row.ClusterID] = used
		total.Sandboxes += used.Sandboxes
		total.Vcpus += used.Vcpus
		total.MemoryMB += used.MemoryMB
	}
	states, err := s.q.ListQuotaStates(ctx, c.org.ID)
	if err != nil {
		return LimitsReport{}, fmt.Errorf("list quota state: %w", err)
	}
	stateByCluster := make(map[string]db.QuotaState, len(states))
	for _, state := range states {
		stateByCluster[state.ClusterID] = state
	}
	clusters, err := s.q.ListClusters(ctx)
	if err != nil {
		return LimitsReport{}, fmt.Errorf("list clusters: %w", err)
	}
	out := LimitsReport{
		Limits:  effectiveLimits(c.org, plans),
		Used:    total,
		Regions: make([]RegionLimits, 0, len(clusters)),
	}
	for _, cluster := range clusters {
		region := RegionLimits{Slug: cluster.Slug, Used: usedByCluster[cluster.ID]}
		if state, ok := stateByCluster[cluster.ID]; ok {
			at := state.PushedAt.UTC()
			region.PushedAt = &at
			region.Error = state.Error
			if state.Quota != "none" {
				var quota aerolvm.TeamQuota
				if json.Unmarshal([]byte(state.Quota), &quota) == nil {
					region.Quota = &quota
					region.Blocked = isBlockingQuota(quota)
				}
			}
		}
		out.Regions = append(out.Regions, region)
	}
	return out, nil
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
	return time.Time{}, errors.New("use a date (2026-11-01) or an RFC3339 timestamp")
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
