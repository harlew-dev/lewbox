package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/harlew-dev/lewbox/internal/db"
)

// SetMemberSharesRequest is the input to SetMemberShares. A null or absent
// value is automatic; a number is an explicit share.
type SetMemberSharesRequest struct {
	MaxSandboxes *int64   `json:"max_sandboxes"`
	MaxVcpus     *float64 `json:"max_vcpus"`
	MaxMemoryMB  *int64   `json:"max_memory_mb"`
}

// MemberLimits is one member's share and live use in one region.
type MemberLimits struct {
	Email    string     `json:"email"`
	Share    Share      `json:"share"`
	Explicit Share      `json:"explicit"`
	Used     LimitsUsed `json:"used"`
	Blocked  bool       `json:"blocked"`
}

// SetMemberShares sets a member's explicit share in one region. The explicit
// shares may not add up to more than the plan; the affected member quotas are
// pushed at once (best effort). Owners, admins and instance admins only.
func (s *Service) SetMemberShares(ctx context.Context, org, ref, region string, req SetMemberSharesRequest) (MemberLimits, error) {
	c, err := s.orgFor(ctx, org, accessManage)
	if err != nil {
		return MemberLimits{}, err
	}
	cluster, err := s.findCluster(ctx, region)
	if err != nil {
		return MemberLimits{}, err
	}
	data, err := s.quotaData(ctx, c.org)
	if err != nil {
		return MemberLimits{}, err
	}
	limits, subscribed := data.limitsFor(cluster.ID)
	if !subscribed {
		return MemberLimits{}, precondition(nil, "the workspace has no plan in %s; assign one first", cluster.Slug)
	}
	member, _, err := s.findMember(ctx, c.org, ref)
	if err != nil {
		return MemberLimits{}, err
	}
	if member.Role == roleViewer {
		return MemberLimits{}, invalid("viewers have no share of the plan")
	}
	explicit, err := memberShareValues(req)
	if err != nil {
		return MemberLimits{}, err
	}
	if err := checkShareSums(limits, data.members, cluster, data.shares, member.ID, explicit); err != nil {
		return MemberLimits{}, err
	}
	if err := s.q.UpsertMemberShare(ctx, db.UpsertMemberShareParams{
		MembershipID: member.ID,
		ClusterID:    cluster.ID,
		MaxSandboxes: nullNullInt(explicit.Sandboxes),
		MaxVcpus:     nullNullFloat(explicit.Vcpus),
		MaxMemoryMb:  nullNullInt(explicit.MemoryMB),
	}); err != nil {
		return MemberLimits{}, fmt.Errorf("set member share: %w", err)
	}
	data.shares[shareKey{member.ID, cluster.ID}] = db.MemberShare{
		MembershipID: member.ID,
		ClusterID:    cluster.ID,
		MaxSandboxes: nullNullInt(explicit.Sandboxes),
		MaxVcpus:     nullNullFloat(explicit.Vcpus),
		MaxMemoryMb:  nullNullInt(explicit.MemoryMB),
	}
	s.pushClusterQuotas(ctx, data, cluster, s.now())
	return data.memberLimits(cluster, member.ID, c.p.InstanceAdmin), nil
}

// memberShareValues validates a request's explicit shares. A positive vCPU
// value is rounded down to 0.01.
func memberShareValues(req SetMemberSharesRequest) (Share, error) {
	if req.MaxSandboxes != nil && *req.MaxSandboxes < 1 {
		return Share{}, invalid("max_sandboxes must be at least 1; use null for automatic")
	}
	if req.MaxMemoryMB != nil && *req.MaxMemoryMB < 1 {
		return Share{}, invalid("max_memory_mb must be at least 1; use null for automatic")
	}
	var vcpus *float64
	if req.MaxVcpus != nil {
		v := floorShare(*req.MaxVcpus)
		if *req.MaxVcpus <= 0 || v == 0 {
			return Share{}, invalid("max_vcpus must be at least 0.01; use null for automatic")
		}
		vcpus = &v
	}
	return Share{Sandboxes: req.MaxSandboxes, Vcpus: vcpus, MemoryMB: req.MaxMemoryMB}, nil
}

