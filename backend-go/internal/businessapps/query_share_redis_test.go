package businessapps

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shilin414/cas/backend-go/internal/platform/config"
	"github.com/shilin414/cas/backend-go/internal/platform/redisx"
)

// This test only creates known short-lived keys in a random private namespace.
// It never publishes to a worker queue or modifies shared keys.
func TestQuerySnapshotRedisLifecycle(t *testing.T) {
	if os.Getenv("BUSINESS_APPS_REDIS_VERIFY") != "1" {
		t.Skip("set BUSINESS_APPS_REDIS_VERIFY=1 for isolated Redis key verification")
	}
	cfg, e := config.Load()
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := NewQuerySnapshot(7, 1, "material-query", "material-query", "测试", Input{Action: "article", Query: "test-fixture"}, json.RawMessage(`[{"item":"test"}]`), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	cfg.Redis.KeyPrefix += ":query-share-verification:" + snapshot.Token
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, e := redisx.Open(ctx, cfg.Redis)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	store := &RedisSnapshotStore{Client: client}
	defer client.Del(context.Background(), store.key(snapshot.Token))
	if e = store.Put(ctx, snapshot); e != nil {
		t.Fatal(e)
	}
	if ttl := client.TTL(ctx, store.key(snapshot.Token)).Val(); ttl < 23*time.Hour || ttl > 24*time.Hour {
		t.Fatal(ttl)
	}
	loaded, e := store.Get(ctx, snapshot.Token)
	if e != nil || string(loaded.Data) != string(snapshot.Data) {
		t.Fatal(loaded, e)
	}
	var claimed atomic.Int32
	var lease string
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state, owner, e := store.Claim(ctx, snapshot.Token, "user:ou-test")
			if e != nil {
				t.Error(e)
			}
			if state == "claimed" {
				claimed.Add(1)
				mu.Lock()
				lease = owner
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if claimed.Load() != 1 {
		t.Fatalf("claimed=%d", claimed.Load())
	}
	if e = store.Finish(ctx, snapshot.Token, "user:ou-test", "stale-lease", "failed"); e != nil {
		t.Fatal(e)
	}
	if state, _, _ := store.Claim(ctx, snapshot.Token, "user:ou-test"); state != "busy" {
		t.Fatal("stale sender removed lease")
	}
	if e = store.Finish(ctx, snapshot.Token, "user:ou-test", lease, "sent"); e != nil {
		t.Fatal(e)
	}
	if state, _, _ := store.Claim(ctx, snapshot.Token, "user:ou-test"); state != "sent" {
		t.Fatal("sent dedupe missing")
	}
	// Simulate process loss / failed Finish older than the provider's 1h UUID
	// window. A processing record must not disappear after the 90s active lease.
	deliveryKey := store.deliveryKey(snapshot.Token, "user:ou-test")
	client.HSet(ctx, store.key(snapshot.Token), deliveryKey, "processing:lost-worker:1")
	if state, _, _ := store.Claim(ctx, snapshot.Token, "user:ou-test"); state != "uncertain" {
		t.Fatal("old abandoned send was released", state)
	}
	if e = store.Finish(ctx, snapshot.Token, "user:ou-test", "lost-worker", "uncertain"); e != nil {
		t.Fatal(e)
	}
	if state, _, _ := store.Claim(ctx, snapshot.Token, "user:ou-test"); state != "uncertain" {
		t.Fatal("uncertain outcome not retained", state)
	}
	snapshot.ExpiresAt = time.Now().Add(-time.Second)
	if e = store.Put(ctx, snapshot); e != nil {
		t.Fatal(e)
	}
	if _, e = store.Get(ctx, snapshot.Token); e == nil {
		t.Fatal("expired snapshot accepted")
	}
}
