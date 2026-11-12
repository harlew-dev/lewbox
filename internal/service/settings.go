package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/harlew-dev/lewbox/internal/db"
)

// Settings are the instance-wide options an operator controls.
type Settings struct {
	InstanceName   string   `json:"instance_name"`
	Signup         string   `json:"signup"`          // open | approval | closed
	DefaultPlan    *string  `json:"default_plan"`    // a plan slug; nil = none
	DefaultRegions []string `json:"default_regions"` // cluster slugs; [] = none
}

// UpdateSettingsRequest changes the fields that are set. DefaultPlan is a plan
// slug, or "none" to clear it; DefaultRegions is a list of cluster slugs, and
// an empty list clears it.
type UpdateSettingsRequest struct {
	InstanceName   *string   `json:"instance_name,omitempty"`
	Signup         *string   `json:"signup,omitempty"`
	DefaultPlan    *string   `json:"default_plan,omitempty"`
	DefaultRegions *[]string `json:"default_regions,omitempty"`
}

// Instance is the public description of this Lewbox instance.
type Instance struct {
	Name   string `json:"name"`
	Signup string `json:"signup"`
}

const (
	signupOpen     = "open"
	signupApproval = "approval"
	signupClosed   = "closed"

	defaultInstanceName = "Lewbox"
)

// GetSettings returns the instance settings. Instance admins only.
func (s *Service) GetSettings(ctx context.Context) (Settings, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return Settings{}, err
	}
	return s.getSettings(ctx)
}

// getSettings reads the settings without a permission check, because sign-up
// and the public instance route need them too. The stored default plan is an
// ID; the response and the rest of the service use its slug.
func (s *Service) getSettings(ctx context.Context) (Settings, error) {
	out := Settings{InstanceName: defaultInstanceName, Signup: signupClosed, DefaultRegions: []string{}}
	rows, err := s.q.ListSettings(ctx)
	if err != nil {
		return Settings{}, fmt.Errorf("list settings: %w", err)
	}
	planID := ""
	for _, row := range rows {
		if row.Key == "default_regions" {
			var regions []string
			if err := json.Unmarshal([]byte(row.Value), &regions); err != nil {
				return Settings{}, fmt.Errorf("decode setting %q: %w", row.Key, err)
			}
			if regions != nil {
				out.DefaultRegions = regions
			}
			continue
		}
		var value string
		if err := json.Unmarshal([]byte(row.Value), &value); err != nil {
			return Settings{}, fmt.Errorf("decode setting %q: %w", row.Key, err)
		}
		switch row.Key {
		case "instance_name":
			if value != "" {
				out.InstanceName = value
			}
		case "signup":
			// An unknown mode fails closed.
			switch value {
			case signupOpen, signupApproval, signupClosed:
				out.Signup = value
			}
		case "default_plan":
			planID = value
		}
	}
	if planID != "" {
		plan, err := s.q.GetPlanByID(ctx, planID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return Settings{}, fmt.Errorf("get default plan: %w", err)
		default:
			slug := plan.Slug
			out.DefaultPlan = &slug
		}
	}
	return out, nil
}

// UpdateSettings changes the instance settings. Instance admins only. Every
// field is checked before the first write.
func (s *Service) UpdateSettings(ctx context.Context, req UpdateSettingsRequest) (Settings, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return Settings{}, err
	}
	if req.InstanceName == nil && req.Signup == nil && req.DefaultPlan == nil && req.DefaultRegions == nil {
		return Settings{}, invalid("nothing to change: set instance_name, signup, default_plan or default_regions")
	}
	name := ""
	if req.InstanceName != nil {
		name = strings.TrimSpace(*req.InstanceName)
		if name == "" {
			return Settings{}, invalid("instance_name must not be empty")
		}
		if err := displayName(name, "instance_name", 100); err != nil {
			return Settings{}, err
		}
	}
	mode := ""
	if req.Signup != nil {
		mode = strings.ToLower(strings.TrimSpace(*req.Signup))
		switch mode {
		case signupOpen, signupApproval, signupClosed:
		default:
			return Settings{}, invalid("signup must be open, approval or closed")
		}
	}
	var plan db.Plan
	if req.DefaultPlan != nil {
		ref := strings.TrimSpace(*req.DefaultPlan)
		if !strings.EqualFold(ref, "none") && ref != "" {
			var err error
			if plan, err = s.findPlan(ctx, ref); err != nil {
				return Settings{}, err
			}
		}
	}
	regions := []string{}
	if req.DefaultRegions != nil {
		seen := map[string]bool{}
		for _, raw := range *req.DefaultRegions {
			slug := strings.ToLower(strings.TrimSpace(raw))
			if slug == "" || strings.EqualFold(slug, "none") || seen[slug] {
				continue
			}
			if _, err := s.findCluster(ctx, slug); err != nil {
				return Settings{}, err
			}
			seen[slug] = true
			regions = append(regions, slug)
		}
	}
	if req.InstanceName != nil {
		if err := s.setSetting(ctx, "instance_name", name); err != nil {
			return Settings{}, err
		}
	}
	if req.Signup != nil {
		if err := s.setSetting(ctx, "signup", mode); err != nil {
			return Settings{}, err
		}
	}
	if req.DefaultPlan != nil {
		var value any
		if plan.ID != "" {
			value = plan.ID
		}
		if err := s.setSetting(ctx, "default_plan", value); err != nil {
			return Settings{}, err
		}
	}
	if req.DefaultRegions != nil {
		if err := s.setSetting(ctx, "default_regions", regions); err != nil {
			return Settings{}, err
		}
	}
	return s.getSettings(ctx)
}

// Instance describes this instance for the sign-in page. It is public.
func (s *Service) Instance(ctx context.Context) (Instance, error) {
	settings, err := s.getSettings(ctx)
	if err != nil {
		return Instance{}, err
	}
	return Instance{Name: settings.InstanceName, Signup: settings.Signup}, nil
}

// setSetting stores value as JSON under key.
func (s *Service) setSetting(ctx context.Context, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode setting %q: %w", key, err)
	}
	if err := s.q.UpsertSetting(ctx, db.UpsertSettingParams{Key: key, Value: string(data)}); err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}
	return nil
}
