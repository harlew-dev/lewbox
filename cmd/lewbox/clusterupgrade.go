package main

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/harlew-dev/lewbox/internal/backend"
	"github.com/harlew-dev/lewbox/internal/machine"
	"github.com/harlew-dev/lewbox/internal/service"
)

// clusterUpgradeOptions describe `lewbox cluster upgrade`.
type clusterUpgradeOptions struct {
	version       string
	batch         string
	pauseAfter    int
	hold          bool
	allowDowntime bool
	check         bool
}

// newClusterUpgradeCmd is the rolling AerolVM upgrade of a cluster, and the
// commands that follow, pause, undo or finish it.
func newClusterUpgradeCmd(a *app) *cobra.Command {
	o := &clusterUpgradeOptions{}
	upgrade := &cobra.Command{
		Use:   "upgrade <slug>",
		Short: "Roll every node of a cluster to a new AerolVM release",
		Long: "Plan and start AerolVM's own rolling upgrade, one batch at a time, with running\n" +
			"sandboxes kept running. The plan is shown before anything changes; --check shows\n" +
			"it alone. Follow it with: cluster upgrade status|stop|resume|rollback|finalize.",
		Example: "  lewbox cluster upgrade eu-west --check\n" +
			"  lewbox cluster upgrade eu-west --version v0.7.1 --allow-downtime --yes",
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runClusterUpgrade(cmd.Context(), args[0], o)
		},
	}
	upgrade.Flags().StringVar(&o.version, "version", machine.PinnedVersion, "the AerolVM release to move to (default: the version this Lewbox release was tested with)")
	upgrade.Flags().StringVar(&o.batch, "batch", "", "worker batch size: a count or a percentage such as 10%")
	upgrade.Flags().IntVar(&o.pauseAfter, "pause-after", 0, "pause after this many workers, to test the release before going on")
	upgrade.Flags().BoolVar(&o.hold, "hold", false, "stop at done, before finalize, so a rollback stays possible")
	upgrade.Flags().BoolVar(&o.allowDowntime, "allow-downtime", false, "accept that the API is down while one of one or two Raft voters restarts")
	upgrade.Flags().BoolVar(&o.check, "check", false, "show the plan and change nothing")

	status := &cobra.Command{
		Use:   "status <slug>",
		Short: "Show where a cluster's upgrade stands",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.GetClusterUpgrade(ctx, args[0])
		}),
	}
	stop := &cobra.Command{
		Use:   "stop <slug>",
		Short: "Finish the nodes in progress and start no more",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.StopClusterUpgrade(ctx, args[0])
		}),
	}
	resume := &cobra.Command{
		Use:   "resume <slug>",
		Short: "Go on after a pause",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.ResumeClusterUpgrade(ctx, args[0])
		}),
	}
	rollback := &cobra.Command{
		Use:   "rollback <slug>",
		Short: "Put every node back on the previous AerolVM release",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm("Roll back the AerolVM upgrade on cluster " + args[0] + "?"); err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.RollbackClusterUpgrade(ctx, args[0])
			})(cmd, args)
		},
	}
	finalize := &cobra.Command{
		Use:   "finalize <slug>",
		Short: "Turn the new AerolVM release on for good",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm("Finalize the AerolVM upgrade on cluster " + args[0] + "? Rollback will no longer be possible."); err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.FinalizeClusterUpgrade(ctx, args[0])
			})(cmd, args)
		},
	}

	upgrade.AddCommand(status, stop, resume, rollback, finalize)
	return upgrade
}

// runClusterUpgrade plans, shows and confirms the upgrade before starting it.
func (a *app) runClusterUpgrade(ctx context.Context, ref string, o *clusterUpgradeOptions) error {
	b, _, err := a.resolve(ctx)
	if err != nil {
		return err
	}
	req := service.PlanClusterUpgradeRequest{
		Version:    o.version,
		Batch:      o.batch,
		PauseAfter: o.pauseAfter,
	}
	plan, err := b.PlanClusterUpgrade(ctx, ref, req)
	if err != nil {
		return err
	}
	if o.check {
		return a.write(plan)
	}
	if err := renderClusterUpgradePlan(a.stderr, plan); err != nil {
		return err
	}
	if !plan.Allowed {
		return &service.Error{
			Code:    service.CodeInvalid,
			Message: strings.Join(plan.Refusals, "; "),
			Details: service.FieldDetails{Field: "version"},
		}
	}
	if err := a.confirm("Upgrade cluster " + ref + " to AerolVM " + plan.TargetVersion + "? Nodes restart one at a time."); err != nil {
		return err
	}
	out, err := b.StartClusterUpgrade(ctx, ref, service.StartClusterUpgradeRequest{
		Version:       plan.TargetVersion,
		Batch:         o.batch,
		PauseAfter:    o.pauseAfter,
		Hold:          o.hold,
		AllowDowntime: o.allowDowntime,
	})
	if err != nil {
		return err
	}
	return a.write(out)
}
