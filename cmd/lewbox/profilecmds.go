package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/harlew-dev/lewbox/internal/backend"
	"github.com/harlew-dev/lewbox/internal/service"
)

// newDNSProfileCmd mirrors /v0/dns-profiles. A profile's API token is only
// ever read from stdin and is never printed.
func newDNSProfileCmd(a *app) *cobra.Command {
	profile := &cobra.Command{
		Use:     "dns-profile",
		Aliases: []string{"dns-profiles"},
		Short:   "Manage DNS profiles (saved Cloudflare credentials)",
	}

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List DNS profiles (GET /v0/dns-profiles)",
		Args:    usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			return b.ListDNSProfiles(ctx)
		}),
	}

	show := &cobra.Command{
		Use:   "show <id|name>",
		Short: "Show a DNS profile (GET /v0/dns-profiles/{id|name})",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.GetDNSProfile(ctx, args[0])
		}),
	}

	var createZone, createACMEEmail string
	var createTokenStdin bool
	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create and test a DNS profile (POST /v0/dns-profiles)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if createZone == "" {
				return usagef("pass --zone <zone>, such as example.com")
			}
			if !createTokenStdin {
				return usagef("pass --api-token-stdin and pipe the token on stdin")
			}
			token, err := readSecret(cmd, createTokenStdin, "--api-token-stdin")
			if err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.CreateDNSProfile(ctx, service.CreateDNSProfileRequest{
					Name:      args[0],
					Zone:      createZone,
					APIToken:  token,
					ACMEEmail: createACMEEmail,
				})
			})(cmd, args)
		},
	}
	create.Flags().StringVar(&createZone, "zone", "", "the Cloudflare zone, such as example.com (required)")
	create.Flags().BoolVar(&createTokenStdin, "api-token-stdin", false, "read the Cloudflare API token from stdin (required)")
	create.Flags().StringVar(&createACMEEmail, "acme-email", "", "the ACME account email, for certificates")

	var updateName, updateZone, updateACMEEmail string
	var updateTokenStdin bool
	update := &cobra.Command{
		Use:   "update <id|name>",
		Short: "Change and test a DNS profile (PATCH /v0/dns-profiles/{id|name})",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := service.UpdateDNSProfileRequest{}
			if cmd.Flags().Changed("name") {
				req.Name = &updateName
			}
			if cmd.Flags().Changed("zone") {
				req.Zone = &updateZone
			}
			if cmd.Flags().Changed("acme-email") {
				req.ACMEEmail = &updateACMEEmail
			}
			if updateTokenStdin {
				token, err := readSecret(cmd, updateTokenStdin, "--api-token-stdin")
				if err != nil {
					return err
				}
				req.APIToken = &token
			}
			if req.Name == nil && req.Zone == nil && req.APIToken == nil && req.ACMEEmail == nil {
				return usagef("nothing to change: pass --name, --zone, --api-token-stdin or --acme-email")
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.UpdateDNSProfile(ctx, args[0], req)
			})(cmd, args)
		},
	}
	update.Flags().StringVar(&updateName, "name", "", "the new name")
	update.Flags().StringVar(&updateZone, "zone", "", "the new zone")
	update.Flags().BoolVar(&updateTokenStdin, "api-token-stdin", false, "read the new Cloudflare API token from stdin")
	update.Flags().StringVar(&updateACMEEmail, "acme-email", "", "the new ACME account email (empty clears it)")

	remove := &cobra.Command{
		Use:     "delete <id|name>",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete a DNS profile no cluster uses (DELETE /v0/dns-profiles/{id|name})",
		Args:    usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(fmt.Sprintf("Delete DNS profile %q?", args[0])); err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.DeleteDNSProfile(ctx, args[0])
			})(cmd, args)
		},
	}

	test := &cobra.Command{
		Use:   "test <id|name>",
		Short: "Test a DNS profile's token now (POST /v0/dns-profiles/{id|name}/test)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.TestDNSProfile(ctx, args[0])
		}),
	}

	credentials := &cobra.Command{
		Use:   "credentials <id|name>",
		Short: "Show a profile's token and settings, for a node (POST /v0/dns-profiles/{id|name}/credentials)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.DNSProfileCredentials(ctx, args[0])
		}),
	}

	profile.AddCommand(list, show, create, update, remove, test, credentials)
	return profile
}

