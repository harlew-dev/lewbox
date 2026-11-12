package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/db"
)

const (
	syncClusterTimeout = 20 * time.Second
	quotaPushEvery     = 10 * time.Minute
	blockingCPU        = 0.001
	blockingMemoryMB   = 1
	syncConcurrency    = 4
)

// Sync mirrors every cluster's sandboxes, meters usage and pushes limits. It
// is the orchestrator's background pass, so it runs as the local principal
// when the caller has none.
func (s *Service) Sync(ctx context.Context) error {
	if _, ok := PrincipalFrom(ctx); ok {
		if _, err := requireAdmin(ctx); err != nil {
			return err
		}
	} else {
		ctx = WithPrincipal(ctx, LocalPrincipal())
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	now := s.now()
	clusters, err := s.q.ListClusters(ctx)
	if err != nil {
		return fmt.Errorf("list clusters: %w", err)
	}
	orgs, err := s.q.ListOrganizations(ctx)
	if err != nil {
		return fmt.Errorf("list organizations: %w", err)
	}
	orgByTeam := make(map[string]string, len(orgs))
	for _, org := range orgs {
		if org.Team.Valid && org.Team.String != "" {
			orgByTeam[org.Team.String] = org.ID
		}
	}

	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
		sem  = make(chan struct{}, syncConcurrency)
	)
	for _, row := range clusters {
		if !row.CheckedAt.Valid {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(row db.Cluster) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := s.syncCluster(ctx, row, orgByTeam, now); err != nil {
				s.log.Warn("cluster sync failed", "cluster", row.Slug, "err", err)
				mu.Lock()
				errs = append(errs, fmt.Errorf("sync cluster %q: %w", row.Slug, err))
				mu.Unlock()
			}
		}(row)
	}
	wg.Wait()

	for _, org := range orgs {
		if err := s.syncOrganizationQuotas(ctx, org); err != nil {
			s.log.Warn("quota sync failed", "organization", org.Slug, "err", err)
		}
	}
	for _, org := range orgs {
		if org.Status == orgSuspended && (org.StatusReason == statusReasonAdmin || org.StatusReason == "credits") {
			s.stopOrganizationSandboxes(ctx, org)
		}
	}
	return errors.Join(errs...)
}

// syncCluster reads one cluster's list and index, updates the mirror and
// records the usage since the last pass.
func (s *Service) syncCluster(ctx context.Context, row db.Cluster, orgByTeam map[string]string, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, syncClusterTimeout)
	defer cancel()

	client, err := s.clusterClient(row)
	if err != nil {
		s.recordSyncError(ctx, row.ID, err)
		return err
	}
	index, err := client.SandboxIndex(ctx)
	if err != nil {
		var apiErr *aerolvm.APIError
		if !errors.As(err, &apiErr) || apiErr.Status != 503 || !strings.Contains(apiErr.Message, "not enabled") {
			s.recordSyncError(ctx, row.ID, err)
			return err
		}
		// A single-node install has no cluster index, so nothing is
		// attributable there (DECISIONS D8).
		index = nil
	}
	list, partial, err := client.ListSandboxes(ctx)
	if err != nil {
		s.recordSyncError(ctx, row.ID, err)
		return err
	}
	placements := make(map[string]aerolvm.Placement, len(index))
	for _, p := range index {
		placements[p.SandboxID] = p
	}

	var (
		seen    = make(map[string]bool, len(list))
		running = make(map[string]int64)
		vcpus   = make(map[string]float64)
		memory  = make(map[string]float64)
	)
	for _, sandbox := range list {
		placement := placements[sandbox.ID]
		team := strings.TrimPrefix(placement.OwnerRef, "team:")
		orgID := sql.NullString{}
		if id, ok := orgByTeam[team]; ok {
			orgID = sql.NullString{String: id, Valid: true}
		}
		createdAt := sandbox.CreatedAt
		if createdAt.IsZero() {
			createdAt = now
		}
		seen[sandbox.ID] = true
		if err := s.q.UpsertSandbox(ctx, db.UpsertSandboxParams{
			ClusterID:    row.ID,
			ID:           sandbox.ID,
			OrgID:        orgID,
			Team:         team,
			Node:         placement.OwnerNodeID,
			Name:         sandbox.Name,
			Image:        sandbox.Image,
			Status:       sandbox.Status,
			StatusReason: sandbox.StatusReason,
			Runtime:      sandbox.Runtime,
			Cpu:          sandbox.CPU,
			MemoryMb:     sandbox.MemoryMB,
			DiskGb:       sandbox.DiskGB,
			CreatedAt:    createdAt,
			LastSeenAt:   now,
		}); err != nil {
			return fmt.Errorf("upsert sandbox %q: %w", sandbox.ID, err)
		}
		if sandbox.Status != "started" {
			continue
		}
		running[orgID.String]++
		vcpus[orgID.String] += sandbox.CPU
		memory[orgID.String] += float64(sandbox.MemoryMB)
	}

	if !partial {
		if err := s.q.MarkSandboxesGone(ctx, db.MarkSandboxesGoneParams{
			GoneAt:     sql.NullTime{Time: now, Valid: true},
			ClusterID:  row.ID,
			LastSeenAt: now,
		}); err != nil {
			return fmt.Errorf("mark sandboxes gone: %w", err)
		}
	}

	dt := time.Duration(0)
	if row.SyncedAt.Valid {
		dt = now.Sub(row.SyncedAt.Time)
		if dt < 0 {
			dt = 0
		}
		if limit := 2 * s.syncInterval; dt > limit {
			dt = limit
		}
	}
	hour := now.Truncate(time.Hour)
	for orgID, count := range running {
		if err := s.q.UpsertUsageHourly(ctx, db.UpsertUsageHourlyParams{
			OrgID:           orgID,
			ClusterID:       row.ID,
			Hour:            hour,
			SandboxSeconds:  int64(dt/time.Second) * count,
			VcpuSeconds:     vcpus[orgID] * dt.Seconds(),
			MemoryMbSeconds: int64(memory[orgID] * dt.Seconds()),
			PeakSandboxes:   count,
		}); err != nil {
			return fmt.Errorf("record usage: %w", err)
		}
	}

	if err := s.q.SetClusterSynced(ctx, db.SetClusterSyncedParams{
		SyncedAt: sql.NullTime{Time: now, Valid: true},
		ID:       row.ID,
	}); err != nil {
		return fmt.Errorf("set synced_at: %w", err)
	}
	return nil
}

