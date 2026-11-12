package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/harlew-dev/lewbox/internal/backend"
	"github.com/harlew-dev/lewbox/internal/service"
)

// setupLinkHours converts --expires to the whole hours the API takes.
func setupLinkHours(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	var d time.Duration
	if days, ok := strings.CutSuffix(raw, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, usagef("--expires must be a duration such as 7d or 48h")
		}
		d = time.Duration(n) * 24 * time.Hour
	} else {
		var err error
		if d, err = time.ParseDuration(raw); err != nil {
			return 0, usagef("--expires must be a duration such as 7d or 48h")
		}
	}
	if d < time.Hour || d > 720*time.Hour || d%time.Hour != 0 {
		return 0, usagef("--expires must be between 1h and 720h, in whole hours")
	}
	return int(d / time.Hour), nil
}

// newSetupLinkCmd mirrors /v0/setup-links.
func newSetupLinkCmd(a *app) *cobra.Command {
	setup := &cobra.Command{
		Use:     "setup-link",
		Aliases: []string{"setup-links"},
		Short:   "Create single-use onboarding links for customers (instance admins)",
	}

	var plan, region, expires, note string
	create := &cobra.Command{
		Use:   "create",
		Short: "Create a setup link; its lbs_ token is shown once (POST /v0/setup-links)",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(plan) == "" || strings.TrimSpace(region) == "" {
				return usagef("pass --plan and --region")
			}
			hours, err := setupLinkHours(expires)
			if err != nil {
				return err
			}
			b, sel, err := a.resolve(cmd.Context())
			if err != nil {
				return err
			}
			link, err := b.CreateSetupLink(cmd.Context(), service.CreateSetupLinkRequest{
				Plan:           plan,
				Region:         region,
				ExpiresInHours: hours,
				Note:           note,
			})
			if err != nil {
				return err
			}
			if a.jsonMode() {
				return a.write(link)
			}
			return renderSetupLinkCreated(a.stdout, link, sel.APIURL, time.Now())
		},
	}
	create.Flags().StringVar(&plan, "plan", "", "the plan's slug (required)")
	create.Flags().StringVar(&region, "region", "", "the region's slug (required)")
	create.Flags().StringVar(&expires, "expires", "7d", "how long the link works, such as 7d or 48h, from 1h to 720h")
	create.Flags().StringVar(&note, "note", "", "a note for yourself")

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List setup links, newest first (GET /v0/setup-links)",
		Args:    usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			return b.ListSetupLinks(ctx)
		}),
	}

	revoke := &cobra.Command{
		Use:     "revoke <id>",
		Aliases: []string{"rm", "delete"},
		Short:   "Revoke an unused setup link (DELETE /v0/setup-links/{id})",
		Args:    usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(fmt.Sprintf("Revoke setup link %q? Anyone with it can no longer join.", args[0])); err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.RevokeSetupLink(ctx, args[0])
			})(cmd, args)
		},
	}

	setup.AddCommand(create, list, revoke)
	return setup
}
