package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/harlew-dev/lewbox/internal/backend"
	"github.com/harlew-dev/lewbox/internal/cliconfig"
	"github.com/harlew-dev/lewbox/internal/service"
)

// orgUseOutput is printed by `org use`.
type orgUseOutput struct {
	Context string `json:"context"`
	Org     string `json:"org"`
}

// orgFor is the organization a member or admin-key command acts on: --org
// (or LEWBOX_ORG), then the context's default from `org use`. Empty leaves
// it to an admin key's own organization, and otherwise the server asks for
// one.
func (a *app) orgFor(sel cliconfig.Selection) string {
	if org := strings.TrimSpace(a.org); org != "" {
		return org
	}
	if store, err := a.store(); err == nil {
		if ctx, ok := store.Context(sel.Context); ok {
			return ctx.Org
		}
	}
	return ""
}

// callOrg is call for commands that act on one organization.
func (a *app) callOrg(fn func(ctx context.Context, b backend.Backend, org string, args []string) (any, error)) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		b, sel, err := a.resolve(cmd.Context())
		if err != nil {
			return err
		}
		resp, err := fn(cmd.Context(), b, a.orgFor(sel), args)
		if err != nil {
			return err
		}
		return a.write(resp)
	}
}

// newOrgCmd mirrors /v0/organizations.
func newOrgCmd(a *app) *cobra.Command {
	org := &cobra.Command{
		Use:     "org",
		Aliases: []string{"orgs", "organization", "organizations"},
		Short:   "Manage organizations",
	}

	var createName, createOwner string
	create := &cobra.Command{
		Use:   "create <slug>",
		Short: "Create an organization (POST /v0/organizations)",
		Long:  "Create an organization. Instance admins only. The owner defaults to you (none in local mode).",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.CreateOrganization(ctx, service.CreateOrganizationRequest{
				Slug:  args[0],
				Name:  createName,
				Owner: createOwner,
			})
		}),
	}
	create.Flags().StringVar(&createName, "name", "", "a display name (default: the slug)")
	create.Flags().StringVar(&createOwner, "owner", "", "the first owner's email (default: you)")

	var listUser string
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List organizations (GET /v0/organizations)",
		Args:    usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			return b.ListOrganizations(ctx, service.ListOrganizationsRequest{User: listUser})
		}),
	}
	list.Flags().StringVar(&listUser, "user", "", "list this account's organizations and its role in each")

	get := &cobra.Command{
		Use:   "get [<id|slug>]",
		Short: "Show an organization (GET /v0/organizations/{id|slug})",
		Long:  "Show an organization: the one named, or --org, the context's default, or an admin key's own (GET /v0/organization).",
		Args:  usageArgs(cobra.MaximumNArgs(1)),
		RunE: a.callOrg(func(ctx context.Context, b backend.Backend, org string, args []string) (any, error) {
			if len(args) == 1 {
				org = args[0]
			}
			return b.GetOrganization(ctx, org)
		}),
	}

	var updateName string
	update := &cobra.Command{
		Use:   "update <id|slug>",
		Short: "Rename an organization (PATCH /v0/organizations/{id|slug})",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("name") {
				return usagef("nothing to change: pass --name")
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.UpdateOrganization(ctx, args[0], service.UpdateOrganizationRequest{Name: &updateName})
			})(cmd, args)
		},
	}
	update.Flags().StringVar(&updateName, "name", "", "the new display name")

	status := func(use, short, value, question string) *cobra.Command {
		return &cobra.Command{
			Use:   use + " <id|slug>",
			Short: short,
			Args:  usageArgs(cobra.ExactArgs(1)),
			RunE: func(cmd *cobra.Command, args []string) error {
				if question != "" {
					if err := a.confirm(fmt.Sprintf(question, args[0])); err != nil {
						return err
					}
				}
				return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
					return b.UpdateOrganization(ctx, args[0], service.UpdateOrganizationRequest{Status: &value})
				})(cmd, args)
			},
		}
	}
	suspend := status("suspend", "Suspend an organization: its admin keys stop working (PATCH /v0/organizations/{id|slug})",
		"suspended", "Suspend organization %q? Its admin keys stop working.")
	restore := status("restore", "Restore a suspended organization (PATCH /v0/organizations/{id|slug})", "active", "")

	use := &cobra.Command{
		Use:   "use <id|slug>",
		Short: "Make an organization this context's default for member and admin-key commands",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, sel, err := a.resolve(cmd.Context())
			if err != nil {
				return err
			}
			if sel.Mode == cliconfig.ModeLocal {
				return usagef("the local context has no default organization: pass --org or set LEWBOX_ORG")
			}
			org, err := b.GetOrganization(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			store, err := a.store()
			if err != nil {
				return err
			}
			if err := store.SetOrg(sel.Context, org.Slug); err != nil {
				return err
			}
			if err := store.Save(); err != nil {
				return err
			}
			return a.write(orgUseOutput{Context: sel.Context, Org: org.Slug})
		},
	}

	org.AddCommand(create, list, get, update, suspend, restore, use)
	return org
}

