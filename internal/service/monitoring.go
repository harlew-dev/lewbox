package service

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/db"
)

const (
	defaultHealthDays = 30
	maxHealthDays     = 90

	// sampleInterval is the background check interval, used to turn missing
	// samples into an approximate number of minutes.
	sampleInterval  = time.Minute
	sampleRetention = 31 * 24 * time.Hour
	pruneEvery      = 24 * time.Hour
)

// UptimeDay is one UTC day of samples for a cluster or a node. Uptime is nil
// when no samples were taken that day.
type UptimeDay struct {
	Date        string   `json:"date"`
	Uptime      *float64 `json:"uptime"`
	Samples     int64    `json:"samples"`
	DownMinutes int64    `json:"down_minutes"`
}

// Uptime is a days series and the figure over all of them.
type Uptime struct {
	Uptime *float64    `json:"uptime"`
	Days   []UptimeDay `json:"days"`
}

// NodeHealth is one node's uptime.
type NodeHealth struct {
	Name   string      `json:"name"`
	Uptime *float64    `json:"uptime"`
	Days   []UptimeDay `json:"days"`
}

// ClusterHealth is returned by ClusterHealth.
type ClusterHealth struct {
	Cluster Uptime       `json:"cluster"`
	Nodes   []NodeHealth `json:"nodes"`
}

// NodeMetric is one bucket of a node's samples. Values are averages; Alive
// is the fraction of samples in the bucket where the node was alive.
type NodeMetric struct {
	Start         time.Time `json:"start"`
	Sandboxes     float64   `json:"sandboxes"`
	CpuCores      float64   `json:"cpu_cores"`
	CpuReserved   float64   `json:"cpu_reserved"`
	MemoryTotalMB float64   `json:"memory_total_mb"`
	MemoryUsedMB  float64   `json:"memory_used_mb"`
	DiskFreeGB    float64   `json:"disk_free_gb"`
	Alive         float64   `json:"alive"`
}

// NodeMetrics is one node's series.
type NodeMetrics struct {
	Name string       `json:"name"`
	Data []NodeMetric `json:"data"`
}

// ClusterMetrics is returned by ClusterMetrics.
type ClusterMetrics struct {
	Range         string        `json:"range"`
	BucketSeconds int64         `json:"bucket_seconds"`
	Nodes         []NodeMetrics `json:"nodes"`
}

// OverviewCustomers counts organizations by status.
type OverviewCustomers struct {
	Total     int64 `json:"total"`
	Pending   int64 `json:"pending"`
	Suspended int64 `json:"suspended"`
}

// OverviewSandboxes counts mirrored sandboxes.
type OverviewSandboxes struct {
	Running int64 `json:"running"`
	Stopped int64 `json:"stopped"`
}

// OverviewCapacity is the fleet's latest capacity.
type OverviewCapacity struct {
	NodesAlive    int64   `json:"nodes_alive"`
	Nodes         int64   `json:"nodes"`
	CpuCores      float64 `json:"cpu_cores"`
	CpuReserved   float64 `json:"cpu_reserved"`
	MemoryTotalMB int64   `json:"memory_total_mb"`
	MemoryUsedMB  int64   `json:"memory_used_mb"`
}

// OverviewCluster is one cluster's status and uptime.
type OverviewCluster struct {
	Slug   string      `json:"slug"`
	Name   string      `json:"name"`
	Status string      `json:"status"`
	Uptime *float64    `json:"uptime"`
	Days   []UptimeDay `json:"days"`
}

// RunningPoint is the average number of sandboxes running in one hour.
type RunningPoint struct {
	Start     time.Time `json:"start"`
	Sandboxes float64   `json:"sandboxes"`
}

// TopCustomer is one organization's vCPU-hours this calendar month.
type TopCustomer struct {
	Slug      string  `json:"slug"`
	Name      string  `json:"name"`
	VcpuHours float64 `json:"vcpu_hours"`
}

