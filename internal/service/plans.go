package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/harlew-dev/lewbox/internal/db"
)

// Plan is a set of limits the operator sells. A nil limit is unlimited.
type Plan struct {
	ID            string    `json:"id"`
	Slug          string    `json:"slug"`
	Name          string    `json:"name"`
	MaxSandboxes  *int64    `json:"max_sandboxes"`
	MaxVcpus      *int64    `json:"max_vcpus"`
	MaxMemoryMB   *int64    `json:"max_memory_mb"`
	Subscriptions int       `json:"subscriptions"`
	Default       bool      `json:"default"`
	CreatedAt     time.Time `json:"created_at"`
}

// PlanList is returned by ListPlans. Data is never null.
type PlanList struct {
	Data []Plan `json:"data"`
}

// CreatePlanRequest is the input to CreatePlan. A nil limit is unlimited.
type CreatePlanRequest struct {
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	MaxSandboxes *int64 `json:"max_sandboxes"`
	MaxVcpus     *int64 `json:"max_vcpus"`
	MaxMemoryMB  *int64 `json:"max_memory_mb"`
}

// UpdatePlanRequest changes the fields that are set. For a limit, absent means
// no change, null means unlimited and a number means that limit.
type UpdatePlanRequest struct {
	Slug         *string     `json:"slug,omitempty"`
	Name         *string     `json:"name,omitempty"`
	MaxSandboxes OptionalInt `json:"max_sandboxes,omitzero"`
	MaxVcpus     OptionalInt `json:"max_vcpus,omitzero"`
	MaxMemoryMB  OptionalInt `json:"max_memory_mb,omitzero"`
}

// ListPlans returns every plan, ordered by slug. Instance admins only.
func (s *Service) ListPlans(ctx context.Context) (PlanList, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return PlanList{}, err
	}
	rows, err := s.q.ListPlans(ctx)
	if err != nil {
		return PlanList{}, fmt.Errorf("list plans: %w", err)
	}
	counts, err := s.planSubscriptionCounts(ctx)
	if err != nil {
		return PlanList{}, err
	}
	settings, err := s.getSettings(ctx)
	if err != nil {
		return PlanList{}, err
	}
	out := PlanList{Data: make([]Plan, 0, len(rows))}
	for _, row := range rows {
		out.Data = append(out.Data, toPlan(row, counts[row.ID], isDefaultPlan(settings, row.Slug)))
	}
	return out, nil
}

// GetPlan returns a plan by ID or slug.
func (s *Service) GetPlan(ctx context.Context, ref string) (Plan, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return Plan{}, err
	}
	row, err := s.findPlan(ctx, ref)
	if err != nil {
		return Plan{}, err
	}
	counts, err := s.planSubscriptionCounts(ctx)
	if err != nil {
		return Plan{}, err
	}
	settings, err := s.getSettings(ctx)
	if err != nil {
		return Plan{}, err
	}
	return toPlan(row, counts[row.ID], isDefaultPlan(settings, row.Slug)), nil
}

// CreatePlan adds a plan. Instance admins only.
func (s *Service) CreatePlan(ctx context.Context, req CreatePlanRequest) (Plan, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return Plan{}, err
	}
	slug, err := planSlug(req.Slug)
	if err != nil {
		return Plan{}, err
	}
	name, err := orgName(req.Name, slug)
	if err != nil {
		return Plan{}, err
	}
	sandboxes, err := limitValue(req.MaxSandboxes, "max_sandboxes", "for unlimited")
	if err != nil {
		return Plan{}, err
	}
	vcpus, err := limitValue(req.MaxVcpus, "max_vcpus", "for unlimited")
	if err != nil {
		return Plan{}, err
	}
	memory, err := limitValue(req.MaxMemoryMB, "max_memory_mb", "for unlimited")
	if err != nil {
		return Plan{}, err
	}
	row, err := s.q.CreatePlan(ctx, db.CreatePlanParams{
		ID:           uuid.NewV7().String(),
		Slug:         slug,
		Name:         name,
		MaxSandboxes: sandboxes,
		MaxVcpus:     vcpus,
		MaxMemoryMb:  memory,
		CreatedAt:    s.now(),
	})
	if db.IsUniqueViolation(err) {
		return Plan{}, conflict("a plan with slug %q already exists", slug)
	}
	if err != nil {
		return Plan{}, fmt.Errorf("create plan: %w", err)
	}
	settings, err := s.getSettings(ctx)
	if err != nil {
		return Plan{}, err
	}
	return toPlan(row, 0, isDefaultPlan(settings, row.Slug)), nil
}

