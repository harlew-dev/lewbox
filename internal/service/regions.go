package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/harlew-dev/lewbox/internal/db"
)

// Region is a cluster customers can place sandboxes in. It deliberately
// carries none of a cluster's admin fields. Plan and Limits are set for the
// regions an organization is subscribed to.
type Region struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	EndpointURL string   `json:"endpoint_url"`
	Runtime     string   `json:"runtime"`
	Status      string   `json:"status"`
	Plan        *PlanRef `json:"plan,omitempty"`
	Limits      *Limits  `json:"limits,omitempty"`
}

// RegionList is returned by ListRegions. Data is never null.
type RegionList struct {
	Data []Region `json:"data"`
}

// ListRegions returns the regions a caller may use. Instance admins get every
// ready or degraded cluster; everyone else gets their organization's
// subscribed regions, with the plan and limits there. A signed-in customer
// with no organization chosen gets none.
func (s *Service) ListRegions(ctx context.Context, org string) (RegionList, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return RegionList{}, err
	}
	rows, err := s.q.ListClusters(ctx)
	if err != nil {
		return RegionList{}, fmt.Errorf("list clusters: %w", err)
	}
	out := RegionList{Data: make([]Region, 0, len(rows))}
	if p.InstanceAdmin {
		for _, row := range rows {
			if regionReady(row.Status) {
				out.Data = append(out.Data, toRegion(row))
			}
		}
		return out, nil
	}

	if strings.TrimSpace(org) == "" && p.Kind == "user" {
		return out, nil
	}
	c, err := s.orgFor(ctx, org, accessRead)
	if err != nil {
		return RegionList{}, err
	}
	subs, err := s.subscriptionsByCluster(ctx, c.org.ID)
	if err != nil {
		return RegionList{}, err
	}
	plans, err := s.planRows(ctx)
	if err != nil {
		return RegionList{}, err
	}
	for _, row := range rows {
		if !regionReady(row.Status) {
			continue
		}
		sub, ok := subs[row.ID]
		if !ok {
			continue
		}
		plan, ok := plans[sub.PlanID]
		if !ok {
			continue
		}
		region := toRegion(row)
		region.Plan = &PlanRef{Slug: plan.Slug, Name: plan.Name}
		limits := effectiveLimits(plan, sub.MaxSandboxes, sub.MaxVcpus, sub.MaxMemoryMb)
		region.Limits = &limits
		out.Data = append(out.Data, region)
	}
	return out, nil
}

func toRegion(c db.Cluster) Region {
	return Region{
		Slug:        c.Slug,
		Name:        c.Name,
		EndpointURL: c.EndpointURL,
		Runtime:     c.Runtime,
		Status:      c.Status,
	}
}
