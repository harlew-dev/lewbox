package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/harlew-dev/lewbox/internal/backend"
	"github.com/harlew-dev/lewbox/internal/service"
)

// newSandboxCmd mirrors /v0/sandboxes.
func newSandboxCmd(a *app) *cobra.Command {
	sandbox := &cobra.Command{
		Use:     "sandbox",
		Aliases: []string{"sandboxes"},
		Short:   "List, stop and delete sandboxes",
	}

	var (
		region, status, member string
		all                    bool
	)
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List sandboxes (GET /v0/sandboxes)",
		Args:    usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			org := a.org
			if all {
				org = ""
			}
			return b.ListSandboxes(ctx, service.ListSandboxesRequest{Region: region, Status: status, Org: org, Member: member})
		}),
	}
	list.Flags().StringVar(&region, "region", "", "only sandboxes in this region")
	list.Flags().StringVar(&status, "status", "", "only sandboxes with this status")
	list.Flags().StringVar(&member, "member", "", "only this member's sandboxes (email or ID)")
	list.Flags().BoolVar(&all, "all", false, "every organization (instance admins)")

	var stopRegion string
	stop := &cobra.Command{
		Use:   "stop <id>",
		Short: "Stop a sandbox (POST /v0/sandboxes/{id}/stop)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.StopSandbox(ctx, args[0], service.SandboxActionRequest{Region: stopRegion, Org: a.org})
		}),
	}
	stop.Flags().StringVar(&stopRegion, "region", "", "the region to stop it in, when the ID is not unique")

	var deleteRegion string
	remove := &cobra.Command{
		Use:     "delete <id>",
		Aliases: []string{"rm"},
		Short:   "Delete a sandbox (DELETE /v0/sandboxes/{id})",
		Args:    usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(fmt.Sprintf("Delete sandbox %q?", args[0])); err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.DeleteSandbox(ctx, args[0], service.SandboxActionRequest{Region: deleteRegion, Org: a.org})
			})(cmd, args)
		},
	}
	remove.Flags().StringVar(&deleteRegion, "region", "", "the region to delete it in, when the ID is not unique")

	sandbox.AddCommand(list, stop, remove)
	return sandbox
}

// newUsageCmd mirrors /v0/usage.
func newUsageCmd(a *app) *cobra.Command {
	var from, to, member string
	var hourly bool
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "Show metered usage (GET /v0/usage)",
		Args:  usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			granularity := ""
			if hourly {
				granularity = "hour"
			}
			return b.Usage(ctx, service.UsageRequest{From: from, To: to, Granularity: granularity, Org: a.org, Member: member})
		}),
	}
	cmd.Flags().StringVar(&from, "from", "", "start date (default: 30 days ago)")
	cmd.Flags().StringVar(&to, "to", "", "end date (default: now)")
	cmd.Flags().StringVar(&member, "member", "", "only this member's usage (email or ID)")
	cmd.Flags().BoolVar(&hourly, "hourly", false, "one bucket per hour instead of per day")
	return cmd
}

// newLimitsCmd mirrors /v0/limits.
func newLimitsCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "limits",
		Short: "Show limits and live use (GET /v0/limits)",
		Args:  usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			return b.Limits(ctx, a.org)
		}),
	}
}
