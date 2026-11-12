package main

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/harlew-dev/lewbox/internal/backend"
	"github.com/harlew-dev/lewbox/internal/service"
)

// limitTarget decides what a null limit means.
type limitTarget int

const (
	planLimits         limitTarget = iota // null is unlimited
	subscriptionLimits                    // null falls back to the plan
)

// limitWord handles the word that clears a limit. The other target's word is
// refused with what to do instead.
func limitWord(raw string, target limitTarget) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return true, nil
	case "unlimited":
		if target == subscriptionLimits {
			return false, usagef("put the organization on a plan without that limit")
		}
		return true, nil
	case "plan":
		if target == planLimits {
			return false, usagef("a plan limit is a positive number or unlimited")
		}
		return true, nil
	}
	return false, nil
}

// parseCountLimit reads a whole-number limit.
func parseCountLimit(raw string, target limitTarget) (service.OptionalInt, error) {
	if clear, err := limitWord(raw, target); clear || err != nil {
		if err != nil {
			return service.OptionalInt{}, err
		}
		return service.OptionalInt{Set: true}, nil
	}
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n < 1 {
		return service.OptionalInt{}, usagef("%q is not a whole number: use a positive number", raw)
	}
	return service.OptionalInt{Set: true, Value: &n}, nil
}

// parseMemoryLimit reads a memory limit in MB. It accepts 4096, 4096MB and
// 4GB.
func parseMemoryLimit(raw string, target limitTarget) (service.OptionalInt, error) {
	if clear, err := limitWord(raw, target); clear || err != nil {
		if err != nil {
			return service.OptionalInt{}, err
		}
		return service.OptionalInt{Set: true}, nil
	}
	s := strings.ToLower(strings.TrimSpace(raw))
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(s, "gb"):
		s, multiplier = strings.TrimSuffix(s, "gb"), 1024
	case strings.HasSuffix(s, "mb"):
		s = strings.TrimSuffix(s, "mb")
	case strings.HasSuffix(s, "g"):
		s, multiplier = strings.TrimSuffix(s, "g"), 1024
	case strings.HasSuffix(s, "m"):
		s = strings.TrimSuffix(s, "m")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n < 1 || n > math.MaxInt64/multiplier {
		return service.OptionalInt{}, usagef("%q is not a memory size: use a positive size such as 4096, 4096MB or 4GB", raw)
	}
	n *= multiplier
	return service.OptionalInt{Set: true, Value: &n}, nil
}

// parseLimitFlag parses a flag only when it was given.
func parseLimitFlag(cmd *cobra.Command, flag, raw string, target limitTarget, parse func(string, limitTarget) (service.OptionalInt, error)) (service.OptionalInt, error) {
	if !cmd.Flags().Changed(flag) {
		return service.OptionalInt{}, nil
	}
	return parse(raw, target)
}

func newRegionCmd(a *app) *cobra.Command {
	region := &cobra.Command{
		Use:     "region",
		Aliases: []string{"regions"},
		Short:   "List the regions customers can use",
	}
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List regions (GET /v0/regions)",
		Args:    usageArgs(cobra.NoArgs),
		RunE: a.callOrg(func(ctx context.Context, b backend.Backend, org string, _ []string) (any, error) {
			return b.ListRegions(ctx, org)
		}),
	}
	region.AddCommand(list)
	return region
}