// checkShareSums refuses explicit shares that add up to more than the plan.
// A reduction is allowed even when it is still over, so an owner can fix a
// plan that shrank below its explicit shares.
func checkShareSums(limits Limits, members []db.ListMembersRow, cluster db.Cluster, current map[shareKey]db.MemberShare, targetID string, target Share) error {
	var sandboxes, memory, oldSandboxes, oldMemory int64
	var vcpus, oldVcpus float64
	for _, member := range members {
		if member.Membership.Role == roleViewer {
			continue
		}
		row := current[shareKey{member.Membership.ID, cluster.ID}]
		old := Share{
			Sandboxes: nullInt(row.MaxSandboxes),
			Vcpus:     nullFloat(row.MaxVcpus),
			MemoryMB:  nullInt(row.MaxMemoryMb),
		}
		share := old
		if member.Membership.ID == targetID {
			share = target
		}
		if share.Sandboxes != nil {
			sandboxes += *share.Sandboxes
		}
		if share.Vcpus != nil {
			vcpus += *share.Vcpus
		}
		if share.MemoryMB != nil {
			memory += *share.MemoryMB
		}
		if old.Sandboxes != nil {
			oldSandboxes += *old.Sandboxes
		}
		if old.Vcpus != nil {
			oldVcpus += *old.Vcpus
		}
		if old.MemoryMB != nil {
			oldMemory += *old.MemoryMB
		}
	}
	if limits.Sandboxes != nil && sandboxes > *limits.Sandboxes && sandboxes > oldSandboxes {
		return conflict("shares add up to %d sandboxes but the plan in %s allows %d sandboxes",
			sandboxes, cluster.Slug, *limits.Sandboxes)
	}
	if limits.Vcpus != nil && vcpus > float64(*limits.Vcpus)+1e-9 && vcpus > oldVcpus+1e-9 {
		return conflict("shares add up to %g vCPUs but the plan in %s allows %g vCPUs",
			vcpus, cluster.Slug, float64(*limits.Vcpus))
	}
	if limits.MemoryMB != nil && memory > *limits.MemoryMB && memory > oldMemory {
		return conflict("shares add up to %s but the plan in %s allows %s",
			memoryText(memory), cluster.Slug, memoryText(*limits.MemoryMB))
	}
	return nil
}

func memoryText(mb int64) string {
	if mb%1024 == 0 {
		return fmt.Sprintf("%d GB", mb/1024)
	}
	return fmt.Sprintf("%d MB", mb)
}

// requireMemberShare refuses a key for a member with no share in the region.
func (s *Service) requireMemberShare(ctx context.Context, org db.Organization, membershipID string, cluster db.Cluster) error {
	data, err := s.quotaData(ctx, org)
	if err != nil {
		return err
	}
	limits, ok := data.limitsFor(cluster.ID)
	if !ok {
		return precondition(nil, "your workspace has no plan in %s; ask your workspace owner", cluster.Slug)
	}
	share, ok := memberShares(limits, data.memberInputs(cluster.ID))[membershipID]
	if !ok || shareBlocked(share) {
		return noShareError(cluster.Slug)
	}
	return nil
}

func noShareError(region string) error {
	return precondition(nil, "you have no share of the plan in %s; ask your workspace owner", region)
}