func (s *Service) recordSyncError(ctx context.Context, id string, err error) {
	if uerr := s.q.SetClusterSyncError(ctx, db.SetClusterSyncErrorParams{
		SyncError: err.Error(),
		ID:        id,
	}); uerr != nil {
		s.log.Warn("store sync error failed", "cluster", id, "err", uerr)
	}
}

// sandboxUse is one organization's or one region's share of the mirror.
type sandboxUse struct {
	sandboxes int64
	vcpus     float64
	memoryMB  int64
}

func (u *sandboxUse) add(n sandboxUse) {
	u.sandboxes += n.sandboxes
	u.vcpus += n.vcpus
	u.memoryMB += n.memoryMB
}

// syncOrganizationQuotas computes and pushes one organization's limits across
// every region. Errors are recorded per cluster and never fail the caller.
func (s *Service) syncOrganizationQuotas(ctx context.Context, org db.Organization) error {
	clusters, err := s.q.ListClusters(ctx)
	if err != nil {
		return fmt.Errorf("list clusters: %w", err)
	}
	plans, err := s.planRows(ctx)
	if err != nil {
		return err
	}
	limits := effectiveLimits(org, plans)
	rows, err := s.q.SumSandboxesByOrg(ctx, sql.NullString{String: org.ID, Valid: true})
	if err != nil {
		return fmt.Errorf("sum sandboxes: %w", err)
	}
	used := make(map[string]sandboxUse, len(rows))
	var total sandboxUse
	for _, row := range rows {
		u := sandboxUse{sandboxes: row.Sandboxes, vcpus: row.Vcpus, memoryMB: row.MemoryMb}
		used[row.ClusterID] = u
		total.add(u)
	}
	states, err := s.q.ListQuotaStates(ctx, org.ID)
	if err != nil {
		return fmt.Errorf("list quota state: %w", err)
	}
	stateByCluster := make(map[string]db.QuotaState, len(states))
	for _, state := range states {
		stateByCluster[state.ClusterID] = state
	}

	now := s.now()
	for _, cluster := range clusters {
		if !org.Team.Valid || org.Team.String == "" {
			continue
		}
		quota, remove := organizationQuota(org.Status, limits, used[cluster.ID], total)
		state, ok := stateByCluster[cluster.ID]
		if !ok {
			state = db.QuotaState{}
		}
		s.pushQuota(ctx, org, cluster, quota, remove, state, ok, now)
	}
	return nil
}

