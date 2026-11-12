package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/harlew-dev/lewbox/internal/db"
)

// Subscription is an organization's plan in one region. Only instance admins
// create or change them. Overrides is only present for instance admins.
type Subscription struct {
	Region    Region    `json:"region"`
	Plan      PlanRef   `json:"plan"`
	Limits    Limits    `json:"limits"`
	Overrides *Limits   `json:"overrides,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// SubscriptionList is returned by ListSubscriptions. Data is never null.
type SubscriptionList struct {
	Data []Subscription `json:"data"`
}

// SetSubscriptionRequest creates or changes a subscription. Plan is a plan
// slug or ID. An absent override keeps the current one on a change and is
// null (the plan's value) on a create; null clears it.
type SetSubscriptionRequest struct {
	Plan         string      `json:"plan"`
	MaxSandboxes OptionalInt `json:"max_sandboxes,omitzero"`
	MaxVcpus     OptionalInt `json:"max_vcpus,omitzero"`
	MaxMemoryMB  OptionalInt `json:"max_memory_mb,omitzero"`
}

// ListSubscriptions returns an organization's subscriptions, by region slug.
// Any member of the organization may read them.
func (s *Service) ListSubscriptions(ctx context.Context, org string) (SubscriptionList, error) {
	c, err := s.orgFor(ctx, org, accessRead)
	if err != nil {
		return SubscriptionList{}, err
	}
	subs, err := s.q.ListSubscriptions(ctx, c.org.ID)
	if err != nil {
		return SubscriptionList{}, fmt.Errorf("list subscriptions: %w", err)
	}
	plans, err := s.planRows(ctx)
	if err != nil {
		return SubscriptionList{}, err
	}
	clusters, err := s.clustersByID(ctx)
	if err != nil {
		return SubscriptionList{}, err
	}
	out := SubscriptionList{Data: make([]Subscription, 0, len(subs))}
	for _, sub := range subs {
		cluster, ok := clusters[sub.ClusterID]
		if !ok {
			continue
		}
		plan, ok := plans[sub.PlanID]
		if !ok {
			continue
		}
		out.Data = append(out.Data, toSubscription(sub, plan, cluster, c.p.InstanceAdmin))
	}
	sort.Slice(out.Data, func(i, j int) bool { return out.Data[i].Region.Slug < out.Data[j].Region.Slug })
	return out, nil
}

// SetSubscription creates or changes an organization's subscription in one
// region and pushes the new quota there at once. Instance admins only.
func (s *Service) SetSubscription(ctx context.Context, org, region string, req SetSubscriptionRequest) (Subscription, error) {
	c, err := s.subscriptionAdmin(ctx, org)
	if err != nil {
		return Subscription{}, err
	}
	cluster, err := s.findCluster(ctx, region)
	if err != nil {
		return Subscription{}, err
	}
	plan, err := s.findPlan(ctx, req.Plan)
	if err != nil {
		return Subscription{}, err
	}
	sandboxes, err := limitValue(req.MaxSandboxes.Value, "max_sandboxes", "to use the plan's limit")
	if err != nil {
		return Subscription{}, err
	}
	vcpus, err := limitValue(req.MaxVcpus.Value, "max_vcpus", "to use the plan's limit")
	if err != nil {
		return Subscription{}, err
	}
	memory, err := limitValue(req.MaxMemoryMB.Value, "max_memory_mb", "to use the plan's limit")
	if err != nil {
		return Subscription{}, err
	}

	existing, err := s.q.GetSubscription(ctx, db.GetSubscriptionParams{OrgID: c.org.ID, ClusterID: cluster.ID})
	switch {
	case err == nil:
		if !req.MaxSandboxes.Set {
			sandboxes = existing.MaxSandboxes
		}
		if !req.MaxVcpus.Set {
			vcpus = existing.MaxVcpus
		}
		if !req.MaxMemoryMB.Set {
			memory = existing.MaxMemoryMb
		}
		existing, err = s.q.UpdateSubscription(ctx, db.UpdateSubscriptionParams{
			PlanID:       plan.ID,
			MaxSandboxes: sandboxes,
			MaxVcpus:     vcpus,
			MaxMemoryMb:  memory,
			OrgID:        c.org.ID,
			ClusterID:    cluster.ID,
		})
		if err != nil {
			return Subscription{}, fmt.Errorf("update subscription: %w", err)
		}
	case errors.Is(err, sql.ErrNoRows):
		existing, err = s.q.CreateSubscription(ctx, db.CreateSubscriptionParams{
			OrgID:        c.org.ID,
			ClusterID:    cluster.ID,
			PlanID:       plan.ID,
			MaxSandboxes: sandboxes,
			MaxVcpus:     vcpus,
			MaxMemoryMb:  memory,
			CreatedAt:    s.now(),
		})
		if err != nil {
			return Subscription{}, fmt.Errorf("create subscription: %w", err)
		}
	default:
		return Subscription{}, fmt.Errorf("get subscription: %w", err)
	}

	out := toSubscription(existing, plan, cluster, true)
	s.pushSubscriptionQuota(ctx, c.org, cluster, plan, existing)
	return out, nil
}

// DeleteSubscription removes an organization's subscription in one region.
// It revokes the organization's live sandbox keys there (refusing unless force
// if that fails), pushes the blocking quota and stops its running sandboxes.
// Instance admins only.
func (s *Service) DeleteSubscription(ctx context.Context, org, region string, force bool) (Subscription, error) {
	c, err := s.subscriptionAdmin(ctx, org)
	if err != nil {
		return Subscription{}, err
	}
	cluster, err := s.findCluster(ctx, region)
	if err != nil {
		return Subscription{}, err
	}
	sub, err := s.q.GetSubscription(ctx, db.GetSubscriptionParams{OrgID: c.org.ID, ClusterID: cluster.ID})
	if errors.Is(err, sql.ErrNoRows) {
		return Subscription{}, notFound("organization %q has no plan in region %q", c.org.Slug, cluster.Slug)
	}
	if err != nil {
		return Subscription{}, fmt.Errorf("get subscription: %w", err)
	}
	plan, err := s.q.GetPlanByID(ctx, sub.PlanID)
	if err != nil {
		return Subscription{}, fmt.Errorf("get plan: %w", err)
	}
	out := toSubscription(sub, plan, cluster, true)

	if err := s.revokeSubscriptionKeys(ctx, c.org, cluster, force); err != nil {
		return Subscription{}, err
	}
	n, err := s.q.DeleteSubscription(ctx, db.DeleteSubscriptionParams{OrgID: c.org.ID, ClusterID: cluster.ID})
	if err != nil {
		return Subscription{}, fmt.Errorf("delete subscription: %w", err)
	}
	if n == 0 {
		return Subscription{}, notFound("organization %q has no plan in region %q", c.org.Slug, cluster.Slug)
	}

	now := s.now()
	state, err := s.q.GetQuotaState(ctx, db.GetQuotaStateParams{OrgID: c.org.ID, ClusterID: cluster.ID})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Subscription{}, fmt.Errorf("get quota state: %w", err)
	}
	s.pushQuota(ctx, c.org, cluster, blockingQuota(), false, state, err == nil, now)
	s.stopSandboxes(ctx, c.org, map[string]bool{cluster.ID: true})
	return out, nil
}

// subscriptionAdmin resolves an organization and requires an instance admin.
func (s *Service) subscriptionAdmin(ctx context.Context, org string) (orgCaller, error) {
	c, err := s.orgFor(ctx, org, accessManage)
	if err != nil {
		return orgCaller{}, err
	}
	if !c.p.InstanceAdmin {
		return orgCaller{}, forbidden("only an instance admin can change an organization's plan in a region")
	}
	return c, nil
}

// revokeSubscriptionKeys revokes an organization's live sandbox keys on one
// cluster, failing closed unless force is set.
func (s *Service) revokeSubscriptionKeys(ctx context.Context, org db.Organization, cluster db.Cluster, force bool) error {
	keys, err := s.q.ListLiveSandboxKeysByOrg(ctx, org.ID)
	if err != nil {
		if !force {
			return sandboxKeysNotRevoked(0, cluster.Slug)
		}
		return nil
	}
	var here []db.SandboxKey
	for _, key := range keys {
		if key.ClusterID == cluster.ID {
			here = append(here, key)
		}
	}
	if len(here) == 0 {
		return nil
	}
	failed, ok := s.revokeKeys(ctx, cluster, here)
	if !ok {
		failed = len(here)
	}
	if failed > 0 && !force {
		return sandboxKeysNotRevoked(failed, cluster.Slug)
	}
	return nil
}

// pushSubscriptionQuota sends one subscription's quota to its cluster now.
// Errors are logged and recorded in quota_state.
func (s *Service) pushSubscriptionQuota(ctx context.Context, org db.Organization, cluster db.Cluster, plan db.Plan, sub db.Subscription) {
	quota, remove := subscriptionQuota(org.Status, true, effectiveLimits(plan, sub.MaxSandboxes, sub.MaxVcpus, sub.MaxMemoryMb))
	state, err := s.q.GetQuotaState(ctx, db.GetQuotaStateParams{OrgID: org.ID, ClusterID: cluster.ID})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.log.Warn("get quota state failed", "cluster", cluster.Slug, "organization", org.Slug, "err", err)
		return
	}
	s.pushQuota(ctx, org, cluster, quota, remove, state, err == nil, s.now())
}

func toSubscription(sub db.Subscription, plan db.Plan, cluster db.Cluster, admin bool) Subscription {
	out := Subscription{
		Region:    toRegion(cluster),
		Plan:      PlanRef{Slug: plan.Slug, Name: plan.Name},
		Limits:    effectiveLimits(plan, sub.MaxSandboxes, sub.MaxVcpus, sub.MaxMemoryMb),
		CreatedAt: sub.CreatedAt.UTC(),
	}
	if admin {
		overrides := Limits{
			Sandboxes: nullInt(sub.MaxSandboxes),
			Vcpus:     nullInt(sub.MaxVcpus),
			MemoryMB:  nullInt(sub.MaxMemoryMb),
		}
		out.Overrides = &overrides
	}
	return out
}

// subscriptionsByCluster maps cluster IDs to an organization's subscriptions.
func (s *Service) subscriptionsByCluster(ctx context.Context, orgID string) (map[string]db.Subscription, error) {
	subs, err := s.q.ListSubscriptions(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	out := make(map[string]db.Subscription, len(subs))
	for _, sub := range subs {
		out[sub.ClusterID] = sub
	}
	return out, nil
}

// subscriptionRegionSlugs returns the slugs of an organization's subscribed
