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
		Short:   "Show, restore and delete archived sandboxes",
	}

	show := &cobra.Command{
		Use:   "show <id>",
		Short: "Show an archived sandbox (GET /v0/archives/{archive})",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.GetArchive(ctx, args[0], service.RegionScope{Org: a.org})
		}),
	}

	restore := &cobra.Command{
		Use:   "restore <id>",
		Short: "Restore an archived sandbox as a new sandbox (POST /v0/archives/{archive}/restore)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.RestoreArchive(ctx, args[0], service.RegionScope{Org: a.org})
		}),
	}

	remove := &cobra.Command{
		Use:     "delete <id>",
		Aliases: []string{"rm"},
		Short:   "Delete an archived sandbox (DELETE /v0/archives/{archive})",
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

	archive.AddCommand(show, restore, remove)
	return archive
}
