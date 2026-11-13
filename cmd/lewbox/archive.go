package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/harlew-dev/lewbox/internal/backend"
	"github.com/harlew-dev/lewbox/internal/service"
)

// newArchiveCmd mirrors /v0/archives.
func newArchiveCmd(a *app) *cobra.Command {
	archive := &cobra.Command{
		Use:     "archive",
		Aliases: []string{"archives"},
		Short:   "List, restore and delete archived sandboxes",
	}

	var listRegion string
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List archived sandboxes (GET /v0/archives)",
		Args:    usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			return b.ListArchives(ctx, service.RegionScope{Region: listRegion, Org: a.org})
		}),
	}
	list.Flags().StringVar(&listRegion, "region", "", "only archives in this region")

	show := &cobra.Command{
		Use:   "show <id>",
		Short: "Show an archived sandbox (GET /v0/archives/{id})",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.GetArchive(ctx, args[0], service.RegionScope{Org: a.org})
		}),
	}

	restore := &cobra.Command{
		Use:   "restore <id>",
		Short: "Restore an archived sandbox as a new sandbox (POST /v0/archives/{id}/restore)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.RestoreArchive(ctx, args[0], service.RegionScope{Org: a.org})
		}),
	}

	remove := &cobra.Command{
		Use:     "delete <id>",
		Aliases: []string{"rm"},
		Short:   "Delete an archived sandbox (DELETE /v0/archives/{id})",
		Args:    usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(fmt.Sprintf("Delete archive %q?", args[0])); err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.DeleteArchive(ctx, args[0], service.RegionScope{Org: a.org})
			})(cmd, args)
		},
	}

	archive.AddCommand(list, show, restore, remove)
	return archive
}