// Overview is the console home.
type Overview struct {
	Customers    OverviewCustomers `json:"customers"`
	Sandboxes    OverviewSandboxes `json:"sandboxes"`
	Capacity     OverviewCapacity  `json:"capacity"`
	Clusters     []OverviewCluster `json:"clusters"`
	Running      []RunningPoint    `json:"running"`
	TopCustomers []TopCustomer     `json:"top_customers"`
}

// ClusterHealth returns uptime per UTC day for a cluster and each of its
// nodes, over the last days days.
func (s *Service) ClusterHealth(ctx context.Context, ref string, days int) (ClusterHealth, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return ClusterHealth{}, err
	}
	days, err := healthDays(days)
	if err != nil {
		return ClusterHealth{}, err
	}
	row, err := s.findCluster(ctx, ref)
	if err != nil {
		return ClusterHealth{}, err
	}
	start, end := dayRange(s.now(), days)
	samples, err := s.q.ListClusterSamples(ctx, db.ListClusterSamplesParams{
		ClusterID: row.ID,
		At:        start,
		At_2:      end,
	})
	if err != nil {
		return ClusterHealth{}, fmt.Errorf("list cluster samples: %w", err)
	}
	counts := newDayCounts(days)
	for _, sample := range samples {
		addDaySample(counts, start, sample.At, sample.Status == "ready" || sample.Status == "degraded")
	}
	clusterDays, clusterUptime := finishDays(counts, start)

	nodes, err := s.q.ListNodesByCluster(ctx, sql.NullString{String: row.ID, Valid: true})
	if err != nil {
		return ClusterHealth{}, fmt.Errorf("list cluster nodes: %w", err)
	}
	nodeSamples, err := s.q.ListNodeSamples(ctx, db.ListNodeSamplesParams{
		ClusterID: sql.NullString{String: row.ID, Valid: true},
		At:        start,
		At_2:      end,
	})
	if err != nil {
		return ClusterHealth{}, fmt.Errorf("list node samples: %w", err)
	}
	byNode := make(map[string][]db.NodeSample, len(nodes))
	for _, sample := range nodeSamples {
		byNode[sample.NodeID] = append(byNode[sample.NodeID], sample)
	}

	out := ClusterHealth{
		Cluster: Uptime{Uptime: clusterUptime, Days: clusterDays},
		Nodes:   make([]NodeHealth, 0, len(nodes)),
	}
	for _, node := range nodes {
		nodeCounts := newDayCounts(days)
		for _, sample := range byNode[node.ID] {
			addDaySample(nodeCounts, start, sample.At, sample.Alive != 0)
		}
		nodeDays, nodeUptime := finishDays(nodeCounts, start)
		out.Nodes = append(out.Nodes, NodeHealth{Name: node.Name, Uptime: nodeUptime, Days: nodeDays})
	}
	return out, nil
}

