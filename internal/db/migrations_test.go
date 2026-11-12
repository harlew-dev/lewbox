package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestSubscriptionsMigration moves existing plans and overrides onto every
// cluster, seeds the default regions and drops the organization's columns.
func TestSubscriptionsMigration(t *testing.T) {
	ctx := context.Background()
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "test.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	dir, err := fs.Sub(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, conn, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 10); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO clusters (id, slug, name, admin_url, endpoint_url, runtime, admin_token_enc, admin_token_id, admin_token_last4, status, created_at)
		 VALUES ('c1', 'eu-west', 'EU West', 'http://a', 'http://a', 'docker', x'00', 't', '0000', 'ready', '2026-11-01T00:00:00Z'),
		        ('c2', 'us-east', 'US East', 'http://b', 'http://b', 'docker', x'00', 't', '0000', 'ready', '2026-11-01T00:00:00Z')`,
		`INSERT INTO organizations (id, slug, name, status, created_at, team, plan_id, max_sandboxes)
		 VALUES ('o1', 'acme', 'Acme', 'active', '2026-11-01T00:00:00Z', 'lbx-o1', '01970000-0000-7000-8000-000000000001', 7)`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := provider.UpTo(ctx, 12); err != nil {
		t.Fatal(err)
	}

	rows, err := conn.QueryContext(ctx, `SELECT cluster_id, plan_id, max_sandboxes FROM subscriptions ORDER BY cluster_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var cluster, plan string
		var sandboxes sql.NullInt64
		if err := rows.Scan(&cluster, &plan, &sandboxes); err != nil {
			t.Fatal(err)
		}
		if !sandboxes.Valid || sandboxes.Int64 != 7 {
			t.Errorf("subscription %q override = %+v", cluster, sandboxes)
		}
		got = append(got, cluster+"/"+plan)
	}
	if len(got) != 2 || got[0] != "c1/01970000-0000-7000-8000-000000000001" || got[1] != "c2/01970000-0000-7000-8000-000000000001" {
		t.Errorf("subscriptions = %v", got)
	}

	var columns []string
	rows, err = conn.QueryContext(ctx, `SELECT name FROM pragma_table_info('organizations')`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, name)
	}
	rows.Close()
	for _, dropped := range []string{"plan_id", "max_sandboxes", "max_vcpus", "max_memory_mb"} {
		for _, name := range columns {
			if name == dropped {
				t.Errorf("organizations still has %q", dropped)
			}
		}
	}

	var value string
	if err := conn.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'default_regions'`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	var regions []string
	if err := json.Unmarshal([]byte(value), &regions); err != nil {
		t.Fatal(err)
	}
	if len(regions) != 2 || regions[0] != "eu-west" || regions[1] != "us-east" {
		t.Errorf("default_regions = %v", regions)
	}
}

// TestOneRegionMigration keeps each organization's oldest subscription, makes
// one per organization the rule, and turns the default regions into one
// region.
func TestOneRegionMigration(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, stored string
		want         *string
	}{
		{"list", `["us-east","eu-west"]`, ptr("us-east")},
		{"empty", `[]`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, provider := migrationConn(t, ctx, 12)
			defer conn.Close()
			for _, stmt := range []string{
				`INSERT INTO clusters (id, slug, name, admin_url, endpoint_url, runtime, admin_token_enc, admin_token_id, admin_token_last4, status, created_at)
				 VALUES ('c1', 'eu-west', 'EU West', 'http://a', 'http://a', 'docker', x'00', 't', '0000', 'ready', '2026-11-01T00:00:00Z'),
				        ('c2', 'us-east', 'US East', 'http://b', 'http://b', 'docker', x'00', 't', '0000', 'ready', '2026-11-01T00:00:00Z')`,
				`INSERT INTO organizations (id, slug, name, status, created_at, team)
				 VALUES ('o1', 'acme', 'Acme', 'active', '2026-11-01T00:00:00Z', 'lbx-o1'),
				        ('o2', 'other', 'Other', 'active', '2026-11-01T00:00:00Z', 'lbx-o2')`,
				`INSERT INTO subscriptions (org_id, cluster_id, plan_id, created_at)
				 VALUES ('o1', 'c2', '01970000-0000-7000-8000-000000000001', '2026-11-02T00:00:00Z'),
				        ('o1', 'c1', '01970000-0000-7000-8000-000000000001', '2026-11-01T00:00:00Z'),
				        ('o2', 'c2', '01970000-0000-7000-8000-000000000001', '2026-11-01T00:00:00Z')`,
				`UPDATE settings SET value = '` + tc.stored + `' WHERE key = 'default_regions'`,
			} {
				if _, err := conn.ExecContext(ctx, stmt); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := provider.UpTo(ctx, 13); err != nil {
				t.Fatal(err)
			}

			rows, err := conn.QueryContext(ctx, `SELECT org_id, cluster_id FROM subscriptions ORDER BY org_id`)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var got []string
			for rows.Next() {
				var org, cluster string
				if err := rows.Scan(&org, &cluster); err != nil {
					t.Fatal(err)
				}
				got = append(got, org+"/"+cluster)
			}
			if len(got) != 2 || got[0] != "o1/c1" || got[1] != "o2/c2" {
				t.Errorf("subscriptions = %v", got)
			}

			if _, err := conn.ExecContext(ctx, `INSERT INTO subscriptions (org_id, cluster_id, plan_id, created_at)
				VALUES ('o1', 'c2', '01970000-0000-7000-8000-000000000001', '2026-11-03T00:00:00Z')`); err == nil {
				t.Error("a second subscription for one organization was accepted")
			}

			var value string
			if err := conn.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'default_region'`).Scan(&value); err != nil {
				t.Fatal(err)
			}
			var region *string
			if err := json.Unmarshal([]byte(value), &region); err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.want == nil && region != nil:
				t.Errorf("default_region = %q, want null", *region)
			case tc.want != nil && (region == nil || *region != *tc.want):
				t.Errorf("default_region = %v, want %q", region, *tc.want)
			}
			if n, err := conn.QueryContext(ctx, `SELECT 1 FROM settings WHERE key = 'default_regions'`); err != nil {
				t.Fatal(err)
			} else {
				defer n.Close()
				if n.Next() {
					t.Error("default_regions is still set")
				}
			}
		})
	}
}

// migrationConn opens a fresh database migrated up to version.
func migrationConn(t *testing.T, ctx context.Context, version int64) (*sql.DB, *goose.Provider) {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "test.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := fs.Sub(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, conn, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, version); err != nil {
		t.Fatal(err)
	}
	return conn, provider
}

func ptr(s string) *string { return &s }