// memberLimits renders one member's share and live use in a region.
func (d quotaData) memberLimits(cluster db.Cluster, membershipID string, callerAdmin bool) MemberLimits {
	limits, _ := d.limitsFor(cluster.ID)
	share, ok := memberShares(limits, d.memberInputs(cluster.ID))[membershipID]
	row := d.shares[shareKey{membershipID, cluster.ID}]
	out := MemberLimits{
		Share: share,
		Explicit: Share{
			Sandboxes: nullInt(row.MaxSandboxes),
			Vcpus:     nullFloat(row.MaxVcpus),
			MemoryMB:  nullInt(row.MaxMemoryMb),
		},
		Used:    d.amounts[cluster.ID][membershipID],
		Blocked: !ok || shareBlocked(share),
	}
	for _, member := range d.members {
		if member.Membership.ID != membershipID {
			continue
		}
		out.Email = member.Email
		if member.InstanceAdmin != 0 && !callerAdmin {
			out.Email = "operator"
		}
	}
	return out
}

func nullNullInt(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

func nullNullFloat(v *float64) sql.NullFloat64 {
	if v == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *v, Valid: true}
}

// memberClusters lists the clusters a member has used: a live key, a
// mirrored sandbox, or a pushed quota.
func (s *Service) memberClusters(ctx context.Context, orgID, membershipID string) (map[string]bool, error) {
	clusters := map[string]bool{}
	keys, err := s.q.ListLiveSandboxKeysByOrg(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("list sandbox keys: %w", err)
	}
	for _, key := range keys {
		if key.MembershipID.Valid && key.MembershipID.String == membershipID {
			clusters[key.ClusterID] = true
		}
	}
	rows, err := s.q.SumSandboxesByOrgMembership(ctx, sql.NullString{String: orgID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("sum sandboxes: %w", err)
	}
	for _, row := range rows {
		if row.MembershipID.Valid && row.MembershipID.String == membershipID {
			clusters[row.ClusterID] = true
		}
	}
	states, err := s.q.ListQuotaStates(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("list quota state: %w", err)
	}
	for _, state := range states {
		if state.MembershipID == membershipID {
			clusters[state.ClusterID] = true
		}
	}
	return clusters, nil
}

// revokeMemberKeys revokes a member's live keys on every cluster, failing
// closed so a removal never leaves a working token behind.
func (s *Service) revokeMemberKeys(ctx context.Context, org db.Organization, membershipID string) error {
	keys, err := s.q.ListLiveSandboxKeysByOrg(ctx, org.ID)
	if err != nil {
		return sandboxKeysNotRevoked(0, "the region")
	}
	byCluster := map[string][]db.SandboxKey{}
	for _, key := range keys {
		if key.MembershipID.Valid && key.MembershipID.String == membershipID {
			byCluster[key.ClusterID] = append(byCluster[key.ClusterID], key)
		}
	}
	clusters, err := s.clustersByID(ctx)
	if err != nil {
		return err
	}
	for clusterID, here := range byCluster {
		cluster, ok := clusters[clusterID]
		if !ok {
			continue
		}
		failed, ok := s.revokeKeys(ctx, cluster, here)
		if !ok {
			failed = len(here)
		}
		if failed > 0 {
			return sandboxKeysNotRevoked(failed, cluster.Slug)
		}
	}
	return nil
}

// blockMemberTeams gives a removed member's team the blocking quota on the
// clusters they used, so any token that survived stops working.
func (s *Service) blockMemberTeams(ctx context.Context, org db.Organization, membershipID string, clusters map[string]bool) {
	rows, err := s.clustersByID(ctx)
	if err != nil {
		s.log.Warn("list clusters failed", "organization", org.Slug, "err", err)
		return
	}
	now := s.now()
	for clusterID := range clusters {
		cluster, ok := rows[clusterID]
		if !ok {
			continue
		}
		state, err := s.q.GetQuotaState(ctx, db.GetQuotaStateParams{
			OrgID:        org.ID,
			MembershipID: membershipID,
			ClusterID:    clusterID,
		})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			s.log.Warn("get quota state failed", "cluster", cluster.Slug, "err", err)
			continue
		}
		s.pushQuota(ctx, org, membershipID, cluster, blockingQuota(), false, state, err == nil, now)
	}
}
