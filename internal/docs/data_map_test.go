package docs_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

var (
	createTable = regexp.MustCompile(`(?im)^CREATE TABLE (?:IF NOT EXISTS )?([a-z_]+)`)
	mapRow      = regexp.MustCompile("(?m)^\\| `([a-z_]+)` \\|")
)

// TestDataMapCoversEveryTable fails when a migration creates a table that the
// data map does not name, or the map names a table that no migration creates.
// The map is the privacy page operators and auditors rely on, so it must not
// drift from the schema.
func TestDataMapCoversEveryTable(t *testing.T) {
	migrations, err := filepath.Glob("../db/migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 10 {
		t.Fatalf("found only %d migrations; has the path changed?", len(migrations))
	}
	tables := make(map[string]bool)
	for _, path := range migrations {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range createTable.FindAllStringSubmatch(string(src), -1) {
			if m[1] != "goose_db_version" { // Goose's own bookkeeping
				tables[m[1]] = true
			}
		}
	}
	if len(tables) < 20 {
		t.Fatalf("found only %d tables in the migrations; has the SQL changed shape?", len(tables))
	}

	src, err := os.ReadFile("../../docs/platform/operations/data-map.md")
	if err != nil {
		t.Fatal(err)
	}
	documented := make(map[string]bool)
	for _, m := range mapRow.FindAllStringSubmatch(string(src), -1) {
		documented[m[1]] = true
	}
	if len(documented) < 20 {
		t.Fatalf("the data map names only %d tables; has the table changed shape?", len(documented))
	}

	for _, table := range sortedKeys(tables) {
		if !documented[table] {
			t.Errorf("migration creates table %q, but docs/platform/operations/data-map.md does not name it", table)
		}
	}
	for _, table := range sortedKeys(documented) {
		if !tables[table] {
			t.Errorf("data map names table %q, but no migration creates it", table)
		}
	}
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
