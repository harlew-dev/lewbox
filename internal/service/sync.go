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
	quotaPushTimeout   = 5 * time.Second
	quotaPushRetry     = 3 * time.Second
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
		mu     sync.Mutex
		errs   []error
		failed = make(map[string]bool)
		wg     sync.WaitGroup
		sem    = make(chan struct{}, syncConcurrency)
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
				failed[row.ID] = true
				mu.Unlock()
			}
		}(row)
	}
	wg.Wait()

	for _, org := range orgs {
		if err := s.syncOrganizationQuotas(ctx, org, failed); err != nil {
			s.log.Warn("quota sync failed", "organization", org.Slug, "err", err)
		}
	}
	for _, org := range orgs {
		if org.Status == orgSuspended {
			s.stopSandboxes(ctx, org, nil)
			continue
		}
		subs, err := s.subscriptionsByCluster(ctx, org.ID)
		if err != nil {
			s.log.Warn("list subscriptions failed", "organization", org.Slug, "err", err)
			continue
		}
		if len(subs) == len(clusters) {
			continue
		}
		unsubscribed := make(map[string]bool, len(clusters)-len(subs))
		for _, cluster := range clusters {
			if _, ok := subs[cluster.ID]; !ok {
				unsubscribed[cluster.ID] = true
			}
		}
		s.stopSandboxes(ctx, org, unsubscribed)
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
		if apiErr, ok := errors.AsType[*aerolvm.APIError](err); !ok || apiErr.Status != 503 || !strings.Contains(apiErr.Message, "not enabled") {
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

// syncOrganizationQuotas pushes one organization's quota on every cluster it
// is subscribed to or has used. Clusters in skip are left until the next pass.
// Errors are recorded per cluster and never fail the caller.
func (s *Service) syncOrganizationQuotas(ctx context.Context, org db.Organization, skip map[string]bool) error {
	if !org.Team.Valid || org.Team.String == "" {
		return nil
	}
	clusters, err := s.q.ListClusters(ctx)
	if err != nil {
		return fmt.Errorf("list clusters: %w", err)
	}
	subs, err := s.subscriptionsByCluster(ctx, org.ID)
	if err != nil {
		return err
	}
	plans, err := s.planRows(ctx)
	if err != nil {
		return err
	}
	rows, err := s.q.SumSandboxesByOrg(ctx, sql.NullString{String: org.ID, Valid: true})
	if err != nil {
		return fmt.Errorf("sum sandboxes: %w", err)
	}
	used := make(map[string]bool, len(rows))
	for _, row := range rows {
		used[row.ClusterID] = true
	}
	keys, err := s.q.ListLiveSandboxKeysByOrg(ctx, org.ID)
	if err != nil {
		return fmt.Errorf("list sandbox keys: %w", err)
	}
	keyed := make(map[string]bool, len(keys))
	for _, key := range keys {
		keyed[key.ClusterID] = true
	}
	states, err := s.q.ListQuotaStates(ctx, org.ID)
	if err != nil {
		return fmt.Errorf("list quota state: %w", err)
	}
	stateByCluster := make(map[string]db.QuotaState, len(states))
	for _, state := range states {
		stateByCluster[state.ClusterID] = state
	}

	type push struct {
		cluster   db.Cluster
		quota     aerolvm.TeamQuota
		remove    bool
		state     db.QuotaState
		haveState bool
	}
	pushes := make([]push, 0, len(clusters))
	for _, cluster := range clusters {
		if skip[cluster.ID] {
			continue
		}
		sub, subscribed := subs[cluster.ID]
		state, haveState := stateByCluster[cluster.ID]
		if !subscribed && !used[cluster.ID] && !keyed[cluster.ID] && !haveState {
			continue
		}
		limits := Limits{}
		if subscribed {
			plan, ok := plans[sub.PlanID]
			if !ok {
				continue
			}
			limits = effectiveLimits(plan, sub.MaxSandboxes, sub.MaxVcpus, sub.MaxMemoryMb)
		}
		quota, remove := subscriptionQuota(org.Status, subscribed, limits)
		pushes = append(pushes, push{cluster: cluster, quota: quota, remove: remove, state: state, haveState: haveState})
	}

	now := s.now()
	var wg sync.WaitGroup
	sem := make(chan struct{}, syncConcurrency)
	for _, p := range pushes {
		wg.Add(1)
		sem <- struct{}{}
		go func(p push) {
			defer wg.Done()
			defer func() { <-sem }()
			s.pushQuota(ctx, org, p.cluster, p.quota, p.remove, p.state, p.haveState, now, quotaPushRetry)
		}(p)
	}
	wg.Wait()
	return nil
}

// subscriptionQuota is the quota an organization should have on one cluster.
// A region the organization is not subscribed to, or an organization that is
// not active, is blocked; all three limits unlimited means no quota.
func subscriptionQuota(status string, subscribed bool, limits Limits) (aerolvm.TeamQuota, bool) {
	if !subscribed || status != orgActive {
		return blockingQuota(), false
	}
	if limits.Sandboxes == nil && limits.Vcpus == nil && limits.MemoryMB == nil {
		return aerolvm.TeamQuota{}, true
	}
	quota := aerolvm.TeamQuota{}
	if limits.Sandboxes != nil {
		quota.MaxSandboxes = int(*limits.Sandboxes)
	}
	if limits.Vcpus != nil {
		quota.MaxCPU = float64(*limits.Vcpus)
	}
	if limits.MemoryMB != nil {
		quota.MaxMemoryMB = int(*limits.MemoryMB)
	}
	return quota, false
}

func blockingQuota() aerolvm.TeamQuota {
	return aerolvm.TeamQuota{MaxCPU: blockingCPU, MaxMemoryMB: blockingMemoryMB}
}

func isBlockingQuota(q aerolvm.TeamQuota) bool {
	return q.MaxCPU > 0 && q.MaxCPU <= blockingCPU
}

// quotaDesired is what quota_state stores for a push: the JSON body, or "none"
// when there is no quota.
func quotaDesired(quota aerolvm.TeamQuota, remove bool) string {
	if remove {
		return "none"
	}
	data, err := json.Marshal(quota)
	if err != nil {
		return ""
	}
	return string(data)
}

// pushQuota sends a quota only when it changed, the last push failed, or it
// is old enough to refresh. retry bounds the 503 retry budget; it is zero in
// a request and a few seconds in the sync loop.
func (s *Service) pushQuota(ctx context.Context, org db.Organization, cluster db.Cluster, quota aerolvm.TeamQuota, remove bool, state db.QuotaState, haveState bool, now time.Time, retry time.Duration) {
	desired := quotaDesired(quota, remove)
	if haveState && state.Error == "" && state.Quota == desired && now.Sub(state.PushedAt) < quotaPushEvery {
		return
	}

	client, err := s.quotaClient(cluster, retry)
	if err == nil {
		pushCtx, cancel := context.WithTimeout(ctx, quotaPushTimeout)
		defer cancel()
		if remove {
			err = client.DeleteTeamQuota(pushCtx, org.Team.String)
		} else {
			err = client.PutTeamQuota(pushCtx, org.Team.String, quota)
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

// stopSandboxes stops an organization's running sandboxes, restricted to
// only's clusters when it is not nil.
func (s *Service) stopSandboxes(ctx context.Context, org db.Organization, only map[string]bool) {
	rows, err := s.q.ListStartedSandboxesByOrg(ctx, sql.NullString{String: org.ID, Valid: true})
	if err != nil {
		s.log.Warn("list started sandboxes failed", "organization", org.Slug, "err", err)
		return
	}
	for _, row := range rows {
		if only != nil && !only[row.Sandbox.ClusterID] {
			continue
		}
		cluster, err := s.q.GetClusterBySlug(ctx, row.ClusterSlug)
		if err != nil {
			s.log.Warn("find sandbox cluster failed", "sandbox", row.Sandbox.ID, "err", err)
			continue
		}
		client, err := s.clusterClient(cluster)
		if err == nil {
			callCtx, cancel := lifecycleContext(ctx)
			err = client.StopSandbox(callCtx, row.Sandbox.ID)
			cancel()
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

// syncPlanQuotas pushes the new limits to every subscription on a plan.
func (s *Service) syncPlanQuotas(ctx context.Context, planID string) error {
	subs, err := s.q.ListSubscriptionsByPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("list subscriptions: %w", err)
	}
	var errs []error
	seen := make(map[string]bool, len(subs))
	for _, sub := range subs {
		if seen[sub.OrgID] {
			continue
		}
		seen[sub.OrgID] = true
		org, err := s.q.GetOrganizationByID(ctx, sub.OrgID)
		if err != nil {
			errs = append(errs, fmt.Errorf("get organization %q: %w", sub.OrgID, err))
			continue
		}
		if err := s.syncOrganizationQuotas(ctx, org, nil); err != nil {
			errs = append(errs, fmt.Errorf("sync %q: %w", org.Slug, err))
		}
	}
	return errors.Join(errs...)
}