// newStorageProfileCmd mirrors /v0/storage-profiles. A profile's secret
// access key is only ever read from stdin and is never printed.
func newStorageProfileCmd(a *app) *cobra.Command {
	profile := &cobra.Command{
		Use:     "storage-profile",
		Aliases: []string{"storage-profiles"},
		Short:   "Manage storage profiles (saved S3-compatible bucket credentials)",
	}

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List storage profiles (GET /v0/storage-profiles)",
		Args:    usageArgs(cobra.NoArgs),
		RunE: a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
			return b.ListStorageProfiles(ctx)
		}),
	}

	show := &cobra.Command{
		Use:   "show <id|name>",
		Short: "Show a storage profile (GET /v0/storage-profiles/{id|name})",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.GetStorageProfile(ctx, args[0])
		}),
	}

	var createEndpoint, createRegion, createBucket, createPrefix, createAccessKey string
	var createPathStyle, createSecretStdin bool
	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create and test a storage profile (POST /v0/storage-profiles)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if createBucket == "" {
				return usagef("pass --bucket <bucket>")
			}
			if createAccessKey == "" {
				return usagef("pass --access-key-id <key id>")
			}
			if !createSecretStdin {
				return usagef("pass --secret-access-key-stdin and pipe the secret on stdin")
			}
			secret, err := readSecret(cmd, createSecretStdin, "--secret-access-key-stdin")
			if err != nil {
				return err
			}
			req := service.CreateStorageProfileRequest{
				Name:            args[0],
				Endpoint:        createEndpoint,
				Region:          createRegion,
				Bucket:          createBucket,
				Prefix:          createPrefix,
				AccessKeyID:     createAccessKey,
				SecretAccessKey: secret,
			}
			if cmd.Flags().Changed("path-style") {
				req.PathStyle = &createPathStyle
			}
			return a.call(func(ctx context.Context, b backend.Backend, _ []string) (any, error) {
				return b.CreateStorageProfile(ctx, req)
			})(cmd, args)
		},
	}
	create.Flags().StringVar(&createEndpoint, "endpoint", "", "the S3-compatible endpoint URL; empty means AWS S3")
	create.Flags().StringVar(&createRegion, "region", "", "the region, such as us-east-1 or auto (required for AWS)")
	create.Flags().StringVar(&createBucket, "bucket", "", "the bucket (required)")
	create.Flags().StringVar(&createPrefix, "prefix", "", "the key prefix, such as clusters")
	create.Flags().BoolVar(&createPathStyle, "path-style", false, "use <endpoint>/<bucket>/<key> (default true when an endpoint is set)")
	create.Flags().StringVar(&createAccessKey, "access-key-id", "", "the access key ID (required)")
	create.Flags().BoolVar(&createSecretStdin, "secret-access-key-stdin", false, "read the secret access key from stdin (required)")

	var updateName, updateEndpoint, updateRegion, updateBucket, updatePrefix, updateAccessKey string
	var updatePathStyle, updateSecretStdin bool
	update := &cobra.Command{
		Use:   "update <id|name>",
		Short: "Change and test a storage profile (PATCH /v0/storage-profiles/{id|name})",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := service.UpdateStorageProfileRequest{}
			if cmd.Flags().Changed("name") {
				req.Name = &updateName
			}
			if cmd.Flags().Changed("endpoint") {
				req.Endpoint = &updateEndpoint
			}
			if cmd.Flags().Changed("region") {
				req.Region = &updateRegion
			}
			if cmd.Flags().Changed("bucket") {
				req.Bucket = &updateBucket
			}
			if cmd.Flags().Changed("prefix") {
				req.Prefix = &updatePrefix
			}
			if cmd.Flags().Changed("path-style") {
				req.PathStyle = &updatePathStyle
			}
			if cmd.Flags().Changed("access-key-id") {
				req.AccessKeyID = &updateAccessKey
			}
			if updateSecretStdin {
				secret, err := readSecret(cmd, updateSecretStdin, "--secret-access-key-stdin")
				if err != nil {
					return err
				}
				req.SecretAccessKey = &secret
			}
			if req.Name == nil && req.Endpoint == nil && req.Region == nil && req.Bucket == nil &&
				req.Prefix == nil && req.PathStyle == nil && req.AccessKeyID == nil && req.SecretAccessKey == nil {
				return usagef("nothing to change: pass --name, --endpoint, --region, --bucket, --prefix, --path-style, --access-key-id or --secret-access-key-stdin")
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.UpdateStorageProfile(ctx, args[0], req)
			})(cmd, args)
		},
	}
	update.Flags().StringVar(&updateName, "name", "", "the new name")
	update.Flags().StringVar(&updateEndpoint, "endpoint", "", "the new endpoint URL (empty means AWS S3)")
	update.Flags().StringVar(&updateRegion, "region", "", "the new region")
	update.Flags().StringVar(&updateBucket, "bucket", "", "the new bucket")
	update.Flags().StringVar(&updatePrefix, "prefix", "", "the new key prefix (empty clears it)")
	update.Flags().BoolVar(&updatePathStyle, "path-style", false, "use <endpoint>/<bucket>/<key>")
	update.Flags().StringVar(&updateAccessKey, "access-key-id", "", "the new access key ID")
	update.Flags().BoolVar(&updateSecretStdin, "secret-access-key-stdin", false, "read the new secret access key from stdin")

	remove := &cobra.Command{
		Use:     "delete <id|name>",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete a storage profile no cluster uses (DELETE /v0/storage-profiles/{id|name})",
		Args:    usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.confirm(fmt.Sprintf("Delete storage profile %q?", args[0])); err != nil {
				return err
			}
			return a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
				return b.DeleteStorageProfile(ctx, args[0])
			})(cmd, args)
		},
	}

	test := &cobra.Command{
		Use:   "test <id|name>",
		Short: "Test a storage profile's bucket and key now (POST /v0/storage-profiles/{id|name}/test)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.TestStorageProfile(ctx, args[0])
		}),
	}

	credentials := &cobra.Command{
		Use:   "credentials <id|name>",
		Short: "Show a profile's secret key and settings, for a node (POST /v0/storage-profiles/{id|name}/credentials)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: a.call(func(ctx context.Context, b backend.Backend, args []string) (any, error) {
			return b.StorageProfileCredentials(ctx, args[0])
		}),
	}

	profile.AddCommand(list, show, create, update, remove, test, credentials)
	return profile
}
