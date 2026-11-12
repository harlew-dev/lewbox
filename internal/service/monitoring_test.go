package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/db"
	"uuid"
)

func TestCheckWritesSamples(t *testing.T) {
	clock := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	s := newClusterService(t, WithClock(func() time.Time { return clock }))
	fake := newClusterFake(t)
	fake.SetMembers([]aerolvm.Member{
		{
			NodeID: "node-a", NodeName: "node-a", APIURL: fake.URL(), Alive: true, Role: "mixed", Version: "v0.7.0",
			Capacity: aerolvm.Capacity{
				HostCPUCores: 4, HostMemoryTotalMB: 1000, LiveMemoryFreeMB: 400, HostDiskFreeGB: 10,
				ReservedCPU: 2, SandboxesActive: 3, CanAdmit: true, SupportedRuntimes: []string{"docker"},
			},
		},
		{
			NodeID: "node-b", NodeName: "node-b", APIURL: "http://node-b.test:21212", Alive: true, Role: "mixed", Version: "v0.7.0",
			Capacity: aerolvm.Capacity{
				HostCPUCores: 8, HostMemoryTotalMB: 2000, LiveMemoryFreeMB: 1000, HostDiskFreeGB: 20,
				ReservedCPU: 5, SandboxesActive: 1, CanAdmit: true, SupportedRuntimes: []string{"docker"},
			},
		},
	})
	detail := addTestCluster(t, s, fake, "eu-west")

	start, end := clock.Add(-time.Hour), clock.Add(time.Hour)
	samples, err := s.q.ListClusterSamples(context.Background(), db.ListClusterSamplesParams{ClusterID: detail.ID, At: start, At_2: end})
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 0 {
		t.Fatalf("AddCluster wrote %d samples", len(samples))
	}

	if _, err := s.CheckCluster(localCtx(), "eu-west"); err != nil {
		t.Fatal(err)
	}
	samples, err = s.q.ListClusterSamples(context.Background(), db.ListClusterSamplesParams{ClusterID: detail.ID, At: start, At_2: end})
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 1 || samples[0].Status != "ready" || samples[0].LatencyMs < 0 {
		t.Fatalf("cluster samples = %+v", samples)
	}
	nodeSamples := listAllNodeSamples(t, s, detail.ID)
	if len(nodeSamples) != 2 {
		t.Fatalf("node samples = %+v", nodeSamples)
	}
	byName := map[string]db.NodeSample{}
	for _, sample := range nodeSamples {
		byName[sample.NodeID] = sample
	}
	for _, node := range detail.NodeList {
		sample := byName[node.ID]
		if node.Name == "node-a" {
			if sample.Alive != 1 || sample.Sandboxes != 3 || sample.CpuCores != 4 || sample.CpuReserved != 2 ||
				sample.MemoryTotalMb != 1000 || sample.MemoryFreeMb != 400 || sample.DiskFreeGb != 10 || sample.CanAdmit != 1 {
				t.Errorf("node-a sample = %+v", sample)
			}
		}
	}

	// An unreachable cluster writes its known nodes as down.
	fake.Stop()
	clock = clock.Add(time.Minute)
	if _, err := s.CheckCluster(localCtx(), "eu-west"); err != nil {
		t.Fatal(err)
	}
	samples, _ = s.q.ListClusterSamples(context.Background(), db.ListClusterSamplesParams{ClusterID: detail.ID, At: start, At_2: end})
	if len(samples) != 2 || samples[1].Status != "unreachable" || samples[1].LatencyMs != -1 {
		t.Fatalf("after stop = %+v", samples)
	}
	nodeSamples = listAllNodeSamples(t, s, detail.ID)
	if len(nodeSamples) != 4 {
		t.Fatalf("node samples after stop = %+v", nodeSamples)
	}
	latest, err := s.q.LatestNodeSamples(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range latest {
		if sample.Alive != 0 {
			t.Errorf("latest sample is alive: %+v", sample)
		}
	}
}

func TestClusterHealth(t *testing.T) {
	clock := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	s := newClusterService(t, WithClock(func() time.Time { return clock }))
	fake := newClusterFake(t)
	setMembers(fake, "node-a", "node-b")
	detail := addTestCluster(t, s, fake, "eu-west")
	na, nb := detail.NodeList[0], detail.NodeList[1]

	insert := func(at time.Time, status string, aliveA, aliveB int64) {
		t.Helper()
		if err := s.q.InsertClusterSample(context.Background(), db.InsertClusterSampleParams{
			ClusterID: detail.ID, At: at, Status: status, LatencyMs: 1,
		}); err != nil {
			t.Fatal(err)
		}
		for id, alive := range map[string]int64{na.ID: aliveA, nb.ID: aliveB} {
			if err := s.q.InsertNodeSample(context.Background(), db.InsertNodeSampleParams{NodeID: id, At: at, Alive: alive}); err != nil {
				t.Fatal(err)
			}
		}
	}
	insert(time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC), "ready", 1, 1)
	insert(time.Date(2026, 11, 10, 11, 0, 0, 0, time.UTC), "ready", 1, 1)
	insert(time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC), "ready", 1, 1)
	insert(time.Date(2026, 11, 11, 11, 0, 0, 0, time.UTC), "unreachable", 0, 1)

	health, err := s.ClusterHealth(localCtx(), "eu-west", 3)
	if err != nil {
		t.Fatal(err)
	}
	if health.Cluster.Uptime == nil || *health.Cluster.Uptime != 0.75 {
		t.Fatalf("cluster uptime = %v", health.Cluster.Uptime)
	}
	if len(health.Cluster.Days) != 3 {
		t.Fatalf("days = %+v", health.Cluster.Days)
	}
	if got := health.Cluster.Days[0]; got.Date != "2026-11-10" || got.Uptime == nil || *got.Uptime != 1 || got.Samples != 2 || got.DownMinutes != 0 {
		t.Errorf("day 0 = %+v", got)
	}
	if got := health.Cluster.Days[1]; got.Uptime == nil || *got.Uptime != 0.5 || got.Samples != 2 || got.DownMinutes != 1 {
		t.Errorf("day 1 = %+v", got)
	}
	if got := health.Cluster.Days[2]; got.Uptime != nil || got.Samples != 0 {
		t.Errorf("no-data day = %+v", got)
	}
	var nodeA, nodeB NodeHealth
	for _, node := range health.Nodes {
		switch node.Name {
		case "node-a":
			nodeA = node
		case "node-b":
			nodeB = node
		}
	}
	if nodeA.Uptime == nil || *nodeA.Uptime != 0.75 {
		t.Errorf("node-a uptime = %v", nodeA.Uptime)
	}
	if nodeB.Uptime == nil || *nodeB.Uptime != 1 {
		t.Errorf("node-b uptime = %v", nodeB.Uptime)
	}

	if _, err := s.ClusterHealth(localCtx(), "eu-west", 91); err == nil {
		t.Error("days=91 was accepted")
	}
}