// ClusterMetrics returns each node's samples averaged into buckets over a
// range of 24h, 7d or 30d.
func (s *Service) ClusterMetrics(ctx context.Context, ref, rng string) (ClusterMetrics, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return ClusterMetrics{}, err
	}
	row, err := s.findCluster(ctx, ref)
	if err != nil {
		return ClusterMetrics{}, err
	}
	rng = strings.ToLower(strings.TrimSpace(rng))
	if rng == "" {
		rng = "24h"
	}
	var (
		span   time.Duration
		bucket int64
	)
	switch rng {
	case "24h":
		span, bucket = 24*time.Hour, 900
	case "7d":
		span, bucket = 7*24*time.Hour, 3600
	case "30d":
		span, bucket = 30*24*time.Hour, 6*3600
	default:
		return ClusterMetrics{}, invalid("range must be 24h, 7d or 30d")
	}

	now := s.now().UTC()
	start := now.Add(-span).Truncate(time.Duration(bucket) * time.Second)
	rows, err := s.q.ListNodeMetricBuckets(ctx, db.ListNodeMetricBucketsParams{
		BucketSeconds: bucket,
		ClusterID:     sql.NullString{String: row.ID, Valid: true},
		FromAt:        start,
		ToAt:          now.Add(time.Second),
	})
	if err != nil {
		return ClusterMetrics{}, fmt.Errorf("list node metrics: %w", err)
	}
	byNode := make(map[string][]NodeMetric, len(rows))
	for _, metric := range rows {
		used := metric.MemoryTotalMb.Float64 - metric.MemoryFreeMb.Float64
		if used < 0 {
			used = 0
		}
		byNode[metric.NodeID] = append(byNode[metric.NodeID], NodeMetric{
			Start:         time.Unix(metric.Bucket*bucket, 0).UTC(),
			Sandboxes:     round2(metric.Sandboxes.Float64),
			CpuCores:      round2(metric.CpuCores.Float64),
			CpuReserved:   round2(metric.CpuReserved.Float64),
			MemoryTotalMB: round2(metric.MemoryTotalMb.Float64),
			MemoryUsedMB:  round2(used),
			DiskFreeGB:    round2(metric.DiskFreeGb.Float64),
			Alive:         round3(metric.Alive.Float64),
		})
	}

	nodes, err := s.q.ListNodesByCluster(ctx, sql.NullString{String: row.ID, Valid: true})
	if err != nil {
		return ClusterMetrics{}, fmt.Errorf("list cluster nodes: %w", err)
	}
	out := ClusterMetrics{Range: rng, BucketSeconds: bucket, Nodes: make([]NodeMetrics, 0, len(nodes))}
	for _, node := range nodes {
		data := byNode[node.ID]
		if data == nil {
			data = []NodeMetric{}
		}
		out.Nodes = append(out.Nodes, NodeMetrics{Name: node.Name, Data: data})
	}
	return out, nil
}

