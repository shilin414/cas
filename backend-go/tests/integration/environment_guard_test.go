package integration

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/shilin414/cas/backend-go/internal/platform/config"
)

func isolatedIntegrationTarget(confirmed bool, dbHost string, redisEnabled bool, redisHost string) error {
	loopback := func(host string) bool {
		host = strings.TrimSpace(host)
		if strings.EqualFold(host, "localhost") {
			return true
		}
		ip := net.ParseIP(host)
		return ip != nil && ip.IsLoopback()
	}
	if !confirmed || !loopback(dbHost) || (redisEnabled && !loopback(redisHost)) {
		return errors.New("live integration requires STUDIO_TEST_ISOLATED=1 and dedicated loopback MySQL/Redis instances; shared application services are prohibited")
	}
	return nil
}

// Validate before ANY live integration test opens a connection. An opt-in flag
// and a key prefix alone never establish data isolation.
func TestMain(m *testing.M) {
	if os.Getenv("STUDIO_TEST_DB") == "1" || os.Getenv("STUDIO_TEST_REDIS") == "1" {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, "integration configuration is invalid")
			os.Exit(2)
		}
		if err = isolatedIntegrationTarget(os.Getenv("STUDIO_TEST_ISOLATED") == "1", cfg.Database.Host, os.Getenv("STUDIO_TEST_REDIS") == "1", cfg.Redis.Host); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(2)
		}
	}
	os.Exit(m.Run())
}
func TestIntegrationIsolationGate(t *testing.T) {
	for _, c := range []struct {
		name      string
		confirmed bool
		db        string
		redis     bool
		host      string
		allowed   bool
	}{
		{"missing explicit isolation", false, "127.0.0.1", true, "127.0.0.1", false},
		{"remote redis even with private prefix", true, "127.0.0.1", true, "10.0.0.2", false},
		{"remote mysql", true, "10.0.0.2", false, "", false},
		{"dedicated loopback services", true, "127.0.0.1", true, "127.0.0.1", true},
		{"local mysql only", true, "localhost", false, "", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := isolatedIntegrationTarget(c.confirmed, c.db, c.redis, c.host) == nil; got != c.allowed {
				t.Fatalf("allowed=%v want %v", got, c.allowed)
			}
		})
	}
}