func TestClusterMetrics(t *testing.T) {
	clock := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	s := newClusterService(t, WithClock(func() time.Time { return clock }))
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	detail := addTestCluster(t, s, fake, "eu-west")
	node := detail.NodeList[0]

	insert := func(at time.Time, sandboxes, reserved float64, free float64, alive int64) {
		t.Helper()
		if err := s.q.InsertNodeSample(context.Background(), db.InsertNodeSampleParams{
			NodeID: node.ID, At: at, Alive: alive, Sandboxes: int64(sandboxes), CpuCores: 4,
			CpuReserved: reserved, MemoryTotalMb: 1000, MemoryFreeMb: int64(free), DiskFreeGb: int64(reserved) * 5,
		}); err != nil {
			t.Fatal(err)
		}
	}
	insert(time.Date(2026, 11, 12, 11, 1, 0, 0, time.UTC), 2, 1, 400, 1)
	insert(time.Date(2026, 11, 12, 11, 2, 0, 0, time.UTC), 4, 3, 200, 0)
	insert(time.Date(2026, 11, 12, 11, 16, 0, 0, time.UTC), 6, 2, 500, 1)

	metrics, err := s.ClusterMetrics(localCtx(), "eu-west", "24h")
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Range != "24h" || metrics.BucketSeconds != 900 || len(metrics.Nodes) != 1 {
		t.Fatalf("metrics = %+v", metrics)
	}
	data := metrics.Nodes[0].Data
	if len(data) != 2 {
		t.Fatalf("buckets = %+v", data)
	}
	first := data[0]
	if !first.Start.Equal(time.Date(2026, 11, 12, 11, 0, 0, 0, time.UTC)) ||
		first.Sandboxes != 3 || first.CpuReserved != 2 || first.MemoryUsedMB != 700 ||
		first.DiskFreeGB != 10 || first.Alive != 0.5 || first.CpuCores != 4 || first.MemoryTotalMB != 1000 {
		t.Errorf("first bucket = %+v", first)
	}
	if data[1].Sandboxes != 6 || data[1].Alive != 1 {
		t.Errorf("second bucket = %+v", data[1])
	}

	if _, err := s.ClusterMetrics(localCtx(), "eu-west", "1h"); err == nil {
		t.Error("range 1h was accepted")
	}
}