// Overview returns the console home: counts, capacity, per-cluster uptime,
// the week's running sandboxes and the month's busiest customers.
func (s *Service) Overview(ctx context.Context) (Overview, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return Overview{}, err
	}
	now := s.now().UTC()

	statusCounts, err := s.q.CountOrganizationsByStatus(ctx)
	if err != nil {
		return Overview{}, fmt.Errorf("count organizations: %w", err)
	}
	var customers OverviewCustomers
	for _, row := range statusCounts {
		customers.Total += row.N
		switch row.Status {
		case orgPending:
			customers.Pending = row.N
		case orgSuspended:
			customers.Suspended = row.N
		}
	}

	sandboxCounts, err := s.q.CountSandboxesByStatus(ctx)
	if err != nil {
		return Overview{}, fmt.Errorf("count sandboxes: %w", err)
	}
	var sandboxes OverviewSandboxes
	for _, row := range sandboxCounts {
		switch row.Status {
		case "started":
			sandboxes.Running = row.N
		case "stopped":
			sandboxes.Stopped = row.N
		}
	}

	nodes, err := s.q.ListNodes(ctx)
	if err != nil {
		return Overview{}, fmt.Errorf("list nodes: %w", err)
	}
	latest, err := s.q.LatestNodeSamples(ctx)
	if err != nil {
		return Overview{}, fmt.Errorf("latest node samples: %w", err)
	}
	latestByNode := make(map[string]db.NodeSample, len(latest))
	for _, sample := range latest {
		latestByNode[sample.NodeID] = sample
	}
	capacity := OverviewCapacity{Nodes: int64(len(nodes))}
	var memoryFreeMB int64
	for _, node := range nodes {
		sample, ok := latestByNode[node.ID]
		if !ok {
			continue
		}
		if sample.Alive != 0 {
			capacity.NodesAlive++
		}
		capacity.CpuCores += float64(sample.CpuCores)
		capacity.CpuReserved += sample.CpuReserved
		capacity.MemoryTotalMB += sample.MemoryTotalMb
		memoryFreeMB += sample.MemoryFreeMb
	}
	capacity.MemoryUsedMB = capacity.MemoryTotalMB - memoryFreeMB
	if capacity.MemoryUsedMB < 0 {
		capacity.MemoryUsedMB = 0
	}
	capacity.CpuReserved = round2(capacity.CpuReserved)

	clusters, err := s.q.ListClusters(ctx)
	if err != nil {
		return Overview{}, fmt.Errorf("list clusters: %w", err)
	}
	out := Overview{
		Customers:    customers,
		Sandboxes:    sandboxes,
		Capacity:     capacity,
		Clusters:     make([]OverviewCluster, 0, len(clusters)),
		Running:      []RunningPoint{},
		TopCustomers: []TopCustomer{},
	}
	start, end := dayRange(now, defaultHealthDays)
	for _, cluster := range clusters {
		samples, err := s.q.ListClusterSamples(ctx, db.ListClusterSamplesParams{
			ClusterID: cluster.ID,
			At:        start,
			At_2:      end,
		})
		if err != nil {
			return Overview{}, fmt.Errorf("list cluster samples: %w", err)
		}
		counts := newDayCounts(defaultHealthDays)
		for _, sample := range samples {
			addDaySample(counts, start, sample.At, sample.Status == "ready" || sample.Status == "degraded")
		}
		days, uptime := finishDays(counts, start)
		out.Clusters = append(out.Clusters, OverviewCluster{
			Slug:   cluster.Slug,
			Name:   cluster.Name,
			Status: cluster.Status,
			Uptime: uptime,
			Days:   days,
		})
	}

	endHour := now.Truncate(time.Hour)
	startHour := endHour.Add(-(7*24 - 1) * time.Hour)
	usage, err := s.q.ListUsageHourly(ctx, db.ListUsageHourlyParams{
		OrgID:    "",
		FromHour: startHour,
		ToHour:   endHour,
	})
	if err != nil {
		return Overview{}, fmt.Errorf("list usage: %w", err)
	}
	perHour := make(map[time.Time]float64, len(usage))
	for _, row := range usage {
		perHour[row.Hour.UTC().Truncate(time.Hour)] += float64(row.SandboxSeconds) / 3600
	}
	for hour := startHour; !hour.After(endHour); hour = hour.Add(time.Hour) {
		out.Running = append(out.Running, RunningPoint{Start: hour, Sandboxes: round3(perHour[hour])})
	}

	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	vcpus, err := s.q.SumVcpuSecondsByOrg(ctx, monthStart)
	if err != nil {
		return Overview{}, fmt.Errorf("sum vcpu seconds: %w", err)
	}
	orgs, err := s.q.ListOrganizations(ctx)
	if err != nil {
		return Overview{}, fmt.Errorf("list organizations: %w", err)
	}
	hoursByOrg := make(map[string]float64, len(vcpus))
	for _, row := range vcpus {
		hoursByOrg[row.OrgID] = row.VcpuSeconds / 3600
	}
	for _, org := range orgs {
		hours := hoursByOrg[org.ID]
		if hours <= 0 {
			continue
		}
		out.TopCustomers = append(out.TopCustomers, TopCustomer{
			Slug:      org.Slug,
			Name:      org.Name,
			VcpuHours: round3(hours),
		})
	}
	sort.Slice(out.TopCustomers, func(i, j int) bool {
		if out.TopCustomers[i].VcpuHours != out.TopCustomers[j].VcpuHours {
			return out.TopCustomers[i].VcpuHours > out.TopCustomers[j].VcpuHours
		}
		return out.TopCustomers[i].Slug < out.TopCustomers[j].Slug
	})
	if len(out.TopCustomers) > 5 {
		out.TopCustomers = out.TopCustomers[:5]
	}
	return out, nil
}

