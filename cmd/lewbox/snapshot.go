package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/harlew-dev/lewbox/internal/backend"
	"github.com/harlew-dev/lewbox/internal/service"
)

// newSnapshotCmd mirrors /v0/snapshots.
func newSnapshotCmd(a *app) *cobra.Command {
	snapshot := &cobra.Command{
		Use:     "snapshot",
		Aliases: []string{"snapshots"},
		Short:   "List and delete sandbox snapshots",
	}

	var listRegion, listOrganization, listKind, listStatus string
	var listSummary bool
	var listQuery service.ListQuery
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List snapshots (GET /v0/snapshots)",
		Args:    usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			return b.ListSnapshots(ctx, service.ListSnapshotsRequest{
				ListQuery:    listQuery,
				Region:       listRegion,
				Org:          a.org,
				Organization: listOrganization,
				Kind:         listKind,
				Status:       listStatus,
				Summary:      summaryValue(listSummary),
			})
		}),
	}
	list.Flags().StringVar(&listRegion, "region", "", "only snapshots in these regions, comma separated")
	list.Flags().StringVar(&listOrganization, "organization", "", "only these workspaces, comma separated (instance admins)")
	list.Flags().StringVar(&listKind, "kind", "", "only snapshot or archive, comma separated")
	list.Flags().StringVar(&listStatus, "status", "", "only pending, pushing, active or error, comma separated")
	list.Flags().BoolVar(&listSummary, "summary", false, "include totals over the whole filtered set")
	addListFlags(list, &listQuery, "-created, name or size; prefix with - to reverse")

	var deleteRegion string
	remove := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"rm"},
		Short:   "Delete a snapshot (DELETE /v0/snapshots/{snapshot})",
		Args:    usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(fmt.Sprintf("Delete snapshot %q?", args[0])); err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.DeleteSnapshot(ctx, args[0], service.RegionScope{Region: deleteRegion, Org: a.org})
			})(cmd, args)
		},
	}
	remove.Flags().StringVar(&deleteRegion, "region", "", "the region to delete it in, when the name is not unique")

	snapshot.AddCommand(list, remove)
	return snapshot
}

// summaryValue maps the CLI's --summary to the API's value.
func summaryValue(include bool) string {
	if include {
		return "include"
	}
	return ""
}
