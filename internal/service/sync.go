package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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

// memberUsageKey identifies one metered member; an unattributed sandbox has
// both fields empty.
type memberUsageKey struct {
	orgID        string
	membershipID string
}

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
	memberships, err := s.q.ListAllMemberships(ctx)
	if err != nil {
		return fmt.Errorf("list memberships: %w", err)
	}
	memberByTeam := make(map[string]db.Membership, len(memberships))
	for _, membership := range memberships {
		memberByTeam[memberTeam(membership.ID)] = membership
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
			if err := s.syncCluster(ctx, row, memberByTeam, now); err != nil {
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
func (s *Service) syncCluster(ctx context.Context, row db.Cluster, memberByTeam map[string]db.Membership, now time.Time) error {
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
		running = make(map[memberUsageKey]int64)
		vcpus   = make(map[memberUsageKey]float64)
		memory  = make(map[memberUsageKey]float64)
	)
	for _, sandbox := range list {
		placement := placements[sandbox.ID]
		team := strings.TrimPrefix(placement.OwnerRef, "team:")
		orgID := sql.NullString{}
		membershipID := sql.NullString{}
		if membership, ok := memberByTeam[team]; ok {
			orgID = sql.NullString{String: membership.OrgID, Valid: true}
			membershipID = sql.NullString{String: membership.ID, Valid: true}
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
			MembershipID: membershipID,
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
		key := memberUsageKey{orgID: orgID.String, membershipID: membershipID.String}
		running[key]++
		vcpus[key] += sandbox.CPU
		memory[key] += float64(sandbox.MemoryMB)
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
	for key, count := range running {
		if err := s.q.UpsertUsageHourly(ctx, db.UpsertUsageHourlyParams{
			OrgID:           key.orgID,
			MembershipID:    key.membershipID,
			ClusterID:       row.ID,
			Hour:            hour,
			SandboxSeconds:  int64(dt/time.Second) * count,
			VcpuSeconds:     vcpus[key] * dt.Seconds(),
			MemoryMbSeconds: int64(memory[key] * dt.Seconds()),
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

// shareKey identifies one member's explicit share in one region.
type shareKey struct {
	membershipID string
	clusterID    string
}

// quotaData is everything needed to push one organization's member quotas.
type quotaData struct {
	org     db.Organization
	members []db.ListMembersRow
	shares  map[shareKey]db.MemberShare
	subs    map[string]db.Subscription
	plans   map[string]db.Plan
	used    map[string]map[string]bool // cluster -> membership
	keyed   map[string]map[string]bool
	states  map[string]map[string]db.QuotaState
	amounts map[string]map[string]LimitsUsed
}

func (d *quotaData) usedOn(clusterID, membershipID string) bool {
	if d.used[clusterID][membershipID] || d.keyed[clusterID][membershipID] {
		return true
	}
	_, ok := d.states[clusterID][membershipID]
	return ok
}

// quotaData loads one organization's members, shares, plan and use.
func (s *Service) quotaData(ctx context.Context, org db.Organization) (quotaData, error) {
	data := quotaData{
		org:     org,
		shares:  map[shareKey]db.MemberShare{},
		used:    map[string]map[string]bool{},
		keyed:   map[string]map[string]bool{},
		states:  map[string]map[string]db.QuotaState{},
		amounts: map[string]map[string]LimitsUsed{},
	}
	members, err := s.q.ListMembers(ctx, org.ID)
	if err != nil {
		return data, fmt.Errorf("list members: %w", err)
	}
	sort.SliceStable(members, func(i, j int) bool {
		if !members[i].Membership.CreatedAt.Equal(members[j].Membership.CreatedAt) {
			return members[i].Membership.CreatedAt.Before(members[j].Membership.CreatedAt)
		}
		return members[i].Membership.ID < members[j].Membership.ID
	})
	data.members = members

	shareRows, err := s.q.ListMemberShares(ctx, org.ID)
	if err != nil {
		return data, fmt.Errorf("list member shares: %w", err)
	}
	for _, row := range shareRows {
		data.shares[shareKey{row.MembershipID, row.ClusterID}] = row
	}
	if data.subs, err = s.subscriptionsByCluster(ctx, org.ID); err != nil {
		return data, err
	}
	if data.plans, err = s.planRows(ctx); err != nil {
		return data, err
	}
	useRows, err := s.q.SumSandboxesByOrgMembership(ctx, sql.NullString{String: org.ID, Valid: true})
	if err != nil {
		return data, fmt.Errorf("sum sandboxes: %w", err)
	}
	for _, row := range useRows {
		if row.MembershipID.Valid && row.MembershipID.String != "" {
			markCluster(data.used, row.ClusterID, row.MembershipID.String)
			if data.amounts[row.ClusterID] == nil {
				data.amounts[row.ClusterID] = map[string]LimitsUsed{}
			}
			data.amounts[row.ClusterID][row.MembershipID.String] = LimitsUsed{
				Sandboxes: row.Sandboxes,
				Vcpus:     row.Vcpus,
				MemoryMB:  row.MemoryMb,
			}
		}
	}
	keys, err := s.q.ListLiveSandboxKeysByOrg(ctx, org.ID)
	if err != nil {
		return data, fmt.Errorf("list sandbox keys: %w", err)
	}
	for _, key := range keys {
		if key.MembershipID.Valid && key.MembershipID.String != "" {
			markCluster(data.keyed, key.ClusterID, key.MembershipID.String)
		}
	}
	states, err := s.q.ListQuotaStates(ctx, org.ID)
	if err != nil {
		return data, fmt.Errorf("list quota state: %w", err)
	}
	for _, state := range states {
		if state.MembershipID == "" {
			continue
		}
		if data.states[state.ClusterID] == nil {
			data.states[state.ClusterID] = map[string]db.QuotaState{}
		}
		data.states[state.ClusterID][state.MembershipID] = state
	}
	return data, nil
}

func markCluster(m map[string]map[string]bool, clusterID, membershipID string) {
	if m[clusterID] == nil {
		m[clusterID] = map[string]bool{}
	}
	m[clusterID][membershipID] = true
}

// memberInputs builds the share inputs for one region.
func (d quotaData) memberInputs(clusterID string) []memberShareInput {
	inputs := make([]memberShareInput, 0, len(d.members))
	for _, member := range d.members {
		explicit := d.shares[shareKey{member.Membership.ID, clusterID}]
		inputs = append(inputs, memberShareInput{
			ID:        member.Membership.ID,
			CreatedAt: member.Membership.CreatedAt,
			Role:      member.Membership.Role,
			Explicit: Share{
				Sandboxes: nullInt(explicit.MaxSandboxes),
				Vcpus:     nullFloat(explicit.MaxVcpus),
				MemoryMB:  nullInt(explicit.MaxMemoryMb),
			},
		})
	}
	return inputs
}

// limitsFor is the plan's effective limits in one region.
func (d quotaData) limitsFor(clusterID string) (Limits, bool) {
	sub, ok := d.subs[clusterID]
	if !ok {
		return Limits{}, false
	}
	plan, ok := d.plans[sub.PlanID]
	if !ok {
		return Limits{}, false
	}
	return effectiveLimits(plan, sub.MaxSandboxes, sub.MaxVcpus, sub.MaxMemoryMb), true
}

// syncOrganizationQuotas pushes one organization's member quotas on every
// cluster it is subscribed to or has used. Errors are recorded per cluster.
func (s *Service) syncOrganizationQuotas(ctx context.Context, org db.Organization) error {
	data, err := s.quotaData(ctx, org)
	if err != nil {
		return err
	}
	clusters, err := s.q.ListClusters(ctx)
	if err != nil {
		return fmt.Errorf("list clusters: %w", err)
	}
	now := s.now()
	for _, cluster := range clusters {
		s.pushClusterQuotas(ctx, data, cluster, now)
	}
	return nil
}

// syncOrganizationClusterQuotas pushes one organization's member quotas on
// one cluster now, best effort.
func (s *Service) syncOrganizationClusterQuotas(ctx context.Context, org db.Organization, cluster db.Cluster) error {
	data, err := s.quotaData(ctx, org)
	if err != nil {
		return err
	}
	s.pushClusterQuotas(ctx, data, cluster, s.now())
	return nil
}

// blockOrganizationCluster gives every member team the blocking quota on one
// cluster.
func (s *Service) blockOrganizationCluster(ctx context.Context, org db.Organization, cluster db.Cluster) {
	data, err := s.quotaData(ctx, org)
	if err != nil {
		s.log.Warn("load member quotas failed", "organization", org.Slug, "err", err)
		return
	}
	now := s.now()
	for _, member := range data.members {
		state, have := data.states[cluster.ID][member.Membership.ID]
		s.pushQuota(ctx, org, member.Membership.ID, cluster, blockingQuota(), false, state, have, now)
	}
}

// pushClusterQuotas pushes each member's share on one cluster, or the blocking
// quota where the workspace is inactive, unsubscribed or the member has no
// share.
func (s *Service) pushClusterQuotas(ctx context.Context, data quotaData, cluster db.Cluster, now time.Time) {
	sub, subscribed := data.subs[cluster.ID]
	limits := Limits{}
	if subscribed {
		plan, ok := data.plans[sub.PlanID]
		if !ok {
			return
		}
		limits = effectiveLimits(plan, sub.MaxSandboxes, sub.MaxVcpus, sub.MaxMemoryMb)
	}
	shares := memberShares(limits, data.memberInputs(cluster.ID))
	for _, member := range data.members {
		id := member.Membership.ID
		state, have := data.states[cluster.ID][id]
		if subscribed && data.org.Status == orgActive && member.Membership.Role != roleViewer {
			quota, remove := shareQuota(shares[id])
			s.pushQuota(ctx, data.org, id, cluster, quota, remove, state, have, now)
			continue
		}
		if !data.usedOn(cluster.ID, id) {
			continue
		}
		s.pushQuota(ctx, data.org, id, cluster, blockingQuota(), false, state, have, now)
	}
}

// shareQuota is the quota a member should have: a blocking quota when a
// limited kind is 0, no quota when every kind is unlimited.
func shareQuota(s shares) (aerolvm.TeamQuota, bool) {
	if shareBlocked(s) {
		return blockingQuota(), false
	}
	if s.Sandboxes == nil && s.Vcpus == nil && s.MemoryMB == nil {
		return aerolvm.TeamQuota{}, true
	}
	quota := aerolvm.TeamQuota{}
	if s.Sandboxes != nil {
		quota.MaxSandboxes = int(*s.Sandboxes)
	}
	if s.Vcpus != nil {
		quota.MaxCPU = *s.Vcpus
	}
	if s.MemoryMB != nil {
		quota.MaxMemoryMB = int(*s.MemoryMB)
	}
	return quota, false
}

func blockingQuota() aerolvm.TeamQuota {
	return aerolvm.TeamQuota{MaxCPU: blockingCPU, MaxMemoryMB: blockingMemoryMB}
}

// pushQuota sends a member team's quota only when it changed, the last push
// failed, or it is old enough to refresh.
func (s *Service) pushQuota(ctx context.Context, org db.Organization, membershipID string, cluster db.Cluster, quota aerolvm.TeamQuota, remove bool, state db.QuotaState, haveState bool, now time.Time) {
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

	team := memberTeam(membershipID)
	client, err := s.clusterClient(cluster)
	if err == nil {
		if remove {
			err = client.DeleteTeamQuota(ctx, team)
		} else {
			err = client.PutTeamQuota(ctx, team, quota)
		}
	}
	errText := ""
	if err != nil {
		errText = err.Error()
		s.log.Warn("push quota failed", "cluster", cluster.Slug, "organization", org.Slug, "member", membershipID, "err", err)
	}
	if uerr := s.q.UpsertQuotaState(ctx, db.UpsertQuotaStateParams{
		OrgID:        org.ID,
		MembershipID: membershipID,
		ClusterID:    cluster.ID,
		Quota:        desired,
		PushedAt:     now,
		Error:        errText,
	}); uerr != nil {
		s.log.Warn("store quota state failed", "cluster", cluster.Slug, "organization", org.Slug, "err", uerr)
	}
}

// stopOrganizationSandboxes stops every running sandbox of a suspended
// organization, so it stops accruing compute.
func (s *Service) stopOrganizationSandboxes(ctx context.Context, org db.Organization) {
	s.stopSandboxes(ctx, org, nil)
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
		s.stopMirroredSandbox(ctx, row.Sandbox)
	}
}

// stopMemberSandboxes stops one member's running sandboxes.
func (s *Service) stopMemberSandboxes(ctx context.Context, membershipID string) {
	rows, err := s.q.ListStartedSandboxesByMembership(ctx, sql.NullString{String: membershipID, Valid: true})
	if err != nil {
		s.log.Warn("list started sandboxes failed", "member", membershipID, "err", err)
		return
	}
	for _, row := range rows {
		s.stopMirroredSandbox(ctx, row.Sandbox)
	}
}

func (s *Service) stopMirroredSandbox(ctx context.Context, sandbox db.Sandbox) {
	cluster, err := s.findCluster(ctx, sandbox.ClusterID)
	if err != nil {
		s.log.Warn("find sandbox cluster failed", "sandbox", sandbox.ID, "err", err)
		return
	}
	client, err := s.clusterClient(cluster)
	if err == nil {
		err = client.StopSandbox(ctx, sandbox.ID)
	}
	if err != nil {
		s.log.Warn("stop sandbox failed", "sandbox", sandbox.ID, "err", err)
		return
	}
	if err := s.q.UpdateSandboxStatus(ctx, db.UpdateSandboxStatusParams{
		Status:    "stopped",
		ClusterID: sandbox.ClusterID,
		ID:        sandbox.ID,
	}); err != nil {
		s.log.Warn("update sandbox status failed", "sandbox", sandbox.ID, "err", err)
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
		if err := s.syncOrganizationQuotas(ctx, org); err != nil {
			errs = append(errs, fmt.Errorf("sync %q: %w", org.Slug, err))
		}
	}
	return errors.Join(errs...)
}
