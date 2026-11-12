package service

import (
	"context"
	"fmt"

	"github.com/harlew-dev/lewbox/internal/db"
)

// Region is a cluster customers can place sandboxes in. It deliberately
// carries none of a cluster's admin fields.
type Region struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	EndpointURL string `json:"endpoint_url"`
	Runtime     string `json:"runtime"`
	Status      string `json:"status"`
}

// RegionList is returned by ListRegions. Data is never null.
type RegionList struct {
	Data []Region `json:"data"`
}

// ListRegions returns the clusters that are ready or degraded, ordered by
// slug. Any signed-in principal may call it, organization API keys included.
func (s *Service) ListRegions(ctx context.Context) (RegionList, error) {
	if _, err := requirePrincipal(ctx); err != nil {
		return RegionList{}, err
	}
	rows, err := s.q.ListClusters(ctx)
	if err != nil {
		return RegionList{}, fmt.Errorf("list clusters: %w", err)
	}
	out := RegionList{Data: make([]Region, 0, len(rows))}
	for _, row := range rows {
		if !regionReady(row.Status) {
			continue
		}
		out.Data = append(out.Data, toRegion(row))
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