// recordSamples stores one check's cluster and node rows, then prunes old
// samples once a day.
func (s *Service) recordSamples(ctx context.Context, clusterID, status string, latency time.Duration, healthOK, membersAvailable bool, members []aerolvm.Member, certExpiries map[string]time.Time) error {
	now := s.now()
	latencyMs := int64(-1)
	if healthOK {
		latencyMs = latency.Milliseconds()
		if latencyMs < 0 {
			latencyMs = 0
		}
	}
	if err := s.q.InsertClusterSample(ctx, db.InsertClusterSampleParams{
		ClusterID: clusterID,
		At:        now,
		Status:    status,
		LatencyMs: latencyMs,
	}); err != nil {
		return fmt.Errorf("insert cluster sample: %w", err)
	}

	byName := make(map[string]aerolvm.Member, len(members))
	for _, member := range members {
		byName[memberName(member)] = member
	}
	nodes, err := s.q.ListNodesByCluster(ctx, sql.NullString{String: clusterID, Valid: true})
	if err != nil {
		return fmt.Errorf("list cluster nodes: %w", err)
	}
	for _, node := range nodes {
		sample := db.InsertNodeSampleParams{NodeID: node.ID, At: now}
		switch {
		case !healthOK:
			// The cluster didn't answer, so none of its nodes can be up.
		case membersAvailable:
			member, ok := byName[node.Name]
			if !ok {
				continue
			}
			sample.Alive = boolInt(member.Alive)
			sample.Sandboxes = member.Capacity.SandboxesActive
			sample.CpuCores = member.Capacity.HostCPUCores
			sample.CpuReserved = member.Capacity.ReservedCPU
			sample.MemoryTotalMb = member.Capacity.HostMemoryTotalMB
			sample.MemoryFreeMb = member.Capacity.LiveMemoryFreeMB
			sample.DiskFreeGb = member.Capacity.HostDiskFreeGB
			sample.CanAdmit = boolInt(member.Capacity.CanAdmit)
			if at, ok := certExpiries[node.Name]; ok {
				sample.CertExpiresAt = sql.NullTime{Time: at, Valid: true}
			}
		default:
			// Reachable but the members call failed: nothing to say about
			// the nodes this pass.
			continue
		}
		if err := s.q.InsertNodeSample(ctx, sample); err != nil {
			return fmt.Errorf("insert node sample %q: %w", node.Name, err)
		}
	}
	return s.pruneSamples(ctx, now)
}

func (s *Service) pruneSamples(ctx context.Context, now time.Time) error {
	s.pruneMu.Lock()
	defer s.pruneMu.Unlock()
	if !s.lastPrune.IsZero() && now.Sub(s.lastPrune) < pruneEvery {
		return nil
	}
	cutoff := now.Add(-sampleRetention)
	if err := s.q.DeleteClusterSamplesBefore(ctx, cutoff); err != nil {
		return fmt.Errorf("prune cluster samples: %w", err)
	}
	if err := s.q.DeleteNodeSamplesBefore(ctx, cutoff); err != nil {
		return fmt.Errorf("prune node samples: %w", err)
	}
	s.lastPrune = now
	return nil
}

func healthDays(days int) (int, error) {
	if days == 0 {
		return defaultHealthDays, nil
	}
	if days < 1 || days > maxHealthDays {
		return 0, invalid("days must be between 1 and %d", maxHealthDays)
	}
	return days, nil
}

// dayRange returns the first day and the exclusive day after the last.
func dayRange(now time.Time, days int) (time.Time, time.Time) {
	today := now.UTC().Truncate(24 * time.Hour)
	start := today.AddDate(0, 0, -(days - 1))
	return start, today.AddDate(0, 0, 1)
}

type dayCount struct {
	samples int64
	ok      int64
}

func newDayCounts(days int) []dayCount {
	return make([]dayCount, days)
}

func addDaySample(counts []dayCount, start, at time.Time, ok bool) {
	index := int(at.UTC().Truncate(24*time.Hour).Sub(start) / (24 * time.Hour))
	if index < 0 || index >= len(counts) {
		return
	}
	counts[index].samples++
	if ok {
		counts[index].ok++
	}
}

func finishDays(counts []dayCount, start time.Time) ([]UptimeDay, *float64) {
	days := make([]UptimeDay, 0, len(counts))
	var samples, ok int64
	for i, count := range counts {
		samples += count.samples
		ok += count.ok
		down := (count.samples - count.ok) * int64(sampleInterval/time.Minute)
		days = append(days, UptimeDay{
			Date:        start.AddDate(0, 0, i).Format("2006-01-02"),
			Uptime:      ratio(count.ok, count.samples),
			Samples:     count.samples,
			DownMinutes: down,
		})
	}
	return days, ratio(ok, samples)
}

func ratio(ok, samples int64) *float64 {
	if samples == 0 {
		return nil
	}
	value := math.Round(float64(ok)/float64(samples)*1e6) / 1e6
	return &value
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}