// organizationQuota is the quota an organization should have on one cluster.
// It returns remove for "no quota", which means every kind is unlimited.
func organizationQuota(status string, limits Limits, used, total sandboxUse) (aerolvm.TeamQuota, bool) {
	if status != orgActive {
		return blockingQuota(), false
	}
	limited := limits.Sandboxes != nil || limits.Vcpus != nil || limits.MemoryMB != nil
	if !limited {
		return aerolvm.TeamQuota{}, true
	}
	quota := aerolvm.TeamQuota{}
	blocked := false
	if limits.Sandboxes != nil {
		n := used.sandboxes + maxInt64(0, *limits.Sandboxes-total.sandboxes)
		if n == 0 {
			blocked = true
		}
		quota.MaxSandboxes = int(n)
	}
	if limits.Vcpus != nil {
		n := used.vcpus + maxFloat(0, float64(*limits.Vcpus)-total.vcpus)
		if n == 0 {
			blocked = true
		}
		quota.MaxCPU = n
	}
	if limits.MemoryMB != nil {
		n := used.memoryMB + maxInt64(0, *limits.MemoryMB-total.memoryMB)
		if n == 0 {
			blocked = true
		}
		quota.MaxMemoryMB = int(n)
	}
	if blocked {
		return blockingQuota(), false
	}
	return quota, false
}

func blockingQuota() aerolvm.TeamQuota {
	return aerolvm.TeamQuota{MaxCPU: blockingCPU, MaxMemoryMB: blockingMemoryMB}
}

func isBlockingQuota(q aerolvm.TeamQuota) bool {
	return q.MaxCPU > 0 && q.MaxCPU <= blockingCPU
}

// pushQuota sends a quota only when it changed, the last push failed, or it
// is old enough to refresh.
func (s *Service) pushQuota(ctx context.Context, org db.Organization, cluster db.Cluster, quota aerolvm.TeamQuota, remove bool, state db.QuotaState, haveState bool, now time.Time) {
	desired := "none"
	if !remove {
		data, err := json.Marshal(quota)
		if err != nil {
			return
		}
		desired = string(data)
	}
	if haveState && state.Error == "" && state.Quota == desired && now.Sub(state.PushedAt) < quotaPushEvery {
		return
	}

	client, err := s.clusterClient(cluster)
	if err == nil {
		if remove {
			err = client.DeleteTeamQuota(ctx, org.Team.String)
		} else {
			err = client.PutTeamQuota(ctx, org.Team.String, quota)
		}
	}
	errText := ""
	if err != nil {
		errText = err.Error()
		s.log.Warn("push quota failed", "cluster", cluster.Slug, "organization", org.Slug, "err", err)
	}
	if uerr := s.q.UpsertQuotaState(ctx, db.UpsertQuotaStateParams{
		OrgID:     org.ID,
		ClusterID: cluster.ID,
		Quota:     desired,
		PushedAt:  now,
		Error:     errText,
	}); uerr != nil {
		s.log.Warn("store quota state failed", "cluster", cluster.Slug, "organization", org.Slug, "err", uerr)
	}
}

// stopOrganizationSandboxes stops every running sandbox of a suspended
// organization, so it stops accruing compute.
func (s *Service) stopOrganizationSandboxes(ctx context.Context, org db.Organization) {
	rows, err := s.q.ListStartedSandboxesByOrg(ctx, sql.NullString{String: org.ID, Valid: true})
	if err != nil {
		s.log.Warn("list started sandboxes failed", "organization", org.Slug, "err", err)
		return
	}
	for _, row := range rows {
		cluster, err := s.findCluster(ctx, row.Sandbox.ClusterID)
		if err != nil {
			s.log.Warn("find sandbox cluster failed", "sandbox", row.Sandbox.ID, "err", err)
			continue
		}
		client, err := s.clusterClient(cluster)
		if err == nil {
			err = client.StopSandbox(ctx, row.Sandbox.ID)
		}
		if err != nil {
			s.log.Warn("stop sandbox failed", "sandbox", row.Sandbox.ID, "err", err)
			continue
		}
		if err := s.q.UpdateSandboxStatus(ctx, db.UpdateSandboxStatusParams{
			Status:    "stopped",
			ClusterID: row.Sandbox.ClusterID,
			ID:        row.Sandbox.ID,
		}); err != nil {
			s.log.Warn("update sandbox status failed", "sandbox", row.Sandbox.ID, "err", err)
		}
	}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// syncPlanQuotas pushes the new limits to every organization on a plan.
func (s *Service) syncPlanQuotas(ctx context.Context, planID string) error {
	orgs, err := s.q.ListOrganizations(ctx)
	if err != nil {
		return fmt.Errorf("list organizations: %w", err)
	}
	var errs []error
	for _, org := range orgs {
		if !org.PlanID.Valid || org.PlanID.String != planID {
			continue
		}
		if err := s.syncOrganizationQuotas(ctx, org); err != nil {
			errs = append(errs, fmt.Errorf("sync %q: %w", org.Slug, err))
		}
	}
	return errors.Join(errs...)
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
