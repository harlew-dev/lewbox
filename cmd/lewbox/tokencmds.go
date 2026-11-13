package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/harlew-dev/lewbox/internal/backend"
	"github.com/harlew-dev/lewbox/internal/service"
)

// newTokenCmd mirrors /v0/access-tokens: an account's tokens for the Lewbox
// API (D38).
func newTokenCmd(a *app) *cobra.Command {
	token := &cobra.Command{
		Use:     "token",
		Aliases: []string{"tokens", "access-token", "access-tokens"},
		Short:   "Manage your access tokens for the Lewbox API",
		Long: `An access token (lbp_…) acts as your account, with your account's rights:
an instance admin's token can manage clusters, nodes and users. Use one for
scripts and CI, with LEWBOX_TOKEN or lewbox login --token.

Instance admins can manage another account's tokens with --user. In local
mode, which has no account of its own, --user is required.`,
	}

	var (
		user         string
		expiresIn    string
		neverExpires bool
	)
	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create an access token; the lbp_ token is shown once (POST /v0/access-tokens)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if neverExpires && cmd.Flags().Changed("expires-in") {
				return usagef("--expires-in and --never-expires cannot be used together")
			}
			lifetime := expiresIn
			if neverExpires {
				lifetime = ""
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.CreateAccessToken(ctx, service.CreateAccessTokenRequest{Name: args[0], ExpiresIn: lifetime, User: user})
			})(cmd, args)
		},
	}
	create.Flags().StringVar(&expiresIn, "expires-in", "90d", "how long the token works, such as 30d or 720h")
	create.Flags().BoolVar(&neverExpires, "never-expires", false, "the token works until it is revoked")

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List access tokens, revoked ones included (GET /v0/access-tokens)",
		Args:    usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			return b.ListAccessTokens(ctx, user)
		}),
	}

	revoke := &cobra.Command{
		Use:     "revoke <id|name>",
		Aliases: []string{"rm", "remove"},
		Short:   "Revoke an access token; it stops working at once (DELETE /v0/access-tokens/{id|name})",
		Args:    usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(fmt.Sprintf("Revoke access token %q? Anything using it stops working.", args[0])); err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.RevokeAccessToken(ctx, user, args[0])
			})(cmd, args)
		},
	}

	for _, c := range []*cobra.Command{create, list, revoke} {
		c.Flags().StringVar(&user, "user", "", "another account's tokens, by ID or email (instance admins; required in local mode)")
	}
	token.AddCommand(create, list, revoke)
	return token
}
