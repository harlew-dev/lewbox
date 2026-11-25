package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/harlew-dev/lewbox/internal/backend"
	"github.com/harlew-dev/lewbox/internal/service"
)

// newEventsCmd lists the audit trail, newest first.
func newEventsCmd(a *app) *cobra.Command {
	var req service.ListEventsRequest
	cmd := &cobra.Command{
		Use:   "events",
		Short: "List the audit trail (GET /v0/events)",
		Long: "List the audit trail, newest first. Every state change made through\n" +
			"Lewbox records who did it, what changed and from where. Instance admins only.",
		Args: usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			return b.ListEvents(ctx, req)
		}),
	}
	cmd.Flags().StringVar(&req.Before, "before", "", "continue after this event ID (the previous page's next value)")
	cmd.Flags().StringVar(&req.Actor, "actor", "", "only events by these account IDs or emails, comma separated")
	cmd.Flags().StringVar(&req.Organization, "workspace", "", "only events in these workspace IDs or slugs, comma separated")
	cmd.Flags().StringVar(&req.Action, "action", "", "only these event names, such as plan.updated, comma separated")
	cmd.Flags().StringVar(&req.From, "from", "", "only events at or after this RFC 3339 time")
	cmd.Flags().StringVar(&req.To, "to", "", "only events before this RFC 3339 time")
	cmd.Flags().IntVar(&req.Limit, "limit", 0, "how many events to return (default 50, at most 200)")
	return cmd
}
