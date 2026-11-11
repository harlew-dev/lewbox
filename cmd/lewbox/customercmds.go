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

// parseCountLimit reads a whole-number limit. "unlimited" or "0" clears it.
func parseCountLimit(raw string) (service.OptionalInt, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" || s == "unlimited" || s == "0" {
		return service.OptionalInt{Set: true}, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return service.OptionalInt{}, usagef("%q is not a whole number: use a number, 0 or unlimited", raw)
	}
	return service.OptionalInt{Set: true, Value: &n}, nil
}

// parseMemoryLimit reads a memory limit in MB. It accepts 4096, 4096MB and
// 4GB; "unlimited" or "0" clears it.
func parseMemoryLimit(raw string) (service.OptionalInt, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" || s == "unlimited" || s == "0" {
		return service.OptionalInt{Set: true}, nil
	}
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
	if err != nil || n < 0 || n > math.MaxInt64/multiplier {
		return service.OptionalInt{}, usagef("%q is not a memory size: use 4096, 4096MB or 4GB", raw)
	}
	n *= multiplier
	return service.OptionalInt{Set: true, Value: &n}, nil
}

// parseLimitFlag parses a flag only when it was given.
func parseLimitFlag(cmd *cobra.Command, flag, raw string, parse func(string) (service.OptionalInt, error)) (service.OptionalInt, error) {
	if !cmd.Flags().Changed(flag) {
		return service.OptionalInt{}, nil
	}
	return parse(raw)
}

func newRegionCmd(a *app) *cobra.Command {
	region := &cobra.Command{
		Use:     "region",
		Aliases: []string{"regions"},
		Short:   "List the clusters customers can use",
	}
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List regions (GET /v0/regions)",
		Args:    usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			return b.ListRegions(ctx)
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

	var name, signup, defaultPlan string
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
			if req.InstanceName == nil && req.Signup == nil && req.DefaultPlan == nil {
				return usagef("nothing to change: pass --name, --signup or --default-plan")
			}
			return a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
				return b.UpdateSettings(ctx, req)
			})(cmd, nil)
		},
	}
	set.Flags().StringVar(&name, "name", "", "the instance's display name")
	set.Flags().StringVar(&signup, "signup", "", "open, approval or closed")
	set.Flags().StringVar(&defaultPlan, "default-plan", "", "the plan new sign-ups get, by slug")

	settings.AddCommand(set)
	return settings
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
			sandboxes, err := parseLimitFlag(cmd, "max-sandboxes", createSandboxes, parseCountLimit)
			if err != nil {
				return err
			}
			vcpus, err := parseLimitFlag(cmd, "max-vcpus", createVcpus, parseCountLimit)
			if err != nil {
				return err
			}
			memory, err := parseLimitFlag(cmd, "max-memory", createMemory, parseMemoryLimit)
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
	create.Flags().StringVar(&createSandboxes, "max-sandboxes", "", "maximum sandboxes (0 or unlimited for none)")
	create.Flags().StringVar(&createVcpus, "max-vcpus", "", "maximum vCPUs (0 or unlimited for none)")
	create.Flags().StringVar(&createMemory, "max-memory", "", "maximum memory: 4096, 4096MB or 4GB (0 or unlimited for none)")

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
			if req.MaxSandboxes, err = parseLimitFlag(cmd, "max-sandboxes", updateSandboxes, parseCountLimit); err != nil {
				return err
			}
			if req.MaxVcpus, err = parseLimitFlag(cmd, "max-vcpus", updateVcpus, parseCountLimit); err != nil {
				return err
			}
			if req.MaxMemoryMB, err = parseLimitFlag(cmd, "max-memory", updateMemory, parseMemoryLimit); err != nil {
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
	update.Flags().StringVar(&updateSandboxes, "max-sandboxes", "", "maximum sandboxes (0 or unlimited for none)")
	update.Flags().StringVar(&updateVcpus, "max-vcpus", "", "maximum vCPUs (0 or unlimited for none)")
	update.Flags().StringVar(&updateMemory, "max-memory", "", "maximum memory: 4096, 4096MB or 4GB (0 or unlimited for none)")

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