func TestSamplePruning(t *testing.T) {
	clock := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	s := newClusterService(t, WithClock(func() time.Time { return clock }))
	fake := newClusterFake(t)
	setMembers(fake, "node-a")
	detail := addTestCluster(t, s, fake, "eu-west")
	node := detail.NodeList[0]

	old := clock.Add(-32 * 24 * time.Hour)
	if err := s.q.InsertClusterSample(context.Background(), db.InsertClusterSampleParams{
		ClusterID: detail.ID, At: old, Status: "ready", LatencyMs: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.q.InsertNodeSample(context.Background(), db.InsertNodeSampleParams{NodeID: node.ID, At: old, Alive: 1}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.CheckCluster(localCtx(), "eu-west"); err != nil {
		t.Fatal(err)
	}
	samples, _ := s.q.ListClusterSamples(context.Background(), db.ListClusterSamplesParams{
		ClusterID: detail.ID, At: old.Add(-time.Hour), At_2: clock.Add(time.Hour),
	})
	for _, sample := range samples {
		if sample.At.Equal(old) {
			t.Error("an old cluster sample survived the prune")
		}
	}
	for _, sample := range listAllNodeSamples(t, s, detail.ID) {
		if sample.At.Equal(old) {
			t.Error("an old node sample survived the prune")
		}
	}

	// Pruning is once a day: a stale row written after it stays until tomorrow.
	if err := s.q.InsertClusterSample(context.Background(), db.InsertClusterSampleParams{
		ClusterID: detail.ID, At: old, Status: "ready", LatencyMs: 1,
	}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Minute)
	if _, err := s.CheckCluster(localCtx(), "eu-west"); err != nil {
		t.Fatal(err)
	}
	samples, _ = s.q.ListClusterSamples(context.Background(), db.ListClusterSamplesParams{
		ClusterID: detail.ID, At: old.Add(-time.Hour), At_2: clock.Add(time.Hour),
	})
	found := false
	for _, sample := range samples {
		found = found || sample.At.Equal(old)
	}
	if !found {
		t.Error("the second check pruned again")
	}
}

func TestOverview(t *testing.T) {
	clock := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	s := newClusterService(t, WithClock(func() time.Time { return clock }))
	fake := newClusterFake(t)
	detail := addTestCluster(t, s, fake, "eu-west")
	if _, err := s.CheckCluster(localCtx(), "eu-west"); err != nil {
		t.Fatal(err)
	}

	createUser(t, s, "owner@example.com", false)
	acme, err := s.CreateOrganization(localCtx(), CreateOrganizationRequest{Slug: "acme", Owner: "owner@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	team := teamOf(t, s, "acme", "owner@example.com")
	if _, err := s.SetSubscription(localCtx(), "acme", "eu-west", SetSubscriptionRequest{Plan: "free"}); err != nil {
		t.Fatal(err)
	}
	for _, org := range []struct{ slug, status string }{{"pending-co", orgPending}, {"suspended-co", orgSuspended}} {
		if _, err := s.q.CreateOrganization(context.Background(), db.CreateOrganizationParams{
			ID:        uuid.NewV7().String(),
			Slug:      org.slug,
			Name:      org.slug,
			Status:    org.status,
			CreatedAt: clock,
		}); err != nil {
			t.Fatal(err)
		}
	}

	fake.AddSandbox(team, aerolvm.Sandbox{ID: "sb-1", Status: "started", Runtime: "docker", CreatedAt: clock})
	fake.AddSandbox(team, aerolvm.Sandbox{ID: "sb-2", Status: "stopped", Runtime: "docker", CreatedAt: clock})
	if err := s.Sync(localCtx()); err != nil {
		t.Fatal(err)
	}

	hour := clock.Truncate(time.Hour)
	for org, row := range map[string]db.UpsertUsageHourlyParams{
		acme.ID:     {OrgID: acme.ID, ClusterID: detail.ID, Hour: hour, SandboxSeconds: 7200, VcpuSeconds: 12600},
		"other-org": {OrgID: "other-org", ClusterID: detail.ID, Hour: hour, SandboxSeconds: 3600, VcpuSeconds: 3600},
	} {
		row.OrgID = org
		if err := s.q.UpsertUsageHourly(context.Background(), row); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.q.CreateOrganization(context.Background(), db.CreateOrganizationParams{
		ID: "other-org", Slug: "other-co", Name: "Other Co", Status: orgActive, CreatedAt: clock,
	}); err != nil {
		t.Fatal(err)
	}

	overview, err := s.Overview(localCtx())
	if err != nil {
		t.Fatal(err)
	}
	if overview.Customers.Total != 4 || overview.Customers.Pending != 1 || overview.Customers.Suspended != 1 {
		t.Errorf("customers = %+v", overview.Customers)
	}
	if overview.Sandboxes.Running != 1 || overview.Sandboxes.Stopped != 1 {
		t.Errorf("sandboxes = %+v", overview.Sandboxes)
	}
	if overview.Capacity.Nodes != 1 || overview.Capacity.NodesAlive != 1 ||
		overview.Capacity.CpuCores != 4 || overview.Capacity.CpuReserved != 1 ||
		overview.Capacity.MemoryTotalMB != 15364 || overview.Capacity.MemoryUsedMB != 6812 {
		t.Errorf("capacity = %+v", overview.Capacity)
	}
	if len(overview.Clusters) != 1 || overview.Clusters[0].Slug != "eu-west" ||
		overview.Clusters[0].Uptime == nil || *overview.Clusters[0].Uptime != 1 {
		t.Errorf("clusters = %+v", overview.Clusters)
	}
	if len(overview.Running) != 168 {
		t.Fatalf("running has %d points", len(overview.Running))
	}
	if last := overview.Running[len(overview.Running)-1]; !last.Start.Equal(hour) || last.Sandboxes != 3 {
		t.Errorf("last running point = %+v", last)
	}
	if len(overview.TopCustomers) != 2 || overview.TopCustomers[0].Slug != "acme" || overview.TopCustomers[0].VcpuHours != 3.5 {
		t.Errorf("top customers = %+v", overview.TopCustomers)
	}
}

func TestMonitoringIsAdminOnly(t *testing.T) {
	s := newClusterService(t)
	ctx := WithPrincipal(context.Background(), Principal{Kind: "user", UserID: "u1", Email: "user@example.com"})
	if _, err := s.ClusterHealth(ctx, "eu-west", 30); err == nil {
		t.Error("a customer read cluster health")
	} else {
		wantCode(t, err, CodeForbidden)
	}
	if _, err := s.ClusterMetrics(ctx, "eu-west", "24h"); err == nil {
		t.Error("a customer read cluster metrics")
	} else {
		wantCode(t, err, CodeForbidden)
	}
	if _, err := s.Overview(ctx); err == nil {
		t.Error("a customer read the overview")
	} else {
		wantCode(t, err, CodeForbidden)
	}
}

func listAllNodeSamples(t *testing.T, s *Service, clusterID string) []db.NodeSample {
	t.Helper()
	rows, err := s.q.ListNodeSamples(context.Background(), db.ListNodeSamplesParams{
		ClusterID: sql.NullString{String: clusterID, Valid: true},
		At:        time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		At_2:      time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
