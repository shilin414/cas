package integration

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Parallel feature branches must not silently ship two migrations with one version.
func TestMigrationVersionsAreUnique(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "db", "migrations", "*.up.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("migration inventory: %v", err)
	}
	seen := map[int]string{}
	for _, file := range files {
		name := filepath.Base(file)
		prefix, _, ok := strings.Cut(name, "_")
		version, err := strconv.Atoi(prefix)
		if !ok || err != nil || version < 1 {
			t.Fatalf("invalid migration name: %s", name)
		}
		if previous, ok := seen[version]; ok {
			t.Errorf("migration version %d is duplicated: %s and %s", version, previous, name)
		}
		seen[version] = name
		if _, err := os.Stat(strings.TrimSuffix(file, ".up.sql") + ".down.sql"); err != nil {
			t.Errorf("missing down migration for %s: %v", name, err)
		}
	}
}