func newSettingsCmd(a *app) *cobra.Command {
	settings := &cobra.Command{
		Use:   "settings",
		Short: "Show the instance settings (GET /v0/settings)",
		Args:  usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			return b.GetSettings(ctx)
		}),
	}

	var name, signup, defaultPlan, defaultRegion string
	set := &cobra.Command{
		Use:   "set",
		Short: "Change the instance settings (PATCH /v0/settings)",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			req := service.UpdateSettingsRequest{}
			if cmd.Flags().Changed("name") {
				req.InstanceName = &name
			}
			if cmd.Flags().Changed("signup") {
				req.Signup = &signup
			}
			if cmd.Flags().Changed("default-plan") {
				req.DefaultPlan = &defaultPlan
			}
			if cmd.Flags().Changed("default-region") {
				req.DefaultRegion = &defaultRegion
			}
			if req.InstanceName == nil && req.Signup == nil && req.DefaultPlan == nil && req.DefaultRegion == nil {
				return usagef("nothing to change: pass --name, --signup, --default-plan or --default-region")
			}
			return a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
				return b.UpdateSettings(ctx, req)
			})(cmd, nil)
		},
	}
	set.Flags().StringVar(&name, "name", "", "the instance's display name")
	set.Flags().StringVar(&signup, "signup", "", "open, approval or closed")
	set.Flags().StringVar(&defaultPlan, "default-plan", "", "the plan new workspaces get, by slug, or none")
	set.Flags().StringVar(&defaultRegion, "default-region", "", "the region slug new workspaces get, or none")

	settings.AddCommand(set)
	return settings
}

func newSubscriptionCmd(a *app) *cobra.Command {
	subscription := &cobra.Command{
		Use:     "subscription",
		Aliases: []string{"subscriptions"},
		Short:   "Assign a plan to an organization in a region (--org)",
	}

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List an organization's subscriptions (GET /v0/subscriptions)",
		Args:    usageArgs(cobra.NoArgs),
		RunE: a.callOrg(func(ctx context.Context, b backend.Backend, org string, _ []string) (any, error) {
			return b.ListSubscriptions(ctx, org)
		}),
	}

	var setPlan, setSandboxes, setVcpus, setMemory string
	set := &cobra.Command{
		Use:   "set <region>",
		Short: "Create or change a subscription (PUT /v0/subscriptions/{region})",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("plan") {
				return usagef("pass --plan <slug>")
			}
			req := service.SetSubscriptionRequest{Plan: setPlan}
			var err error
			if req.MaxSandboxes, err = parseLimitFlag(cmd, "max-sandboxes", setSandboxes, subscriptionLimits, parseCountLimit); err != nil {
				return err
			}
			if req.MaxVcpus, err = parseLimitFlag(cmd, "max-vcpus", setVcpus, subscriptionLimits, parseCountLimit); err != nil {
				return err
			}
			if req.MaxMemoryMB, err = parseLimitFlag(cmd, "max-memory", setMemory, subscriptionLimits, parseMemoryLimit); err != nil {
				return err
			}
			return a.callOrg(func(ctx context.Context, b backend.Backend, org string, args []string) (any, error) {
				return b.SetSubscription(ctx, org, args[0], req)
			})(cmd, args)
		},
	}
	set.Flags().StringVar(&setPlan, "plan", "", "the plan's slug (required)")
	set.Flags().StringVar(&setSandboxes, "max-sandboxes", "", "maximum sandboxes: a positive number, or plan (the plan's limit)")
	set.Flags().StringVar(&setVcpus, "max-vcpus", "", "maximum vCPUs: a positive number, or plan (the plan's limit)")
	set.Flags().StringVar(&setMemory, "max-memory", "", "maximum memory: a positive size such as 4096, 4096MB or 4GB, or plan (the plan's limit)")

	var force bool
	remove := &cobra.Command{
		Use:     "remove <region>",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a subscription and block the organization there (DELETE /v0/subscriptions/{region})",
		Args:    usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(fmt.Sprintf("Remove the plan in %q? Its keys there stop working.", args[0])); err != nil {
				return err
			}
			return a.callOrg(func(ctx context.Context, b backend.Backend, org string, args []string) (any, error) {
				return b.DeleteSubscription(ctx, org, args[0], force)
			})(cmd, args)
		},
	}
	remove.Flags().BoolVar(&force, "force", false, "remove it even if its sandbox keys cannot be revoked")

	subscription.AddCommand(list, set, remove)
	return subscription
}