// UpdatePlan changes a plan by ID or slug. Instance admins only.
func (s *Service) UpdatePlan(ctx context.Context, ref string, req UpdatePlanRequest) (Plan, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return Plan{}, err
	}
	if req.Slug == nil && req.Name == nil && !req.MaxSandboxes.Set && !req.MaxVcpus.Set && !req.MaxMemoryMB.Set {
		return Plan{}, invalid("nothing to change: set slug, name or a limit")
	}
	row, err := s.findPlan(ctx, ref)
	if err != nil {
		return Plan{}, err
	}
	if req.Slug != nil {
		row.Slug, err = planSlug(*req.Slug)
		if err != nil {
			return Plan{}, err
		}
	}
	if req.Name != nil {
		row.Name, err = orgName(*req.Name, row.Slug)
		if err != nil {
			return Plan{}, err
		}
	}
	if req.MaxSandboxes.Set {
		row.MaxSandboxes, err = limitValue(req.MaxSandboxes.Value, "max_sandboxes", "for unlimited")
		if err != nil {
			return Plan{}, err
		}
	}
	if req.MaxVcpus.Set {
		row.MaxVcpus, err = limitValue(req.MaxVcpus.Value, "max_vcpus", "for unlimited")
		if err != nil {
			return Plan{}, err
		}
	}
	if req.MaxMemoryMB.Set {
		row.MaxMemoryMb, err = limitValue(req.MaxMemoryMB.Value, "max_memory_mb", "for unlimited")
		if err != nil {
			return Plan{}, err
		}
	}
	updated, err := s.q.UpdatePlan(ctx, db.UpdatePlanParams{
		Slug:         row.Slug,
		Name:         row.Name,
		MaxSandboxes: row.MaxSandboxes,
		MaxVcpus:     row.MaxVcpus,
		MaxMemoryMb:  row.MaxMemoryMb,
		ID:           row.ID,
	})
	if db.IsUniqueViolation(err) {
		return Plan{}, conflict("a plan with slug %q already exists", row.Slug)
	}
	if err != nil {
		return Plan{}, fmt.Errorf("update plan: %w", err)
	}

	if req.MaxSandboxes.Set || req.MaxVcpus.Set || req.MaxMemoryMB.Set {
		if err := s.syncPlanQuotas(ctx, updated.ID); err != nil {
			s.log.Warn("quota sync failed", "plan", updated.Slug, "err", err)
		}
	}
	counts, err := s.planSubscriptionCounts(ctx)
	if err != nil {
		return Plan{}, err
	}
	settings, err := s.getSettings(ctx)
	if err != nil {
		return Plan{}, err
	}
	return toPlan(updated, counts[updated.ID], isDefaultPlan(settings, updated.Slug)), nil
}

// DeletePlan removes a plan that no subscription uses and that is not the
// default. Instance admins only. The guards are in the DELETE, so a row that
// becomes used between the lookup and the delete is not removed.
func (s *Service) DeletePlan(ctx context.Context, ref string) (Plan, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return Plan{}, err
	}
	row, err := s.findPlan(ctx, ref)
	if err != nil {
		return Plan{}, err
	}
	n, err := s.q.DeletePlan(ctx, row.ID)
	if err != nil {
		return Plan{}, fmt.Errorf("delete plan: %w", err)
	}
	if n == 0 {
		counts, err := s.planSubscriptionCounts(ctx)
		if err != nil {
			return Plan{}, err
		}
		if count := counts[row.ID]; count > 0 {
			return Plan{}, conflict("plan %q is used by %d subscription(s); move them to another plan first", row.Slug, count)
		}
		return Plan{}, conflict("plan %q is the default plan; choose another default first", row.Slug)
	}
	return toPlan(row, 0, false), nil
}

// findPlan looks a plan up by ID or slug.
func (s *Service) findPlan(ctx context.Context, ref string) (db.Plan, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return db.Plan{}, invalid("a plan ID or slug is required")
	}
	var (
		row db.Plan
		err error
	)
	if id, perr := uuid.Parse(ref); perr == nil {
		row, err = s.q.GetPlanByID(ctx, id.String())
	} else {
		row, err = s.q.GetPlanBySlug(ctx, strings.ToLower(ref))
	}
	if errors.Is(err, sql.ErrNoRows) {
		return db.Plan{}, notFound("plan %q not found", ref)
	}
	if err != nil {
		return db.Plan{}, fmt.Errorf("get plan: %w", err)
	}
	return row, nil
}

// planRows maps plan IDs to rows, for turning subscriptions into responses.
func (s *Service) planRows(ctx context.Context) (map[string]db.Plan, error) {
	rows, err := s.q.ListPlans(ctx)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	plans := make(map[string]db.Plan, len(rows))
	for _, row := range rows {
		plans[row.ID] = row
	}
	return plans, nil
}

// planSubscriptionCounts maps plan IDs to how many subscriptions use them.
func (s *Service) planSubscriptionCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.q.CountSubscriptionsByPlan(ctx)
	if err != nil {
		return nil, fmt.Errorf("count subscriptions by plan: %w", err)
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[row.PlanID] = int(row.Subscriptions)
	}
	return counts, nil
}

func isDefaultPlan(settings Settings, slug string) bool {
	return settings.DefaultPlan != nil && *settings.DefaultPlan == slug
}

func toPlan(row db.Plan, subscriptions int, isDefault bool) Plan {
	return Plan{
		ID:            row.ID,
		Slug:          row.Slug,
		Name:          row.Name,
		MaxSandboxes:  nullInt(row.MaxSandboxes),
		MaxVcpus:      nullInt(row.MaxVcpus),
		MaxMemoryMB:   nullInt(row.MaxMemoryMb),
		Subscriptions: subscriptions,
		Default:       isDefault,
		CreatedAt:     row.CreatedAt.UTC(),
	}
}

// planSlug checks a plan slug, which is looked up like an ID.
func planSlug(raw string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if !nodeName.MatchString(slug) {
		return "", invalid("slug must be 1-63 lowercase letters, digits or hyphens, and start and end with a letter or digit")
	}
	if _, err := uuid.Parse(slug); err == nil {
		return "", invalid("slug must not be a UUID")
	}
	return slug, nil
}

// limitValue checks a limit: a positive number, or nil for the null meaning
// (unlimited on a plan, the plan's value on an override).
func limitValue(n *int64, field, nullMeaning string) (sql.NullInt64, error) {
	if n == nil {
		return sql.NullInt64{}, nil
	}
	if *n < 1 {
		return sql.NullInt64{}, invalid("a limit must be at least 1; use null %s", nullMeaning)
	}
	return sql.NullInt64{Int64: *n, Valid: true}, nil
}

func nullInt(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}
