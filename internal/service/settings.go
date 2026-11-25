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
	InstanceName  string  `json:"instance_name"`
	Signup        string  `json:"signup"`         // open | approval | closed
	DefaultPlan   *string `json:"default_plan"`   // a plan slug; nil = none
	DefaultRegion *string `json:"default_region"` // a cluster slug; nil = none
}

// UpdateSettingsRequest changes the fields that are set. DefaultPlan and
// DefaultRegion are slugs, or "none" to clear them.
type UpdateSettingsRequest struct {
	InstanceName  *string `json:"instance_name,omitempty"`
	Signup        *string `json:"signup,omitempty"`
	DefaultPlan   *string `json:"default_plan,omitempty"`
	DefaultRegion *string `json:"default_region,omitempty"`
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
	out := Settings{InstanceName: defaultInstanceName, Signup: signupClosed}
	rows, err := s.queries(ctx).ListSettings(ctx)
	if err != nil {
		return Settings{}, fmt.Errorf("list settings: %w", err)
	}
	planID := ""
	for _, row := range rows {
		if row.Key == "default_region" {
			var region *string
			if err := json.Unmarshal([]byte(row.Value), &region); err != nil {
				return Settings{}, fmt.Errorf("decode setting %q: %w", row.Key, err)
			}
			if region != nil && *region != "" {
				out.DefaultRegion = region
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
		plan, err := s.queries(ctx).GetPlanByID(ctx, planID)
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
	if req.InstanceName == nil && req.Signup == nil && req.DefaultPlan == nil && req.DefaultRegion == nil {
		return Settings{}, invalid("nothing to change: set instance_name, signup, default_plan or default_region")
	}
	before, err := s.getSettings(ctx)
	if err != nil {
		return Settings{}, err
	}
	name := ""
	if req.InstanceName != nil {
		name = strings.TrimSpace(*req.InstanceName)
		if name == "" {
			return Settings{}, invalidField("instance_name", "instance_name must not be empty")
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
			return Settings{}, invalidField("signup", "signup must be open, approval or closed")
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
	region := ""
	if req.DefaultRegion != nil {
		slug := strings.ToLower(strings.TrimSpace(*req.DefaultRegion))
		if slug != "" && !strings.EqualFold(slug, "none") {
			cluster, err := s.findCluster(ctx, slug)
			if err != nil {
				return Settings{}, err
			}
			region = cluster.Slug
		}
	}
	var after Settings
	err = s.withTx(ctx, func(ctx context.Context) error {
		if req.InstanceName != nil {
			if err := s.setSetting(ctx, "instance_name", name); err != nil {
				return err
			}
		}
		if req.Signup != nil {
			if err := s.setSetting(ctx, "signup", mode); err != nil {
				return err
			}
		}
		if req.DefaultPlan != nil {
			var value any
			if plan.ID != "" {
				value = plan.ID
			}
			if err := s.setSetting(ctx, "default_plan", value); err != nil {
				return err
			}
		}
		if req.DefaultRegion != nil {
			var value any
			if region != "" {
				value = region
			}
			if err := s.setSetting(ctx, "default_region", value); err != nil {
				return err
			}
		}
		var err error
		after, err = s.getSettings(ctx)
		if err != nil {
			return err
		}
		changes := settingsChanges(before, after)
		if len(changes) == 0 {
			return nil
		}
		return s.record(ctx, Event{
			Name:       "settings.updated",
			TargetType: "settings",
			TargetID:   "instance",
			TargetName: after.InstanceName,
			Changes:    changes,
		})
	})
	if err != nil {
		return Settings{}, err
	}
	return after, nil
}

// settingsChanges lists the settings an update actually changed.
func settingsChanges(before, after Settings) map[string]Change {
	changes := map[string]Change{}
	if before.InstanceName != after.InstanceName {
		changes["instance_name"] = Change{From: before.InstanceName, To: after.InstanceName}
	}
	if before.Signup != after.Signup {
		changes["signup"] = Change{From: before.Signup, To: after.Signup}
	}
	if optString(before.DefaultPlan) != optString(after.DefaultPlan) {
		changes["default_plan"] = Change{From: optString(before.DefaultPlan), To: optString(after.DefaultPlan)}
	}
	if optString(before.DefaultRegion) != optString(after.DefaultRegion) {
		changes["default_region"] = Change{From: optString(before.DefaultRegion), To: optString(after.DefaultRegion)}
	}
	return changes
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
	if err := s.queries(ctx).UpsertSetting(ctx, db.UpsertSettingParams{Key: key, Value: string(data)}); err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}
	return nil
}
