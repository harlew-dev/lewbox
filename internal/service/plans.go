package service

import (
	"context"
	"database/sql"
	"encoding/json"
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
	Organizations int       `json:"organizations"`
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
// no change and null means unlimited.
type UpdatePlanRequest struct {
	Slug         *string     `json:"slug,omitempty"`
	Name         *string     `json:"name,omitempty"`
	MaxSandboxes OptionalInt `json:"max_sandboxes,omitempty"`
	MaxVcpus     OptionalInt `json:"max_vcpus,omitempty"`
	MaxMemoryMB  OptionalInt `json:"max_memory_mb,omitempty"`
}

// MarshalJSON omits limits that were not in the request, so the remote client
// sends the same body the caller meant.
func (r UpdatePlanRequest) MarshalJSON() ([]byte, error) {
	type wire struct {
		Slug         *string         `json:"slug,omitempty"`
		Name         *string         `json:"name,omitempty"`
		MaxSandboxes json.RawMessage `json:"max_sandboxes,omitempty"`
		MaxVcpus     json.RawMessage `json:"max_vcpus,omitempty"`
		MaxMemoryMB  json.RawMessage `json:"max_memory_mb,omitempty"`
	}
	return json.Marshal(wire{
		Slug:         r.Slug,
		Name:         r.Name,
		MaxSandboxes: r.MaxSandboxes.raw(),
		MaxVcpus:     r.MaxVcpus.raw(),
		MaxMemoryMB:  r.MaxMemoryMB.raw(),
	})
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
	counts, err := s.planOrganizationCounts(ctx)
	if err != nil {
		return PlanList{}, err
	}
	settings, err := s.getSettings(ctx)
	if err != nil {
		return PlanList{}, err
	}
	out := PlanList{Data: make([]Plan, 0, len(rows))}
	for _, row := range rows {
		out.Data = append(out.Data, toPlan(row, counts[row.ID], row.Slug == settings.DefaultPlan))
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
	counts, err := s.planOrganizationCounts(ctx)
	if err != nil {
		return Plan{}, err
	}
	settings, err := s.getSettings(ctx)
	if err != nil {
		return Plan{}, err
	}
	return toPlan(row, counts[row.ID], row.Slug == settings.DefaultPlan), nil
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
	sandboxes, err := planLimit(req.MaxSandboxes, "max_sandboxes")
	if err != nil {
		return Plan{}, err
	}
	vcpus, err := planLimit(req.MaxVcpus, "max_vcpus")
	if err != nil {
		return Plan{}, err
	}
	memory, err := planLimit(req.MaxMemoryMB, "max_memory_mb")
	if err != nil {
		return Plan{}, err
	}
	row, err := s.q.CreatePlan(ctx, db.CreatePlanParams{
		ID:           uuid.NewV7().String(),
		Slug:         slug,
		Name:         name,
		MaxSandboxes: nullInt64(sandboxes),
		MaxVcpus:     nullInt64(vcpus),
		MaxMemoryMb:  nullInt64(memory),
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
	return toPlan(row, 0, row.Slug == settings.DefaultPlan), nil
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
	oldSlug := row.Slug
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
		row.MaxSandboxes, err = planLimitNull(req.MaxSandboxes.Value, "max_sandboxes")
		if err != nil {
			return Plan{}, err
		}
	}
	if req.MaxVcpus.Set {
		row.MaxVcpus, err = planLimitNull(req.MaxVcpus.Value, "max_vcpus")
		if err != nil {
			return Plan{}, err
		}
	}
	if req.MaxMemoryMB.Set {
		row.MaxMemoryMb, err = planLimitNull(req.MaxMemoryMB.Value, "max_memory_mb")
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

	// Keep the default-plan setting pointing at the same plan after a rename.
	settings, err := s.getSettings(ctx)
	if err != nil {
		return Plan{}, err
	}
	if settings.DefaultPlan == oldSlug && updated.Slug != oldSlug {
		if err := s.setSetting(ctx, "default_plan", updated.Slug); err != nil {
			return Plan{}, err
		}
	}
	if req.MaxSandboxes.Set || req.MaxVcpus.Set || req.MaxMemoryMB.Set {
		if err := s.syncPlanQuotas(ctx, updated.ID); err != nil {
			s.log.Warn("quota sync failed", "plan", updated.Slug, "err", err)
		}
	}
	counts, err := s.planOrganizationCounts(ctx)
	if err != nil {
		return Plan{}, err
	}
	return toPlan(updated, counts[updated.ID], updated.Slug == settings.DefaultPlan || settings.DefaultPlan == oldSlug), nil
}

// DeletePlan removes a plan that no organization uses and that is not the
// default. Instance admins only.
func (s *Service) DeletePlan(ctx context.Context, ref string) (Plan, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return Plan{}, err
	}
	row, err := s.findPlan(ctx, ref)
	if err != nil {
		return Plan{}, err
	}
	counts, err := s.planOrganizationCounts(ctx)
	if err != nil {
		return Plan{}, err
	}
	if n := counts[row.ID]; n > 0 {
		return Plan{}, conflict("plan %q is used by %d organization(s); move them to another plan first", row.Slug, n)
	}
	settings, err := s.getSettings(ctx)
	if err != nil {
		return Plan{}, err
	}
	if settings.DefaultPlan == row.Slug {
		return Plan{}, conflict("plan %q is the default plan; choose another default first", row.Slug)
	}
	n, err := s.q.DeletePlan(ctx, row.ID)
	if err != nil {
		return Plan{}, fmt.Errorf("delete plan: %w", err)
	}
	if n == 0 {
		return Plan{}, notFound("plan %q not found", ref)
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

// defaultPlan is the plan new sign-ups and organizations get.
func (s *Service) defaultPlan(ctx context.Context) (db.Plan, error) {
	settings, err := s.getSettings(ctx)
	if err != nil {
		return db.Plan{}, err
	}
	if settings.DefaultPlan == "" {
		return db.Plan{}, unavailable("this instance has no default plan: set one with lewbox settings set --default-plan <slug>")
	}
	row, err := s.q.GetPlanBySlug(ctx, settings.DefaultPlan)
	if errors.Is(err, sql.ErrNoRows) {
		return db.Plan{}, unavailable("the default plan %q no longer exists: set one with lewbox settings set --default-plan <slug>", settings.DefaultPlan)
	}
	if err != nil {
		return db.Plan{}, fmt.Errorf("get default plan: %w", err)
	}
	return row, nil
}

// planRows maps plan IDs to rows, for turning organizations into responses.
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

// planOrganizationCounts maps plan IDs to how many organizations use them.
func (s *Service) planOrganizationCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.q.CountOrganizationsByPlan(ctx)
	if err != nil {
		return nil, fmt.Errorf("count organizations by plan: %w", err)
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		if row.PlanID.Valid {
			counts[row.PlanID.String] = int(row.Organizations)
		}
	}
	return counts, nil
}

func toPlan(row db.Plan, organizations int, isDefault bool) Plan {
	return Plan{
		ID:            row.ID,
		Slug:          row.Slug,
		Name:          row.Name,
		MaxSandboxes:  nullInt(row.MaxSandboxes),
		MaxVcpus:      nullInt(row.MaxVcpus),
		MaxMemoryMB:   nullInt(row.MaxMemoryMb),
		Organizations: organizations,
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

// planLimit normalizes an API limit: nil and 0 are unlimited.
func planLimit(n *int64, field string) (*int64, error) {
	if n == nil || *n == 0 {
		return nil, nil
	}
	if *n < 0 {
		return nil, invalid("%s must be 0 or more", field)
	}
	return n, nil
}

// planLimitNull is planLimit for a PATCH value.
func planLimitNull(n *int64, field string) (sql.NullInt64, error) {
	value, err := planLimit(n, field)
	if err != nil {
		return sql.NullInt64{}, err
	}
	return nullInt64(value), nil
}

func nullInt64(n *int64) sql.NullInt64 {
	if n == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *n, Valid: true}
}

func nullInt(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}