// newMemberCmd mirrors /v0/members.
func newMemberCmd(a *app) *cobra.Command {
	member := &cobra.Command{
		Use:     "member",
		Aliases: []string{"members"},
		Short:   "Manage the members of an organization (--org)",
	}

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List members (GET /v0/members)",
		Args:    usageArgs(cobra.NoArgs),
		RunE: a.callOrg(func(ctx context.Context, b backend.Backend, org string, _ []string) (any, error) {
			return b.ListMembers(ctx, org)
		}),
	}

	var addRole string
	add := &cobra.Command{
		Use:   "add <email>",
		Short: "Give an existing account a role in the organization (POST /v0/members)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.callOrg(func(ctx context.Context, b backend.Backend, org string, args []string) (any, error) {
			return b.AddMember(ctx, org, service.AddMemberRequest{Email: args[0], Role: addRole})
		}),
	}
	add.Flags().StringVar(&addRole, "role", "member", "owner, admin, member or viewer")

	setRole := &cobra.Command{
		Use:   "set-role <email|id> <role>",
		Short: "Change a member's role (PATCH /v0/members/{email|id})",
		Args:  usageArgs(cobra.ExactArgs(2)),
		RunE: a.callOrg(func(ctx context.Context, b backend.Backend, org string, args []string) (any, error) {
			return b.SetMemberRole(ctx, org, args[0], service.SetMemberRoleRequest{Role: args[1]})
		}),
	}

	remove := &cobra.Command{
		Use:     "remove <email|id>",
		Aliases: []string{"rm"},
		Short:   "Take an account out of the organization (DELETE /v0/members/{email|id})",
		Args:    usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(fmt.Sprintf("Remove %q from the organization?", args[0])); err != nil {
				return err
			}
			return a.callOrg(func(ctx context.Context, b backend.Backend, org string, args []string) (any, error) {
				return b.RemoveMember(ctx, org, args[0])
			})(cmd, args)
		},
	}

	member.AddCommand(list, add, setRole, remove)
	return member
}

// newAdminKeyCmd mirrors /v0/admin-keys.
func newAdminKeyCmd(a *app) *cobra.Command {
	key := &cobra.Command{
		Use:     "admin-key",
		Aliases: []string{"admin-keys"},
		Short:   "Manage an organization's admin keys for the Lewbox API (--org)",
	}

	var (
		expiresIn    string
		neverExpires bool
	)
	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create an admin key; the lba_ token is shown once (POST /v0/admin-keys)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if neverExpires && cmd.Flags().Changed("expires-in") {
				return usagef("--expires-in and --never-expires cannot be used together")
			}
			lifetime := expiresIn
			if neverExpires {
				lifetime = ""
			}
			return a.callOrg(func(ctx context.Context, b backend.Backend, org string, args []string) (any, error) {
				return b.CreateAdminKey(ctx, org, service.CreateAdminKeyRequest{Name: args[0], ExpiresIn: lifetime})
			})(cmd, args)
		},
	}
	create.Flags().StringVar(&expiresIn, "expires-in", "90d", "how long the key works, such as 30d or 720h")
	create.Flags().BoolVar(&neverExpires, "never-expires", false, "the key works until it is revoked")

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List admin keys, revoked ones included (GET /v0/admin-keys)",
		Args:    usageArgs(cobra.NoArgs),
		RunE: a.callOrg(func(ctx context.Context, b backend.Backend, org string, _ []string) (any, error) {
			return b.ListAdminKeys(ctx, org)
		}),
	}

	revoke := &cobra.Command{
		Use:     "revoke <id|name>",
		Aliases: []string{"rm", "remove"},
		Short:   "Revoke an admin key; it stops working at once (DELETE /v0/admin-keys/{id|name})",
		Args:    usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(fmt.Sprintf("Revoke admin key %q? Anything using it stops working.", args[0])); err != nil {
				return err
			}
			return a.callOrg(func(ctx context.Context, b backend.Backend, org string, args []string) (any, error) {
				return b.RevokeAdminKey(ctx, org, args[0])
			})(cmd, args)
		},
	}

	key.AddCommand(create, list, revoke)
	return key
}