func newPlanCmd(a *app) *cobra.Command {
	plan := &cobra.Command{
		Use:     "plan",
		Aliases: []string{"plans"},
		Short:   "Manage plans and their limits",
	}

	var createName, createSandboxes, createVcpus, createMemory string
	create := &cobra.Command{
		Use:   "create <slug>",
		Short: "Create a plan (POST /v0/plans)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			sandboxes, err := parseLimitFlag(cmd, "max-sandboxes", createSandboxes, planLimits, parseCountLimit)
			if err != nil {
				return err
			}
			vcpus, err := parseLimitFlag(cmd, "max-vcpus", createVcpus, planLimits, parseCountLimit)
			if err != nil {
				return err
			}
			memory, err := parseLimitFlag(cmd, "max-memory", createMemory, planLimits, parseMemoryLimit)
			if err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.CreatePlan(ctx, service.CreatePlanRequest{
					Slug:         args[0],
					Name:         createName,
					MaxSandboxes: sandboxes.Value,
					MaxVcpus:     vcpus.Value,
					MaxMemoryMB:  memory.Value,
				})
			})(cmd, args)
		},
	}
	create.Flags().StringVar(&createName, "name", "", "a display name (default: the slug)")
	create.Flags().StringVar(&createSandboxes, "max-sandboxes", "", "maximum sandboxes: a positive number, or unlimited")
	create.Flags().StringVar(&createVcpus, "max-vcpus", "", "maximum vCPUs: a positive number, or unlimited")
	create.Flags().StringVar(&createMemory, "max-memory", "", "maximum memory: a positive size such as 4096, 4096MB or 4GB, or unlimited")

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List plans (GET /v0/plans)",
		Args:    usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			return b.ListPlans(ctx)
		}),
	}

	show := &cobra.Command{
		Use:   "show <id|slug>",
		Short: "Show a plan (GET /v0/plans/{id|slug})",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.GetPlan(ctx, args[0])
		}),
	}

	var updateName, updateSandboxes, updateVcpus, updateMemory string
	update := &cobra.Command{
		Use:   "update <id|slug>",
		Short: "Change a plan (PATCH /v0/plans/{id|slug})",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := service.UpdatePlanRequest{}
			if cmd.Flags().Changed("name") {
				req.Name = &updateName
			}
			var err error
			if req.MaxSandboxes, err = parseLimitFlag(cmd, "max-sandboxes", updateSandboxes, planLimits, parseCountLimit); err != nil {
				return err
			}
			if req.MaxVcpus, err = parseLimitFlag(cmd, "max-vcpus", updateVcpus, planLimits, parseCountLimit); err != nil {
				return err
			}
			if req.MaxMemoryMB, err = parseLimitFlag(cmd, "max-memory", updateMemory, planLimits, parseMemoryLimit); err != nil {
				return err
			}
			if req.Name == nil && !req.MaxSandboxes.Set && !req.MaxVcpus.Set && !req.MaxMemoryMB.Set {
				return usagef("nothing to change: pass --name, --max-sandboxes, --max-vcpus or --max-memory")
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.UpdatePlan(ctx, args[0], req)
			})(cmd, args)
		},
	}
	update.Flags().StringVar(&updateName, "name", "", "the new display name")
	update.Flags().StringVar(&updateSandboxes, "max-sandboxes", "", "maximum sandboxes: a positive number, or unlimited")
	update.Flags().StringVar(&updateVcpus, "max-vcpus", "", "maximum vCPUs: a positive number, or unlimited")
	update.Flags().StringVar(&updateMemory, "max-memory", "", "maximum memory: a positive size such as 4096, 4096MB or 4GB, or unlimited")

	remove := &cobra.Command{
		Use:     "delete <id|slug>",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete a plan no organization uses (DELETE /v0/plans/{id|slug})",
		Args:    usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(fmt.Sprintf("Delete plan %q?", args[0])); err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.DeletePlan(ctx, args[0])
			})(cmd, args)
		},
	}

	plan.AddCommand(list, show, create, update, remove)
	return plan
}
