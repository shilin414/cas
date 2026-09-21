package integration

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shilin414/cas/backend-go/internal/platform/config"
	_ "github.com/go-sql-driver/mysql"
)

// This isolated test never inserts into the shared applications table. It may
// be explicitly enabled without enabling any other database fixture suite.
func TestBusinessAppsMigrationTemporaryTable(t *testing.T) {
	if os.Getenv("BUSINESS_APPS_MIGRATION_VERIFY") != "1" && os.Getenv("STUDIO_TEST_DB") != "1" {
		t.Skip("set BUSINESS_APPS_MIGRATION_VERIFY=1 for connection-local migration verification")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", cfg.Database.DSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `CREATE TEMPORARY TABLE verify_business_applications LIKE applications`); err != nil {
		t.Fatal(err)
	}
	run := func(name string) {
		t.Helper()
		raw, err := os.ReadFile("../../db/migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		// Only the target table is substituted; category lookup is a read-only query
		// on the existing catalog. One pinned connection owns all temporary data.
		query := strings.ReplaceAll(strings.TrimPrefix(string(raw), "\ufeff"), "applications", "verify_business_applications")
		if _, err = conn.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	count := func(where string, want int) {
		t.Helper()
		var got int
		if err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM verify_business_applications "+where).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s: got %d want %d", where, got, want)
		}
	}
	run("0042_business_apps.up.sql")
	run("0042_business_apps.up.sql")
	count("WHERE enabled=1 AND access_mode='admin_only' AND is_public=0", 3)
	run("0042_business_apps.down.sql")
	count("WHERE enabled=0", 3)
	run("0042_business_apps.up.sql")
	count("", 3)
	count("WHERE enabled=0", 3)
}
