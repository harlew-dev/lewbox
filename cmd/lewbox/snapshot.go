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

	var listRegion string
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List snapshots (GET /v0/snapshots)",
		Args:    usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			return b.ListSnapshots(ctx, service.ListSnapshotsRequest{Region: listRegion, Org: a.org})
		}),
	}
	list.Flags().StringVar(&listRegion, "region", "", "only snapshots in this region")

	var deleteRegion string
	remove := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"rm"},
		Short:   "Delete a snapshot (DELETE /v0/snapshots/{name})",
		Args:    usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(fmt.Sprintf("Delete snapshot %q?", args[0])); err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.DeleteSnapshot(ctx, args[0], service.SnapshotActionRequest{Region: deleteRegion, Org: a.org})
			})(cmd, args)
		},
	}
	remove.Flags().StringVar(&deleteRegion, "region", "", "the region to delete it in, when the name is not unique")

	snapshot.AddCommand(list, remove)
	return snapshot
}
