package service

import (
	"math"
	"sort"
	"time"
)

// Share is a member's quantity of a plan in one region. A nil value is
// unlimited; for a limited kind the value may be 0, which blocks the member.
type Share struct {
	Sandboxes *int64   `json:"sandboxes"`
	Vcpus     *float64 `json:"vcpus"`
	MemoryMB  *int64   `json:"memory_mb"`
}

type shares = Share
type membershipID = string

// memberShareInput is one member's explicit share in a region.
type memberShareInput struct {
	ID        membershipID
	CreatedAt time.Time
	Role      string
	Explicit  Share
}

// memberShares splits a region's plan between its members. Explicit shares
// are kept; the rest is split equally between the others (the remainder goes
// to the earliest members), rounded down to 0.01 vCPU. When explicit shares
// add up to more than the plan, they are scaled down proportionally. Viewers
// are absent from the result: they have no share.
func memberShares(plan Limits, members []memberShareInput) map[membershipID]shares {
	ordered := make([]memberShareInput, 0, len(members))
	out := make(map[membershipID]shares, len(members))
	for _, m := range members {
		if m.Role == roleViewer {
			continue
		}
		ordered = append(ordered, m)
		out[m.ID] = shares{}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
		}
		return ordered[i].ID < ordered[j].ID
	})

	whole := func(limit *int64, explicit func(memberShareInput) *int64) map[membershipID]*int64 {
		if limit == nil {
			values := make(map[membershipID]*int64, len(ordered))
			for _, m := range ordered {
				values[m.ID] = explicit(m)
			}
			return values
		}
		return splitWhole(*limit, ordered, explicit)
	}
	if values := whole(plan.Sandboxes, func(m memberShareInput) *int64 { return m.Explicit.Sandboxes }); values != nil {
		for id, v := range values {
			share := out[id]
			share.Sandboxes = v
			out[id] = share
		}
	}
	if values := whole(plan.MemoryMB, func(m memberShareInput) *int64 { return m.Explicit.MemoryMB }); values != nil {
		for id, v := range values {
			share := out[id]
			share.MemoryMB = v
			out[id] = share
		}
	}
	var vcpuLimit *float64
	if plan.Vcpus != nil {
		limit := float64(*plan.Vcpus)
		vcpuLimit = &limit
	}
	if values := splitVcpus(vcpuLimit, ordered, func(m memberShareInput) *float64 { return m.Explicit.Vcpus }); values != nil {
		for id, v := range values {
			share := out[id]
			share.Vcpus = v
			out[id] = share
		}
	}
	return out
}

// splitWhole divides a whole-number limit. Explicit shares are kept unless
// they add up to more than the limit, when everything is scaled down.
func splitWhole(limit int64, ordered []memberShareInput, explicit func(memberShareInput) *int64) map[membershipID]*int64 {
	values := make(map[membershipID]*int64, len(ordered))
	var sum int64
	var automatic []memberShareInput
	for _, m := range ordered {
		if v := explicit(m); v != nil {
			values[m.ID] = v
			sum += *v
		} else {
			automatic = append(automatic, m)
		}
	}
	if sum > limit {
		for _, m := range ordered {
			v := int64(0)
			if explicitValue := explicit(m); explicitValue != nil {
				v = int64(float64(*explicitValue) * float64(limit) / float64(sum))
			}
			values[m.ID] = &v
		}
		return values
	}
	if len(automatic) > 0 {
		each := (limit - sum) / int64(len(automatic))
		remainder := (limit - sum) % int64(len(automatic))
		for i, m := range automatic {
			v := each
			if int64(i) < remainder {
				v++
			}
			values[m.ID] = &v
		}
	}
	return values
}

// splitVcpus divides a fractional limit, rounded down to 0.01 vCPU.
func splitVcpus(limit *float64, ordered []memberShareInput, explicit func(memberShareInput) *float64) map[membershipID]*float64 {
	values := make(map[membershipID]*float64, len(ordered))
	if limit == nil {
		for _, m := range ordered {
			if v := explicit(m); v != nil {
				floored := floorShare(*v)
				values[m.ID] = &floored
			}
		}
		return values
	}
	var sum float64
	var automatic []memberShareInput
	for _, m := range ordered {
		if v := explicit(m); v != nil {
			floored := floorShare(*v)
			values[m.ID] = &floored
			sum += floored
		} else {
			automatic = append(automatic, m)
		}
	}
	if sum > *limit {
		for _, m := range ordered {
			v := 0.0
			if explicitValue := explicit(m); explicitValue != nil {
				v = floorShare(*explicitValue * *limit / sum)
			}
			values[m.ID] = &v
		}
		return values
	}
	if len(automatic) > 0 {
		each := floorShare((*limit - sum) / float64(len(automatic)))
		for _, m := range automatic {
			values[m.ID] = &each
		}
	}
	return values
}

func floorShare(v float64) float64 {
	return math.Floor(v*100+1e-9) / 100
}

// shareBlocked reports whether a share of a limited kind is 0, which blocks
// the member in that region.
func shareBlocked(s shares) bool {
	return (s.Sandboxes != nil && *s.Sandboxes == 0) ||
		(s.Vcpus != nil && *s.Vcpus == 0) ||
		(s.MemoryMB != nil && *s.MemoryMB == 0)
}
